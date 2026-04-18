//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHooks_PreAndPostRunSuccess verifies the happy path: both hooks
// fire, in order: pre-run → main run → post-run.
func TestHooks_PreAndPostRunSuccess(t *testing.T) {
	cfg := `name: hooks-ok
phases:
  - name: withhooks
    type: script
    pre-run: echo pre >> $ARTIFACTS_DIR/hooks.txt
    post-run: echo post >> $ARTIFACTS_DIR/hooks.txt
    run: echo main >> $ARTIFACTS_DIR/hooks.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "HOOKS-OK-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "HOOKS-OK-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	content := w.ReadHistoryFile("hooks.txt")
	got := strings.Split(strings.TrimSpace(content), "\n")
	want := []string{"pre", "main", "post"}
	if len(got) != 3 {
		t.Fatalf("hooks.txt lines = %d (%q), want 3 (pre/main/post)", len(got), content)
	}
	for i, line := range got {
		if line != want[i] {
			t.Errorf("hooks.txt line %d = %q, want %q", i, line, want[i])
		}
	}
}

// TestHooks_PreRunFailureSkipsDispatch verifies that a pre-run hook
// failing prevents the main dispatch from running and fails the workflow.
func TestHooks_PreRunFailureSkipsDispatch(t *testing.T) {
	cfg := `name: hooks-prefail
phases:
  - name: prefail
    type: script
    pre-run: exit 1
    run: echo should-not-run > $ARTIFACTS_DIR/nope.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "HOOKS-PRE-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "HOOKS-PRE-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero (pre-run failure should fail the run)\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}

	// nope.txt should NOT exist anywhere (dispatch was skipped).
	// Check live dir AND history dir.
	paths := []string{
		filepath.Join(w.ArtifactsDir, "nope.txt"),
	}
	// If the run has a history dir (fail paths may not archive), check there too.
	histParent := filepath.Join(w.ArtifactsDir, "history")
	if entries, err := os.ReadDir(histParent); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				paths = append(paths, filepath.Join(histParent, e.Name(), "nope.txt"))
			}
		}
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("nope.txt exists at %s; dispatch should have been skipped", p)
		}
	}
}

// TestHooks_PostRunFailureOverridesSuccess verifies that a post-run
// failure causes the overall run to fail even though the main dispatch
// succeeded.
func TestHooks_PostRunFailureOverridesSuccess(t *testing.T) {
	cfg := `name: hooks-postfail
phases:
  - name: postfail
    type: script
    post-run: exit 1
    run: echo main > $ARTIFACTS_DIR/main.txt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "HOOKS-POST-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "HOOKS-POST-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")

	if res.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero (post-run failure should fail the run)\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}

	// main.txt exists — dispatch DID run before the post-run hook failed.
	// Look in live dir, then any history dir.
	found := false
	candidates := []string{filepath.Join(w.ArtifactsDir, "main.txt")}
	histParent := filepath.Join(w.ArtifactsDir, "history")
	if entries, err := os.ReadDir(histParent); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(histParent, e.Name(), "main.txt"))
			}
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("main.txt not found in any of %v; dispatch should have run", candidates)
	}
}
