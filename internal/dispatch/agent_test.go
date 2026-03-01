package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestRenderAndSavePrompt_InjectsFeedback(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}

	// Write a minimal prompt template file
	promptDir := filepath.Join(dir, ".orc", "phases")
	os.MkdirAll(promptDir, 0755)
	promptFile := filepath.Join(promptDir, "implement.md")
	os.WriteFile(promptFile, []byte("Implement ticket $TICKET in $WORK_DIR."), 0644)

	// Write feedback from a previous failed phase
	if err := state.WriteFeedback(artDir, "review", "review found bugs: missing error handling"); err != nil {
		t.Fatal(err)
	}

	env := &Environment{
		ProjectRoot:  dir,
		WorkDir:      "/work",
		ArtifactsDir: artDir,
		Ticket:       "TEST-42",
		PhaseIndex:   0,
	}
	phase := config.Phase{
		Name:   "implement",
		Type:   "agent",
		Prompt: ".orc/phases/implement.md",
	}

	rendered, err := RenderAndSavePrompt(phase, env)
	if err != nil {
		t.Fatal(err)
	}

	// Verify variable expansion worked
	if !strings.Contains(rendered, "TEST-42") {
		t.Fatalf("rendered prompt missing expanded TICKET; got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "/work") {
		t.Fatalf("rendered prompt missing expanded WORK_DIR; got:\n%s", rendered)
	}

	// Verify feedback was injected into the rendered prompt
	if !strings.Contains(rendered, "--- Feedback from review ---") {
		t.Fatalf("rendered prompt missing feedback header; got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "review found bugs: missing error handling") {
		t.Fatalf("rendered prompt missing feedback content; got:\n%s", rendered)
	}

	// Verify the prompt was saved to the correct artifacts/prompts/ path
	savedPath := state.PromptPath(artDir, 0)
	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("prompt file not saved: %v", err)
	}
	saved := string(savedData)
	if saved != rendered {
		t.Fatalf("saved prompt differs from returned prompt;\nsaved:\n%s\nreturned:\n%s", saved, rendered)
	}
	if !strings.Contains(saved, "review found bugs: missing error handling") {
		t.Fatalf("saved prompt file missing feedback; got:\n%s", saved)
	}
}

func TestRenderAndSavePrompt_NoFeedback(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}

	promptDir := filepath.Join(dir, ".orc", "phases")
	os.MkdirAll(promptDir, 0755)
	promptFile := filepath.Join(promptDir, "plan.md")
	os.WriteFile(promptFile, []byte("Plan the work for $TICKET."), 0644)

	env := &Environment{
		ProjectRoot:  dir,
		WorkDir:      "/work",
		ArtifactsDir: artDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
	}
	phase := config.Phase{
		Name:   "plan",
		Type:   "agent",
		Prompt: ".orc/phases/plan.md",
	}

	rendered, err := RenderAndSavePrompt(phase, env)
	if err != nil {
		t.Fatal(err)
	}

	// No feedback should be appended
	if strings.Contains(rendered, "Feedback") {
		t.Fatalf("rendered prompt should not contain feedback; got:\n%s", rendered)
	}
	if rendered != "Plan the work for TEST-1." {
		t.Fatalf("unexpected rendered prompt: %q", rendered)
	}
}
