//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestParallel_WithCompletes verifies that two phases running via
// parallel-with execute concurrently (total wall time less than sum of
// their individual durations) and both produce their artifacts.
func TestParallel_WithCompletes(t *testing.T) {
	cfg := `name: parallel
phases:
  - name: setup
    type: script
    run: echo ready

  - name: fast
    type: script
    parallel-with: slow
    run: sleep 1 && echo fast > $ARTIFACTS_DIR/fast.txt

  - name: slow
    type: script
    run: sleep 2 && echo slow > $ARTIFACTS_DIR/slow.txt

  - name: verify
    type: script
    run: |
      set -e
      test -f $ARTIFACTS_DIR/fast.txt
      test -f $ARTIFACTS_DIR/slow.txt
      grep -qx "fast" $ARTIFACTS_DIR/fast.txt
      grep -qx "slow" $ARTIFACTS_DIR/slow.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "PAR-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "PAR-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	timing := w.ReadTiming()
	entries, _ := timing["entries"].([]any)

	// Inspect the overlap from timing.json rather than wall-clock. Overall
	// wall-clock is inflated by orc startup + bookkeeping per phase; the
	// per-phase start/end timestamps are the authoritative signal.
	var fastStart, fastEnd, slowStart, slowEnd time.Time
	for _, e := range entries {
		m, _ := e.(map[string]any)
		name := m["phase"]
		s, _ := time.Parse(time.RFC3339Nano, m["start"].(string))
		ee, _ := time.Parse(time.RFC3339Nano, m["end"].(string))
		switch name {
		case "fast":
			fastStart, fastEnd = s, ee
		case "slow":
			slowStart, slowEnd = s, ee
		}
	}
	if fastStart.IsZero() || slowStart.IsZero() {
		t.Fatalf("missing fast/slow timing entries: %v", entries)
	}
	// Overlap: either interval contains the other's start, or intervals
	// intersect in any way.
	overlaps := fastStart.Before(slowEnd) && slowStart.Before(fastEnd)
	if !overlaps {
		t.Errorf("fast [%v..%v] and slow [%v..%v] did not overlap",
			fastStart, fastEnd, slowStart, slowEnd)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status = %v, want \"completed\"", got)
	}

	// All 4 phases should be accounted for. Parallel partners are usually
	// both marked completed; the intermediate index between them (none here
	// since fast is at index 1 and slow at index 2, adjacent) does not get
	// a synthetic "skipped" slot.
	phases, _ := rr["phases"].([]any)
	if len(phases) != 4 {
		t.Fatalf("phases len = %d, want 4", len(phases))
	}
	for i, p := range phases {
		m, _ := p.(map[string]any)
		if got := m["status"]; got != "completed" {
			t.Errorf("phase[%d] (%v).status = %v, want \"completed\"",
				i, m["name"], got)
		}
	}

}

// TestParallel_WithFailureCancels verifies that when one phase in a
// parallel pair fails, the run fails non-zero. (We don't assert that
// the sibling was hard-killed mid-sleep — that's an implementation
// detail — just that the overall run reflects failure.)
func TestParallel_WithFailureCancels(t *testing.T) {
	cfg := `name: parallel-fail
phases:
  - name: doomed
    type: script
    parallel-with: sibling
    run: exit 1

  - name: sibling
    type: script
    run: sleep 3 && echo sibling-ran > $ARTIFACTS_DIR/sibling.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "PAR-FAIL-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "PAR-FAIL-001", 1)

	start := time.Now()
	res := w.RunOrc("run", w.Ticket, "--auto")
	elapsed := time.Since(start)

	if res.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero (one partner failed)")
	}

	// If the sibling had run to completion, this would be >= 3s.
	// Cancellation should bring it in well under that.
	if elapsed >= 3*time.Second {
		t.Errorf("wall time = %v, want < 3s (sibling should be cancelled when partner fails)", elapsed)
	}
}
