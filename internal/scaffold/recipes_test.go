package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

// TestAllRecipes_ProduceValidConfigs writes each recipe's files to a temp dir
// and verifies config.Load succeeds.
func TestAllRecipes_ProduceValidConfigs(t *testing.T) {
	for _, recipe := range AllRecipes() {
		t.Run(recipe.Name, func(t *testing.T) {
			dir := t.TempDir()
			for relPath, content := range recipe.Files {
				fullPath := filepath.Join(dir, relPath)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			configPath := filepath.Join(dir, ".orc", "config.yaml")
			if _, err := config.Load(configPath, dir); err != nil {
				t.Fatalf("recipe %q: config.Load failed: %v", recipe.Name, err)
			}
		})
	}
}

func TestGetRecipe_Unknown(t *testing.T) {
	_, err := GetRecipe("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown recipe")
	}
}

func TestGetRecipe_AllNamed(t *testing.T) {
	for _, name := range []string{"simple", "standard", "full-pipeline", "review-loop"} {
		if _, err := GetRecipe(name); err != nil {
			t.Errorf("GetRecipe(%q) failed: %v", name, err)
		}
	}
}

func TestRecipes_HaveRequiredFiles(t *testing.T) {
	for _, recipe := range AllRecipes() {
		t.Run(recipe.Name, func(t *testing.T) {
			if _, ok := recipe.Files[".orc/config.yaml"]; !ok {
				t.Error("missing .orc/config.yaml")
			}
			hasPhase := false
			for path := range recipe.Files {
				if strings.HasPrefix(path, ".orc/phases/") && strings.HasSuffix(path, ".md") {
					hasPhase = true
					break
				}
			}
			if !hasPhase {
				t.Error("no .orc/phases/*.md files")
			}
		})
	}
}

func TestRecipes_PromptFilesReferenced(t *testing.T) {
	for _, recipe := range AllRecipes() {
		t.Run(recipe.Name, func(t *testing.T) {
			dir := t.TempDir()
			for relPath, content := range recipe.Files {
				fullPath := filepath.Join(dir, relPath)
				os.MkdirAll(filepath.Dir(fullPath), 0755)
				os.WriteFile(fullPath, []byte(content), 0644)
			}
			configPath := filepath.Join(dir, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, dir)
			if err != nil {
				t.Fatalf("config.Load failed: %v", err)
			}
			for _, phase := range cfg.Phases {
				if phase.Prompt == "" {
					continue
				}
				if _, ok := recipe.Files[phase.Prompt]; !ok {
					t.Errorf("phase %q: prompt %q not in recipe.Files", phase.Name, phase.Prompt)
				}
			}
		})
	}
}

func TestInitRecipe_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := InitRecipe(dir, "standard"); err != nil {
		t.Fatalf("InitRecipe failed: %v", err)
	}

	recipe, _ := GetRecipe("standard")
	for relPath := range recipe.Files {
		full := filepath.Join(dir, relPath)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("%s not created: %v", relPath, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", relPath)
		}
	}

	// .gitignore must also exist
	gitignore := filepath.Join(dir, ".orc", ".gitignore")
	if _, err := os.Stat(gitignore); err != nil {
		t.Fatal(".orc/.gitignore not created")
	}

	// Config must validate
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	if _, err := config.Load(configPath, dir); err != nil {
		t.Fatalf("config invalid after InitRecipe: %v", err)
	}
}

func TestInitRecipe_FailsIfDirExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc"), 0755); err != nil {
		t.Fatal(err)
	}
	err := InitRecipe(dir, "standard")
	if err == nil {
		t.Fatal("expected error when .orc already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

func TestInitRecipe_UnknownRecipe(t *testing.T) {
	dir := t.TempDir()
	err := InitRecipe(dir, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown recipe name")
	}
}

func TestRecipeSimple_MatchesFallback(t *testing.T) {
	// Write simple recipe to temp dir
	simpleDir := t.TempDir()
	if err := InitRecipe(simpleDir, "simple"); err != nil {
		t.Fatalf("InitRecipe simple failed: %v", err)
	}

	// Write fallback to temp dir
	fallbackDir := t.TempDir()
	if err := writeFallbackConfig(fallbackDir); err != nil {
		t.Fatalf("writeFallbackConfig failed: %v", err)
	}

	simpleCfg, err := config.Load(filepath.Join(simpleDir, ".orc", "config.yaml"), simpleDir)
	if err != nil {
		t.Fatalf("loading simple config: %v", err)
	}
	fallbackCfg, err := config.Load(filepath.Join(fallbackDir, ".orc", "config.yaml"), fallbackDir)
	if err != nil {
		t.Fatalf("loading fallback config: %v", err)
	}

	if len(simpleCfg.Phases) != len(fallbackCfg.Phases) {
		t.Fatalf("phase count: simple=%d fallback=%d", len(simpleCfg.Phases), len(fallbackCfg.Phases))
	}
	for i, sp := range simpleCfg.Phases {
		fp := fallbackCfg.Phases[i]
		if sp.Name != fp.Name {
			t.Errorf("phase[%d].Name: simple=%q fallback=%q", i, sp.Name, fp.Name)
		}
		if sp.Type != fp.Type {
			t.Errorf("phase[%d].Type: simple=%q fallback=%q", i, sp.Type, fp.Type)
		}
		simpleHasLoop := sp.Loop != nil
		fallbackHasLoop := fp.Loop != nil
		if simpleHasLoop != fallbackHasLoop {
			t.Errorf("phase[%d] loop presence mismatch", i)
		}
		if simpleHasLoop && fallbackHasLoop {
			if sp.Loop.Goto != fp.Loop.Goto || sp.Loop.Max != fp.Loop.Max {
				t.Errorf("phase[%d] loop fields differ", i)
			}
		}
	}
}
