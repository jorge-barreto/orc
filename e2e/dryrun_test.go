//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDryRun_PrintsPlanWithoutExecuting verifies that --dry-run prints
// the phase plan but does not execute any phase, create artifacts, or
// spawn claude. Script-only in terms of cost — no agent phase is
// dispatched even though the config declares one.
func TestDryRun_PrintsPlanWithoutExecuting(t *testing.T) {
	cfg := `name: dryrun
phases:
  - name: greet
    type: script
    run: echo hello && touch $ARTIFACTS_DIR/side-effect.txt

  - name: think
    type: agent
    prompt: .orc/prompts/noop.md
    model: haiku

  - name: approve
    type: gate
    description: dry-run should not prompt
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "DRY-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "DRY-001", 1)

	w.WritePrompt("prompts/noop.md", "Do nothing")

	res := w.RunOrc("run", w.Ticket, "--dry-run")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// Phase names/types should appear in the plan output.
	combined := res.Stdout + res.Stderr
	wantFragments := []string{"greet", "think", "approve"}
	for _, s := range wantFragments {
		if !strings.Contains(combined, s) {
			t.Errorf("dry-run output missing %q; got: %s", s, combined)
		}
	}

	// Artifacts dir for this ticket should not exist — nothing was executed.
	if _, err := os.Stat(w.ArtifactsDir); err == nil {
		t.Errorf("%s exists; dry-run should not create artifacts dir", w.ArtifactsDir)
	}

	// The script's side-effect file must not exist anywhere.
	side := filepath.Join(w.ArtifactsDir, "side-effect.txt")
	if _, err := os.Stat(side); err == nil {
		t.Errorf("%s exists; dry-run should not execute script phases", side)
	}
}
