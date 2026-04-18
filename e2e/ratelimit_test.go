//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockClaudePathDir returns a directory containing a symlink named "claude"
// that points at the given mock script (path relative to the e2e/ dir).
// Callers prepend the returned directory to PATH so the real claude CLI
// is masked — no real API quota consumed.
func mockClaudePathDir(t *testing.T, mockScriptRelPath string) string {
	t.Helper()
	absScript, err := filepath.Abs(mockScriptRelPath)
	if err != nil {
		t.Fatalf("abs %s: %v", mockScriptRelPath, err)
	}
	if _, err := os.Stat(absScript); err != nil {
		t.Fatalf("mock script missing: %v", err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "claude")
	if err := os.Symlink(absScript, link); err != nil {
		t.Fatalf("symlink claude → %s: %v", absScript, err)
	}
	return dir
}

// envWithMaskedClaude returns an os.Environ-style slice with CLAUDECODE
// stripped and PATH prefixed with pathDir so a mock claude masks the real one.
func envWithMaskedClaude(pathDir string) []string {
	var filtered []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CLAUDECODE") || strings.HasPrefix(kv, "PATH=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	filtered = append(filtered, "PATH="+pathDir+":"+os.Getenv("PATH"))
	return filtered
}

// runOrcMaskingClaude invokes the orc binary from the workspace dir with
// the given mock script in place of a real `claude` on PATH.
func runOrcMaskingClaude(w *Workspace, mockPathDir string, args ...string) *Result {
	w.t.Helper()
	cmd := exec.Command(orcBinary, args...)
	cmd.Dir = w.Dir
	cmd.Env = envWithMaskedClaude(mockPathDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			w.t.Fatalf("run orc: %v (stderr: %s)", err, stderr.String())
		}
	}
	return &Result{ExitCode: exit, Stdout: stdout.String(), Stderr: stderr.String()}
}

// TestRateLimit_DefaultExitsWithCode8 verifies that with no on-rate-limit
// set, the runner exits immediately with code 8 (ExitRateLimit) when
// claude returns a rate_limit_event rejected.
func TestRateLimit_DefaultExitsWithCode8(t *testing.T) {
	cfg := `name: rl-default
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/noop.md
    model: haiku
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "RL-DEFAULT-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "RL-DEFAULT-001", 1)
	w.WritePrompt("prompts/noop.md", "noop")

	mockDir := mockClaudePathDir(t, "mocks/claude-ratelimit.sh")

	start := time.Now()
	res := runOrcMaskingClaude(w, mockDir, "run", w.Ticket, "--auto")
	elapsed := time.Since(start)

	if res.ExitCode != 8 {
		t.Errorf("exit code = %d, want 8 (ExitRateLimit)\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}
	// Should exit almost immediately — no waiting.
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want < 5s (default policy should NOT wait)", elapsed)
	}

	state := w.ReadState()
	if got := state["failure_category"]; got != "rate_limit" {
		t.Errorf("state.failure_category = %v, want rate_limit", got)
	}
}

// TestRateLimit_ConfigWaitEngagesWaitLoop verifies that on-rate-limit: wait
// at the config level triggers the wait-and-resume path. Rather than
// actually waiting an hour, we start orc and observe it's still running
// after 2s (would have exited immediately with code 8 otherwise).
func TestRateLimit_ConfigWaitEngagesWaitLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 60s+ wait-engagement test in -short mode")
	}
	cfg := `name: rl-wait
on-rate-limit: wait
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/noop.md
    model: haiku
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "RL-WAIT-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "RL-WAIT-001", 1)
	w.WritePrompt("prompts/noop.md", "noop")

	mockDir := mockClaudePathDir(t, "mocks/claude-ratelimit.sh")

	cmd := exec.Command(orcBinary, "run", w.Ticket, "--auto")
	cmd.Dir = w.Dir
	cmd.Env = envWithMaskedClaude(mockDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start orc: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Exited on its own before our deadline → wait was NOT engaged.
		t.Errorf("orc exited early (err=%v, code=%d); on-rate-limit: wait should have engaged wait loop.\nstdout: %s\nstderr: %s",
			err, cmd.ProcessState.ExitCode(), stdout.String(), stderr.String())
	case <-time.After(2 * time.Second):
		// Still running after 2s → wait is engaged. Good. Kill it.
		_ = cmd.Process.Signal(os.Interrupt)
		<-done
	}
}

// TestRateLimit_PhaseOverrideWaitInsideExitConfig verifies that a phase's
// on-rate-limit: wait overrides a config's on-rate-limit: exit.
func TestRateLimit_PhaseOverrideWaitInsideExitConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 60s+ wait-engagement test in -short mode")
	}
	cfg := `name: rl-override
on-rate-limit: exit
phases:
  - name: agent
    type: agent
    prompt: .orc/prompts/noop.md
    model: haiku
    on-rate-limit: wait
`
	w := NewWorkspace(t, cfg)
	w.Ticket = "RL-OVERRIDE-001"
	w.ArtifactsDir = strings.Replace(w.ArtifactsDir, "TEST-1", "RL-OVERRIDE-001", 1)
	w.WritePrompt("prompts/noop.md", "noop")

	mockDir := mockClaudePathDir(t, "mocks/claude-ratelimit.sh")

	cmd := exec.Command(orcBinary, "run", w.Ticket, "--auto")
	cmd.Dir = w.Dir
	cmd.Env = envWithMaskedClaude(mockDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start orc: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		t.Errorf("orc exited early; phase override (wait) should have engaged wait loop despite config exit")
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Signal(os.Interrupt)
		<-done
	}
}
