package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	if p.Effort != "high" {
		t.Fatalf("Effort = %q, want high", p.Effort)
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

func TestValidate_InvalidEffort(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Effort: "extreme"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown effort") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_ValidEfforts(t *testing.T) {
	for _, effort := range []string{"", "low", "medium", "high"} {
		cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", Effort: effort})
		if err := Validate(cfg, t.TempDir()); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
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

func TestValidate_OnFailRejectedWithHint(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", OnFail: &OnFail{Goto: "a", Max: 2}},
	)
	err := Validate(cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected on-fail rejection error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "replaced by 'loop'") {
		t.Fatalf("expected migration hint, got: %v", err)
	}
	if !strings.Contains(msg, "max: 3") {
		t.Fatalf("expected max: 3 (on-fail.max+1) in hint, got: %v", err)
	}
}

func TestValidate_LoopGotoRequired(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "", Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "loop.goto is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_LoopGotoMustBeEarlier(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "c", Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "must reference an earlier phase") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_LoopGotoEarlierOK(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LoopMaxRequired(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 0}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "loop.max is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_LoopMaxLessThanMin(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Min: 5, Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "loop.max (3) must be >= loop.min (5)") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_LoopMinDefault(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[1].Loop.Min != 1 {
		t.Fatalf("Min = %d, want 1", cfg.Phases[1].Loop.Min)
	}
}

func TestValidate_LoopMultipleAllowed(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3}},
		scriptPhase("c"),
		Phase{Name: "d", Type: "script", Run: "echo", Loop: &Loop{Goto: "c", Max: 2}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_OnExhaustGotoMustBeEarlier(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3, OnExhaust: &OnExhaust{Goto: "nonexistent"}}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "on-exhaust.goto") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_OnExhaustMaxDefault(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3, OnExhaust: &OnExhaust{Goto: "a"}}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[1].Loop.OnExhaust.Max != 1 {
		t.Fatalf("OnExhaust.Max = %d, want 1", cfg.Phases[1].Loop.OnExhaust.Max)
	}
}

func TestValidate_ParallelWithUnknown(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", ParallelWith: "nonexistent"})
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown phase") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_ParallelWithLoop(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", ParallelWith: "a", Loop: &Loop{Goto: "a", Max: 3}},
	)
	if err := Validate(cfg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected parallel+loop error, got %v", err)
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
				Loop:    &Loop{Goto: "design", Max: 3}},
			{Name: "test", Type: "script", Run: "make test", Condition: "test -f Makefile",
				Loop: &Loop{Goto: "implement", Max: 2}},
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
	if cfg.Phases[1].Effort != "high" {
		t.Fatalf("design effort = %q", cfg.Phases[1].Effort)
	}
	if cfg.Phases[1].Timeout != 30 {
		t.Fatalf("design timeout = %d", cfg.Phases[1].Timeout)
	}
	if cfg.Phases[3].Model != "sonnet" {
		t.Fatalf("implement model = %q", cfg.Phases[3].Model)
	}
	if cfg.Phases[3].Loop.Min != 1 {
		t.Fatalf("implement loop.min = %d, want 1", cfg.Phases[3].Loop.Min)
	}
	if cfg.Phases[4].Loop.Min != 1 {
		t.Fatalf("test loop.min = %d, want 1", cfg.Phases[4].Loop.Min)
	}
}

// OnExhaust YAML parsing tests

func TestValidate_OnExhaustStringForm(t *testing.T) {
	yamlStr := `
name: test
phases:
  - name: a
    type: script
    run: echo
  - name: b
    type: script
    run: echo
    loop:
      goto: a
      max: 3
      on-exhaust: a
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if err := Validate(&cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Phases[1].Loop.OnExhaust.Goto != "a" {
		t.Fatalf("OnExhaust.Goto = %q, want a", cfg.Phases[1].Loop.OnExhaust.Goto)
	}
	if cfg.Phases[1].Loop.OnExhaust.Max != 1 {
		t.Fatalf("OnExhaust.Max = %d, want 1 (default)", cfg.Phases[1].Loop.OnExhaust.Max)
	}
}

func TestValidate_OnExhaustObjectForm(t *testing.T) {
	yamlStr := `
name: test
phases:
  - name: a
    type: script
    run: echo
  - name: b
    type: script
    run: echo
    loop:
      goto: a
      max: 3
      on-exhaust:
        goto: a
        max: 2
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if err := Validate(&cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Phases[1].Loop.OnExhaust.Goto != "a" {
		t.Fatalf("OnExhaust.Goto = %q, want a", cfg.Phases[1].Loop.OnExhaust.Goto)
	}
	if cfg.Phases[1].Loop.OnExhaust.Max != 2 {
		t.Fatalf("OnExhaust.Max = %d, want 2", cfg.Phases[1].Loop.OnExhaust.Max)
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
		Name:   "test",
		Vars:   OrderedVars{{Key: "TICKET", Value: "bad"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "overrides a built-in") {
		t.Fatalf("expected built-in override error, got %v", err)
	}
}

func TestValidate_VarsEmptyName(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "empty variable name") {
		t.Fatalf("expected empty name error, got %v", err)
	}
}

func TestValidate_VarsDuplicate(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "FOO", Value: "1"}, {Key: "FOO", Value: "2"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "duplicate variable") {
		t.Fatalf("expected duplicate var error, got %v", err)
	}
}

func TestValidate_VarsCustomAccepted(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "MY_DIR", Value: "/tmp/test"}},
		Phases: []Phase{scriptPhase("a")},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_GateCwdWithoutRunRejected(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "g", Type: "gate", Cwd: "/tmp"})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "'cwd' requires 'run'") {
		t.Fatalf("expected gate+cwd error, got %v", err)
	}
}

func TestValidate_GateRunWithCwdAllowed(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "g", Type: "gate", Run: "cat plan.md", Cwd: "/tmp"})
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_GateRunWithoutCwdAllowed(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "g", Type: "gate", Run: "echo hello"})
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
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

// Fix 2+9: UnmarshalYAML scalar validation and error prefix

func TestUnmarshalYAML_RejectsNonScalarValue(t *testing.T) {
	input := "FOO:\n  nested: value\n"
	var ov OrderedVars
	err := yaml.Unmarshal([]byte(input), &ov)
	if err == nil || !strings.Contains(err.Error(), "not a scalar") {
		t.Fatalf("expected non-scalar error, got %v", err)
	}
	if !strings.Contains(err.Error(), "config: vars:") {
		t.Fatalf("expected 'config: vars:' prefix, got %v", err)
	}
}

func TestUnmarshalYAML_AcceptsScalarValues(t *testing.T) {
	input := "FOO: bar\nBAZ: \"123\"\n"
	var ov OrderedVars
	if err := yaml.Unmarshal([]byte(input), &ov); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ov) != 2 || ov[0].Key != "FOO" || ov[0].Value != "bar" || ov[1].Key != "BAZ" || ov[1].Value != "123" {
		t.Fatalf("unexpected result: %v", ov)
	}
}

func TestUnmarshalYAML_RejectsNonMapping(t *testing.T) {
	input := "- item1\n- item2\n"
	var ov OrderedVars
	err := yaml.Unmarshal([]byte(input), &ov)
	if err == nil || !strings.Contains(err.Error(), "must be a mapping") {
		t.Fatalf("expected mapping error, got %v", err)
	}
	if !strings.Contains(err.Error(), "config: vars:") {
		t.Fatalf("expected 'config: vars:' prefix, got %v", err)
	}
}

// Fix 3: Variable name format validation

func TestValidate_VarsInvalidName_Hyphen(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "my-var", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a valid variable name") {
		t.Fatalf("expected invalid name error, got %v", err)
	}
}

func TestValidate_VarsInvalidName_StartsWithDigit(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "1FOO", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a valid variable name") {
		t.Fatalf("expected invalid name error, got %v", err)
	}
}

func TestValidate_VarsInvalidName_Spaces(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "MY VAR", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a valid variable name") {
		t.Fatalf("expected invalid name error, got %v", err)
	}
}

func TestValidate_VarsInvalidName_Equals(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "FOO=BAR", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a valid variable name") {
		t.Fatalf("expected invalid name error, got %v", err)
	}
}

func TestValidate_VarsValidName_Underscore(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "_MY_VAR_2", Value: "val"}},
		Phases: []Phase{scriptPhase("a")},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Fix 4: PHASE_INDEX and PHASE_COUNT in builtins blocklist

func TestValidate_VarsBuiltinOverride_PhaseIndex(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "PHASE_INDEX", Value: "0"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "overrides a built-in") {
		t.Fatalf("expected built-in override error, got %v", err)
	}
}

// allow-tools validation

func TestValidate_AllowToolsOnAgent(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", AllowTools: []string{"Read", "Bash"}})
	if err := Validate(cfg, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AllowToolsOnScript(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", AllowTools: []string{"Read"}})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("expected allow-tools error, got %v", err)
	}
}

func TestValidate_AllowToolsOnGate(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "gate", AllowTools: []string{"Read"}})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("expected allow-tools error, got %v", err)
	}
}

func TestValidate_AllowToolsEmptyEntry(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", AllowTools: []string{"Read", ""}})
	err := Validate(cfg, root)
	if err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("expected non-empty error, got %v", err)
	}
}

func TestValidate_AllowToolsWhitespaceEntry(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", AllowTools: []string{"  "}})
	err := Validate(cfg, root)
	if err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("expected non-empty error, got %v", err)
	}
}

func TestValidate_DefaultAllowToolsValid(t *testing.T) {
	cfg := &Config{
		Name:              "test",
		DefaultAllowTools: []string{"mcp__atlassian__*", "Bash"},
		Phases:            []Phase{scriptPhase("a")},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_DefaultAllowToolsEmptyEntry(t *testing.T) {
	cfg := &Config{
		Name:              "test",
		DefaultAllowTools: []string{"Bash", ""},
		Phases:            []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "default-allow-tools") {
		t.Fatalf("expected default-allow-tools error, got %v", err)
	}
}

func TestValidate_DefaultAllowToolsWhitespaceEntry(t *testing.T) {
	cfg := &Config{
		Name:              "test",
		DefaultAllowTools: []string{"  "},
		Phases:            []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "default-allow-tools") {
		t.Fatalf("expected default-allow-tools error, got %v", err)
	}
}

// Top-level defaults tests

func TestValidate_TopLevelModelInherited(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := &Config{
		Name:   "test",
		Model:  "sonnet",
		Phases: []Phase{{Name: "a", Type: "agent", Prompt: "p.md"}},
	}
	if err := Validate(cfg, root); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Model != "sonnet" {
		t.Fatalf("Model = %q, want sonnet", cfg.Phases[0].Model)
	}
}

func TestValidate_TopLevelEffortInherited(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := &Config{
		Name:   "test",
		Effort: "low",
		Phases: []Phase{{Name: "a", Type: "agent", Prompt: "p.md"}},
	}
	if err := Validate(cfg, root); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Effort != "low" {
		t.Fatalf("Effort = %q, want low", cfg.Phases[0].Effort)
	}
}

func TestValidate_TopLevelCwdInheritedByAgent(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := &Config{
		Name:   "test",
		Cwd:    "/work",
		Phases: []Phase{{Name: "a", Type: "agent", Prompt: "p.md"}},
	}
	if err := Validate(cfg, root); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Cwd != "/work" {
		t.Fatalf("Cwd = %q, want /work", cfg.Phases[0].Cwd)
	}
}

func TestValidate_TopLevelCwdInheritedByScript(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Cwd:    "/work",
		Phases: []Phase{scriptPhase("a")},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Cwd != "/work" {
		t.Fatalf("Cwd = %q, want /work", cfg.Phases[0].Cwd)
	}
}

func TestValidate_TopLevelCwdNotInheritedByGateWithoutRun(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Cwd:    "/work",
		Phases: []Phase{{Name: "g", Type: "gate"}},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Cwd != "" {
		t.Fatalf("Cwd = %q, want empty (gate without run should not inherit cwd)", cfg.Phases[0].Cwd)
	}
}

func TestValidate_TopLevelCwdInheritedByGateWithRun(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Cwd:    "/work",
		Phases: []Phase{{Name: "g", Type: "gate", Run: "ls"}},
	}
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if cfg.Phases[0].Cwd != "/work" {
		t.Fatalf("Cwd = %q, want /work (gate with run should inherit cwd)", cfg.Phases[0].Cwd)
	}
}

func TestValidate_PerPhaseOverridesTopLevel(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := &Config{
		Name:   "test",
		Model:  "sonnet",
		Effort: "low",
		Cwd:    "/default",
		Phases: []Phase{{Name: "a", Type: "agent", Prompt: "p.md", Model: "haiku", Effort: "medium", Cwd: "/override"}},
	}
	if err := Validate(cfg, root); err != nil {
		t.Fatal(err)
	}
	p := cfg.Phases[0]
	if p.Model != "haiku" {
		t.Fatalf("Model = %q, want haiku", p.Model)
	}
	if p.Effort != "medium" {
		t.Fatalf("Effort = %q, want medium", p.Effort)
	}
	if p.Cwd != "/override" {
		t.Fatalf("Cwd = %q, want /override", p.Cwd)
	}
}

func TestValidate_NoTopLevelDefaultsApply(t *testing.T) {
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
	if p.Effort != "high" {
		t.Fatalf("Effort = %q, want high", p.Effort)
	}
	if p.Cwd != "" {
		t.Fatalf("Cwd = %q, want empty", p.Cwd)
	}
}

func TestValidate_TopLevelInvalidModel(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Model:  "gpt-4",
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("expected unknown model error, got %v", err)
	}
}

func TestValidate_TopLevelInvalidEffort(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Effort: "extreme",
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown effort") {
		t.Fatalf("expected unknown effort error, got %v", err)
	}
}

func TestValidate_TopLevelValidModels(t *testing.T) {
	for _, model := range []string{"", "opus", "sonnet", "haiku"} {
		cfg := &Config{
			Name:   "test",
			Model:  model,
			Phases: []Phase{scriptPhase("a")},
		}
		if err := Validate(cfg, t.TempDir()); err != nil {
			t.Fatalf("model %q: %v", model, err)
		}
	}
}

func TestValidate_TopLevelValidEfforts(t *testing.T) {
	for _, effort := range []string{"", "low", "medium", "high"} {
		cfg := &Config{
			Name:   "test",
			Effort: effort,
			Phases: []Phase{scriptPhase("a")},
		}
		if err := Validate(cfg, t.TempDir()); err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
	}
}

// max-cost validation tests

func TestValidate_MaxCostNegative(t *testing.T) {
	cfg := minimalConfig(scriptPhase("a"))
	cfg.MaxCost = -1.0
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "'max-cost' must not be negative") {
		t.Fatalf("expected negative max-cost error, got %v", err)
	}
}

func TestValidate_MaxCostPositive(t *testing.T) {
	cfg := minimalConfig(scriptPhase("a"))
	cfg.MaxCost = 10.0
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MaxCostZero(t *testing.T) {
	cfg := minimalConfig(scriptPhase("a"))
	cfg.MaxCost = 0
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PhaseMaxCostNegative(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", MaxCost: -5.0})
	err := Validate(cfg, root)
	if err == nil || !strings.Contains(err.Error(), "'max-cost' must not be negative") {
		t.Fatalf("expected negative phase max-cost error, got %v", err)
	}
}

func TestValidate_PhaseMaxCostPositive(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0644)
	cfg := minimalConfig(Phase{Name: "a", Type: "agent", Prompt: "p.md", MaxCost: 5.0})
	if err := Validate(cfg, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PhaseMaxCostOnScript(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "script", Run: "echo", MaxCost: 5.0})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("expected agent-only error, got %v", err)
	}
}

func TestValidate_PhaseMaxCostOnGate(t *testing.T) {
	cfg := minimalConfig(Phase{Name: "a", Type: "gate", MaxCost: 5.0})
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "only valid on agent") {
		t.Fatalf("expected agent-only error, got %v", err)
	}
}

func TestValidate_LoopCheckAccepted(t *testing.T) {
	cfg := minimalConfig(
		scriptPhase("a"),
		Phase{Name: "b", Type: "script", Run: "echo", Loop: &Loop{Goto: "a", Max: 3, Check: "test -f review-pass.txt"}},
	)
	if err := Validate(cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LoopCheckYAMLParsing(t *testing.T) {
	yamlStr := `
name: test
phases:
  - name: a
    type: script
    run: echo
  - name: b
    type: script
    run: echo
    loop:
      goto: a
      max: 3
      check: test -f review-pass.txt
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if err := Validate(&cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Phases[1].Loop.Check != "test -f review-pass.txt" {
		t.Fatalf("Loop.Check = %q, want %q", cfg.Phases[1].Loop.Check, "test -f review-pass.txt")
	}
}

func TestValidate_VarsBuiltinOverride_PhaseCount(t *testing.T) {
	cfg := &Config{
		Name:   "test",
		Vars:   OrderedVars{{Key: "PHASE_COUNT", Value: "5"}},
		Phases: []Phase{scriptPhase("a")},
	}
	err := Validate(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "overrides a built-in") {
		t.Fatalf("expected built-in override error, got %v", err)
	}
}
