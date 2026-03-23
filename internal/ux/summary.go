package ux

import (
	"fmt"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

type phaseOutcome struct {
	index    int // 0-based index in phases slice (for 1-indexed display)
	name     string
	typ      string
	runs     int
	duration time.Duration
	result   string // "pass", "FAIL", "skip"
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm %02ds", minutes, seconds)
}

func countRuns(timing *state.Timing, phaseName string) int {
	if timing == nil {
		return 0
	}
	count := 0
	for _, e := range timing.Entries {
		if e.Phase == phaseName {
			count++
		}
	}
	return count
}

func lastDuration(timing *state.Timing, phaseName string) time.Duration {
	if timing == nil {
		return 0
	}
	for i := len(timing.Entries) - 1; i >= 0; i-- {
		e := timing.Entries[i]
		if e.Phase == phaseName && !e.End.IsZero() {
			return e.End.Sub(e.Start)
		}
	}
	return 0
}

// RunSummary prints the run summary table showing phase outcomes.
func RunSummary(phases []config.Phase, timing *state.Timing, failedPhase int, skipped map[string]bool) {
	if QuietMode {
		return
	}
	lastIdx := len(phases) - 1
	if failedPhase >= 0 {
		lastIdx = failedPhase
	}

	var outcomes []phaseOutcome
	var totalDuration, agentDuration, scriptDuration time.Duration

	for i := 0; i <= lastIdx; i++ {
		name := phases[i].Name
		runs := countRuns(timing, name)

		if skipped[name] {
			outcomes = append(outcomes, phaseOutcome{
				index:  i,
				name:   name,
				typ:    phases[i].Type,
				runs:   0,
				result: "skip",
			})
			continue
		}

		if runs == 0 {
			continue // phantom phase — never ran
		}

		dur := lastDuration(timing, name)
		var result string
		if i == failedPhase {
			result = "FAIL"
		} else {
			result = "pass"
		}

		totalDuration += dur
		if phases[i].Type == "agent" {
			agentDuration += dur
		} else if phases[i].Type == "script" {
			scriptDuration += dur
		}

		outcomes = append(outcomes, phaseOutcome{
			index:    i,
			name:     name,
			typ:      phases[i].Type,
			runs:     runs,
			duration: dur,
			result:   result,
		})
	}

	// Compute max phase name length for column alignment
	nameWidth := 5 // minimum: len("Phase")
	for _, o := range outcomes {
		if len(o.name) > nameWidth {
			nameWidth = len(o.name)
		}
	}
	if nameWidth > 20 {
		nameWidth = 20
	}

	displayCount := len(outcomes)

	// Header line
	if failedPhase < 0 {
		fmt.Printf("  Run complete — %d phases, %s\n", displayCount, fmtDuration(totalDuration))
	} else {
		fmt.Printf("  Run failed — %d phases, %s\n", displayCount, fmtDuration(totalDuration))
	}

	// Table header
	fmt.Printf("\n  %-3s %-*s %-8s %5s %10s  %s\n", "#", nameWidth, "Phase", "Type", "Runs", "Duration", "Result")

	// Table rows
	for _, o := range outcomes {
		num := fmt.Sprintf("%d", o.index+1)
		var runsStr, durStr, resultStr string

		if o.result == "skip" {
			runsStr = "—"
			durStr = "—"
			resultStr = fmt.Sprintf("%s%s%s", Dim, "skip", Reset)
		} else {
			runsStr = fmt.Sprintf("%d", o.runs)
			if o.duration == 0 {
				durStr = "<1s"
			} else {
				durStr = fmtDuration(o.duration)
			}
			if o.result == "FAIL" {
				resultStr = fmt.Sprintf("%s%sFAIL%s", Red, Bold, Reset)
			} else {
				resultStr = fmt.Sprintf("%spass%s", Green, Reset)
			}
		}

		fmt.Printf("  %-3s %-*s %-8s %5s %10s  %s\n", num, nameWidth, o.name, o.typ, runsStr, durStr, resultStr)
	}

	// Totals
	fmt.Println()
	if agentDuration > 0 {
		fmt.Printf("  Total agent time: %s\n", fmtDuration(agentDuration))
	}
	if scriptDuration > 0 {
		fmt.Printf("  Total script time: %s\n", fmtDuration(scriptDuration))
	}
	fmt.Println()
}
