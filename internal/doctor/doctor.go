package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

const maxLogLines = 200

const diagPrompt = `You are diagnosing a failed orc workflow phase. Analyze the context below and provide a concise diagnosis.

## Failed Phase Config
%s

## Phase Log Output (last %d lines)
%s
%s%s%s
Instructions:
1. Identify what went wrong from the log output.
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

	phase := cfg.Phases[st.PhaseIndex]

	phaseConfig := gatherPhaseConfig(phase)
	log := gatherLog(artifactsDir, st.PhaseIndex)
	prompt := gatherPrompt(artifactsDir, st.PhaseIndex, phase)
	feedback := gatherFeedback(artifactsDir)
	timing := gatherTiming(artifactsDir, phase.Name)
	loops := gatherLoopCounts(artifactsDir)

	diagText := buildPrompt(phaseConfig, log, prompt, feedback, timing, loops)

	// Print header
	fmt.Printf("\n%s%s══ Doctor: diagnosing phase %d/%d (%s) ══%s\n\n",
		ux.Bold, ux.Cyan, st.PhaseIndex+1, len(cfg.Phases), phase.Name, ux.Reset)

	if err := runClaude(ctx, diagText); err != nil {
		return fmt.Errorf("failed to run claude: %w", err)
	}

	fmt.Println()
	ux.ResumeHint(st.Ticket)
	return nil
}

func buildPrompt(phaseConfig, log, prompt, feedback, timing, loops string) string {
	var promptSection, feedbackSection, timingSection string
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

	return fmt.Sprintf(diagPrompt, phaseConfig, maxLogLines, log, promptSection, feedbackSection, timingSection)
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
	if phase.OnFail != nil {
		parts = append(parts, fmt.Sprintf("On-fail: goto %s (max %d)", phase.OnFail.Goto, phase.OnFail.Max))
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

func gatherTiming(artifactsDir, phaseName string) string {
	timing, err := state.LoadTiming(artifactsDir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range timing.Entries {
		if e.Phase != phaseName {
			continue
		}
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

func filteredEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDECODE") {
			continue
		}
		env = append(env, e)
	}
	return env
}

func runClaude(ctx context.Context, prompt string) error {
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--model", "sonnet")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = filteredEnv()
	return cmd.Run()
}
