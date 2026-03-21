package ux

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// formatTokens returns a compact "in/out" string for token counts.
func formatTokens(input, output int) string {
	if input == 0 && output == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", input, output)
}

// formatCache returns a compact "read/write" string for cache token counts.
func formatCache(read, creation int) string {
	if read == 0 && creation == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", read, creation)
}

// RenderStatus prints the full status display for a ticket.
// It loads timing and costs from auditDir first, falling back to artifactsDir.
func RenderStatus(cfg *config.Config, st *state.State, artifactsDir, auditDir string) {
	timing, err := state.LoadTiming(auditDir)
	if err != nil {
		timing, _ = state.LoadTiming(artifactsDir)
	}
	costs, err := state.LoadCosts(auditDir)
	if err != nil {
		costs, _ = state.LoadCosts(artifactsDir)
	}

	// Header
	fmt.Printf("%sTicket:%s  %s\n", Bold, Reset, st.GetTicket())
	if st.GetPhaseIndex() >= len(cfg.Phases) {
		fmt.Printf("%sState:%s   %s%scompleted%s\n", Bold, Reset, Green, Bold, Reset)
	} else {
		phase := cfg.Phases[st.GetPhaseIndex()]
		fmt.Printf("%sState:%s   %d/%d (%s) — %s\n",
			Bold, Reset, st.GetPhaseIndex()+1, len(cfg.Phases), phase.Name, st.GetStatus())
	}
	if timing != nil {
		if elapsed := timing.TotalElapsed(); elapsed > 0 {
			fmt.Printf("%sElapsed:%s %s\n", Bold, Reset, state.FormatDuration(elapsed))
		}
	}

	// Completed phases — show full execution trace from timing entries
	hasCompleted := timing != nil && len(timing.Entries) > 0
	if hasCompleted {
		fmt.Printf("\n  %s%-4s%-20s%8s%10s%16s%18s%8s%s\n",
			Bold, "#", "PHASE", "TIME", "COST", "TOKENS IN/OUT", "CACHE R/W", "RUN", Reset)

		// Count total occurrences of each phase to know which ones repeated
		phaseTotal := make(map[string]int)
		for _, te := range timing.Entries {
			phaseTotal[te.Phase]++
		}

		costIdx := 0
		phaseSeen := make(map[string]int)
		for i, te := range timing.Entries {
			phaseSeen[te.Phase]++

			dur := te.Duration
			if dur == "" {
				dur = "-"
			}

			// Match cost entry (costs are chronological, only for agent phases)
			var costStr, tokenStr, cacheStr string
			if costs != nil && costIdx < len(costs.Phases) && costs.Phases[costIdx].Name == te.Phase {
				ce := costs.Phases[costIdx]
				costIdx++
				if ce.CostUSD > 0 {
					costStr = fmt.Sprintf("$%.2f", ce.CostUSD)
				}
				tokenStr = formatTokens(ce.InputTokens, ce.OutputTokens)
				cacheStr = formatCache(ce.CacheReadInputTokens, ce.CacheCreationInputTokens)
			}

			// Run number — only show for phases that ran more than once
			var runStr string
			if phaseTotal[te.Phase] > 1 {
				runStr = fmt.Sprintf("%d", phaseSeen[te.Phase])
			}

			fmt.Printf("  %s%-4d%s%-20s%8s%10s%16s%18s%8s\n",
				Dim, i+1, Reset, te.Phase, dur, costStr, tokenStr, cacheStr, runStr)
		}

		// Totals line
		totalElapsed := state.FormatDuration(timing.TotalElapsed())
		totalCost := ""
		if costs != nil && costs.TotalCostUSD > 0 {
			totalCost = fmt.Sprintf("$%.2f", costs.TotalCostUSD)
		}
		if totalElapsed != "0m 00s" || totalCost != "" {
			fmt.Printf("  %s%-4s%-20s%8s%10s%s\n",
				"", "", "", Bold+totalElapsed+Reset, Bold+totalCost+Reset, "")
		}
	} else if st.GetPhaseIndex() > 0 {
		// Fallback: no timing data, just list completed phase names
		fmt.Printf("\n%sCompleted:%s\n", Bold, Reset)
		for i := 0; i < st.GetPhaseIndex() && i < len(cfg.Phases); i++ {
			p := cfg.Phases[i]
			fmt.Printf("  %s%d%s  %-20s %sdone%s\n",
				Dim, i+1, Reset, p.Name, Green, Reset)
		}
	}

	// Remaining phases
	if st.GetPhaseIndex() < len(cfg.Phases) {
		fmt.Printf("\n%sRemaining:%s\n", Bold, Reset)
		for i := st.GetPhaseIndex(); i < len(cfg.Phases); i++ {
			p := cfg.Phases[i]
			marker := "  "
			if i == st.GetPhaseIndex() {
				marker = fmt.Sprintf("%s→%s ", Yellow, Reset)
			}
			fmt.Printf("  %s%s%d%s  %-20s %s(%s)%s\n",
				marker, Dim, i+1, Reset, p.Name, Dim, p.Type, Reset)
		}
	}

	// Artifacts listing
	fmt.Printf("\n%sArtifacts:%s\n", Bold, Reset)
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		fmt.Printf("  %s(none)%s\n", Dim, Reset)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			subEntries, _ := os.ReadDir(filepath.Join(artifactsDir, e.Name()))
			if len(subEntries) > 0 {
				first := subEntries[0].Name()
				last := subEntries[len(subEntries)-1].Name()
				if first == last {
					fmt.Printf("  %s/%s/%s\n", artifactsDir, e.Name(), first)
				} else {
					fmt.Printf("  %s/%s/%s .. %s\n", artifactsDir, e.Name(), first, last)
				}
			}
		} else {
			fmt.Printf("  %s/%s\n", artifactsDir, e.Name())
		}
	}
	fmt.Println()
}

// RenderStatusAll prints a compact summary table of all tickets.
func RenderStatusAll(cfg *config.Config, tickets []state.TicketSummary) {
	if len(tickets) == 0 {
		fmt.Printf("%sNo tickets found.%s\n", Dim, Reset)
		return
	}

	// Check if any ticket has a workflow name
	hasWorkflow := false
	for _, t := range tickets {
		if t.State.GetWorkflow() != "" {
			hasWorkflow = true
			break
		}
	}

	if hasWorkflow {
		fmt.Printf("%s%-14s%-14s%-14s%-17s%-10s%s%s\n", Bold, "TICKET", "WORKFLOW", "STATUS", "PHASE", "COST", "TIME", Reset)
	} else {
		fmt.Printf("%s%-14s%-14s%-17s%-10s%s%s\n", Bold, "TICKET", "STATUS", "PHASE", "COST", "TIME", Reset)
	}

	for _, t := range tickets {
		statusColor := Dim
		switch t.State.GetStatus() {
		case state.StatusCompleted:
			statusColor = Green
		case state.StatusRunning:
			statusColor = Cyan
		case state.StatusFailed:
			statusColor = Red
		case state.StatusInterrupted:
			statusColor = Yellow
		}

		var phase string
		if len(cfg.Phases) == 0 {
			phase = fmt.Sprintf("phase %d", t.State.GetPhaseIndex()+1)
		} else if t.State.GetPhaseIndex() >= len(cfg.Phases) {
			phase = fmt.Sprintf("%d/%d", len(cfg.Phases), len(cfg.Phases))
		} else {
			p := cfg.Phases[t.State.GetPhaseIndex()]
			phase = fmt.Sprintf("%d/%d (%s)", t.State.GetPhaseIndex()+1, len(cfg.Phases), p.Name)
		}

		cost := fmt.Sprintf("$%.2f", t.Costs.TotalCostUSD)

		var elapsed string
		if t.Timing != nil {
			if d := t.Timing.TotalElapsed(); d > 0 {
				elapsed = state.FormatDuration(d)
			}
		}

		if hasWorkflow {
			wf := t.State.GetWorkflow()
			if wf == "" {
				wf = "-"
			}
			fmt.Printf("%-14s%-14s%s%-14s%s%-17s%-10s%s\n",
				t.Ticket, wf,
				statusColor, t.State.GetStatus(), Reset,
				phase, cost, elapsed)
		} else {
			fmt.Printf("%-14s%s%-14s%s%-17s%-10s%s\n",
				t.Ticket,
				statusColor, t.State.GetStatus(), Reset,
				phase, cost, elapsed)
		}
	}
	fmt.Println()
}
