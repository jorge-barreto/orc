package ux

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
)

type flowColors struct {
	phaseName   string
	agentIcon   string
	scriptIcon  string
	gateIcon    string
	modelBadge  string
	modelQual   string
	scopeColors []string
	outputs     string
	loopBack    string
	loopCond    string
	description string
	phaseNum    string
	projectName string
	statsLine   string
	complete    string
	reset       string
	bold        string
	dim         string
}

func (c flowColors) scopeColor(idx int) string {
	if len(c.scopeColors) == 0 {
		return ""
	}
	return c.scopeColors[idx%len(c.scopeColors)]
}

func newFlowColors() flowColors {
	return flowColors{
		phaseName:   BoldCyan,
		agentIcon:   Cyan,
		scriptIcon:  Yellow,
		gateIcon:    Red,
		modelBadge:  Blue,
		modelQual:   BoldBlue,
		scopeColors: []string{Magenta, Cyan, Yellow, Blue, Green},
		outputs:     Green,
		loopBack:    Yellow,
		loopCond:    Dim,
		description: "",
		phaseNum:    Dim,
		projectName: BoldCyan,
		statsLine:   Dim,
		complete:    BoldGreen,
		reset:       Reset,
		bold:        Bold,
		dim:         Dim,
	}
}

func newFlowColorsPlain() flowColors {
	return flowColors{}
}

type vizScope struct {
	startIdx int
	endIdx   int
	label    string
}

func computeVizScopes(cfg *config.Config) []vizScope {
	loopTargets := map[int][]int{}
	targetLabels := map[int]string{}
	for i, p := range cfg.Phases {
		if p.Loop != nil {
			targetIdx := cfg.PhaseIndex(p.Loop.Goto)
			if targetIdx >= 0 {
				loopTargets[targetIdx] = append(loopTargets[targetIdx], i)
				targetLabels[targetIdx] = p.Loop.Goto + " loop"
			}
		}
	}

	var scopes []vizScope
	for targetIdx, sources := range loopTargets {
		maxSrc := sources[0]
		for _, s := range sources[1:] {
			if s > maxSrc {
				maxSrc = s
			}
		}
		scopes = append(scopes, vizScope{
			startIdx: targetIdx,
			endIdx:   maxSrc,
			label:    targetLabels[targetIdx],
		})
	}

	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].startIdx != scopes[j].startIdx {
			return scopes[i].startIdx < scopes[j].startIdx
		}
		return scopes[i].endIdx > scopes[j].endIdx
	})

	return scopes
}

func buildGutter(scopes []vizScope, phaseIdx int, c flowColors) string {
	var parts []string
	for si, s := range scopes {
		if phaseIdx >= s.startIdx && phaseIdx <= s.endIdx {
			parts = append(parts, c.scopeColor(si)+"│"+c.reset+"  ")
		}
	}
	return strings.Join(parts, "")
}

func buildOtherGutter(scopes []vizScope, excludeIdx int, phaseIdx int, c flowColors) string {
	var parts []string
	for si, s := range scopes {
		if si == excludeIdx {
			continue
		}
		if phaseIdx >= s.startIdx && phaseIdx <= s.endIdx {
			parts = append(parts, c.scopeColor(si)+"│"+c.reset+"  ")
		}
	}
	return strings.Join(parts, "")
}

func phaseIcon(phaseType string, c flowColors) string {
	switch phaseType {
	case "agent":
		return c.agentIcon + "◆" + c.reset
	case "script":
		return c.scriptIcon + "▸" + c.reset
	case "gate":
		return c.gateIcon + "⏸" + c.reset
	}
	return " "
}

func pluralize(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

func isInAnyScope(scopes []vizScope, idx int) bool {
	for _, s := range scopes {
		if idx >= s.startIdx && idx <= s.endIdx {
			return true
		}
	}
	return false
}

// FlowViz renders a rich, colored visualization of the workflow config.
func FlowViz(cfg *config.Config) {
	c := newFlowColors()
	if Reset == "" {
		c = newFlowColorsPlain()
	}

	scopes := computeVizScopes(cfg)

	// Header
	fmt.Printf("  %sorc%s · %s%s%s\n", c.bold, c.reset, c.projectName, cfg.Name, c.reset)

	// Stats
	var agents, scripts, gates, loops int
	for _, p := range cfg.Phases {
		switch p.Type {
		case "agent":
			agents++
		case "script":
			scripts++
		case "gate":
			gates++
		}
		if p.Loop != nil {
			loops++
		}
	}
	var statParts []string
	statParts = append(statParts, pluralize(len(cfg.Phases), "phase"))
	if agents > 0 {
		statParts = append(statParts, pluralize(agents, "agent"))
	}
	if scripts > 0 {
		statParts = append(statParts, pluralize(scripts, "script"))
	}
	if gates > 0 {
		statParts = append(statParts, pluralize(gates, "gate"))
	}
	if loops > 0 {
		statParts = append(statParts, pluralize(loops, "loop"))
	}
	fmt.Printf("  %s%s%s\n", c.statsLine, strings.Join(statParts, " · "), c.reset)

	// Separator
	fmt.Println()
	fmt.Println(strings.Repeat("─", 70))

	// Phases
	for i, p := range cfg.Phases {
		// Bracket open lines: check if any scope starts at this index
		for si, s := range scopes {
			if s.startIdx == i {
				sc := c.scopeColor(si)
				pg := buildOtherGutter(scopes, si, s.startIdx, c)
				fmt.Printf("  %s%s╭─%s %s%s%s\n", pg, sc, c.reset, sc, s.label, c.reset)
				fmt.Printf("  %s%s│%s\n", pg, sc, c.reset)
			}
		}

		gutter := buildGutter(scopes, i, c)

		// Line 1: phase number + icon + name + model badge
		num := fmt.Sprintf("%s%2d%s", c.phaseNum, i+1, c.reset)
		icon := phaseIcon(p.Type, c)
		name := fmt.Sprintf("%s%s%s", c.phaseName, p.Name, c.reset)

		mainLine := fmt.Sprintf("  %s  %s  %s %s", gutter, num, icon, name)

		// Model badge for agent phases
		if p.Type == "agent" && p.Model != "" {
			badge := fmt.Sprintf("%s%s%s", c.modelBadge, p.Model, c.reset)
			effort := p.Effort
			if effort == "" {
				effort = "high"
			}
			if effort != "high" {
				badge += fmt.Sprintf(" %s⚡%s", c.modelQual, c.reset)
			}
			// Right-align model badge
			// Approximate: pad to ~60 columns total
			plainLen := 2 + gutterPlainLen(scopes, i) + 2 + 2 + 2 + 1 + 1 + len(p.Name)
			pad := 60 - plainLen
			if pad < 2 {
				pad = 2
			}
			mainLine += strings.Repeat(" ", pad) + badge
		}

		fmt.Println(mainLine)

		// Line 2: Description
		indent := fmt.Sprintf("  %s       ", gutter)
		if p.Description != "" {
			fmt.Printf("%s%s%s%s\n", indent, c.description, p.Description, c.reset)
		}

		// Hook annotations
		if p.PreRun != "" {
			pre := p.PreRun
			if len(pre) > 50 {
				pre = pre[:47] + "..."
			}
			fmt.Printf("%s%s▸ pre: %s%s\n", indent, c.scriptIcon, pre, c.reset)
		}
		if p.PostRun != "" {
			post := p.PostRun
			if len(post) > 50 {
				post = post[:47] + "..."
			}
			fmt.Printf("%s%s▸ post: %s%s\n", indent, c.scriptIcon, post, c.reset)
		}

		// Line 3: Outputs
		if len(p.Outputs) > 0 {
			fmt.Printf("%s%s→ %s%s\n", indent, c.outputs, strings.Join(p.Outputs, "  "), c.reset)
		}

		// Line 4: Loop-back annotation
		if p.Loop != nil {
			loopLine := fmt.Sprintf("%s%s↩ %s (max %d)%s", indent, c.loopBack, p.Loop.Goto, p.Loop.Max, c.reset)
			if p.Loop.Check != "" {
				check := p.Loop.Check
				if len(check) > 40 {
					check = check[:37] + "..."
				}
				loopLine += fmt.Sprintf("  %sif %s%s", c.loopCond, check, c.reset)
			}
			fmt.Println(loopLine)
		}

		// Bracket close lines: check if any scope ends at this index
		// Process inner-to-outer (reverse order since scopes are outer-first)
		for si := len(scopes) - 1; si >= 0; si-- {
			s := scopes[si]
			if s.endIdx == i {
				sc := c.scopeColor(si)
				pg := buildOtherGutter(scopes, si, s.endIdx, c)
				fmt.Printf("  %s%s│%s\n", pg, sc, c.reset)
				fmt.Printf("  %s%s╰─%s\n", pg, sc, c.reset)
				if pg != "" {
					fmt.Printf("  %s\n", pg)
				} else {
					fmt.Println()
				}
			}
		}

		// Spacing between phases
		if i < len(cfg.Phases)-1 {
			closedScope := false
			for _, s := range scopes {
				if s.endIdx == i {
					closedScope = true
					break
				}
			}
			if !closedScope {
				inScope := isInAnyScope(scopes, i) && isInAnyScope(scopes, i+1)
				if inScope {
					// Blank line with gutter between phases in a loop
					nextGutter := buildGutter(scopes, i, c)
					fmt.Printf("  %s\n", nextGutter)
				} else {
					// Separator between top-level sections
					fmt.Println(strings.Repeat("─", 70))
				}
			}
		}
	}

	// Footer
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("  %s✓ complete%s\n", c.complete, c.reset)
}

// gutterPlainLen returns the plain-text length of the gutter for a phase.
func gutterPlainLen(scopes []vizScope, phaseIdx int) int {
	n := 0
	for _, s := range scopes {
		if phaseIdx >= s.startIdx && phaseIdx <= s.endIdx {
			n += 3 // "│  " or "   "
		}
	}
	return n
}
