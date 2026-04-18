//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestCost_MidStreamKill verifies that when an agent phase's cost
// exceeds its configured max-cost, orc kills the running claude session
// BEFORE the session completes normally — not after.
//
// Design:
//   - Prompt asks for a large response ("write 3000 words ...") so the
//     session runs for many seconds if left alone.
//   - max-cost is set to a small fraction of the baseline session cost
//     (~$0.014 for input + cache). Because the baseline is already
//     above the cap once message_start arrives, orc must kill within
//     a handful of seconds — NOT wait for the full response.
//   - Assertions: non-zero exit, failure_category=cost_overrun, AND
//     phase duration is substantially less than a full haiku response
//     would take (bounded well below 15s — a 3000-word haiku response
//     is typically 30-60s).
func TestCost_MidStreamKill(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: cost-kill
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/cost-long.md
    model: haiku
    max-cost: 0.001
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "COST-KILL-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "COST-KILL-001", 1)

	promptBytes, _ := os.ReadFile("prompts/cost-long.md")
	w.WritePrompt("prompts/cost-long.md", string(promptBytes))

	start := time.Now()
	res := w.RunOrc("run", w.Ticket, "--auto")
	elapsed := time.Since(start)

	if res.ExitCode == 0 {
		t.Fatalf("exit = 0, want non-zero\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}

	state := w.ReadState()
	if got := state["failure_category"]; got != "cost_overrun" {
		t.Errorf("state.failure_category = %v, want cost_overrun", got)
	}

	// CORE ASSERTION: orc must kill the session essentially at
	// message_start time, not wait for the response to stream. A full
	// haiku response to this prompt takes 60-90s unbounded. 10s is a
	// ceiling that tolerates orc startup + first-token latency (~3-4s)
	// while catching regression to "wait for full response."
	if elapsed > 10*time.Second {
		t.Errorf("phase duration = %v, want < 10s — orc should kill at message_start, not wait for response",
			elapsed)
	}

	// Error message should come from the in-flight kill path ("killed
	// mid-stream"), not the post-hoc path ("cost $X exceeded limit $Y").
	// This distinguishes "kill worked" from "fell back to post-hoc check".
	if !strings.Contains(res.Stderr, "killed mid-stream") {
		t.Errorf("stderr missing \"killed mid-stream\" — in-flight kill didn't fire; got: %s",
			res.Stderr)
	}

	// Cost recorded should be low — either 0 (result event never arrived)
	// or only the baseline cache/input charges. An unbounded run costs
	// ~$0.05-0.08 for this prompt; we want to be well under that.
	rr := w.ReadRunResult()
	phases, _ := rr["phases"].([]any)
	if len(phases) < 1 {
		t.Fatalf("no phase entry in run-result")
	}
	agent, _ := phases[0].(map[string]any)
	cost, _ := agent["cost_usd"].(float64)
	if cost > 0.020 {
		t.Errorf("phase cost = $%.4f, want < $0.020 — mid-stream kill should cap cost near baseline",
			cost)
	}
}
