//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestResume_FromByName verifies that --from=<phase-name> starts
// execution from the named phase and skips earlier phases. We use a
// single tempdir across two orc invocations: the first writes a
// sentinel into step1; the second invocation (with --from=third)
// should NOT re-touch that sentinel, proving phases 1 and 2 were
// skipped.
func TestResume_FromByName(t *testing.T) {
	cfg := `name: resume
phases:
  - name: first
    type: script
    run: |
      test ! -f $ARTIFACTS_DIR/first-ran.sentinel
      touch $ARTIFACTS_DIR/first-ran.sentinel
      echo step1 > $ARTIFACTS_DIR/step1.txt

  - name: second
    type: script
    run: |
      test ! -f $ARTIFACTS_DIR/second-ran.sentinel
      touch $ARTIFACTS_DIR/second-ran.sentinel
      echo step2 > $ARTIFACTS_DIR/step2.txt

  - name: third
    type: script
    run: echo step3 > $ARTIFACTS_DIR/step3.txt

  - name: fourth
    type: script
    run: echo step4 > $ARTIFACTS_DIR/step4.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "RESUME-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "RESUME-001", 1)

	// First run: all four phases succeed. Sentinels are touched (exactly once).
	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("first run exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// Second run with --from=third. If phases 1 or 2 ran again, their
	// `test ! -f ... .sentinel` guards would fail — the test would exit
	// non-zero before we ever got to assertions.
	res2 := w.RunOrc("run", w.Ticket, "--auto", "--from", "third")
	if res2.ExitCode != 0 {
		t.Fatalf("resumed run (--from=third) exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res2.ExitCode, res2.Stdout, res2.Stderr)
	}

	// The resumed run produced step3 and step4 artifacts (under history
	// since the run is complete). Use ReadHistoryFile which checks live
	// then latest history.
	for _, name := range []string{"step3.txt", "step4.txt"} {
		content := w.ReadHistoryFile(name)
		if !strings.Contains(content, "step") {
			t.Errorf("%s missing or wrong: %q", name, content)
		}
	}

	// Run-result of the resumed run should show status=completed.
	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("resumed run-result.status = %v, want \"completed\"", got)
	}
}

// TestResume_FromByNumber verifies the same with a numeric --from.
func TestResume_FromByNumber(t *testing.T) {
	cfg := `name: resume-num
phases:
  - name: a
    type: script
    run: |
      test ! -f $ARTIFACTS_DIR/a.sentinel
      touch $ARTIFACTS_DIR/a.sentinel

  - name: b
    type: script
    run: |
      test ! -f $ARTIFACTS_DIR/b.sentinel
      touch $ARTIFACTS_DIR/b.sentinel

  - name: c
    type: script
    run: echo c-ran > $ARTIFACTS_DIR/c.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "RESUME-NUM-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "RESUME-NUM-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("first run exit = %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// --from=3 means phase c only. If phase a or b re-executed, their
	// `test ! -f ... .sentinel` guards would fail and the run would exit
	// non-zero. So a clean exit is itself the proof that a and b were
	// skipped.
	res2 := w.RunOrc("run", w.Ticket, "--auto", "--from", "3")
	if res2.ExitCode != 0 {
		t.Fatalf("--from=3 exit = %d (phases a/b likely re-ran): %s %s",
			res2.ExitCode, res2.Stdout, res2.Stderr)
	}

	rr := w.ReadRunResult()
	if got := rr["status"]; got != "completed" {
		t.Errorf("run-result.status after --from=3 = %v, want \"completed\"", got)
	}
}
