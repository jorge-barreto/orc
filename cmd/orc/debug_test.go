package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jorge-barreto/orc/internal/runner"
	cli "github.com/urfave/cli/v3"
)

func TestDebugCmd_MissingPhase_ExitConfigError(t *testing.T) {
	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{debugCmd()},
	}
	err := app.Run(context.Background(), []string{"orc", "debug"})
	if err == nil {
		t.Fatal("expected error for missing phase arg")
	}
	var exitErr *runner.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *runner.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != runner.ExitConfigError {
		t.Fatalf("expected exit code %d, got %d", runner.ExitConfigError, exitErr.Code)
	}
}
