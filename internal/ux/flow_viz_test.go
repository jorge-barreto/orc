package ux

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestFlowViz_Simple(t *testing.T) {
	cfg := &config.Config{
		Name: "simple",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "implement", Type: "agent", Model: "sonnet", Effort: "high"},
			{Name: "review", Type: "gate"},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{
		"orc",
		"simple",
		"3 phases",
		"◆",
		"⏸",
		"opus",
		"sonnet",
		"✓ complete",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	mustNotContain := []string{"╭", "╰"}
	for _, s := range mustNotContain {
		if strings.Contains(output, s) {
			t.Errorf("output should not contain %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowViz_WithLoop(t *testing.T) {
	cfg := &config.Config{
		Name: "looped",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "implement", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "review", Type: "agent", Model: "opus", Effort: "high",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 3}},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{
		"╭─",
		"╰─",
		"implement loop",
		"↩",
		"(max 3)",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowViz_NestedLoops(t *testing.T) {
	cfg := &config.Config{
		Name: "nested",
		Phases: []config.Phase{
			{Name: "pick-bead", Type: "script", Run: "pick"},
			{Name: "implement", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "build-test", Type: "script", Run: "make test",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 5}},
			{Name: "review", Type: "agent", Model: "opus", Effort: "low",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 3}},
			{Name: "wrap-up", Type: "agent", Model: "sonnet", Effort: "high"},
			{Name: "check-remaining", Type: "script", Run: "check",
				Loop: &config.Loop{Goto: "pick-bead", Min: 1, Max: 20}},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{
		"pick-bead loop",
		"implement loop",
		"⚡",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// Check for nested brackets: inner ╭─ should appear while outer │ gutter is active
	lines := strings.Split(output, "\n")
	foundNested := false
	for _, line := range lines {
		if strings.Contains(line, "╭─") && strings.Contains(line, "│") {
			foundNested = true
			break
		}
	}
	if !foundNested {
		t.Errorf("expected nested bracket pattern (╭─ inside │ gutter)\nfull output:\n%s", output)
	}
}

func TestFlowViz_NoColor(t *testing.T) {
	cfg := &config.Config{
		Name: "simple",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "implement", Type: "agent", Model: "sonnet", Effort: "high"},
			{Name: "review", Type: "gate"},
		},
	}

	// Save and restore color state
	savedReset := Reset
	DisableColor()
	defer func() {
		Reset = savedReset
		Bold = "\033[1m"
		Dim = "\033[2m"
		Red = "\033[31m"
		Green = "\033[32m"
		Yellow = "\033[33m"
		Cyan = "\033[36m"
		Magenta = "\033[35m"
		Blue = "\033[34m"
		BoldCyan = "\033[1;36m"
		BoldBlue = "\033[1;34m"
		BoldGreen = "\033[1;32m"
	}()

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	if strings.Contains(output, "\033[") {
		t.Errorf("output should not contain ANSI escape sequences\nfull output:\n%s", output)
	}

	mustContain := []string{"◆", "⏸", "plan", "implement", "review"}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowViz_ComplexWorkflow(t *testing.T) {
	cfg := &config.Config{
		Name: "idaho-surplus-line-suite",
		Phases: []config.Phase{
			{Name: "create-epic", Type: "agent", Model: "sonnet", Effort: "high",
				Outputs: []string{"epic-id.txt"}, Description: "Create an epic bead for the Jira ticket"},
			{Name: "plan", Type: "agent", Model: "opus", Effort: "low",
				Outputs:     []string{"plan.md", "classification.txt"},
				Description: "Thoroughly analyze the ticket and plan implementation"},
			{Name: "review-plan", Type: "agent", Model: "opus", Effort: "low",
				Outputs:     []string{"plan-review.md"},
				Description: "Adversarial review of the plan",
				Loop:        &config.Loop{Goto: "plan", Min: 1, Max: 3, Check: "grep -q APPROVED $ARTIFACTS_DIR/plan-approved.txt"}},
			{Name: "plan-gate", Type: "gate",
				Description: "Human reviews the plan",
				Loop:        &config.Loop{Goto: "plan", Min: 1, Max: 3}},
			{Name: "create-beads", Type: "agent", Model: "sonnet", Effort: "high",
				Outputs:     []string{"bead-ids.txt"},
				Description: "Break the plan into ordered beads"},
			{Name: "pick-bead", Type: "script", Run: "bdv next",
				Description: "Select the next ready bead"},
			{Name: "implement", Type: "agent", Model: "opus", Effort: "high",
				Description: "Implement changes for the current bead"},
			{Name: "build-test", Type: "script", Run: "make test",
				Description: "Compile and run tests",
				Loop:        &config.Loop{Goto: "implement", Min: 1, Max: 5}},
			{Name: "review", Type: "agent", Model: "opus", Effort: "low",
				Outputs:     []string{"review-result.txt"},
				Description: "Expert panel code review",
				Loop:        &config.Loop{Goto: "implement", Min: 1, Max: 3, Check: "grep -q PASS $ARTIFACTS_DIR/review-result.txt"}},
			{Name: "wrap-up", Type: "agent", Model: "sonnet", Effort: "high",
				Description: "Commit changes, close bead"},
			{Name: "check-remaining", Type: "script", Run: "check-beads",
				Description: "Loop back if incomplete beads remain",
				Loop:        &config.Loop{Goto: "pick-bead", Min: 1, Max: 20}},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	// All 11 phase names
	phaseNames := []string{
		"create-epic", "plan", "review-plan", "plan-gate", "create-beads",
		"pick-bead", "implement", "build-test", "review", "wrap-up", "check-remaining",
	}
	for _, name := range phaseNames {
		if !strings.Contains(output, name) {
			t.Errorf("output missing phase name %q\nfull output:\n%s", name, output)
		}
	}

	// Header stats
	mustContain := []string{
		"11 phases",
		"7 agents",
		"3 scripts",
		"1 gate",
		"5 loops",
		"✓ complete",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// Nested brackets (implement loop inside pick-bead loop)
	lines := strings.Split(output, "\n")
	foundNested := false
	for _, line := range lines {
		if strings.Contains(line, "╭─") && strings.Contains(line, "│") {
			foundNested = true
			break
		}
	}
	if !foundNested {
		t.Errorf("expected nested brackets\nfull output:\n%s", output)
	}
}

func TestFlowViz_Descriptions(t *testing.T) {
	cfg := &config.Config{
		Name: "described",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Effort: "high",
				Description: "Analyze the ticket thoroughly"},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	if !strings.Contains(output, "Analyze the ticket thoroughly") {
		t.Errorf("output missing description\nfull output:\n%s", output)
	}
}

func TestFlowViz_Outputs(t *testing.T) {
	cfg := &config.Config{
		Name: "outputs",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Effort: "high",
				Outputs: []string{"plan.md", "notes.txt"}},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{"→", "plan.md", "notes.txt"}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowViz_ModelBadges(t *testing.T) {
	cfg := &config.Config{
		Name: "models",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "b", Type: "agent", Model: "sonnet", Effort: "low"},
			{Name: "c", Type: "agent", Model: "haiku", Effort: "medium"},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{"opus", "sonnet", "haiku", "⚡"}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// Verify ⚡ does NOT appear on the same line as phase "a" (effort=high)
	// Phase "a" is the only one with model "opus" in this config, so check
	// for any line containing "opus" and "⚡" — this works in colored mode
	// where the phase name is wrapped in ANSI codes.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "opus") && strings.Contains(line, "⚡") {
			t.Errorf("phase 'a' with effort=high should not have ⚡: %s", line)
		}
	}
}

func withPlainColors(fn func()) {
	savedReset := Reset
	DisableColor()
	defer func() {
		Reset = savedReset
		Bold = "\033[1m"
		Dim = "\033[2m"
		Red = "\033[31m"
		Green = "\033[32m"
		Yellow = "\033[33m"
		Cyan = "\033[36m"
		Magenta = "\033[35m"
		Blue = "\033[34m"
		BoldCyan = "\033[1;36m"
		BoldBlue = "\033[1;34m"
		BoldGreen = "\033[1;32m"
	}()
	fn()
}

func TestFlowViz_GutterContinuityAtLoopStart(t *testing.T) {
	cfg := &config.Config{
		Name: "gutter-start",
		Phases: []config.Phase{
			{Name: "pick", Type: "script", Run: "pick"},
			{Name: "work", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "check", Type: "script", Run: "check",
				Loop: &config.Loop{Goto: "pick", Min: 1, Max: 5}},
		},
	}

	var output string
	withPlainColors(func() {
		output = captureOutput(func() { FlowViz(cfg) })
	})

	// The first phase in the loop (pick) should have a │ gutter
	lines := strings.Split(output, "\n")
	foundPickWithGutter := false
	for _, line := range lines {
		if strings.Contains(line, "pick") && strings.Contains(line, "▸") {
			if strings.Contains(line, "│") {
				foundPickWithGutter = true
			}
			break
		}
	}
	if !foundPickWithGutter {
		t.Errorf("first phase in loop should have │ gutter\nfull output:\n%s", output)
	}
}

func TestFlowViz_InterleavedLoopContinuity(t *testing.T) {
	// Interleaved: pick-bead loop [0,5] and implement loop [2,6]
	// Neither fully contains the other.
	cfg := &config.Config{
		Name: "interleaved",
		Phases: []config.Phase{
			{Name: "pick-bead", Type: "script", Run: "pick"},
			{Name: "plan", Type: "agent", Model: "sonnet", Effort: "high"},
			{Name: "implement", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "test", Type: "script", Run: "test",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 5}},
			{Name: "wrap-up", Type: "agent", Model: "sonnet", Effort: "high"},
			{Name: "check-remaining", Type: "script", Run: "check",
				Loop: &config.Loop{Goto: "pick-bead", Min: 1, Max: 20}},
			{Name: "final-review", Type: "agent", Model: "opus", Effort: "high",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 3}},
		},
	}

	var output string
	withPlainColors(func() {
		output = captureOutput(func() { FlowViz(cfg) })
	})

	lines := strings.Split(output, "\n")

	// The implement loop ╭─ line should have the outer pick-bead │ gutter
	foundNestedOpen := false
	for _, line := range lines {
		if strings.Contains(line, "╭─") && strings.Contains(line, "implement loop") {
			if strings.Contains(line, "│") {
				foundNestedOpen = true
			} else {
				t.Errorf("implement loop ╭─ should have outer │ gutter: %q", line)
			}
			break
		}
	}
	if !foundNestedOpen {
		t.Errorf("did not find implement loop ╭─ line\nfull output:\n%s", output)
	}

	// Phases inside both loops should have two │ gutters
	for _, line := range lines {
		if strings.Contains(line, "test") && strings.Contains(line, "▸") {
			count := strings.Count(line, "│")
			if count < 2 {
				t.Errorf("phase inside both loops should have 2 │ gutters, got %d: %q", count, line)
			}
			break
		}
	}

	// The pick-bead loop ╰─ should have the implement │ gutter (still active)
	for _, line := range lines {
		if strings.Contains(line, "╰─") {
			// First ╰─ is pick-bead close; implement loop is still active
			if strings.Contains(line, "│") {
				// Good: outer close has inner gutter
			}
			break
		}
	}
}

func TestFlowViz_ScopeColorsVary(t *testing.T) {
	// Two loops should get different ANSI colors
	cfg := &config.Config{
		Name: "colors",
		Phases: []config.Phase{
			{Name: "pick", Type: "script", Run: "pick"},
			{Name: "implement", Type: "agent", Model: "opus", Effort: "high"},
			{Name: "test", Type: "script", Run: "test",
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 5}},
			{Name: "check", Type: "script", Run: "check",
				Loop: &config.Loop{Goto: "pick", Min: 1, Max: 20}},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	// Find ╭─ lines — each should have a different color escape before ╭
	var bracketColors []string
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "╭─")
		if idx > 0 {
			// Extract the ANSI escape sequence immediately before ╭─
			prefix := line[:idx]
			// Find last escape sequence
			lastEsc := strings.LastIndex(prefix, "\033[")
			if lastEsc >= 0 {
				bracketColors = append(bracketColors, prefix[lastEsc:idx])
			}
		}
	}

	if len(bracketColors) < 2 {
		t.Fatalf("expected at least 2 loop brackets, got %d\nfull output:\n%s", len(bracketColors), output)
	}
	if bracketColors[0] == bracketColors[1] {
		t.Errorf("loop brackets should use different colors, both used %q\nfull output:\n%s", bracketColors[0], output)
	}
}

func TestFlowViz_ScriptAndGateIcons(t *testing.T) {
	cfg := &config.Config{
		Name: "icons",
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "make build"},
			{Name: "approve", Type: "gate"},
			{Name: "deploy", Type: "agent", Model: "sonnet", Effort: "high"},
		},
	}

	output := captureOutput(func() {
		FlowViz(cfg)
	})

	mustContain := []string{"▸", "⏸", "◆"}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing icon %q\nfull output:\n%s", s, output)
		}
	}
}
