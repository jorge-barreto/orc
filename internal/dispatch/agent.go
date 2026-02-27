package dispatch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
func buildAgentArgs(phase config.Phase, prompt, sessionID string, isFirst bool, extraTools []string) []string {
	args := []string{"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--model", phase.Model,
	}

	if sessionID != "" {
		if isFirst {
			args = append(args, "--session-id", sessionID)
		} else {
			args = append(args, "--resume", sessionID)
		}
	}

	// Merge default tools, phase allow-tools, and dynamically approved tools
	seen := make(map[string]bool)
	var tools []string
	for _, list := range [][]string{defaultAllowTools, phase.AllowTools, extraTools} {
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
func runAgentTurn(ctx context.Context, phase config.Phase, env *Environment, prompt, sessionID string, isFirst bool, logFile io.Writer, extraTools []string) (*turnResult, error) {
	args := buildAgentArgs(phase, prompt, sessionID, isFirst, extraTools)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = PhaseWorkDir(phase, env)
	cmd.Env = BuildEnv(env)
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

	streamResult, streamErr := processStream(ctx, stdout, os.Stdout, logFile)

	code, waitErr := exitCode(cmd.Wait())
	if waitErr != nil {
		return nil, waitErr
	}
	if streamErr != nil && ctx.Err() == nil {
		return nil, streamErr
	}

	return &turnResult{Stream: streamResult, ExitCode: code}, nil
}

// RunAgent executes an agent phase in unattended mode (no stdin monitoring).
// Uses stream-json parsing for real-time output.
func RunAgent(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	// Read and render the prompt template
	promptData, err := os.ReadFile(filepath.Join(env.ProjectRoot, phase.Prompt))
	if err != nil {
		return nil, err
	}
	rendered := ExpandVars(string(promptData), env.Vars())

	// Save rendered prompt for inspection
	if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(rendered), 0644); err != nil {
		return nil, err
	}

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	sessionID := uuid.New().String()
	tr, err := runAgentTurn(ctx, phase, env, rendered, sessionID, true, logFile, nil)
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

	output := ""
	if tr.Stream != nil {
		output = tr.Stream.Text
	}
	return &Result{ExitCode: tr.ExitCode, Output: output}, nil
}

// RunAgentWithPrompt invokes claude with an explicit prompt string (for output re-prompting).
// Uses a new session with no steering.
func RunAgentWithPrompt(ctx context.Context, phase config.Phase, env *Environment, prompt string) (*Result, error) {
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

	tr, err := runAgentTurn(ctx, phase, env, prompt, "", false, logFile, nil)
	if err != nil {
		return nil, err
	}

	output := ""
	if tr.Stream != nil {
		output = tr.Stream.Text
	}
	return &Result{ExitCode: tr.ExitCode, Output: output}, nil
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

	// Read and render the prompt template
	promptData, err := os.ReadFile(filepath.Join(env.ProjectRoot, phase.Prompt))
	if err != nil {
		return nil, err
	}
	rendered := ExpandVars(string(promptData), env.Vars())

	// Save rendered prompt for inspection
	if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(rendered), 0644); err != nil {
		return nil, err
	}

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	sessionID := uuid.New().String()
	reader := NewStdinReader(os.Stdin)
	defer reader.Stop()

	var extraTools []string
	isFirst := true
	prompt := rendered
	var lastTurn *turnResult

	for {
		tr, err := runAgentTurn(ctx, phase, env, prompt, sessionID, isFirst, logFile, extraTools)
		if err != nil {
			return nil, err
		}
		lastTurn = tr
		isFirst = false

		// Handle permission denials
		if tr.Stream != nil && len(tr.Stream.PermissionDenials) > 0 {
			approved := handleDenials(tr.Stream.PermissionDenials)
			if len(approved) > 0 {
				extraTools = append(extraTools, approved...)
				prompt = "Continue — the previously denied tools have now been approved."
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
	return &Result{ExitCode: exitCode, Output: output}, nil
}

// handleDenials prompts the user about permission denials and returns
// the tool names that should be approved for retry.
func handleDenials(denials []PermissionDenial) []string {
	var names []string
	for _, d := range denials {
		names = append(names, d.String())
		ux.ToolDenied(d.Tool, d.Input)
	}

	ux.PermissionPrompt(names)
	fmt.Printf("  Retry with these tools approved? [y/n]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
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
