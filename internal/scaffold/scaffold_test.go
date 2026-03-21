package scaffold

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/fileblocks"
)

func stubClaude(t *testing.T) {
	t.Helper()
	orig := runClaude
	t.Cleanup(func() { runClaude = orig })
	runClaude = func(_ context.Context, _ string) (string, error) {
		return "```yaml file=.orc/config.yaml\n" +
			"name: test-project\n" +
			"phases:\n" +
			"  - name: plan\n" +
			"    type: agent\n" +
			"    prompt: .orc/phases/plan.md\n" +
			"```\n\n" +
			"```markdown file=.orc/phases/plan.md\n" +
			"You are a planning assistant.\n" +
			"```\n", nil
	}
}

func stubClaudeCapture(t *testing.T) *string {
	t.Helper()
	var captured string
	orig := runClaude
	t.Cleanup(func() { runClaude = orig })
	runClaude = func(_ context.Context, prompt string) (string, error) {
		captured = prompt
		return "```yaml file=.orc/config.yaml\n" +
			"name: test-project\n" +
			"phases:\n" +
			"  - name: plan\n" +
			"    type: agent\n" +
			"    prompt: .orc/phases/plan.md\n" +
			"```\n\n" +
			"```markdown file=.orc/phases/plan.md\n" +
			"You are a planning assistant.\n" +
			"```\n", nil
	}
	return &captured
}

func stubGetRecipe(t *testing.T, fn func(string) (Recipe, error)) {
	t.Helper()
	orig := getRecipeFn
	t.Cleanup(func() { getRecipeFn = orig })
	getRecipeFn = fn
}

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestInit_CreatesDirectoryStructure(t *testing.T) {
	stubClaude(t)
	dir := t.TempDir()
	if err := Init(context.Background(), dir, ""); err != nil {
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
	stubClaude(t)
	dir := t.TempDir()
	if err := Init(context.Background(), dir, ""); err != nil {
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

	err := Init(context.Background(), dir, "")
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

	err := Init(context.Background(), dir, "")
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

func TestInit_UserPromptPassedToClaude(t *testing.T) {
	captured := stubClaudeCapture(t)
	dir := t.TempDir()

	if err := Init(context.Background(), dir, "documentation drafting with critique loop"); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if !strings.Contains(*captured, "documentation drafting with critique loop") {
		t.Fatal("user prompt not found in the prompt sent to claude")
	}
	if !strings.Contains(*captured, "User Description") {
		t.Fatal("expected 'User Description' section header in prompt")
	}
}

func TestInit_NoUserPromptNoSection(t *testing.T) {
	captured := stubClaudeCapture(t)
	dir := t.TempDir()

	if err := Init(context.Background(), dir, ""); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if strings.Contains(*captured, "User Description") {
		t.Fatal("'User Description' section should not appear when no user prompt is given")
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
	if cfg.Phases[2].Loop == nil || cfg.Phases[2].Loop.Goto != "implement" {
		t.Fatal("review phase should have loop pointing to implement")
	}
	if cfg.Phases[2].Loop.Max != 3 {
		t.Fatalf("loop.max = %d, want 3", cfg.Phases[2].Loop.Max)
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

func TestInitWorkflow_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc"), 0755)

	if err := InitWorkflow(dir, "bugfix", ""); err != nil {
		t.Fatalf("InitWorkflow failed: %v", err)
	}

	path := filepath.Join(dir, ".orc", "workflows", "bugfix.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("workflow file not created: %v", err)
	}
	if !strings.Contains(string(data), "name: bugfix") {
		t.Fatalf("workflow content missing name, got: %s", data)
	}
}

func TestInitWorkflow_FailsIfNoOrcDir(t *testing.T) {
	dir := t.TempDir()
	err := InitWorkflow(dir, "bugfix", "")
	if err == nil {
		t.Fatal("expected error when no .orc/ dir")
	}
	if !strings.Contains(err.Error(), "orc init") {
		t.Fatalf("error should mention 'orc init', got: %v", err)
	}
}

func TestInitWorkflow_FailsIfAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755)
	os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("existing"), 0644)

	err := InitWorkflow(dir, "bugfix", "")
	if err == nil {
		t.Fatal("expected error for existing workflow")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error should mention 'already exists', got: %v", err)
	}
}

func TestInitWorkflow_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc"), 0755)

	cases := []string{"../evil", "../../etc/evil", "foo/bar", "..", "."}
	for _, name := range cases {
		err := InitWorkflow(dir, name, "")
		if err == nil {
			t.Fatalf("expected error for workflow name %q", name)
		}
		if !strings.Contains(err.Error(), "must not contain path separators") {
			t.Fatalf("for %q: expected path-traversal error, got: %v", name, err)
		}
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

func TestWriteBlocks_ErrorOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	blocks := []fileblocks.FileBlock{
		{Path: ".orc/config.yaml", Content: "name: test\n"},
	}

	_, err := writeBlocks(blocker, blocks)
	if err == nil {
		t.Fatal("expected error when target dir is a file, got nil")
	}
	if !strings.Contains(err.Error(), ".orc/config.yaml") {
		t.Fatalf("error should reference the failing path, got: %v", err)
	}
}

func TestListRecipes(t *testing.T) {
	out := captureOutput(func() {
		ListRecipes()
	})
	if !strings.Contains(out, "Available recipes:") {
		t.Errorf("missing header 'Available recipes:'\noutput:\n%s", out)
	}
	for _, r := range AllRecipes() {
		if !strings.Contains(out, r.Name) {
			t.Errorf("missing recipe name %q\noutput:\n%s", r.Name, out)
		}
		if !strings.Contains(out, r.Description) {
			t.Errorf("missing description for %q: %q\noutput:\n%s", r.Name, r.Description, out)
		}
		if !strings.Contains(out, r.Workflow) {
			t.Errorf("missing workflow for %q: %q\noutput:\n%s", r.Name, r.Workflow, out)
		}
	}
	if !strings.Contains(out, "orc init --recipe") {
		t.Errorf("missing usage hint\noutput:\n%s", out)
	}
}

func TestInitWorkflow_WithValidRecipe(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc"), 0755)

	captureOutput(func() {
		if err := InitWorkflow(dir, "myworkflow", "simple"); err != nil {
			t.Fatalf("InitWorkflow failed: %v", err)
		}
	})

	data, err := os.ReadFile(filepath.Join(dir, ".orc", "workflows", "myworkflow.yaml"))
	if err != nil {
		t.Fatalf("workflow file not created: %v", err)
	}

	r, err := GetRecipe("simple")
	if err != nil {
		t.Fatalf("GetRecipe failed: %v", err)
	}
	want := r.Files[".orc/config.yaml"]
	if string(data) != want {
		t.Fatalf("content mismatch\ngot:  %q\nwant: %q", string(data), want)
	}
}

func TestInitWorkflow_UnknownRecipe(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc"), 0755)

	err := InitWorkflow(dir, "myworkflow", "nonexistent-recipe")
	if err == nil {
		t.Fatal("expected error for unknown recipe")
	}
	if !strings.Contains(err.Error(), "unknown recipe") {
		t.Fatalf("expected error containing 'unknown recipe', got: %v", err)
	}
}

func TestInitWorkflow_RecipeMissingConfig(t *testing.T) {
	stubGetRecipe(t, func(name string) (Recipe, error) {
		return Recipe{
			Name:  name,
			Files: map[string]string{".orc/phases/plan.md": "prompt content"},
		}, nil
	})

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc"), 0755)

	err := InitWorkflow(dir, "myworkflow", "broken-recipe")
	if err == nil {
		t.Fatal("expected error when recipe has no config.yaml")
	}
	if !strings.Contains(err.Error(), "has no config.yaml") {
		t.Fatalf("expected error containing 'has no config.yaml', got: %v", err)
	}
}
