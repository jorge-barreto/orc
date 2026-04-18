//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOutputs_HappyPath verifies that declared outputs are satisfied by
// the agent on first try — result.txt appears in artifacts, run
// completes. This overlaps with gge.2's basic test but specifically
// exercises the `outputs:` contract.
func TestOutputs_HappyPath(t *testing.T) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("CLAUDE_CODE_OAUTH_TOKEN not set; skipping agent-phase test")
	}

	cfg := `name: outputs-happy
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/outputs-happy.md
    model: haiku
    outputs: [result.txt]

  - name: verify
    type: script
    run: cat $ARTIFACTS_DIR/result.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "OUTPUTS-OK-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "OUTPUTS-OK-001", 1)

	promptBytes, _ := os.ReadFile("prompts/outputs-happy.md")
	w.WritePrompt("prompts/outputs-happy.md", string(promptBytes))

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("status = %v, want completed", got)
	}

	// dispatch-counts.json should show 1 attempt for the agent phase (index 0).
	auditPath := filepath.Join(w.Dir, ".orc", "audit", w.Ticket, "dispatch-counts.json")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read dispatch-counts: %v", err)
	}
	var counts map[string]int
	if err := json.Unmarshal(data, &counts); err != nil {
		t.Fatalf("parse dispatch-counts: %v", err)
	}
	// Agent is phase index 0. One attempt expected.
	if c := counts["0"]; c != 1 {
		t.Errorf("dispatch-counts[0] = %d, want 1 (no re-prompt on happy path)", c)
	}
}
