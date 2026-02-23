package ux

import (
	"fmt"
	"strings"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
)

// ANSI color helpers
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
)

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

// LoopBack prints a loop-back message for on-fail retries.
func LoopBack(fromPhase, toPhase string, attempt, max int) {
	fmt.Printf("%s[%s]%s  %s↺ Phase %q failed. Looping back to %q (attempt %d/%d)%s\n",
		Dim, timestamp(), Reset, Yellow, fromPhase, toPhase, attempt, max, Reset)
}

// PhaseSkip prints a phase skip message (condition not met).
func PhaseSkip(index int, phaseName string) {
	fmt.Printf("%s[%s]%s  %s– Phase %d (%s) skipped (condition not met)%s\n",
		Dim, timestamp(), Reset, Dim, index+1, phaseName, Reset)
}

// ToolUse prints an inline tool call.
func ToolUse(name, input string) {
	summary := input
	if len(summary) > 80 {
		summary = summary[:77] + "..."
	}
	fmt.Printf("  %s⚡ %s%s %s\n", Cyan, name, Reset, summary)
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

// Success prints a final success message.
func Success(total int) {
	fmt.Printf("\n%s[%s]%s  %s%s══ All %d phases complete ══%s\n\n",
		Dim, timestamp(), Reset, Bold, Green, total, Reset)
}
