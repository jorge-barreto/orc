package ux

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestFlowDiagram_Simple(t *testing.T) {
	cfg := &config.Config{
		Name: "simple",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "implement", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "review", Type: "gate"},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	mustContain := []string{
		"Workflow: simple (3 phases)",
		"1. plan [agent/opus]",
		"2. implement [agent/opus]",
		"3. review [gate]",
		"✓ complete",
		"│",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	mustNotContain := []string{"┌─▶", "loop"}
	for _, s := range mustNotContain {
		if strings.Contains(output, s) {
			t.Errorf("output should not contain %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowDiagram_WithLoop(t *testing.T) {
	cfg := &config.Config{
		Name: "looped",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "implement", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "review", Type: "agent", Model: "opus", Timeout: 30,
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 3}},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	mustContain := []string{
		"┌─▶",
		"loop ──┘ (max 3)",
		"│",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	if strings.Contains(output, "min") {
		t.Errorf("output should not contain 'min' when min=1\nfull output:\n%s", output)
	}
}

func TestFlowDiagram_WithLoopMinCheckAndOnExhaust(t *testing.T) {
	cfg := &config.Config{
		Name: "full",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "implement", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "review", Type: "agent", Model: "opus", Timeout: 30,
				Loop: &config.Loop{
					Goto: "implement", Min: 3, Max: 5,
					Check:     "make test",
					OnExhaust: &config.OnExhaust{Goto: "plan", Max: 2},
				}},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	mustContain := []string{
		"loop ──┘ (min 3, max 5)",
		"on-exhaust → plan (max 2)",
		"check: make test",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}
}

func TestFlowDiagram_WithOutputsConditionRunAndTimeout(t *testing.T) {
	cfg := &config.Config{
		Name: "detailed",
		Phases: []config.Phase{
			{Name: "setup", Type: "script", Run: "echo setup", Timeout: 10},
			{Name: "plan", Type: "agent", Model: "opus", Timeout: 45,
				Outputs: []string{"plan.md"},
				MaxCost: 2.50},
			{Name: "test", Type: "script", Run: "make test", Timeout: 10,
				Condition: "test -f Makefile"},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	mustContain := []string{
		"outputs: plan.md",
		"condition: test -f Makefile",
		"run: echo setup",
		"run: make test",
		"timeout: 45m",
		"max-cost: $2.50",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	if strings.Contains(output, "timeout: 30m") {
		t.Errorf("output should not contain default timeout 30m\nfull output:\n%s", output)
	}
}

func TestFlowDiagram_WithParallelWith(t *testing.T) {
	cfg := &config.Config{
		Name: "parallel",
		Phases: []config.Phase{
			{Name: "test", Type: "script", Run: "make test", Timeout: 10},
			{Name: "lint", Type: "script", Run: "make lint", Timeout: 10,
				ParallelWith: "test"},
			{Name: "review", Type: "gate"},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	if !strings.Contains(output, "parallel-with: test") {
		t.Errorf("output missing 'parallel-with: test'\nfull output:\n%s", output)
	}
}

func TestFlowDiagram_Complex9Phase(t *testing.T) {
	cfg := &config.Config{
		Name: "prepdesk",
		Phases: []config.Phase{
			{Name: "setup", Type: "script", Run: "echo init", Timeout: 5},
			{Name: "plan", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "approve", Type: "gate"},
			{Name: "implement", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "quality", Type: "script", Run: "make test", Timeout: 10,
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 3}},
			{Name: "push-pr", Type: "script", Run: "git push", Timeout: 5,
				Outputs: []string{"pr.txt"}},
			{Name: "ci-check", Type: "script", Run: "make ci", Timeout: 15,
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 2}},
			{Name: "self-review", Type: "agent", Model: "opus", Timeout: 30},
			{Name: "review-gate", Type: "script", Run: "check-review", Timeout: 10,
				Loop: &config.Loop{Goto: "implement", Min: 1, Max: 2}},
		},
	}

	output := captureOutput(func() {
		FlowDiagram(cfg, nil, nil)
	})

	mustContain := []string{
		"Workflow: prepdesk (9 phases)",
		"┌─▶",
		"1. setup [script]",
		"2. plan [agent/opus]",
		"3. approve [gate]",
		"4. implement [agent/opus]",
		"outputs: pr.txt",
		"✓ complete",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// Count loop ──┘ occurrences — should be exactly 3
	count := strings.Count(output, "loop ──┘")
	if count != 3 {
		t.Errorf("expected 3 'loop ──┘' occurrences, got %d\nfull output:\n%s", count, output)
	}

	// Phases inside the loop scope (implement through review-gate) should have │ prefix
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "4. implement") {
			if !strings.Contains(line, "┌─▶") {
				t.Errorf("implement line should have ┌─▶ prefix: %s", line)
			}
		}
		// setup line should NOT have │ prefix
		if strings.Contains(line, "1. setup") {
			if strings.Contains(line, "│") {
				t.Errorf("setup line should not have │ prefix: %s", line)
			}
		}
	}
}

func TestFlowDiagram_VarsAndMaxCost(t *testing.T) {
	cfg := &config.Config{
		Name:    "budgeted",
		MaxCost: 5.00,
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Timeout: 10},
		},
	}
	vars := map[string]string{"WORKTREE": "/tmp/wt", "SRC": "/tmp/wt/src"}

	output := captureOutput(func() {
		FlowDiagram(cfg, vars, nil)
	})

	mustContain := []string{
		"max-cost: $5.00",
		"Vars:",
	}
	for _, s := range mustContain {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, output)
		}
	}

	// SRC should appear before WORKTREE (alphabetical order)
	srcIdx := strings.Index(output, "SRC")
	wtIdx := strings.Index(output, "WORKTREE")
	if srcIdx < 0 || wtIdx < 0 {
		t.Fatalf("expected both SRC and WORKTREE in output:\n%s", output)
	}
	if srcIdx >= wtIdx {
		t.Errorf("SRC should appear before WORKTREE (alphabetical)\nfull output:\n%s", output)
	}
}
