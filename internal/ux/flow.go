package ux

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
)

// loopScope tracks the start and end indices of a loop region.
type loopScope struct {
	startIdx int
	endIdx   int
}

// FlowDiagram prints a visual flow diagram for a workflow config.
func FlowDiagram(cfg *config.Config, customVars map[string]string, expandFn func(string) string) {
	if expandFn == nil {
		expandFn = func(s string) string { return s }
	}

	phases := cfg.Phases

	// Pre-scan: identify loop targets and build scopes.
	// loopTargets maps a target phase index to the list of source phase indices.
	loopTargets := map[int][]int{}
	var scopes []loopScope
	for i, p := range phases {
		if p.Loop != nil {
			targetIdx := cfg.PhaseIndex(p.Loop.Goto)
			if targetIdx >= 0 {
				loopTargets[targetIdx] = append(loopTargets[targetIdx], i)
			}
		}
	}

	// Build scopes: for each unique target, the scope spans from targetIdx to the max source.
	for targetIdx, sources := range loopTargets {
		maxSrc := sources[0]
		for _, s := range sources[1:] {
			if s > maxSrc {
				maxSrc = s
			}
		}
		scopes = append(scopes, loopScope{startIdx: targetIdx, endIdx: maxSrc})
	}

	// Header
	fmt.Printf("Workflow: %s (%d phases)\n", cfg.Name, len(phases))
	if cfg.MaxCost > 0 {
		fmt.Printf("  max-cost: $%.2f\n", cfg.MaxCost)
	}
	if len(customVars) > 0 {
		fmt.Printf("  Vars:\n")
		keys := make([]string, 0, len(customVars))
		for k := range customVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    %s = %s\n", k, customVars[k])
		}
	}
	fmt.Println()

	// Render each phase
	for i, p := range phases {
		// Determine active scopes for this phase index
		margin := buildMargin(scopes, i)

		// Phase line
		typeLabel := p.Type
		if p.Type == "agent" && p.Model != "" {
			typeLabel = p.Type + "/" + p.Model
		}

		phaseLine := fmt.Sprintf("%d. %s [%s]", i+1, p.Name, typeLabel)
		if p.Description != "" {
			phaseLine += fmt.Sprintf(" — %s", p.Description)
		}
		fmt.Printf("  %s%s\n", margin, phaseLine)

		// Detail lines
		detailMargin := buildDetailMargin(scopes, i)

		// Script: run command
		if p.Type == "script" && p.Run != "" {
			expanded := expandFn(p.Run)
			if len(expanded) > 60 {
				expanded = expanded[:57] + "..."
			}
			fmt.Printf("  %s  run: %s\n", detailMargin, expanded)
		}

		// Agent: non-default timeout
		if p.Type == "agent" && p.Timeout != 30 && p.Timeout > 0 {
			fmt.Printf("  %s  timeout: %dm\n", detailMargin, p.Timeout)
		}

		// Agent: phase-level max-cost
		if p.Type == "agent" && p.MaxCost > 0 {
			fmt.Printf("  %s  max-cost: $%.2f\n", detailMargin, p.MaxCost)
		}

		// Outputs
		if len(p.Outputs) > 0 {
			fmt.Printf("  %s  outputs: %s\n", detailMargin, strings.Join(p.Outputs, ", "))
		}

		// Condition
		if p.Condition != "" {
			fmt.Printf("  %s  condition: %s\n", detailMargin, expandFn(p.Condition))
		}

		// Parallel-with
		if p.ParallelWith != "" {
			fmt.Printf("  %s  parallel-with: %s\n", detailMargin, p.ParallelWith)
		}

		// Loop details
		if p.Loop != nil {
			// Check line (before loop annotation)
			if p.Loop.Check != "" {
				fmt.Printf("  %s  check: %s\n", detailMargin, expandFn(p.Loop.Check))
			}

			// Loop annotation
			loopLabel := formatLoopLabel(p.Loop)
			fmt.Printf("  %s  loop ──┘ %s\n", detailMargin, loopLabel)

			// On-exhaust (after loop annotation)
			if p.Loop.OnExhaust != nil {
				oe := p.Loop.OnExhaust
				fmt.Printf("  %s  on-exhaust → %s (max %d)\n", detailMargin, oe.Goto, oe.Max)
			}
		}

		// Connector line between phases (not after last)
		if i < len(phases)-1 {
			connMargin := buildConnectorMargin(scopes, i)
			fmt.Printf("  %s\n", connMargin)
		}
	}

	// Footer
	fmt.Println()
	fmt.Println("  ✓ complete")
}

// buildMargin returns the left margin string for a phase line at index i.
// If the phase is a loop target, the outermost scope gets ┌─▶ instead of │.
func buildMargin(scopes []loopScope, i int) string {
	// Sort scopes by startIdx for consistent rendering
	sorted := sortedScopes(scopes)

	var parts []string
	for _, s := range sorted {
		if i == s.startIdx {
			parts = append(parts, "┌─▶ ")
		} else if i > s.startIdx && i <= s.endIdx {
			parts = append(parts, "│   ")
		}
	}
	return strings.Join(parts, "")
}

// buildDetailMargin returns the margin for detail lines under phase i.
// Detail lines always use │ for active scopes.
func buildDetailMargin(scopes []loopScope, i int) string {
	sorted := sortedScopes(scopes)

	var parts []string
	for _, s := range sorted {
		if i >= s.startIdx && i <= s.endIdx {
			parts = append(parts, "│   ")
		}
	}
	return strings.Join(parts, "")
}

// buildConnectorMargin returns the margin for the │ connector line after phase i.
func buildConnectorMargin(scopes []loopScope, i int) string {
	sorted := sortedScopes(scopes)

	var parts []string
	for _, s := range sorted {
		if i >= s.startIdx && i < s.endIdx {
			parts = append(parts, "│   ")
		}
	}

	// Add the main connector │
	parts = append(parts, "│")
	return strings.Join(parts, "")
}

// sortedScopes returns scopes sorted by startIdx for deterministic rendering.
func sortedScopes(scopes []loopScope) []loopScope {
	sorted := make([]loopScope, len(scopes))
	copy(sorted, scopes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].startIdx < sorted[j].startIdx
	})
	return sorted
}

// formatLoopLabel formats the (min N, max M) or (max M) label.
func formatLoopLabel(loop *config.Loop) string {
	if loop.Min > 1 {
		return fmt.Sprintf("(min %d, max %d)", loop.Min, loop.Max)
	}
	return fmt.Sprintf("(max %d)", loop.Max)
}
