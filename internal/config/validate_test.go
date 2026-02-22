package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func minimalConfig(phases ...Phase) *Config {
	return &Config{Name: "test", Phases: phases}
}

func scriptPhase(name string) Phase {
	return Phase{Name: name, Type: "script", Run: "echo ok"}
}

func TestValidate_NameRequired(t *testing.T) {
	cfg := &Config{Phases: []Phase{scriptPhase("a")}}
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "'name' is required") {
		t.Fatalf("expected name required error, got %v", err)
	}
}

func TestValidate_NoPhasesError(t *testing.T) {
	cfg := &Config{Name: "test"}
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "at least one phase") {
		t.Fatalf("expected phases error, got %v", err)
	}
}

func TestValidate_PhaseNameRequired(t *testing.T) {
	cfg := minimalConfig(Phase{Type: "script", Run: "echo"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "'name' is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_PhaseTypeRequired(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "'type' is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_DuplicatePhaseNames(t *testing.T) {
	cfg := minimalConfig(scriptPhase("dup"), scriptPhase("dup"))
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_UnknownPhaseType(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "unknown"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_AgentRequiresPrompt(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "agent"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "'prompt' is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_AgentPromptMissing(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "prompts/missing.md"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_AgentPromptExists(t *testing.T) {
	root := t.TempDir()
	promptDir := filepath.Join(root, "prompts")
	os.MkdirAll(promptDir, 0755)
	os.WriteFile(filepath.Join(promptDir, "design.md"), []byte("prompt"), 0644)

	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "prompts/design.md"})
	if err := Validate(cfg, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentDefaults(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)

	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md"})
	if err := Validate(cfg, root); err != nil {
		t.Fatal(err)
	}
	p := cfg.Phases[0]
	if p.Model != "opus" {
		t.Fatalf("Model = %q, want opus", p.Model)
	}
	if p.Timeout != 30 {
		t.Fatalf("Timeout = %d, want 30", p.Timeout)
	}
}

func TestValidate_ScriptRequiresRun(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "'run' is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_ScriptDefaultTimeout(t *testing.T) {
	cfg := minimalConfig(scriptPhase("a"))
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Timeout != 10 {
		t.Fatalf("Timeout = %d, want 10", cfg.Phases[0].Timeout)
	}
}

func TestValidate_InvalidModel(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Model: "gpt-4"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_ValidModels(t *testing.T) {
	for _, model := range []string{"", "opus", "sonnet", "haiku"} {
		cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Model: model})
		if err := Validate(cfg, t.TempDir()); err != nil {
			t.Fatalf("model %q: %v", model, err)
		}
	}
}

func TestValidate_NegativeTimeout(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Timeout: -1})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "timeout must be >= 0") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_OutputsNoPathSeparators(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Outputs: []string{"sub/file.md"}})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_OnFailGotoRequired(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", OnFail: &OnFail{Goto: ""}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "on-fail.goto is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_OnFailGotoMustBeEarlier(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", OnFail: &OnFail{Goto: "c"}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "must reference an earlier phase") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_OnFailGotoEarlierOK(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", OnFail: &OnFail{Goto: "a"}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_OnFailMaxDefault(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", OnFail: &OnFail{Goto: "a", Max: 0}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[1].OnFail.Max != 2 {
		t.Fatalf("Max = %d, want 2", cfg.Phases[1].OnFail.Max)
	}
}

func TestValidate_ParallelWithUnknown(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", ParallelWith: "nonexistent"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown phase") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_ParallelWithOnFail(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", ParallelWith: "a", OnFail: &OnFail{Goto: "a"}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected parallel+on-fail error, got %v", err)
	}
}

func TestValidate_GateMinimal(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "review", Type: "gate"})
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_FullConfig(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".orc", "prompts"), 0755)
	os.WriteFile(filepath.Join(root, ".orc", "prompts", "design.md"), []byte("design"), 0644)
	os.WriteFile(filepath.Join(root, ".orc", "prompts", "impl.md"), []byte("impl"), 0644)

	cfg := &Config{
		Name: "full-workflow",
		Phases: []Phase{
			{Name: "setup", Type: "script", Run: "echo setup"},
			{Name: "design", Type: "agent", Prompt: ".orc/prompts/design.md"},
			{Name: "review", Type: "gate"},
			{Name: "implement", Type: "agent", Prompt: ".orc/prompts/impl.md", Model: "sonnet", Timeout: 45,
				Outputs: []string{"result.md"},
				OnFail:  &OnFail{Goto: "design", Max: 3}},
			{Name: "test", Type: "script", Run: "make test", Condition: "test -f Makefile",
				OnFail: &OnFail{Goto: "implement"}},
		},
	}

	if err := Validate(cfg, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults were applied
	if cfg.Phases[0].Timeout != 10 {
		t.Fatalf("setup timeout = %d", cfg.Phases[0].Timeout)
	}
	if cfg.Phases[1].Model != "opus" {
		t.Fatalf("design model = %q", cfg.Phases[1].Model)
	}
	if cfg.Phases[1].Timeout != 30 {
		t.Fatalf("design timeout = %d", cfg.Phases[1].Timeout)
	}
	if cfg.Phases[3].Model != "sonnet" {
		t.Fatalf("implement model = %q", cfg.Phases[3].Model)
	}
	if cfg.Phases[4].OnFail.Max != 2 {
		t.Fatalf("test on-fail max = %d", cfg.Phases[4].OnFail.Max)
	}
}

func TestPhaseIndex_Found(t *testing.T) {
	cfg := &Config{Phases: []Phase{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	if idx := cfg.PhaseIndex("b"); idx != 1 {
		t.Fatalf("got %d, want 1", idx)
	}
}

func TestPhaseIndex_NotFound(t *testing.T) {
	cfg := &Config{Phases: []Phase{{Name: "a"}}}
	if idx := cfg.PhaseIndex("z"); idx != -1 {
		t.Fatalf("got %d, want -1", idx)
	}
}

func TestValidateTicket_EmptyPattern(t *testing.T) {
	if err := ValidateTicket("", "anything"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTicket_Match(t *testing.T) {
	if err := ValidateTicket(`^[A-Z]+-\d+$`, "ABC-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTicket_NoMatch(t *testing.T) {
	err := ValidateTicket(`^[A-Z]+-\d+$`, "bad-ticket")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected no-match error, got %v", err)
	}
}

func TestValidateTicket_InvalidRegex(t *testing.T) {
	err := ValidateTicket(`[invalid`, "ABC-123")
	if err == nil || !strings.Contains(err.Error(), "invalid ticket-pattern") {
		t.Fatalf("expected invalid pattern error, got %v", err)
	}
}

func TestValidateTicket_PartialMatchRejected(t *testing.T) {
	// Unanchored pattern should NOT match a ticket with trailing injection
	err := ValidateTicket(`[A-Z]+-\d+`, "PROJ-1 && rm -rf /")
	if err == nil {
		t.Fatal("expected partial match to be rejected")
	}
}

func TestValidateTicket_UnanchoredFullMatch(t *testing.T) {
	// Unanchored pattern should still match a valid ticket
	if err := ValidateTicket(`[A-Z]+-\d+`, "PROJ-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_VarsBuiltinOverride(t *testing.T) {
	cfg := &Config{
		Name: "test",
		Vars: OrderedVars{{Key: "TICKET", Value: "bad"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "overrides a built-in") {
		t.Fatalf("expected built-in override error, got %v", err)
	}
}

func TestValidate_VarsEmptyName(t *testing.T) {
	cfg := &Config{
		Name: "test",
		Vars: OrderedVars{{Key: "", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "empty variable name") {
		t.Fatalf("expected empty name error, got %v", err)
	}
}

func TestValidate_VarsDuplicate(t *testing.T) {
	cfg := &Config{
		Name: "test",
		Vars: OrderedVars{{Key: "FOO", Value: "1"}, {Key: "FOO", Value: "2"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "duplicate variable") {
		t.Fatalf("expected duplicate var error, got %v", err)
	}
}

func TestValidate_VarsCustomAccepted(t *testing.T) {
	cfg := &Config{
		Name: "test",
		Vars: OrderedVars{{Key: "MY_DIR", Value: "/tmp/test"}},
		Phases: []Phase{scriptPhase("a")},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_GateCwdRejected(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "g", Type: "gate", Cwd: "/tmp"})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not supported on gate") {
		t.Fatalf("expected gate+cwd error, got %v", err)
	}
}

func TestValidate_ScriptCwdAccepted(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "s", Type: "script", Run: "echo", Cwd: "/tmp"})
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentCwdAccepted(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", Cwd: "/tmp"})
	if err := Validate(cfg, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
