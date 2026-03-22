package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jorge-barreto/orc/internal/runner"
	cli "github.com/urfave/cli/v3"
)

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
