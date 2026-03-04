package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

const (
	maxLogLines      = 200
	maxOtherLogLines = 100
	maxIterLogLines  = 150
)

const diagPrompt = `You are diagnosing a failed orc workflow phase. Analyze the context below and provide a concise diagnosis.

## Failed Phase Config
%s

## Failed Phase Log Output (last %d lines)
%s
%s%s%s%s%s
Instructions:
1. Identify what went wrong from the log output. Cross-reference with other phase logs and previous iterations if available.
   - If a phase log mentions "timed out", it was killed by orc's phase timeout. This usually means the agent ran out of time — possibly due to network issues, slow API responses, or the task being too large for the configured timeout.
   - Check the Execution Context timing: if a phase's duration closely matches its configured timeout, it likely timed out even if upstream of the current failed phase.
2. Classify this as a WORKFLOW problem (config, phase ordering, missing outputs) or a CODE problem (the task the agent was working on).
3. Suggest specific fixes.
4. Recommend the next command to run:
   - orc run --retry N <ticket>  (re-run just the failed phase)
   - orc run --from N <ticket>   (re-run from phase N onward)
   - Fix the underlying issue first, then retry

Be direct and concise. Focus on actionable advice.`

// Run gathers failure context from artifacts and sends it to claude for diagnosis.
func Run(ctx context.Context, projectRoot, artifactsDir string, cfg *config.Config, st *state.State) error {
	if st.Status != state.StatusFailed && st.Status != state.StatusInterrupted {
		fmt.Println("No failed run to diagnose.")
		return nil
	}

	if st.PhaseIndex >= len(cfg.Phases) {
		return fmt.Errorf("phase index %d out of range (config has %d phases)", st.PhaseIndex, len(cfg.Phases))
	}

	auditDir := state.AuditDir(projectRoot, st.Ticket)
	phase := cfg.Phases[st.PhaseIndex]

	phaseConfig := gatherPhaseConfig(phase)
	log := gatherLog(artifactsDir, st.PhaseIndex)
	prompt := gatherPrompt(artifactsDir, st.PhaseIndex, phase)
	feedback := gatherFeedback(artifactsDir)
	timing := gatherTimingWithFallback(auditDir, artifactsDir)
	loops := gatherLoopCounts(artifactsDir)
	otherLogs := gatherAllLogs(artifactsDir, cfg.Phases, st.PhaseIndex)
	iterLogs := gatherIterationLogs(auditDir, st.PhaseIndex)

	diagText := buildPrompt(phaseConfig, log, prompt, feedback, timing, loops, otherLogs, iterLogs)

	model := cfg.Model
	if model == "" {
		model = "opus"
	}

	// Print header
	fmt.Printf("\n%s%s══ Doctor: diagnosing phase %d/%d (%s) ══%s\n\n",
		ux.Bold, ux.Cyan, st.PhaseIndex+1, len(cfg.Phases), phase.Name, ux.Reset)

	if err := runClaude(ctx, diagText, model); err != nil {
		return fmt.Errorf("failed to run claude: %w", err)
	}

	fmt.Println()
	ux.ResumeHint(st.Ticket)
	return nil
}

func buildPrompt(phaseConfig, log, prompt, feedback, timing, loops, otherLogs, iterLogs string) string {
	var promptSection, feedbackSection, timingSection, otherLogsSection, iterLogsSection string
	if prompt != "" {
		promptSection = fmt.Sprintf("\n## Agent Prompt\n%s\n", prompt)
	}
	if feedback != "" {
		feedbackSection = fmt.Sprintf("\n## Feedback Files\n%s\n", feedback)
	}

	var extras []string
	if timing != "" {
		extras = append(extras, fmt.Sprintf("Timing: %s", timing))
	}
	if loops != "" {
		extras = append(extras, fmt.Sprintf("Loop counts: %s", loops))
	}
	if len(extras) > 0 {
		timingSection = fmt.Sprintf("\n## Execution Context\n%s\n", strings.Join(extras, "\n"))
	}
	if otherLogs != "" {
		otherLogsSection = fmt.Sprintf("\n## Other Phase Logs\n%s\n", otherLogs)
	}
	if iterLogs != "" {
		iterLogsSection = fmt.Sprintf("\n## Previous Iterations of Failed Phase\n%s\n", iterLogs)
	}

	return fmt.Sprintf(diagPrompt, phaseConfig, maxLogLines, log, promptSection, feedbackSection, timingSection, otherLogsSection, iterLogsSection)
}

func gatherPhaseConfig(phase config.Phase) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Name: %s", phase.Name))
	parts = append(parts, fmt.Sprintf("Type: %s", phase.Type))
	if phase.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", phase.Description))
	}
	if phase.Run != "" {
		parts = append(parts, fmt.Sprintf("Run: %s", phase.Run))
	}
	if phase.Prompt != "" {
		parts = append(parts, fmt.Sprintf("Prompt file: %s", phase.Prompt))
	}
	if phase.Model != "" {
		parts = append(parts, fmt.Sprintf("Model: %s", phase.Model))
	}
	if phase.Timeout > 0 {
		parts = append(parts, fmt.Sprintf("Timeout: %ds", phase.Timeout))
	}
	if len(phase.Outputs) > 0 {
		parts = append(parts, fmt.Sprintf("Expected outputs: %s", strings.Join(phase.Outputs, ", ")))
	}
	if phase.Condition != "" {
		parts = append(parts, fmt.Sprintf("Condition: %s", phase.Condition))
	}
	if phase.Loop != nil {
		s := fmt.Sprintf("Loop: goto %s (min %d, max %d)", phase.Loop.Goto, phase.Loop.Min, phase.Loop.Max)
		if phase.Loop.OnExhaust != nil {
			s += fmt.Sprintf(", on-exhaust: goto %s (max %d)", phase.Loop.OnExhaust.Goto, phase.Loop.OnExhaust.Max)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

func gatherLog(artifactsDir string, phaseIndex int) string {
	path := state.LogPath(artifactsDir, phaseIndex)
	data, err := os.ReadFile(path)
	if err != nil {
		return "(no log file found)"
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
		return fmt.Sprintf("... (truncated to last %d lines)\n%s", maxLogLines, strings.Join(lines, "\n"))
	}
	return string(data)
}

func gatherPrompt(artifactsDir string, phaseIndex int, phase config.Phase) string {
	if phase.Type != "agent" {
		return ""
	}
	path := state.PromptPath(artifactsDir, phaseIndex)
	data, err := os.ReadFile(path)
	if err != nil {
		return "(no rendered prompt found)"
	}
	return string(data)
}

func gatherFeedback(artifactsDir string) string {
	dir := filepath.Join(artifactsDir, "feedback")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("--- %s ---\n%s", e.Name(), string(data)))
	}
	return strings.Join(parts, "\n")
}

// gatherTimingWithFallback tries the audit dir first, falls back to artifacts dir.
// Returns timing for ALL phases so the doctor can correlate durations with timeouts.
func gatherTimingWithFallback(auditDir, artifactsDir string) string {
	result := gatherTiming(auditDir)
	if result == "" {
		result = gatherTiming(artifactsDir)
	}
	return result
}

func gatherTiming(dir string) string {
	timing, err := state.LoadTiming(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range timing.Entries {
		if e.Duration != "" {
			parts = append(parts, fmt.Sprintf("%s started %s, duration %s",
				e.Phase, e.Start.Format("15:04:05"), e.Duration))
		} else {
			parts = append(parts, fmt.Sprintf("%s started %s (did not complete)",
				e.Phase, e.Start.Format("15:04:05")))
		}
	}
	return strings.Join(parts, "; ")
}

func gatherLoopCounts(artifactsDir string) string {
	counts, err := state.LoadLoopCounts(artifactsDir)
	if err != nil {
		return ""
	}
	if len(counts) == 0 {
		return ""
	}
	var parts []string
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	return strings.Join(parts, ", ")
}

// gatherAllLogs reads log files from all phases except the failed one.
// Each phase's log is truncated to maxOtherLogLines lines.
func gatherAllLogs(artifactsDir string, phases []config.Phase, failedIdx int) string {
	var parts []string
	for i, p := range phases {
		if i == failedIdx {
			continue
		}
		path := state.LogPath(artifactsDir, i)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		content = truncateLines(content, maxOtherLogLines)
		parts = append(parts, fmt.Sprintf("### Phase %d: %s\n%s", i+1, p.Name, content))
	}
	return strings.Join(parts, "\n\n")
}

// gatherIterationLogs reads archived iteration logs from the audit dir.
// Returns formatted content from all previous iterations of the given phase.
func gatherIterationLogs(auditDir string, phaseIdx int) string {
	logsDir := filepath.Join(auditDir, "logs")
	pattern := fmt.Sprintf("phase-%d.iter-*.log", phaseIdx+1)
	matches, err := filepath.Glob(filepath.Join(logsDir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)

	var parts []string
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		content = truncateLines(content, maxIterLogLines)
		name := filepath.Base(m)
		parts = append(parts, fmt.Sprintf("### %s\n%s", name, content))
	}
	return strings.Join(parts, "\n\n")
}

// truncateLines returns the last maxLines lines of content, with a truncation notice if needed.
func truncateLines(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	lines = lines[len(lines)-maxLines:]
	return fmt.Sprintf("... (truncated to last %d lines)\n%s", maxLines, strings.Join(lines, "\n"))
}

func runClaude(ctx context.Context, prompt, model string) error {
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--model", model, "--effort", "high",
		"--output-format", "stream-json",
		"--verbose",
	)
	cmd.Env = dispatch.FilteredEnv()
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting claude: %w", err)
	}

	_, streamErr := dispatch.ProcessStream(ctx, stdout, os.Stdout, nil, nil)

	if err := cmd.Wait(); err != nil && streamErr == nil {
		return fmt.Errorf("claude: %w", err)
	}
	return streamErr
}
