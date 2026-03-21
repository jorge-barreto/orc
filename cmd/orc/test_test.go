package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestCheckMissingArtifacts_NoPriorPhases(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan"}}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 0, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if got != "" {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestCheckMissingArtifacts_AllPresent(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan", Outputs: []string{"plan.md"}}}
	if err := os.WriteFile(filepath.Join(tmpDir, "plan.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 1, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if got != "" {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestCheckMissingArtifacts_SomeMissing(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan", Outputs: []string{"plan.md"}}}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 1, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if !strings.Contains(got, "plan.md") {
		t.Errorf("expected output to contain %q, got: %q", "plan.md", got)
	}
	if !strings.Contains(got, "plan") {
		t.Errorf("expected output to contain %q, got: %q", "plan", got)
	}
}

func TestCheckMissingArtifacts_MultiplePriorPhases(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{
		{Name: "plan", Outputs: []string{"plan.md"}},
		{Name: "code", Outputs: []string{"code.md"}},
		{Name: "review"},
	}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 2, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if !strings.Contains(got, "plan.md") {
		t.Errorf("expected output to contain %q, got: %q", "plan.md", got)
	}
	if !strings.Contains(got, "code.md") {
		t.Errorf("expected output to contain %q, got: %q", "code.md", got)
	}
	if !strings.Contains(got, "phase 1: plan") {
		t.Errorf("expected output to contain %q, got: %q", "phase 1: plan", got)
	}
	if !strings.Contains(got, "phase 2: code") {
		t.Errorf("expected output to contain %q, got: %q", "phase 2: code", got)
	}
}

func TestOrcTest_HooksNotRun(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "hook-ran")
	phase := config.Phase{
		Name:    "check",
		Type:    "script",
		Run:     "echo ok",
		PreRun:  "touch " + sentinel,
		PostRun: "touch " + sentinel + ".post",
	}

	artifactsDir := filepath.Join(t.TempDir(), "artifacts")
	if err := state.EnsureDir(artifactsDir); err != nil {
		t.Fatal(err)
	}

	env := &dispatch.Environment{
		ProjectRoot:  t.TempDir(),
		WorkDir:      t.TempDir(),
		ArtifactsDir: artifactsDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}

	result, err := dispatch.Dispatch(context.Background(), phase, env)
	if err != nil {
		t.Fatalf("dispatch.Dispatch returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}

	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("orc test calls dispatch.Dispatch directly, not dispatchWithHooks — pre-run hook must not run")
	}
	if _, err := os.Stat(sentinel + ".post"); !os.IsNotExist(err) {
		t.Fatal("orc test calls dispatch.Dispatch directly, not dispatchWithHooks — post-run hook must not run")
	}
}
