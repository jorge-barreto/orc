package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jorge-barreto/orc/internal/runner"
	cli "github.com/urfave/cli/v3"
)

func TestFlowCmd_NoProjectRoot_ExitConfigError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{flowCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "flow"})
	if err == nil {
		t.Fatal("expected error when no project root exists")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}
