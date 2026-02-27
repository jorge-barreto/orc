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
		filepath.Join(".orc", ".gitignore"),
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

	// Verify .gitignore content
	gitignore, err := os.ReadFile(filepath.Join(dir, ".orc", ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignore), "artifacts/") {
		t.Fatalf(".gitignore missing artifacts/ entry, got: %q", string(gitignore))
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

func TestInit_FallbackWhenClaudeUnavailable(t *testing.T) {
	dir := t.TempDir()

	// Clear PATH so claude binary cannot be found — should fall back to default template.
	t.Setenv("PATH", "")

	err := Init(context.Background(), dir)
	if err != nil {
		t.Fatalf("Init should succeed via fallback, got: %v", err)
	}

	// Verify fallback created valid config
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("fallback config is invalid: %v", err)
	}
	if len(cfg.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(cfg.Phases))
	}
}

func TestWriteFallbackConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeFallbackConfig(dir); err != nil {
		t.Fatalf("writeFallbackConfig failed: %v", err)
	}

	// Verify all expected files exist
	for _, path := range []string{
		".orc/config.yaml",
		".orc/phases/plan.md",
		".orc/phases/implement.md",
		".orc/.gitignore",
	} {
		full := filepath.Join(dir, path)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("%s not created: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", path)
		}
	}

	// Verify config is valid and has expected structure
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("fallback config is invalid: %v", err)
	}
	if len(cfg.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(cfg.Phases))
	}
	if cfg.Phases[0].Name != "plan" {
		t.Fatalf("phase 0 = %q, want plan", cfg.Phases[0].Name)
	}
	if cfg.Phases[1].Name != "implement" {
		t.Fatalf("phase 1 = %q, want implement", cfg.Phases[1].Name)
	}
	if cfg.Phases[2].Name != "review" {
		t.Fatalf("phase 2 = %q, want review", cfg.Phases[2].Name)
	}
	if cfg.Phases[2].OnFail == nil || cfg.Phases[2].OnFail.Goto != "implement" {
		t.Fatal("review phase should have on-fail pointing to implement")
	}
	if cfg.Phases[2].OnFail.Max != 3 {
		t.Fatalf("on-fail.max = %d, want 3", cfg.Phases[2].OnFail.Max)
	}

	// Verify .gitignore
	gitignore, err := os.ReadFile(filepath.Join(dir, ".orc", ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignore), "artifacts/") {
		t.Fatalf(".gitignore missing artifacts/ entry")
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
