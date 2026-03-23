package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/runner"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
)

func TestDiscoverWorkflows_None(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	got := discoverWorkflows(dir)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDiscoverWorkflows_WithWorkflows(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "refactor.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	got := discoverWorkflows(dir)
	if len(got) != 2 {
		t.Fatalf("expected 2 workflows, got %d: %v", len(got), got)
	}
	found := map[string]bool{"bugfix": false, "refactor": false}
	for _, name := range got {
		found[name] = true
	}
	for name, ok := range found {
		if !ok {
			t.Fatalf("expected workflow %q in results, got %v", name, got)
		}
	}
}

func TestResolveWorkflow_SingleConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	if err := os.WriteFile(configPath, []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	name, path, err := resolveWorkflow(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty workflowName, got %q", name)
	}
	if path != configPath {
		t.Fatalf("expected %q, got %q", configPath, path)
	}
}

func TestResolveWorkflow_ExplicitFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(dir, ".orc", "workflows", "bugfix.yaml")
	if err := os.WriteFile(wfPath, []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	name, path, err := resolveWorkflow(dir, "bugfix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "bugfix" {
		t.Fatalf("expected workflowName %q, got %q", "bugfix", name)
	}
	if path != wfPath {
		t.Fatalf("expected %q, got %q", wfPath, path)
	}
}

func TestResolveWorkflow_DefaultWithConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	if err := os.WriteFile(configPath, []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	name, path, err := resolveWorkflow(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "default" {
		t.Fatalf("expected workflowName %q, got %q", "default", name)
	}
	if path != configPath {
		t.Fatalf("expected %q, got %q", configPath, path)
	}
}

func TestResolveWorkflow_SoleWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(dir, ".orc", "workflows", "bugfix.yaml")
	if err := os.WriteFile(wfPath, []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	name, path, err := resolveWorkflow(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "bugfix" {
		t.Fatalf("expected workflowName %q, got %q", "bugfix", name)
	}
	if path != wfPath {
		t.Fatalf("expected %q, got %q", wfPath, path)
	}
}

func TestResolveWorkflow_MultipleNoDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "refactor.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveWorkflow(dir, "")
	if err == nil {
		t.Fatal("expected error for multiple workflows with no default")
	}
	if !strings.Contains(err.Error(), "specify one with -w") {
		t.Fatalf("expected 'specify one with -w' in error, got: %v", err)
	}
}

func TestResolveWorkflow_UnknownName(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveWorkflow(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown workflow name")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestResolveWorkflowByName(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	wfPath := filepath.Join(dir, ".orc", "workflows", "bugfix.yaml")
	if err := os.WriteFile(wfPath, []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	path, ok := resolveWorkflowByName(dir, "bugfix")
	if !ok {
		t.Fatal("expected to find bugfix workflow")
	}
	if path != wfPath {
		t.Fatalf("expected %q, got %q", wfPath, path)
	}

	_, ok = resolveWorkflowByName(dir, "missing")
	if ok {
		t.Fatal("expected not to find missing workflow")
	}
}

func TestResolveWorkflowByName_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create config.yaml so ../config would resolve to a real file without the guard
	if err := os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []string{"../config", "../../etc", "foo/bar", "..", "."}
	for _, name := range cases {
		_, ok := resolveWorkflowByName(dir, name)
		if ok {
			t.Fatalf("expected not-found for traversal name %q", name)
		}
	}

	// Verify valid name still works
	_, ok := resolveWorkflowByName(dir, "bugfix")
	if !ok {
		t.Fatal("expected to find bugfix workflow")
	}
}

func TestResolveWorkflow_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []string{"../evil", "../../etc", "foo/bar", "..", "."}
	for _, name := range cases {
		_, _, err := resolveWorkflow(dir, name)
		if err == nil {
			t.Fatalf("expected error for workflow name %q", name)
		}
		if !strings.Contains(err.Error(), "must not contain path separators") {
			t.Fatalf("for %q: expected path-traversal error, got: %v", name, err)
		}
	}
}

func TestResolveWorkflow_FlagWithNoWorkflowsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("phases: []"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveWorkflow(dir, "bugfix")
	if err == nil {
		t.Fatal("expected error when --workflow specified without workflows dir")
	}
	if !strings.Contains(err.Error(), "no .orc/workflows/ directory") {
		t.Fatalf("expected 'no workflows dir' error, got: %v", err)
	}
}

func TestShouldArchiveStale(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{state.StatusRunning, true},
		{state.StatusCompleted, true},
		{state.StatusFailed, true},
		{state.StatusInterrupted, true},
		{"unknown", true},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			got := shouldArchiveStale(tc.status)
			if got != tc.want {
				t.Errorf("shouldArchiveStale(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestFindProjectRoot_WorkflowsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	got, err := findProjectRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %q, got %q", dir, got)
	}
}

func TestCancelCmd_MissingTicket_ExitConfigError(t *testing.T) {
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{cancelCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "cancel"})
	if err == nil {
		t.Fatal("expected error for missing ticket arg")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

func TestStatusCmd_BadTicket_ExitConfigError(t *testing.T) {
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{statusCmd()},
	}
	// statusCmd without args is valid (shows all tickets), so use bad path to trigger validateTicketPath
	err := app.Run(context.Background(), []string{"orc", "status", "../evil"})
	if err == nil {
		t.Fatal("expected error for bad ticket path")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

func TestDoctorCmd_MissingTicket_ExitConfigError(t *testing.T) {
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{doctorCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "doctor"})
	if err == nil {
		t.Fatal("expected error for missing ticket arg")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

func TestRunCmd_HeadlessAndStepMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	// Save and restore globals that EnableQuiet() mutates
	origQuiet := ux.QuietMode
	origReset := ux.Reset
	origBold := ux.Bold
	origDim := ux.Dim
	origRed := ux.Red
	origGreen := ux.Green
	origYellow := ux.Yellow
	origCyan := ux.Cyan
	origMagenta := ux.Magenta
	origBlue := ux.Blue
	origBoldCyan := ux.BoldCyan
	origBoldBlue := ux.BoldBlue
	origBoldGreen := ux.BoldGreen
	t.Cleanup(func() {
		ux.QuietMode = origQuiet
		ux.Reset = origReset
		ux.Bold = origBold
		ux.Dim = origDim
		ux.Red = origRed
		ux.Green = origGreen
		ux.Yellow = origYellow
		ux.Cyan = origCyan
		ux.Magenta = origMagenta
		ux.Blue = origBlue
		ux.BoldCyan = origBoldCyan
		ux.BoldBlue = origBoldBlue
		ux.BoldGreen = origBoldGreen
	})
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte("name: test\nphases:\n  - name: a\n    type: script\n    run: echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	// Clear CLAUDECODE so the guard doesn't fire before the mutual exclusivity check
	t.Setenv("CLAUDECODE", "")

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{runCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "run", "TEST-1", "--headless", "--step"})
	if err == nil {
		t.Fatal("expected error for --headless + --step")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual exclusivity error, got: %v", err)
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

func TestRunCmd_OrcHeadlessEnvActivatesQuietMode(t *testing.T) {
	dir := t.TempDir()
	// Save and restore globals that EnableQuiet() mutates
	origQuiet := ux.QuietMode
	origReset := ux.Reset
	origBold := ux.Bold
	origDim := ux.Dim
	origRed := ux.Red
	origGreen := ux.Green
	origYellow := ux.Yellow
	origCyan := ux.Cyan
	origMagenta := ux.Magenta
	origBlue := ux.Blue
	origBoldCyan := ux.BoldCyan
	origBoldBlue := ux.BoldBlue
	origBoldGreen := ux.BoldGreen
	t.Cleanup(func() {
		ux.QuietMode = origQuiet
		ux.Reset = origReset
		ux.Bold = origBold
		ux.Dim = origDim
		ux.Red = origRed
		ux.Green = origGreen
		ux.Yellow = origYellow
		ux.Cyan = origCyan
		ux.Magenta = origMagenta
		ux.Blue = origBlue
		ux.BoldCyan = origBoldCyan
		ux.BoldBlue = origBoldBlue
		ux.BoldGreen = origBoldGreen
	})

	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte("name: test\nphases:\n  - name: a\n    type: script\n    run: echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	t.Setenv("CLAUDECODE", "")
	t.Setenv("ORC_HEADLESS", "1")

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{runCmd()},
	}
	// The command may fail during dispatch — we don't care about the error,
	// only that ORC_HEADLESS=1 activated quiet mode.
	_ = app.Run(context.Background(), []string{"orc", "run", "TEST-1"})

	if !ux.QuietMode {
		t.Fatal("expected ux.QuietMode to be true when ORC_HEADLESS=1 is set")
	}
}

func TestNoColorFlag_DisablesColor(t *testing.T) {
	origReset := ux.Reset
	origBold := ux.Bold
	origDim := ux.Dim
	origRed := ux.Red
	origGreen := ux.Green
	origYellow := ux.Yellow
	origCyan := ux.Cyan
	origMagenta := ux.Magenta
	origBlue := ux.Blue
	origBoldCyan := ux.BoldCyan
	origBoldBlue := ux.BoldBlue
	origBoldGreen := ux.BoldGreen
	t.Cleanup(func() {
		ux.Reset = origReset
		ux.Bold = origBold
		ux.Dim = origDim
		ux.Red = origRed
		ux.Green = origGreen
		ux.Yellow = origYellow
		ux.Cyan = origCyan
		ux.Magenta = origMagenta
		ux.Blue = origBlue
		ux.BoldCyan = origBoldCyan
		ux.BoldBlue = origBoldBlue
		ux.BoldGreen = origBoldGreen
	})

	colorDisabled := false
	sub := &cli.Command{
		Name: "sub",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			colorDisabled = ux.Red == ""
			return nil
		},
	}
	app := &cli.Command{
		Name: "orc",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "no-color"},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if cmd.Bool("no-color") || os.Getenv("NO_COLOR") != "" || os.Getenv("ORC_NO_COLOR") != "" || !ux.IsTerminal(os.Stdout) {
				ux.DisableColor()
			}
			return ctx, nil
		},
		Commands: []*cli.Command{sub},
	}
	if err := app.Run(context.Background(), []string{"orc", "--no-color", "sub"}); err != nil {
		t.Fatal(err)
	}
	if !colorDisabled {
		t.Fatal("expected colors to be disabled after --no-color flag")
	}
}

func TestNoColorEnvVars(t *testing.T) {
	for _, tc := range []struct {
		name   string
		envKey string
	}{
		{"NO_COLOR", "NO_COLOR"},
		{"ORC_NO_COLOR", "ORC_NO_COLOR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origReset := ux.Reset
			origBold := ux.Bold
			origDim := ux.Dim
			origRed := ux.Red
			origGreen := ux.Green
			origYellow := ux.Yellow
			origCyan := ux.Cyan
			origMagenta := ux.Magenta
			origBlue := ux.Blue
			origBoldCyan := ux.BoldCyan
			origBoldBlue := ux.BoldBlue
			origBoldGreen := ux.BoldGreen
			t.Cleanup(func() {
				ux.Reset = origReset
				ux.Bold = origBold
				ux.Dim = origDim
				ux.Red = origRed
				ux.Green = origGreen
				ux.Yellow = origYellow
				ux.Cyan = origCyan
				ux.Magenta = origMagenta
				ux.Blue = origBlue
				ux.BoldCyan = origBoldCyan
				ux.BoldBlue = origBoldBlue
				ux.BoldGreen = origBoldGreen
			})

			// Restore color vars so we test the env var path, not the non-TTY path
			ux.Reset = origReset
			ux.Bold = origBold
			ux.Dim = origDim
			ux.Red = origRed
			ux.Green = origGreen
			ux.Yellow = origYellow
			ux.Cyan = origCyan
			ux.Magenta = origMagenta
			ux.Blue = origBlue
			ux.BoldCyan = origBoldCyan
			ux.BoldBlue = origBoldBlue
			ux.BoldGreen = origBoldGreen

			t.Setenv(tc.envKey, "1")

			colorDisabled := false
			sub := &cli.Command{
				Name: "sub",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					colorDisabled = ux.Red == ""
					return nil
				},
			}
			app := &cli.Command{
				Name: "orc",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "no-color"},
				},
				Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
					if cmd.Bool("no-color") || os.Getenv("NO_COLOR") != "" || os.Getenv("ORC_NO_COLOR") != "" || !ux.IsTerminal(os.Stdout) {
						ux.DisableColor()
					}
					return ctx, nil
				},
				Commands: []*cli.Command{sub},
			}
			if err := app.Run(context.Background(), []string{"orc", "sub"}); err != nil {
				t.Fatal(err)
			}
			if !colorDisabled {
				t.Fatalf("expected colors to be disabled when %s is set", tc.envKey)
			}
		})
	}
}
