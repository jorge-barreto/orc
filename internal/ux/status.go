package ux

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RenderStatus prints the full status display for a ticket.
func RenderStatus(cfg *config.Config, st *state.State, artifactsDir string) {
	timing, _ := state.LoadTiming(artifactsDir)

	// Header
	fmt.Printf("%sTicket:%s  %s\n", Bold, Reset, st.Ticket)
	if st.PhaseIndex >= len(cfg.Phases) {
		fmt.Printf("%sState:%s   %s%scompleted%s\n", Bold, Reset, Green, Bold, Reset)
	} else {
		phase := cfg.Phases[st.PhaseIndex]
		fmt.Printf("%sState:%s   %d/%d (%s) — %s\n",
			Bold, Reset, st.PhaseIndex+1, len(cfg.Phases), phase.Name, st.Status)
	}

	// Completed phases
	if st.PhaseIndex > 0 {
		fmt.Printf("\n%sCompleted:%s\n", Bold, Reset)
		for i := 0; i < st.PhaseIndex && i < len(cfg.Phases); i++ {
			p := cfg.Phases[i]
			dur := findDuration(timing, p.Name)
			fmt.Printf("  %s%d%s  %-20s %sdone%s  %s\n",
				Dim, i+1, Reset, p.Name, Green, Reset, dur)
		}
	}

	// Remaining phases
	if st.PhaseIndex < len(cfg.Phases) {
		fmt.Printf("\n%sRemaining:%s\n", Bold, Reset)
		for i := st.PhaseIndex; i < len(cfg.Phases); i++ {
			p := cfg.Phases[i]
			marker := "  "
			if i == st.PhaseIndex {
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

func findDuration(timing *state.Timing, phaseName string) string {
	if timing == nil {
		return ""
	}
	for i := len(timing.Entries) - 1; i >= 0; i-- {
		if timing.Entries[i].Phase == phaseName && timing.Entries[i].Duration != "" {
			return fmt.Sprintf("(%s)", timing.Entries[i].Duration)
		}
	}
	return ""
}
