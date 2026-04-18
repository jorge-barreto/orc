//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mixedMode updates w.ArtifactsDir to the "default" workflow namespace,
// which is where parent artifacts land when both config.yaml and workflows/ exist.
func mixedMode(w *Workspace) {
	w.ArtifactsDir = w.ArtifactsDirForWorkflow("default")
}

// historyFileExists checks if a file exists in the latest history entry
// of a given workflow's artifacts dir.
func historyFileExists(t *testing.T, w *Workspace, workflowName, fileName string) bool {
	t.Helper()
	artDir := w.ArtifactsDirForWorkflow(workflowName)
	histDir := filepath.Join(artDir, "history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		return false
	}
	var latest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		return false
	}
	_, err = os.Stat(filepath.Join(histDir, latest, fileName))
	return err == nil
}

func assertHistoryFile(t *testing.T, w *Workspace, workflowName, fileName string) {
	t.Helper()
	if !historyFileExists(t, w, workflowName, fileName) {
		t.Errorf("expected %s in %s history", fileName, workflowName)
	}
}

func assertNoHistoryFile(t *testing.T, w *Workspace, workflowName, fileName string) {
	t.Helper()
	if historyFileExists(t, w, workflowName, fileName) {
		t.Errorf("expected %s NOT in %s history", fileName, workflowName)
	}
}

func readHistoryFileForWorkflow(t *testing.T, w *Workspace, workflowName, fileName string) string {
	t.Helper()
	artDir := w.ArtifactsDirForWorkflow(workflowName)
	histDir := filepath.Join(artDir, "history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatalf("read history dir for %s: %v", workflowName, err)
	}
	var latest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		t.Fatalf("no history entries for %s", workflowName)
	}
	data, err := os.ReadFile(filepath.Join(histDir, latest, fileName))
	if err != nil {
		t.Fatalf("read %s/%s: %v", workflowName, fileName, err)
	}
	return string(data)
}

func TestCompose_Basic(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: before
    type: script
    run: echo before > $ARTIFACTS_DIR/before.txt
  - name: run-child
    type: workflow
    workflow: child
  - name: after
    type: script
    run: echo after > $ARTIFACTS_DIR/after.txt
`)
	w.WriteWorkflow("child", `
name: child
phases:
  - name: step-one
    type: script
    run: echo one > $ARTIFACTS_DIR/step-one.txt
  - name: step-two
    type: script
    run: echo two > $ARTIFACTS_DIR/step-two.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	rr := w.ReadRunResult()
	if rr["status"] != "completed" {
		t.Errorf("status = %v", rr["status"])
	}

	// Parent artifacts (archived to history after successful run)
	assertHistoryFile(t, w, "default", "before.txt")
	assertHistoryFile(t, w, "default", "after.txt")

	// Child artifacts in namespaced directory
	assertHistoryFile(t, w, "child", "step-one.txt")
	assertHistoryFile(t, w, "child", "step-two.txt")
}

func TestCompose_ChildFailure(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: run-child
    type: workflow
    workflow: child
  - name: after
    type: script
    run: echo after > $ARTIFACTS_DIR/after.txt
`)
	w.WriteWorkflow("child", `
name: child
phases:
  - name: fail
    type: script
    run: exit 1
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}

	// "after" should NOT have run (no history entry for it)
	assertNoHistoryFile(t, w, "default", "after.txt")
}

func TestCompose_ConditionSkipsWorkflow(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: run-child
    type: workflow
    workflow: child
    condition: "false"
  - name: after
    type: script
    run: echo after > $ARTIFACTS_DIR/after.txt
`)
	w.WriteWorkflow("child", `
name: child
phases:
  - name: step
    type: script
    run: echo child > $ARTIFACTS_DIR/marker.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// Workflow skipped — child artifacts should not exist
	assertNoHistoryFile(t, w, "child", "marker.txt")

	// Parent continued — after.txt written
	assertHistoryFile(t, w, "default", "after.txt")
}

func TestCompose_WithLoop(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: prepare
    type: script
    run: |
      count=$(cat $ARTIFACTS_DIR/count.txt 2>/dev/null || echo 0)
      echo $((count + 1)) > $ARTIFACTS_DIR/count.txt
  - name: check-work
    type: workflow
    workflow: checker
    loop:
      goto: prepare
      max: 5
      check: test "$(cat $ARTIFACTS_DIR/count.txt)" -ge "2"
`)
	w.WriteWorkflow("checker", `
name: checker
phases:
  - name: verify
    type: script
    run: echo ok > $ARTIFACTS_DIR/verified.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// Loop ran prepare twice (count went 1 → 2, check passes at 2)
	content := w.ReadHistoryFile("count.txt")
	if strings.TrimSpace(content) != "2" {
		t.Errorf("count.txt = %q, want 2", strings.TrimSpace(content))
	}
}

func TestCompose_Nested(t *testing.T) {
	w := NewWorkspace(t, `
name: top
phases:
  - name: top-step
    type: script
    run: echo top > $ARTIFACTS_DIR/top.txt
  - name: go-middle
    type: workflow
    workflow: middle
`)
	w.WriteWorkflow("middle", `
name: middle
phases:
  - name: mid-step
    type: script
    run: echo mid > $ARTIFACTS_DIR/mid.txt
  - name: go-bottom
    type: workflow
    workflow: bottom
`)
	w.WriteWorkflow("bottom", `
name: bottom
phases:
  - name: bot-step
    type: script
    run: echo bot > $ARTIFACTS_DIR/bot.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	assertHistoryFile(t, w, "default", "top.txt")
	assertHistoryFile(t, w, "middle", "mid.txt")
	assertHistoryFile(t, w, "bottom", "bot.txt")
}

func TestCompose_EnvInheritance(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: run-child
    type: workflow
    workflow: child
`)
	w.WriteWorkflow("child", `
name: child
phases:
  - name: check-env
    type: script
    run: |
      echo $TICKET > $ARTIFACTS_DIR/ticket.txt
      echo $PROJECT_ROOT > $ARTIFACTS_DIR/root.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	ticket := readHistoryFileForWorkflow(t, w, "child", "ticket.txt")
	if got := strings.TrimSpace(ticket); got != w.Ticket {
		t.Errorf("child $TICKET = %q, want %q", got, w.Ticket)
	}
	root := readHistoryFileForWorkflow(t, w, "child", "root.txt")
	if got := strings.TrimSpace(root); got != w.Dir {
		t.Errorf("child $PROJECT_ROOT = %q, want %q", got, w.Dir)
	}
}

func TestCompose_ArtifactIsolation(t *testing.T) {
	w := NewWorkspace(t, `
name: parent
phases:
  - name: write-parent
    type: script
    run: echo parent > $ARTIFACTS_DIR/output.txt
  - name: run-child
    type: workflow
    workflow: child
`)
	w.WriteWorkflow("child", `
name: child
phases:
  - name: write-child
    type: script
    run: echo child > $ARTIFACTS_DIR/output.txt
`)
	mixedMode(w)

	res := w.RunOrc("run", w.Ticket, "--auto")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s %s", res.ExitCode, res.Stdout, res.Stderr)
	}

	parentContent := readHistoryFileForWorkflow(t, w, "default", "output.txt")
	if !strings.Contains(parentContent, "parent") {
		t.Errorf("parent output.txt = %q", parentContent)
	}

	childContent := readHistoryFileForWorkflow(t, w, "child", "output.txt")
	if !strings.Contains(childContent, "child") {
		t.Errorf("child output.txt = %q", childContent)
	}
}

func TestCompose_CycleDetectedAtValidation(t *testing.T) {
	w := NewWorkspace(t, `
name: dummy
phases:
  - name: s
    type: script
    run: echo
`)
	w.DeleteDefaultConfig()
	w.WriteWorkflow("a", `
name: a
phases:
  - name: call-b
    type: workflow
    workflow: b
`)
	w.WriteWorkflow("b", `
name: b
phases:
  - name: call-a
    type: workflow
    workflow: a
`)

	res := w.RunOrc("-w", "a", "run", w.Ticket, "--auto")
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit for cycle")
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "circular workflow reference") {
		t.Errorf("expected cycle error in output, got: %s", combined)
	}
}
