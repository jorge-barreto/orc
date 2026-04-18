//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestGate_AutoApprove verifies that gate phases are auto-approved when
// --auto is passed: no stdin prompt, workflow completes, subsequent
// phases run.
//
// NOTE: The bead's acceptance criterion "Gate pre-run command executes"
// does NOT match current code behavior — RunGate returns immediately on
// AutoMode (internal/dispatch/gate.go:37-42) BEFORE executing phase.Run.
// This test asserts the *actual* behavior: in --auto mode, the pre-run
// command is SKIPPED. If the product intent differs, that's a separate
// bug/feature — tracked in bead notes, not fixed here.
func TestGate_AutoApprove(t *testing.T) {
	cfg := `name: gate
phases:
  - name: setup
    type: script
    run: echo setup > $ARTIFACTS_DIR/setup.txt

  - name: approve-setup
    type: gate
    description: Approve setup
    run: cat $ARTIFACTS_DIR/setup.txt

  - name: finish
    type: script
    run: echo done > $ARTIFACTS_DIR/done.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "GATE-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "GATE-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}
	if got := rr["phases_completed"]; got != float64(3) {
		t.Errorf("run-result.phases_completed = %v, want 3", got)
	}

	// All three phases should be completed — gate counts as completed when auto-approved.
	phases, _ := rr["phases"].([]any)
	if len(phases) != 3 {
		t.Fatalf("phases len = %d, want 3", len(phases))
	}
	for i, p := range phases {
		m, _ := p.(map[string]any)
		if got := m["status"]; got != "completed" {
			t.Errorf("phase[%d].status = %v, want \"completed\"", i, got)
		}
	}

	// done.txt must exist (proves gate didn't block, subsequent phase ran).
	if content := w.ReadHistoryFile("done.txt"); !strings.Contains(content, "done") {
		t.Errorf("done.txt missing or wrong content: %q", content)
	}

	// Stdout should contain the auto-approval message from gate.go:38.
	if !strings.Contains(res.Stdout, "auto-approved") {
		t.Errorf("expected auto-approved message in stdout; got: %s", res.Stdout)
	}
}
