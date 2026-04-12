package ux

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
)

// ANSI color helpers
var (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Red       = "\033[31m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Cyan      = "\033[36m"
	Magenta   = "\033[35m"
	Blue      = "\033[34m"
	BoldCyan  = "\033[1;36m"
	BoldBlue  = "\033[1;34m"
	BoldGreen = "\033[1;32m"
)

// QuietMode suppresses decorated output and emits JSON lines instead.
var QuietMode bool

var quietMu sync.Mutex

// IsTerminal reports whether the given file is a terminal.
// It is a var so tests can override it to control the TTY check.
var IsTerminal = func(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// DisableColor sets all color variables to empty strings, disabling ANSI output globally.
func DisableColor() {
	Reset = ""
	Bold = ""
	Dim = ""
	Red = ""
	Green = ""
	Yellow = ""
	Cyan = ""
	Magenta = ""
	Blue = ""
	BoldCyan = ""
	BoldBlue = ""
	BoldGreen = ""
}

// EnableQuiet activates machine-friendly JSON output and disables color.
func EnableQuiet() {
	QuietMode = true
	DisableColor()
}

// QuietPhaseEvent emits a single JSON line for a phase transition.
// extra keys are merged into the event object.
func QuietPhaseEvent(phase string, status string, extra map[string]interface{}) {
	event := map[string]interface{}{
		"phase":  phase,
		"status": status,
	}
	for k, v := range extra {
		event[k] = v
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	quietMu.Lock()
	fmt.Println(string(data))
	quietMu.Unlock()
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}

// PhaseHeader prints a timestamped phase header.
func PhaseHeader(index, total int, phase config.Phase) {
	if QuietMode {
		QuietPhaseEvent(phase.Name, "started", nil)
		return
	}
	fmt.Printf("\n%s[%s]%s %s══════════════════════════════════════%s\n",
		Dim, timestamp(), Reset, Cyan, Reset)
	desc := ""
	if phase.Description != "" {
		desc = fmt.Sprintf(" — %s", phase.Description)
	}
	fmt.Printf("%s[%s]%s  %sPhase %d/%d: %s (%s)%s%s\n",
		Dim, timestamp(), Reset, Bold, index+1, total, phase.Name, phase.Type, desc, Reset)
	fmt.Printf("%s[%s]%s %s══════════════════════════════════════%s\n",
		Dim, timestamp(), Reset, Cyan, Reset)
}

// PhaseComplete prints a phase completion message.
func PhaseComplete(index int, phaseName string, duration time.Duration) {
	if QuietMode {
		QuietPhaseEvent(phaseName, "complete", map[string]interface{}{"duration_s": duration.Seconds()})
		return
	}
	m := int(duration.Minutes())
	s := int(duration.Seconds()) % 60
	fmt.Printf("%s[%s]%s  %s✓ Phase %d complete (%dm %02ds)%s\n",
		Dim, timestamp(), Reset, Green, index+1, m, s, Reset)
}

// PhaseFail prints a phase failure message.
func PhaseFail(index int, phaseName, errMsg string) {
	if QuietMode {
		QuietPhaseEvent(phaseName, "failed", map[string]interface{}{"error": errMsg})
		return
	}
	fmt.Printf("%s[%s]%s  %s✗ Phase %d (%s) failed: %s%s\n",
		Dim, timestamp(), Reset, Red, index+1, phaseName, errMsg, Reset)
}

// ResumeHint prints a resume command hint.
// If sessionResumable is true, suggests --resume to continue the interrupted agent session.
func ResumeHint(ticket string, sessionResumable bool) {
	if QuietMode {
		return
	}
	if sessionResumable {
		fmt.Printf("\n%sResume:%s orc run %s --resume\n", Yellow, Reset, ticket)
	} else {
		fmt.Printf("\n%sResume:%s orc run %s\n", Yellow, Reset, ticket)
	}
}

// LoopBack prints a loop-back message for loop iterations.
func LoopBack(fromPhase, toPhase string, iteration, max int) {
	if QuietMode {
		QuietPhaseEvent(fromPhase, "loop_back", map[string]interface{}{"goto": toPhase, "iteration": iteration, "max": max})
		return
	}
	fmt.Printf("%s[%s]%s  %s↻ %q iteration %d/%d — looping back to %q%s\n",
		Dim, timestamp(), Reset, Yellow, fromPhase, iteration, max, toPhase, Reset)
}

// LoopExhausted prints a message when a loop has exhausted its max iterations.
func LoopExhausted(phaseName string, iteration int) {
	if QuietMode {
		QuietPhaseEvent(phaseName, "loop_exhausted", map[string]interface{}{"iterations": iteration})
		return
	}
	fmt.Printf("%s[%s]%s  %s✗ %q: loop exhausted after %d iterations%s\n",
		Dim, timestamp(), Reset, Red, phaseName, iteration, Reset)
}

// PhaseSkip prints a phase skip message (condition not met).
func PhaseSkip(index int, phaseName string) {
	if QuietMode {
		QuietPhaseEvent(phaseName, "skipped", nil)
		return
	}
	fmt.Printf("%s[%s]%s  %s– Phase %d (%s) skipped (condition not met)%s\n",
		Dim, timestamp(), Reset, Dim, index+1, phaseName, Reset)
}

// ToolUse prints an inline tool call.
func ToolUse(name, input string) {
	if QuietMode {
		return
	}
	fmt.Printf("  %s⚡ %s%s %s\n", Cyan, name, Reset, input)
}

// ToolDenied prints a denied tool call.
func ToolDenied(name, input string) {
	if QuietMode {
		return
	}
	summary := input
	if len(summary) > 80 {
		summary = summary[:77] + "..."
	}
	fmt.Printf("  %s✗ %s(denied)%s %s\n", Red, name, Reset, summary)
}

// PermissionPrompt prints a permission denial prompt header.
func PermissionPrompt(tools []string) {
	if QuietMode {
		return
	}
	fmt.Printf("\n  %s⚠ Tools denied: %s%s\n", Yellow, strings.Join(tools, ", "), Reset)
}

// wrapLines splits text into lines of at most maxWidth characters,
// breaking at word boundaries.
func wrapLines(text string, maxWidth int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	var current strings.Builder
	for _, word := range words {
		if current.Len() > 0 && current.Len()+1+len(word) > maxWidth {
			lines = append(lines, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

// AgentQuestion displays an AskUserQuestion from the agent with numbered options.
func AgentQuestion(question string, options []string) {
	if QuietMode {
		return
	}
	fmt.Printf("\n  %s┌─ Agent Question ─────────────────────────%s\n", BoldCyan, Reset)
	for _, line := range wrapLines(question, 60) {
		fmt.Printf("  %s│%s %s\n", BoldCyan, Reset, line)
	}
	if len(options) > 0 {
		fmt.Printf("  %s│%s\n", BoldCyan, Reset)
		for i, opt := range options {
			fmt.Printf("  %s│%s  %s%d)%s %s\n", BoldCyan, Reset, Bold, i+1, Reset, opt)
		}
	}
	fmt.Printf("  %s│%s\n", BoldCyan, Reset)
	if len(options) > 0 {
		fmt.Printf("  %s│%s [1-%d or type custom answer]: ", BoldCyan, Reset, len(options))
	} else {
		fmt.Printf("  %s│%s [type your answer]: ", BoldCyan, Reset)
	}
}

// RateLimitWait prints a rate-limit wait message with the expected reset time.
func RateLimitWait(resetTime time.Time) {
	if QuietMode {
		QuietPhaseEvent("", "rate_limit_wait", map[string]interface{}{
			"resets_at": resetTime.Format(time.RFC3339),
		})
		return
	}
	fmt.Printf("%s[%s]%s  %s⏱ Usage limit reached — waiting until %s (+ 60s buffer)%s\n",
		Dim, timestamp(), Reset, Yellow, resetTime.Format("15:04"), Reset)
}

// RateLimitHeartbeat prints a periodic heartbeat during rate-limit wait.
func RateLimitHeartbeat(remaining time.Duration) {
	if QuietMode {
		return
	}
	fmt.Printf("%s[%s]%s  %s⏱ Waiting for rate limit reset (%s remaining)%s\n",
		Dim, timestamp(), Reset, Dim, FormatWaitDuration(remaining), Reset)
}

// FormatWaitDuration formats a duration for human display in wait messages.
// Outputs "Xm YYs" for durations >= 1 minute, or "Xs" for shorter durations.
func FormatWaitDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %02ds", m, s)
}

// RateLimitHint prints a rate-limit failure hint with the reset time for interactive mode.
func RateLimitHint(resetTime time.Time) {
	if QuietMode {
		return
	}
	fmt.Printf("  %shint: usage limit reached, resets at %s — use --resume to continue later%s\n",
		Yellow, resetTime.Format("15:04"), Reset)
}
