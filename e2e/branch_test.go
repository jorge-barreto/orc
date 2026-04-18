//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestBranch_Basic(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: route
    type: branch
    check: echo fast
    branches:
      fast: quick-wf
      slow: full-wf
`)
	w.WriteWorkflow("quick-wf", `
name: quick-wf
phases:
  - name: quick
    type: script
    run: echo quick > $ARTIFACTS_DIR/quick.txt
`)
	w.WriteWorkflow("full-wf", `
name: full-wf
phases:
  - name: full
    type: script
    run: echo full > $ARTIFACTS_DIR/full.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// "fast" branch should have run
	assertHistoryFile(t, w, "quick-wf", "quick.txt")
	// "slow" branch should NOT have run
	assertNoHistoryFile(t, w, "full-wf", "full.txt")
}

func TestBranch_Default(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: route
    type: branch
    check: echo unknown
    branches:
      a: wf-a
    default: wf-b
`)
	w.WriteWorkflow("wf-a", `
name: wf-a
phases:
  - name: step
    type: script
    run: echo a > $ARTIFACTS_DIR/a.txt
`)
	w.WriteWorkflow("wf-b", `
name: wf-b
phases:
  - name: step
    type: script
    run: echo b > $ARTIFACTS_DIR/b.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	assertHistoryFile(t, w, "wf-b", "b.txt")
	assertNoHistoryFile(t, w, "wf-a", "a.txt")
}

func TestBranch_NoMatchNoDefault(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: route
    type: branch
    check: echo unknown
    branches:
      a: wf-a
`)
	w.WriteWorkflow("wf-a", `
name: wf-a
phases:
  - name: step
    type: script
    run: echo a > $ARTIFACTS_DIR/a.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for unmatched branch")
	}
}

func TestBranch_CheckFails(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: route
    type: branch
    check: exit 1
    branches:
      a: wf-a
`)
	w.WriteWorkflow("wf-a", `
name: wf-a
phases:
  - name: step
    type: script
    run: echo a > $ARTIFACTS_DIR/a.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for failed check")
	}

	assertNoHistoryFile(t, w, "wf-a", "a.txt")
}

func TestBranch_MultipleOptions(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: route
    type: branch
    check: echo b
    branches:
      a: wf-a
      b: wf-b
      c: wf-c
`)
	w.WriteWorkflow("wf-a", `
name: wf-a
phases:
  - name: step
    type: script
    run: echo a > $ARTIFACTS_DIR/a.txt
`)
	w.WriteWorkflow("wf-b", `
name: wf-b
phases:
  - name: step
    type: script
    run: echo b > $ARTIFACTS_DIR/b.txt
`)
	w.WriteWorkflow("wf-c", `
name: wf-c
phases:
  - name: step
    type: script
    run: echo c > $ARTIFACTS_DIR/c.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	assertNoHistoryFile(t, w, "wf-a", "a.txt")
	assertHistoryFile(t, w, "wf-b", "b.txt")
	assertNoHistoryFile(t, w, "wf-c", "c.txt")
}

func TestBranch_WithLoop(t *testing.T) {
	// Branch + loop: on first attempt, check outputs "retry" (child fails → loop).
	// On second attempt, check outputs "done" (child succeeds → loop check passes).
	// loop.goto references "init" (an earlier phase) to reset nothing — it just re-enters.
	w := NewWorkspace(t, `
name: parent
phases:
  - name: init
    type: script
    run: "true"
  - name: route
    type: branch
    check: |
      n=$(cat $ARTIFACTS_DIR/attempt.txt 2>/dev/null || echo 0)
      n=$((n + 1))
      echo $n > $ARTIFACTS_DIR/attempt.txt
      if [ "$n" -lt "2" ]; then echo retry; else echo done; fi
    branches:
      retry: retry-wf
      done: done-wf
    loop:
      goto: init
      max: 5
      check: test "$(cat $ARTIFACTS_DIR/attempt.txt)" -ge "2"
`)
	w.WriteWorkflow("retry-wf", `
name: retry-wf
phases:
  - name: step
    type: script
    run: echo retried > $ARTIFACTS_DIR/retried.txt
`)
	w.WriteWorkflow("done-wf", `
name: done-wf
phases:
  - name: step
    type: script
    run: echo done > $ARTIFACTS_DIR/done.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	assertHistoryFile(t, w, "retry-wf", "retried.txt")
	assertHistoryFile(t, w, "done-wf", "done.txt")

	content := w.ReadHistoryFile("attempt.txt")
	if strings.TrimSpace(content) != "2" {
		t.Errorf("attempt.txt = %q, want 2", strings.TrimSpace(content))
	}
}
