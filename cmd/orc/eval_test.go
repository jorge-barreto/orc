package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jorge-barreto/orc/internal/runner"
	cli "github.com/urfave/cli/v3"
)

// writeEvalProject sets up a minimal .orc project (config + one eval case) in a
// temp dir and chdirs into it, so eval-command code that calls findProjectRoot
// resolves it. Returns the project root.
func writeEvalProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(root, ".orc", "config.yaml"),
		"phases:\n  - name: p\n    type: script\n    run: \"true\"\n")
	must(filepath.Join(root, ".orc", "evals", "bug-fix", "fixture.yaml"),
		"ref: HEAD\nticket: T-001\nspec: spec.md\n")
	must(filepath.Join(root, ".orc", "evals", "bug-fix", "spec.md"), "do the thing\n")
	must(filepath.Join(root, ".orc", "evals", "bug-fix", "rubric.yaml"),
		"criteria:\n  - name: ok\n    check: \"exit 0\"\n    weight: 1\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	return root
}

func TestEvalCmd_CLAUDECODE_ExitConfigError(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{evalCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "eval"})
	if err == nil {
		t.Fatal("expected error when CLAUDECODE is set")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

// BLOCKING 3 + W2: an invalid (traversal) run id on --regrade must exit
// ExitConfigError (3), not plain 1.
func TestEvalCmd_RegradeInvalidRunID_ExitConfigError(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	writeEvalProject(t)
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{evalCmd()},
	}
	err := app.Run(context.Background(),
		[]string{"orc", "eval", "bug-fix", "../../etc/passwd", "--regrade"})
	if err == nil {
		t.Fatal("expected error for traversal run id")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

// NOTE: --regrade combined with --list/--report is a contradiction (the latter
// would silently win) → reject with ExitConfigError (3).
func TestEvalCmd_RegradeWithList_ExitConfigError(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	writeEvalProject(t)
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{evalCmd()},
	}
	err := app.Run(context.Background(),
		[]string{"orc", "eval", "bug-fix", "--regrade", "--list"})
	if err == nil {
		t.Fatal("expected error for --regrade with --list")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}

// W2: a --regrade with no saved runs is a setup error → ExitConfigError (3).
func TestEvalCmd_RegradeNoSavedRuns_ExitConfigError(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	writeEvalProject(t)
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{evalCmd()},
	}
	err := app.Run(context.Background(),
		[]string{"orc", "eval", "bug-fix", "--regrade"})
	if err == nil {
		t.Fatal("expected error when no saved runs exist")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}
