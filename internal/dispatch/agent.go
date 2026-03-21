package dispatch

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// defaultAllowTools are always passed to claude -p so agent phases can
// perform basic file operations without manual permission approval.
var defaultAllowTools = []string{
	"Read", "Edit", "Write", "Glob", "Grep",
	"Task", "WebFetch", "WebSearch",
}

// buildAgentArgs constructs the claude CLI arguments for an agent turn.
// If sessionID is non-empty and isFirst is true, uses --session-id.
// If sessionID is non-empty and isFirst is false, uses --resume.
func buildAgentArgs(phase config.Phase, env *Environment, sessionID string, isFirst bool, extraTools []string) []string {
	args := []string{"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--model", phase.Model,
		"--effort", phase.Effort,
	}

	if sessionID != "" {
		if isFirst {
			args = append(args, "--session-id", sessionID)
		} else {
			args = append(args, "--resume", sessionID)
		}
	}

	if phase.MCPConfig != "" {
		expanded := ExpandVars(phase.MCPConfig, env.Vars())
		args = append(args, "--mcp-config", expanded)
	}

	// Merge default tools, config-level tools, phase allow-tools, and dynamically approved tools
	seen := make(map[string]bool)
	var tools []string
	for _, list := range [][]string{defaultAllowTools, env.DefaultAllowTools, phase.AllowTools, extraTools} {
		for _, t := range list {
			if !seen[t] {
				seen[t] = true
				tools = append(tools, t)
			}
		}
	}
	if len(tools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, tools...)
	}

	return args
}

// turnResult holds the outcome of a single agent turn (subprocess invocation).
type turnResult struct {
	Stream   *StreamResult
	ExitCode int
}

// runAgentTurn executes a single agent turn: starts subprocess, processes stream, waits.
func runAgentTurn(ctx context.Context, phase config.Phase, env *Environment, prompt, sessionID string, isFirst bool, logFile io.Writer, rawLog io.Writer, extraTools []string) (*turnResult, error) {
	args := buildAgentArgs(phase, env, sessionID, isFirst, extraTools)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = PhaseWorkDir(phase, env)
	cmd.Env = BuildEnv(env)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	streamResult, streamErr := ProcessStream(ctx, stdout, os.Stdout, logFile, rawLog)

	code, waitErr := exitCode(cmd.Wait())
	if waitErr != nil {
		return nil, waitErr
	}
	if streamErr != nil && ctx.Err() == nil {
		return nil, streamErr
	}

	return &turnResult{Stream: streamResult, ExitCode: code}, nil
}

// resumePrompt is the continuation prompt used when resuming an interrupted session.
const resumePrompt = "The previous session was interrupted. Continue from where you left off and complete the remaining work."

// dispatchWithResume handles the resume-or-fresh decision for agent turns.
// If resumeSessionID is non-empty, it first tries to dispatch with --resume.
// If that fails (error OR non-zero exit code), it logs a warning and falls
// back to a fresh start.
//
// Returns the turn result, the active session ID, and whether it was a first turn.
func dispatchWithResume(
	resumeSessionID string,
	renderPrompt func() (string, error),
	newSessionID func() string,
	dispatch func(prompt, sessionID string, isFirst bool) (*turnResult, error),
	warn func(err error),
) (tr *turnResult, sessionID string, isFirst bool, err error) {
	if resumeSessionID != "" {
		tr, err = dispatch(resumePrompt, resumeSessionID, false)
		if err == nil && tr.ExitCode == 0 {
			return tr, resumeSessionID, false, nil
		}
		// Resume failed — either error or non-zero exit code
		if err != nil {
			warn(err)
		} else {
			warn(fmt.Errorf("exit code %d", tr.ExitCode))
		}
		// Fall through to fresh start
	}

	prompt, err := renderPrompt()
	if err != nil {
		return nil, "", false, err
	}
	sid := newSessionID()
	tr, err = dispatch(prompt, sid, true)
	if err != nil {
		return nil, "", false, err
	}
	return tr, sid, true, nil
}

// RenderAndSavePrompt reads the prompt template, expands variables, injects
// feedback from previous failures, and saves the rendered prompt to artifacts/prompts/.
// Returns the fully rendered prompt string.
func RenderAndSavePrompt(phase config.Phase, env *Environment) (string, error) {
	promptData, err := os.ReadFile(filepath.Join(env.ProjectRoot, phase.Prompt))
	if err != nil {
		return "", err
	}
	rendered := ExpandVars(string(promptData), env.Vars())

	feedback, err := state.ReadAllFeedback(env.ArtifactsDir)
	if err != nil {
		return "", fmt.Errorf("reading feedback: %w", err)
	}
	if feedback != "" {
		rendered += "\n\n## ⚠️ LOOP FEEDBACK — ACTION REQUIRED\n\n" +
			"A previous phase failed and this phase is being re-run. " +
			"You MUST address the following feedback before proceeding with any other work.\n\n" +
			feedback
	}

	if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(rendered), 0644); err != nil {
		return "", err
	}
	return rendered, nil
}

// RunAgent executes an agent phase in unattended mode (no stdin monitoring).
// Uses stream-json parsing for real-time output.
func RunAgent(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	// Save resume prompt for observability if resuming
	if env.ResumeSessionID != "" {
		if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(resumePrompt), 0644); err != nil {
			return nil, err
		}
	}

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var rawLog io.Writer
	if env.Verbose {
		f, err := os.OpenFile(state.StreamLogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rawLog = f
	}

	dispatch := func(prompt, sid string, first bool) (*turnResult, error) {
		return runAgentTurn(ctx, phase, env, prompt, sid, first, logFile, rawLog, nil)
	}
	renderPrompt := func() (string, error) {
		return RenderAndSavePrompt(phase, env)
	}
	newSID := func() string { return uuid.New().String() }
	warn := func(err error) {
		fmt.Fprintf(os.Stderr, "  warning: resume failed (%v), falling back to fresh start\n", err)
	}

	tr, sessionID, _, err := dispatchWithResume(env.ResumeSessionID, renderPrompt, newSID, dispatch, warn)
	if err != nil {
		return nil, err
	}

	// In unattended mode, log permission denials but don't retry
	if tr.Stream != nil && len(tr.Stream.PermissionDenials) > 0 {
		var names []string
		for _, d := range tr.Stream.PermissionDenials {
			names = append(names, d.String())
		}
		fmt.Fprintf(os.Stderr, "  permission denials: %s\n", strings.Join(names, ", "))
	}

	// In unattended mode, log user questions as warnings
	if tr.Stream != nil && len(tr.Stream.UserQuestions) > 0 {
		for _, q := range tr.Stream.UserQuestions {
			fmt.Fprintf(os.Stderr, "  warning: agent asked %q (unanswered in --auto mode)\n", q.Question)
		}
	}

	output := ""
	if tr.Stream != nil {
		output = tr.Stream.Text
	}
	res := &Result{ExitCode: tr.ExitCode, Output: output, Turns: 1, SessionID: sessionID}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if tr.Stream != nil {
		res.CostUSD = tr.Stream.CostUSD
		res.InputTokens = tr.Stream.InputTokens
		res.OutputTokens = tr.Stream.OutputTokens
		res.CacheCreationInputTokens = tr.Stream.CacheCreationInputTokens
		res.CacheReadInputTokens = tr.Stream.CacheReadInputTokens
	}
	return res, nil
}

// RunAgentWithPrompt invokes claude with an explicit prompt string (for output re-prompting).
// If sessionID is non-empty, resumes that session so the agent retains prior context.
func RunAgentWithPrompt(ctx context.Context, phase config.Phase, env *Environment, prompt, sessionID string) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var rawLog io.Writer
	if env.Verbose {
		f, err := os.OpenFile(state.StreamLogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rawLog = f
	}

	tr, err := runAgentTurn(ctx, phase, env, prompt, sessionID, false, logFile, rawLog, nil)
	if err != nil {
		return nil, err
	}

	output := ""
	if tr.Stream != nil {
		output = tr.Stream.Text
	}
	res := &Result{ExitCode: tr.ExitCode, Output: output, Turns: 1, SessionID: sessionID}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if tr.Stream != nil {
		res.CostUSD = tr.Stream.CostUSD
		res.InputTokens = tr.Stream.InputTokens
		res.OutputTokens = tr.Stream.OutputTokens
		res.CacheCreationInputTokens = tr.Stream.CacheCreationInputTokens
		res.CacheReadInputTokens = tr.Stream.CacheReadInputTokens
	}
	return res, nil
}

// RunAgentAttended executes an agent phase in attended mode with steering support.
// A background stdin reader lets the user type follow-up instructions.
// After each agent turn completes, if user input is available, it resumes
// the conversation with that input. Permission denials prompt the user
// to approve the denied tools.
func RunAgentAttended(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var rawLog io.Writer
	if env.Verbose {
		f, err := os.OpenFile(state.StreamLogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rawLog = f
	}

	// Save resume prompt for observability if resuming
	if env.ResumeSessionID != "" {
		if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(resumePrompt), 0644); err != nil {
			return nil, err
		}
	}

	dispatch := func(prompt, sid string, first bool) (*turnResult, error) {
		return runAgentTurn(ctx, phase, env, prompt, sid, first, logFile, rawLog, nil)
	}
	renderFresh := func() (string, error) {
		return RenderAndSavePrompt(phase, env)
	}
	newSID := func() string { return uuid.New().String() }
	warn := func(err error) {
		fmt.Fprintf(os.Stderr, "  warning: resume failed (%v), falling back to fresh start\n", err)
	}

	// First turn: handles resume-or-fresh decision (no stdin reader needed yet)
	firstTR, sessionID, _, err := dispatchWithResume(env.ResumeSessionID, renderFresh, newSID, dispatch, warn)
	if err != nil {
		return nil, err
	}

	reader := NewStdinReader(os.Stdin)
	defer reader.Stop()

	var extraTools []string
	var prompt string // for subsequent turns — set by denial/question/steering handlers
	var lastTurn *turnResult
	var totalCost float64
	var totalInput, totalOutput int
	var totalCacheCreation, totalCacheRead int
	turns := 0
	tr := firstTR

	for {
		// Dispatch subsequent turns (first turn already handled by dispatchWithResume)
		if turns > 0 {
			var err error
			tr, err = runAgentTurn(ctx, phase, env, prompt, sessionID, false, logFile, rawLog, extraTools)
			if err != nil {
				return nil, err
			}
		}
		lastTurn = tr
		turns++
		if tr.Stream != nil {
			totalCost += tr.Stream.CostUSD
			totalInput += tr.Stream.InputTokens
			totalOutput += tr.Stream.OutputTokens
			totalCacheCreation += tr.Stream.CacheCreationInputTokens
			totalCacheRead += tr.Stream.CacheReadInputTokens
		}

		// Handle permission denials
		if tr.Stream != nil && len(tr.Stream.PermissionDenials) > 0 {
			approved := handleDenials(tr.Stream.PermissionDenials, reader)
			if len(approved) > 0 {
				extraTools = append(extraTools, approved...)
				prompt = "Continue — the previously denied tools have now been approved."
				continue
			}
		}

		// Handle user questions from AskUserQuestion tool calls
		if tr.Stream != nil && len(tr.Stream.UserQuestions) > 0 {
			var lastAnswer string
			for _, q := range tr.Stream.UserQuestions {
				ux.AgentQuestion(q.Question, q.Options)
				if line, ok := reader.ReadLineBlocking(); ok {
					answer := strings.TrimSpace(line)
					// If user typed a number, map to the option text
					if len(q.Options) > 0 {
						if idx, err := strconv.Atoi(answer); err == nil && idx >= 1 && idx <= len(q.Options) {
							answer = q.Options[idx-1]
						}
					}
					lastAnswer = answer
					fmt.Fprintf(logFile, "\n--- user answer to agent question: %s ---\n", answer)
				}
			}
			if lastAnswer != "" {
				prompt = lastAnswer
				continue
			}
		}

		// Check for user steering input
		if line, ok := reader.ReadLine(); ok {
			prompt = line
			fmt.Fprintf(logFile, "\n--- user steering: %s ---\n", line)
			continue
		}

		// No user input, no denials to handle — phase is done
		break
	}

	output := ""
	if lastTurn != nil && lastTurn.Stream != nil {
		output = lastTurn.Stream.Text
	}
	exitCode := 0
	if lastTurn != nil {
		exitCode = lastTurn.ExitCode
	}
	return &Result{
		ExitCode:                 exitCode,
		Output:                   output,
		TimedOut:                 ctx.Err() == context.DeadlineExceeded,
		CostUSD:                  totalCost,
		InputTokens:              totalInput,
		OutputTokens:             totalOutput,
		CacheCreationInputTokens: totalCacheCreation,
		CacheReadInputTokens:     totalCacheRead,
		Turns:                    turns,
		SessionID:                sessionID,
	}, nil
}

// handleDenials prompts the user about permission denials and returns
// the tool names that should be approved for retry.
func handleDenials(denials []PermissionDenial, reader *StdinReader) []string {
	var names []string
	for _, d := range denials {
		names = append(names, d.String())
		ux.ToolDenied(d.Tool, d.Input)
	}

	ux.PermissionPrompt(names)
	fmt.Printf("  Retry with these tools approved? [y/n]: ")

	line, ok := reader.ReadLineBlocking()
	if ok {
		answer := strings.TrimSpace(strings.ToLower(line))
		if answer == "y" || answer == "yes" {
			var tools []string
			for _, d := range denials {
				tools = append(tools, d.Tool)
			}
			return tools
		}
	}
	return nil
}
