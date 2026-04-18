//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConditions_PhaseSkipping verifies that phases with a `condition:`
// field are skipped when the condition exits non-zero and run when it
// exits zero. Script-only — no token needed.
func TestConditions_PhaseSkipping(t *testing.T) {
	cfg := `name: conditions
phases:
  - name: setup
    type: script
    run: echo setup

  - name: should-skip
    type: script
    condition: "false"
    run: echo should-be-skipped > $ARTIFACTS_DIR/skipped.txt

  - name: should-run
    type: script
    condition: "true"
    run: echo should-run > $ARTIFACTS_DIR/ran.txt

  - name: verify
    type: script
    run: |
      set -e
      test ! -f $ARTIFACTS_DIR/skipped.txt
      test -f $ARTIFACTS_DIR/ran.txt
      grep -qx "should-run" $ARTIFACTS_DIR/ran.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "COND-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "COND-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}

	phases, ok := rr["phases"].([]any)
	if !ok || len(phases) != 4 {
		t.Fatalf("run-result.phases not 4-element array: %v", rr["phases"])
	}
	wantStatus := []string{"completed", "skipped", "completed", "completed"}
	wantNames := []string{"setup", "should-skip", "should-run", "verify"}
	for i, p := range phases {
		m, _ := p.(map[string]any)
		if got := m["name"]; got != wantNames[i] {
			t.Errorf("phase[%d].name = %v, want %q", i, got, wantNames[i])
		}
		if got := m["status"]; got != wantStatus[i] {
			t.Errorf("phase[%d].status = %v, want %q", i, got, wantStatus[i])
		}
	}

	// Belt-and-suspenders: the verify phase already checks filesystem, but
	// assert directly so a failure here points at the right culprit.
	hist := w.HistoryDir()
	if _, err := os.Stat(filepath.Join(hist, "skipped.txt")); !os.IsNotExist(err) {
		t.Errorf("skipped.txt should NOT exist in artifacts; stat err = %v", err)
	}
	// ran.txt lives under the ticket artifacts dir; may be archived.
	if w.ReadHistoryFile("ran.txt") == "" {
		t.Errorf("ran.txt should be non-empty")
	}

	// UX should announce the skip somewhere in stdout/stderr.
	combined := res.Stdout + res.Stderr
	if !strings.Contains(strings.ToLower(combined), "skip") {
		t.Errorf("expected skip indication in output; got stdout=%q stderr=%q",
			res.Stdout, res.Stderr)
	}
}
