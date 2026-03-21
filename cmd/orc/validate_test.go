package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

// --- runValidate tests ---

func TestRunValidate_ValidConfig(t *testing.T) {
	root := t.TempDir()
	cfgContent := `name: test-wf
phases:
  - name: build
    type: script
    run: make build
`
	configPath := filepath.Join(root, "config.yaml")
	os.WriteFile(configPath, []byte(cfgContent), 0644)

	cfg, err := runValidate(configPath, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "test-wf" {
		t.Fatalf("Name = %q, want test-wf", cfg.Name)
	}
	if len(cfg.Phases) != 1 {
		t.Fatalf("Phases = %d, want 1", len(cfg.Phases))
	}
}

func TestRunValidate_InvalidConfig(t *testing.T) {
	root := t.TempDir()
	cfgContent := `name: test
phases:
  - type: script
    run: echo
`
	configPath := filepath.Join(root, "config.yaml")
	os.WriteFile(configPath, []byte(cfgContent), 0644)

	_, err := runValidate(configPath, root)
	if err == nil || !strings.Contains(err.Error(), "'name' is required") {
		t.Fatalf("expected name required error, got %v", err)
	}
}

func TestRunValidate_ConfigFlag(t *testing.T) {
	root := t.TempDir()
	customDir := filepath.Join(root, "custom")
	os.MkdirAll(customDir, 0755)

	cfgContent := `name: custom-wf
phases:
  - name: test
    type: script
    run: echo ok
`
	configPath := filepath.Join(customDir, "myconfig.yaml")
	os.WriteFile(configPath, []byte(cfgContent), 0644)

	cfg, err := runValidate(configPath, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "custom-wf" {
		t.Fatalf("Name = %q, want custom-wf", cfg.Name)
	}
}

func TestRunValidate_MissingConfig(t *testing.T) {
	_, err := runValidate("/nonexistent/path.yaml", "/tmp")
	if err == nil || !strings.Contains(err.Error(), "reading config") {
		t.Fatalf("expected reading config error, got %v", err)
	}
}

func TestRunValidate_InvalidYAML(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "bad.yaml")
	os.WriteFile(configPath, []byte("{{{{"), 0644)

	_, err := runValidate(configPath, root)
	if err == nil || !strings.Contains(err.Error(), "parsing config") {
		t.Fatalf("expected parsing config error, got %v", err)
	}
}

func TestRunValidate_AgentPromptResolution(t *testing.T) {
	root := t.TempDir()
	promptDir := filepath.Join(root, "prompts")
	os.MkdirAll(promptDir, 0755)
	os.WriteFile(filepath.Join(promptDir, "plan.md"), []byte("plan prompt"), 0644)

	cfgContent := `name: agent-wf
phases:
  - name: plan
    type: agent
    prompt: prompts/plan.md
`
	configPath := filepath.Join(root, "config.yaml")
	os.WriteFile(configPath, []byte(cfgContent), 0644)

	cfg, err := runValidate(configPath, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Phases[0].Prompt != "prompts/plan.md" {
		t.Fatalf("Prompt = %q", cfg.Phases[0].Prompt)
	}
}

// --- printConfigSummary tests ---

func TestPrintConfigSummary_BasicOutput(t *testing.T) {
	cfg := &config.Config{
		Name: "test-wf",
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "make build", Timeout: 10},
		},
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, "/tmp")
	out := buf.String()

	for _, want := range []string{
		"Config valid",
		"test-wf",
		"1 phase",
		"TICKET",
		"(built-in)",
		"build",
		"script",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestPrintConfigSummary_WithVars(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Vars: config.OrderedVars{
			{Key: "FOO", Value: "bar"},
			{Key: "BAZ", Value: "$FOO/sub"},
		},
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Timeout: 10},
		},
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, "/tmp")
	out := buf.String()

	if !strings.Contains(out, "FOO = bar") {
		t.Errorf("output missing FOO = bar\noutput:\n%s", out)
	}
	if !strings.Contains(out, "(from config)") {
		t.Errorf("output missing (from config)\noutput:\n%s", out)
	}
	if !strings.Contains(out, "BAZ = bar/sub") {
		t.Errorf("output missing BAZ = bar/sub\noutput:\n%s", out)
	}
}

func TestPrintConfigSummary_Loop(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "check", Type: "script", Run: "make test"},
			{Name: "build", Type: "script", Run: "make build", Loop: &config.Loop{Goto: "check", Min: 1, Max: 3}},
		},
	}
	if err := config.Validate(cfg, root); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, root)
	out := buf.String()

	if !strings.Contains(out, "loop: goto check (min 1, max 3)") {
		t.Errorf("output missing loop line\noutput:\n%s", out)
	}
}

func TestPrintConfigSummary_LoopCheck(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "check", Type: "script", Run: "make test"},
			{Name: "build", Type: "script", Run: "make build", Loop: &config.Loop{Goto: "check", Min: 1, Max: 3, Check: "test -f review-pass.txt"}},
		},
	}
	if err := config.Validate(cfg, root); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, root)
	out := buf.String()

	if !strings.Contains(out, "loop.check: test -f review-pass.txt") {
		t.Errorf("output missing loop.check line\noutput:\n%s", out)
	}
}

func TestPrintConfigSummary_ParallelWith(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "check", Type: "script", Run: "make test"},
			{Name: "lint", Type: "script", Run: "make lint", ParallelWith: "check"},
		},
	}
	if err := config.Validate(cfg, root); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, root)
	out := buf.String()

	if !strings.Contains(out, "parallel-with: check") {
		t.Errorf("output missing parallel-with\noutput:\n%s", out)
	}
}

func TestPrintConfigSummary_TicketPattern(t *testing.T) {
	cfg := &config.Config{
		Name:          "test",
		TicketPattern: `^[A-Z]+-\d+$`,
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Timeout: 10},
		},
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, "/tmp")
	out := buf.String()

	if !strings.Contains(out, "ticket-pattern:") {
		t.Errorf("output missing ticket-pattern\noutput:\n%s", out)
	}
	if !strings.Contains(out, `^[A-Z]+-\d+$`) {
		t.Errorf("output missing regex pattern\noutput:\n%s", out)
	}
}

func TestPrintConfigSummary_AgentPhase(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "p.md"), []byte("prompt"), 0644)

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Prompt: "p.md"},
		},
	}
	if err := config.Validate(cfg, root); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, root)
	out := buf.String()

	for _, want := range []string{"model=opus", "effort=high", "timeout=30m", "prompt="} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestPrintConfigSummary_Hooks(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "make build", Timeout: 10, PreRun: "echo before", PostRun: "echo after"},
		},
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, "/tmp")
	out := buf.String()

	for _, want := range []string{"pre-run: echo before", "post-run: echo after"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestPrintConfigSummary_HooksTruncation(t *testing.T) {
	long := strings.Repeat("x", 80)
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "make build", Timeout: 10, PreRun: long},
		},
	}

	var buf bytes.Buffer
	printConfigSummary(&buf, cfg, "/tmp")
	out := buf.String()

	if !strings.Contains(out, "...") {
		t.Errorf("long pre-run not truncated\noutput:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Errorf("full long string should not appear\noutput:\n%s", out)
	}
}

func TestValidateMultiWorkflow(t *testing.T) {
	root := t.TempDir()
	orcDir := filepath.Join(root, ".orc")
	wfDir := filepath.Join(orcDir, "workflows")
	os.MkdirAll(wfDir, 0755)

	validCfg := `name: default-wf
phases:
  - name: build
    type: script
    run: make build
`
	os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte(validCfg), 0644)

	bugfixCfg := `name: bugfix-wf
phases:
  - name: fix
    type: script
    run: echo fix
`
	os.WriteFile(filepath.Join(wfDir, "bugfix.yaml"), []byte(bugfixCfg), 0644)

	// Both configs should validate independently without error
	cfg1, err := runValidate(filepath.Join(orcDir, "config.yaml"), root)
	if err != nil {
		t.Fatalf("default config validation failed: %v", err)
	}
	if cfg1.Name != "default-wf" {
		t.Errorf("Name = %q, want default-wf", cfg1.Name)
	}

	cfg2, err := runValidate(filepath.Join(wfDir, "bugfix.yaml"), root)
	if err != nil {
		t.Fatalf("bugfix config validation failed: %v", err)
	}
	if cfg2.Name != "bugfix-wf" {
		t.Errorf("Name = %q, want bugfix-wf", cfg2.Name)
	}
}
