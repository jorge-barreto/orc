package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestInit_CreatesDirectoryStructure(t *testing.T) {
	dir := t.TempDir()
	if err := Init(context.Background(), dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	for _, path := range []string{
		".orc",
		".orc/phases",
		filepath.Join(".orc", "config.yaml"),
	} {
		full := filepath.Join(dir, path)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("%s not created: %v", path, err)
		}
		if !info.IsDir() && info.Size() == 0 {
			t.Fatalf("%s is empty", path)
		}
	}
}

func TestInit_GeneratedConfigIsValid(t *testing.T) {
	dir := t.TempDir()
	if err := Init(context.Background(), dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	configPath := filepath.Join(dir, ".orc", "config.yaml")
	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("config.Load failed on generated config: %v", err)
	}

	if len(cfg.Phases) < 1 {
		t.Fatal("expected at least 1 phase")
	}
}

func TestInit_FailsIfDirExists(t *testing.T) {
	dir := t.TempDir()
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := Init(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error when .orc already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected error containing 'already exists', got: %s", err)
	}
}

func TestInit_FailsWhenClaudeUnavailable(t *testing.T) {
	dir := t.TempDir()

	// Clear PATH so claude binary cannot be found.
	t.Setenv("PATH", "")

	err := Init(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error when claude is not available")
	}
}

func TestRenderWorkflowSummary_Sequential(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan"},
		{Name: "implement"},
		{Name: "review"},
	}
	got := renderWorkflowSummary(phases)
	want := "plan → implement → review"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderWorkflowSummary_WithParallel(t *testing.T) {
	phases := []config.Phase{
		{Name: "plan"},
		{Name: "test"},
		{Name: "lint", ParallelWith: "test"},
		{Name: "review"},
	}
	got := renderWorkflowSummary(phases)
	want := "plan → test ∥ lint → review"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderWorkflowSummary_Single(t *testing.T) {
	phases := []config.Phase{
		{Name: "implement"},
	}
	got := renderWorkflowSummary(phases)
	want := "implement"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
