//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLoops_ConvergesOnCheck exercises a loop that increments a counter
// and terminates once the check condition passes.
func TestLoops_ConvergesOnCheck(t *testing.T) {
	cfg := `name: loop-converge
phases:
  - name: init
    type: script
    run: |
      test -f $ARTIFACTS_DIR/counter.txt || echo 0 > $ARTIFACTS_DIR/counter.txt

  - name: work
    type: script
    run: |
      n=$(cat $ARTIFACTS_DIR/counter.txt)
      echo $((n+1)) > $ARTIFACTS_DIR/counter.txt
    loop:
      goto: init
      min: 1
      max: 5
      check: test $(cat $ARTIFACTS_DIR/counter.txt) -ge 3

  - name: done
    type: script
    run: echo complete > $ARTIFACTS_DIR/done.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "LOOP-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "LOOP-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// counter should be 3 (or more, up to max=5; with check `>= 3`,
	// loop exits as soon as work completes with counter 3).
	// NOTE: counter.txt is rewritten by `init` each iteration, then `work`
	// increments. After the loop finishes, the final value is whatever
	// `work` last wrote — which is 3 if it exited on first check pass.
	counter := strings.TrimSpace(w.ReadHistoryFile("counter.txt"))
	if counter != "3" {
		t.Errorf("counter.txt = %q, want \"3\"", counter)
	}

	// loop-counts.json records per-phase iteration counts.
	data := w.ReadHistoryFile("loop-counts.json")
	var counts map[string]int
	if err := json.Unmarshal([]byte(data), &counts); err != nil {
		t.Fatalf("parse loop-counts.json: %v (%q)", err, data)
	}
	if counts["work"] == 0 {
		t.Errorf("loop-counts.json has no entry for \"work\"; got %v", counts)
	}

	// done.txt proves we exited the loop and ran the post-loop phase.
	if c := w.ReadHistoryFile("done.txt"); !strings.Contains(c, "complete") {
		t.Errorf("done.txt = %q, want \"complete\"", c)
	}
}

// TestLoops_ExhaustRecoversViaOnExhaust exhausts a loop that never passes
// its check, then jumps to a recovery phase via on-exhaust.
func TestLoops_ExhaustRecoversViaOnExhaust(t *testing.T) {
	// on-exhaust.goto must reference an earlier phase (validate.go:231).
	// Pattern:
	// - "recover" at index 0 writes the recovered.txt sentinel only when
	//   the loop feedback file exists (i.e., after on-exhaust triggered).
	// - "setup" at index 1 does nothing; exists so attempt's loop.goto has
	//   a distinct earlier target.
	// - "attempt" loops back to setup (NOT recover — we don't want recover
	//   running every iteration). When loop exhausts, on-exhaust jumps to
	//   recover (which now has a valid sentinel from WriteFeedback).
	// - Once recover runs, attempt's check still fails, exhaust counter
	//   hits 1 (>Max), run fails non-zero. That's OK: bead asks for
	//   recovered.txt to exist, not for the run to complete.
	cfg := `name: loop-exhaust
phases:
  - name: recover
    type: script
    condition: test -f $ARTIFACTS_DIR/feedback/from-attempt.md
    run: echo recovering > $ARTIFACTS_DIR/recovered.txt

  - name: setup
    type: script
    run: echo setup

  - name: attempt
    type: script
    run: echo trying
    loop:
      goto: setup
      min: 1
      max: 3
      check: "false"
      on-exhaust:
        goto: recover
        max: 1

  - name: done
    type: script
    run: echo done > $ARTIFACTS_DIR/done.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "LOOP-EX-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "LOOP-EX-001", 1)

	// We expect non-zero exit: recover runs once, but attempt's check is
	// hard-coded false so it fails again, exhausts recovery. What we care
	// about is that recover DID execute (proving on-exhaust routing works).
	res := w.RunOrc("run", w.Ticket, "--auto")
	_ = res // exit code is expected non-zero

	if c := w.ReadHistoryFile("recovered.txt"); !strings.Contains(c, "recovering") {
		t.Errorf("recovered.txt missing/wrong: %q (on-exhaust should have run recover)", c)
	}
}

// TestLoops_MinEnforcedEvenWhenCheckPasses verifies that min is honored:
// the loop iterates at least `min` times even if check passes earlier.
func TestLoops_MinEnforcedEvenWhenCheckPasses(t *testing.T) {
	cfg := `name: loop-min
phases:
  - name: init
    type: script
    run: |
      test -f $ARTIFACTS_DIR/iters.txt || echo 0 > $ARTIFACTS_DIR/iters.txt

  - name: work
    type: script
    run: |
      n=$(cat $ARTIFACTS_DIR/iters.txt)
      echo $((n+1)) > $ARTIFACTS_DIR/iters.txt
    loop:
      goto: init
      min: 3
      max: 5
      check: "true"

  - name: done
    type: script
    run: echo done
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "LOOP-MIN-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "LOOP-MIN-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	iters := strings.TrimSpace(w.ReadHistoryFile("iters.txt"))
	// `init` resets to 0 each loop, `work` increments. After min=3
	// iterations, iters.txt should be "3" (the final work write).
	if iters != "3" {
		t.Errorf("iters.txt = %q, want \"3\" (min enforced)", iters)
	}

	// Sanity: loop-counts confirms 3 runs of work.
	data := w.ReadHistoryFile("loop-counts.json")
	var counts map[string]int
	if err := json.Unmarshal([]byte(data), &counts); err == nil {
		if c := counts["work"]; c < 3 {
			t.Errorf("loop-counts[work] = %d, want >= 3", c)
		}
	}
}
