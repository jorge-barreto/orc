//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestTimeout_PhaseKilled verifies that a script phase running beyond
// its timeout is killed and the run fails. Subsequent phases don't run.
//
// SLOW: the minimum timeout granularity is 1 minute (config: `timeout`
// is an int of minutes, internal/config/config.go:53), so this test
// takes ~60-70s. Gated behind -short so `go test -short` skips it.
func TestTimeout_PhaseKilled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1-minute timeout test in -short mode")
	}

	cfg := `name: timeout
phases:
  - name: sleeper
    type: script
    timeout: 1
    run: sleep 120

  - name: later
    type: script
    run: echo should-not-run > $ARTIFACTS_DIR/later.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "TIMEOUT-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "TIMEOUT-001", 1)

	start := time.Now()
	res := w.RunOrc("run", w.Ticket, "--auto")
	elapsed := time.Since(start)

	if res.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero (timeout should fail the run)")
	}

	// Must finish in ~1 minute + small grace for cleanup; NOT 2 minutes
	// (that would mean sleep 120 ran to completion).
	if elapsed > 90*time.Second {
		t.Errorf("wall time = %v, want < 90s (timeout should kill sleep 120 after 60s)", elapsed)
	}
	if elapsed < 50*time.Second {
		t.Errorf("wall time = %v, want >= ~55s (timeout should not fire before its window)", elapsed)
	}

	// State should record timeout as the failure category.
	state := w.ReadState()
	if got := state["failure_category"]; got != "timeout" {
		t.Errorf("state.failure_category = %v, want \"timeout\"", got)
	}

	// Second phase should not appear as completed. Find it in run-result.
	rr := w.ReadRunResult()
	phases, _ := rr["phases"].([]any)
	for _, p := range phases {
		m, _ := p.(map[string]any)
		if m["name"] == "later" {
			if s := m["status"]; s == "completed" {
				t.Errorf("phase \"later\" status = completed; should not have run")
			}
		}
	}
}
