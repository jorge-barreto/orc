//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkflow_SingleDefaultConfigFlatLayout confirms the existing smoke
// behavior: with only .orc/config.yaml, artifacts land at
// .orc/artifacts/<ticket>/ (NO workflow segment). This is the baseline
// reference for the multi-workflow tests below.
func TestWorkflow_SingleDefaultConfigFlatLayout(t *testing.T) {
	cfg := `name: single
phases:
  - name: greet
    type: script
    run: echo hi
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "WF-FLAT-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "WF-FLAT-001", 1)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// Artifacts at .orc/artifacts/<ticket>/ — not nested under a workflow.
	flatPath := filepath.Join(w.Dir, ".orc", "artifacts", w.Ticket, "run-result.json")
	if _, err := os.Stat(flatPath); err != nil {
		t.Errorf("expected flat layout run-result at %s: %v", flatPath, err)
	}
}

// TestWorkflow_MultiWorkflow_RequiresFlagOrDefault confirms that with
// multiple workflows and no default config.yaml, running without -w
// fails — orc cannot choose for us.
func TestWorkflow_MultiWorkflow_RequiresFlagOrDefault(t *testing.T) {
	// Start from a minimal valid config, then delete it so we have pure
	// workflows-only layout.
	stub := `name: stub
phases:
  - name: a
    type: script
    run: echo hi
`
	w := NewWorkspace(t, stub)
	w.DeleteDefaultConfig()

	bugfix := `name: bugfix
phases:
  - name: work
    type: script
    run: echo fixing > $ARTIFACTS_DIR/fix.txt
`
	feature := `name: feature
phases:
  - name: build
    type: script
    run: echo building > $ARTIFACTS_DIR/feat.txt
`
	w.WriteWorkflow("bugfix", bugfix)
	w.WriteWorkflow("feature", feature)

	w.Ticket = "WF-AMBIG-001"
	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Fatalf("exit = 0, want non-zero (ambiguous workflow)\nstdout: %s\nstderr: %s",
			res.Stdout, res.Stderr)
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "multiple workflows") {
		t.Errorf("expected 'multiple workflows' hint in output; got: %s", combined)
	}
}

// TestWorkflow_FlagSelectsWorkflow verifies that -w <name> picks the
// named workflow and artifacts land under .orc/artifacts/<name>/<ticket>/.
func TestWorkflow_FlagSelectsWorkflow(t *testing.T) {
	stub := `name: stub
phases:
  - name: a
    type: script
    run: echo hi
`
	w := NewWorkspace(t, stub)
	w.DeleteDefaultConfig()

	bugfix := `name: bugfix-wf
phases:
  - name: work
    type: script
    run: echo fixing > $ARTIFACTS_DIR/fix.txt
`
	feature := `name: feature-wf
phases:
  - name: build
    type: script
    run: echo building > $ARTIFACTS_DIR/feat.txt
`
	w.WriteWorkflow("bugfix", bugfix)
	w.WriteWorkflow("feature", feature)

	w.Ticket = "WF-FLAG-001"
	res := w.RunOrc("-w", "bugfix", "run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// Artifacts must be under the workflow-namespaced path.
	nestedRR := filepath.Join(w.Dir, ".orc", "artifacts", "bugfix", w.Ticket, "run-result.json")
	if _, err := os.Stat(nestedRR); err != nil {
		t.Errorf("expected run-result at %s (workflow-namespaced): %v", nestedRR, err)
	}
	// Must NOT exist at the flat path.
	flatRR := filepath.Join(w.Dir, ".orc", "artifacts", w.Ticket, "run-result.json")
	if _, err := os.Stat(flatRR); err == nil {
		t.Errorf("run-result should not exist at flat path %s", flatRR)
	}
	// feature should not have run.
	featureSide := filepath.Join(w.Dir, ".orc", "artifacts", "feature", w.Ticket)
	if _, err := os.Stat(featureSide); err == nil {
		t.Errorf("feature workflow should not have any artifacts")
	}
}

// TestWorkflow_UnknownWorkflowErrors verifies -w <nonexistent> exits
// non-zero with a helpful message listing available workflows.
func TestWorkflow_UnknownWorkflowErrors(t *testing.T) {
	stub := `name: stub
phases:
  - name: a
    type: script
    run: echo hi
`
	w := NewWorkspace(t, stub)
	w.DeleteDefaultConfig()
	w.WriteWorkflow("bugfix", `name: bugfix
phases:
  - name: a
    type: script
    run: echo hi
`)

	w.Ticket = "WF-MISSING-001"
	res := w.RunOrc("-w", "doesnotexist", "run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Errorf("exit = 0, want non-zero for unknown workflow")
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "doesnotexist") {
		t.Errorf("error output missing the bad workflow name: %s", combined)
	}
	if !strings.Contains(combined, "bugfix") {
		t.Errorf("error output missing the available workflow 'bugfix': %s", combined)
	}
}

// TestWorkflow_PositionalDisambiguation verifies that when the first
// positional arg matches a workflow name and a second arg is given,
// orc treats arg[0] as workflow name, arg[1] as ticket, and prints a
// hint.
func TestWorkflow_PositionalDisambiguation(t *testing.T) {
	stub := `name: stub
phases:
  - name: a
    type: script
    run: echo hi
`
	w := NewWorkspace(t, stub)
	w.DeleteDefaultConfig()
	w.WriteWorkflow("bugfix", `name: bugfix
phases:
  - name: work
    type: script
    run: echo fixing > $ARTIFACTS_DIR/fix.txt
`)

	w.Ticket = "WF-POS-001"
	res := w.RunOrc("run", "bugfix", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// Should print the documented hint to stderr.
	if !strings.Contains(res.Stderr, "treating") || !strings.Contains(res.Stderr, "bugfix") {
		t.Errorf("expected disambiguation hint in stderr; got: %s", res.Stderr)
	}

	// Artifacts must be workflow-namespaced under bugfix.
	nestedRR := filepath.Join(w.Dir, ".orc", "artifacts", "bugfix", w.Ticket, "run-result.json")
	if _, err := os.Stat(nestedRR); err != nil {
		t.Errorf("expected run-result at %s: %v", nestedRR, err)
	}
}

// TestWorkflow_MixedModeDefaultConfigNamedDefault verifies that when
// both .orc/config.yaml AND .orc/workflows/*.yaml exist, running without
// -w picks the default config and uses the workflow name "default" for
// artifact namespacing.
func TestWorkflow_MixedModeDefaultConfigNamedDefault(t *testing.T) {
	defaultCfg := `name: root
phases:
  - name: greet
    type: script
    run: echo default-ran > $ARTIFACTS_DIR/d.txt
`
	w := NewWorkspace(t, defaultCfg)
	w.WriteWorkflow("bugfix", `name: bugfix
phases:
  - name: work
    type: script
    run: echo fixing > $ARTIFACTS_DIR/fix.txt
`)

	w.Ticket = "WF-MIX-001"
	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}

	// In mixed mode the default config gets the synthetic workflow name
	// "default", so artifacts land at .orc/artifacts/default/<ticket>/.
	defaultRR := filepath.Join(w.Dir, ".orc", "artifacts", "default", w.Ticket, "run-result.json")
	if _, err := os.Stat(defaultRR); err != nil {
		t.Errorf("expected run-result at %s (mixed-mode default namespace): %v",
			defaultRR, err)
	}
}
