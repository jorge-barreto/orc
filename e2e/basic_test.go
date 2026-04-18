//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBasic_ScriptAgentScript runs a 3-phase workflow: seed (script) →
// agent (haiku) → verify (script). Proves agent dispatch, artifact output,
// and cost recording work end-to-end.
//
// Gated on CLAUDE_CODE_OAUTH_TOKEN. Skipped when unset so `make e2e` on a
// fresh clone stays green; runs in Docker where .env is forwarded.
func TestBasic_ScriptAgentScript(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: basic
phases:
  - name: seed
    type: script
    run: echo "seeded at $(date -Iseconds)" > $ARTIFACTS_DIR/seed.txt

  - name: agent
    type: agent
    prompt: .orc/prompts/basic-agent.md
    model: haiku
    outputs: [result.txt]

  - name: verify
    type: script
    run: grep -q "^hello$" $ARTIFACTS_DIR/result.txt
`
	w := NewWorkspace(t, cfg)

	promptBytes, err := os.ReadFile("prompts/basic-agent.md")
	if err != nil {
		t.Fatalf("read prompt source: %v", err)
	}
	w.WritePrompt("prompts/basic-agent.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}
	if got := rr["exit_code"]; got != float64(0) {
		t.Errorf("run-result.exit_code = %v, want 0", got)
	}
	if got := rr["phases_completed"]; got != float64(3) {
		t.Errorf("run-result.phases_completed = %v, want 3", got)
	}
	if cost, _ := rr["total_cost_usd"].(float64); cost <= 0 {
		t.Errorf("run-result.total_cost_usd = %v, want > 0", rr["total_cost_usd"])
	}

	phases, ok := rr["phases"].([]any)
	if !ok || len(phases) != 3 {
		t.Fatalf("run-result.phases is not a 3-element array: %v", rr["phases"])
	}
	agentPhase, _ := phases[1].(map[string]any)
	if got := agentPhase["name"]; got != "agent" {
		t.Errorf("phase[1].name = %v, want \"agent\"", got)
	}
	if got := agentPhase["status"]; got != "completed" {
		t.Errorf("phase[1].status = %v, want \"completed\"", got)
	}
	if cost, _ := agentPhase["cost_usd"].(float64); cost <= 0 {
		t.Errorf("phase[1].cost_usd = %v, want > 0", agentPhase["cost_usd"])
	}

	costs := w.ReadCosts()
	costPhases, ok := costs["phases"].([]any)
	if !ok {
		t.Fatalf("costs.phases missing or wrong type: %v", costs["phases"])
	}
	var foundAgentCost bool
	for _, p := range costPhases {
		m, _ := p.(map[string]any)
		if m["name"] == "agent" {
			if c, _ := m["cost_usd"].(float64); c > 0 {
				foundAgentCost = true
			}
		}
	}
	if !foundAgentCost {
		t.Errorf("costs.json has no agent phase with cost_usd > 0; got %v", costPhases)
	}

	timing := w.ReadTiming()
	entries, _ := timing["entries"].([]any)
	if len(entries) != 3 {
		t.Errorf("timing.entries has %d entries, want 3", len(entries))
	}

	hist := w.HistoryDir()
	mustExist := []string{
		"logs/phase-1.log",
		"logs/phase-2.log",
		"logs/phase-3.log",
		"logs/phase-2.meta.json",
		"prompts/phase-2.md",
	}
	for _, rel := range mustExist {
		full := filepath.Join(hist, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %s to exist: %v", full, err)
		}
	}
}
