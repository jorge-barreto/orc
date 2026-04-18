//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
)

// TestCost_TrackingRecordsRealData verifies that after a trivial agent
// call, costs.json has a plausible entry with non-zero token counts
// and a non-zero cost_usd.
func TestCost_TrackingRecordsRealData(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: cost-track
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/cost-trivial.md
    model: haiku

  - name: done
    type: script
    run: echo done
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "COST-A-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "COST-A-001", 1)

	promptBytes, _ := os.ReadFile("prompts/cost-trivial.md")
	w.WritePrompt("prompts/cost-trivial.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	costs := w.ReadCosts()
	phases, _ := costs["phases"].([]any)
	var agentEntry map[string]any
	for _, p := range phases {
		m, _ := p.(map[string]any)
		if m["name"] == "agent" {
			agentEntry = m
		}
	}
	if agentEntry == nil {
		t.Fatalf("costs.phases has no \"agent\" entry: %v", phases)
	}
	if c, _ := agentEntry["cost_usd"].(float64); c <= 0 {
		t.Errorf("agent.cost_usd = %v, want > 0", agentEntry["cost_usd"])
	}
	if it, _ := agentEntry["input_tokens"].(float64); it <= 0 {
		t.Errorf("agent.input_tokens = %v, want > 0", agentEntry["input_tokens"])
	}
	if ot, _ := agentEntry["output_tokens"].(float64); ot <= 0 {
		t.Errorf("agent.output_tokens = %v, want > 0", agentEntry["output_tokens"])
	}
	if turns, _ := agentEntry["turns"].(float64); turns <= 0 {
		t.Errorf("agent.turns = %v, want > 0", agentEntry["turns"])
	}

	// Totals should be at least the single phase's cost.
	total, _ := costs["total_cost_usd"].(float64)
	phaseCost, _ := agentEntry["cost_usd"].(float64)
	if total < phaseCost {
		t.Errorf("total_cost_usd %v < phase cost %v", total, phaseCost)
	}
}

// TestCost_RunLevelMaxCostEnforced verifies that exceeding the run-level
// max-cost causes the run to fail with failure_category=cost_overrun.
//
// Strategy: use a very low max-cost (like $0.0001) so even a trivial
// haiku call exceeds it after phase 1. Avoids needing to generate long
// essays — much cheaper and more reliable.
func TestCost_RunLevelMaxCostEnforced(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: cost-run-limit
max-cost: 0.0001
phases:
  - name: agent1
    type: agent
    prompt: .orc/prompts/cost-trivial.md
    model: haiku

  - name: agent2
    type: agent
    prompt: .orc/prompts/cost-trivial.md
    model: haiku
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "COST-B-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "COST-B-001", 1)

	promptBytes, _ := os.ReadFile("prompts/cost-trivial.md")
	w.WritePrompt("prompts/cost-trivial.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Errorf("exit = 0, want non-zero (run-level max-cost should have fired)\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}

	state := w.ReadState()
	if got := state["failure_category"]; got != "cost_overrun" {
		t.Errorf("state.failure_category = %v, want cost_overrun", got)
	}
}

// TestCost_PhaseLevelMaxCostEnforced verifies that a single phase
// exceeding its own max-cost causes the run to fail with
// failure_category=cost_overrun.
//
// Strategy: one agent phase with max-cost: 0.0001 — a trivial haiku
// call costs more than that, so the check fires after phase 1 completes.
func TestCost_PhaseLevelMaxCostEnforced(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: cost-phase-limit
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/cost-trivial.md
    model: haiku
    max-cost: 0.0001
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "COST-C-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "COST-C-001", 1)

	promptBytes, _ := os.ReadFile("prompts/cost-trivial.md")
	w.WritePrompt("prompts/cost-trivial.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Errorf("exit = 0, want non-zero (phase-level max-cost should have fired)\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}

	state := w.ReadState()
	if got := state["failure_category"]; got != "cost_overrun" {
		t.Errorf("state.failure_category = %v, want cost_overrun", got)
	}
}
