package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestInit_CreatesDirectoryStructure(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	for _, path := range []string{
		".orc",
		".orc/phases",
		filepath.Join(".orc", "config.yaml"),
		filepath.Join(".orc", "phases", "example.md"),
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
	if err := Init(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	configPath := filepath.Join(dir, ".orc", "config.yaml")
	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("config.Load failed on generated config: %v", err)
	}

	if len(cfg.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(cfg.Phases))
	}

	wantTypes := []string{"script", "agent", "gate"}
	for i, want := range wantTypes {
		if cfg.Phases[i].Type != want {
			t.Fatalf("phase %d: expected type %q, got %q", i, want, cfg.Phases[i].Type)
		}
	}
}

func TestInit_FailsIfDirExists(t *testing.T) {
	dir := t.TempDir()
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := Init(dir)
	if err == nil {
		t.Fatal("expected error when .orc already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected error containing 'already exists', got: %s", err)
	}
}
