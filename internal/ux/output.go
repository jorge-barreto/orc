package ux

import (
	"fmt"
	"os"
	"strings"
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

// IsTerminal reports whether the given file is a terminal.
func IsTerminal(f *os.File) bool {
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

func timestamp() string {
	return time.Now().Format("15:04:05")
}

// PhaseHeader prints a timestamped phase header.
func PhaseHeader(index, total int, phase config.Phase) {
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
func PhaseComplete(index int, duration time.Duration) {
	m := int(duration.Minutes())
	s := int(duration.Seconds()) % 60
	fmt.Printf("%s[%s]%s  %s✓ Phase %d complete (%dm %02ds)%s\n",
		Dim, timestamp(), Reset, Green, index+1, m, s, Reset)
}

// PhaseFail prints a phase failure message.
func PhaseFail(index int, phaseName, errMsg string) {
	fmt.Printf("%s[%s]%s  %s✗ Phase %d (%s) failed: %s%s\n",
		Dim, timestamp(), Reset, Red, index+1, phaseName, errMsg, Reset)
}

// ResumeHint prints a resume command hint.
func ResumeHint(ticket string) {
	fmt.Printf("\n%sResume:%s orc run %s\n", Yellow, Reset, ticket)
}

// LoopBack prints a loop-back message for loop iterations.
func LoopBack(fromPhase, toPhase string, iteration, max int) {
	fmt.Printf("%s[%s]%s  %s↻ %q iteration %d/%d — looping back to %q%s\n",
		Dim, timestamp(), Reset, Yellow, fromPhase, iteration, max, toPhase, Reset)
}

// LoopExhausted prints a message when a loop has exhausted its max iterations.
func LoopExhausted(phaseName string, iteration int) {
	fmt.Printf("%s[%s]%s  %s✗ %q: loop exhausted after %d iterations%s\n",
		Dim, timestamp(), Reset, Red, phaseName, iteration, Reset)
}

// PhaseSkip prints a phase skip message (condition not met).
func PhaseSkip(index int, phaseName string) {
	fmt.Printf("%s[%s]%s  %s– Phase %d (%s) skipped (condition not met)%s\n",
		Dim, timestamp(), Reset, Dim, index+1, phaseName, Reset)
}

// ToolUse prints an inline tool call.
func ToolUse(name, input string) {
	fmt.Printf("  %s⚡ %s%s %s\n", Cyan, name, Reset, input)
}

// ToolDenied prints a denied tool call.
func ToolDenied(name, input string) {
	summary := input
	if len(summary) > 80 {
		summary = summary[:77] + "..."
	}
	fmt.Printf("  %s✗ %s(denied)%s %s\n", Red, name, Reset, summary)
}

// PermissionPrompt prints a permission denial prompt header.
func PermissionPrompt(tools []string) {
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
