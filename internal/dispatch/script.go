package dispatch

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RunScript executes a script phase via bash.
func RunScript(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	expanded := ExpandVars(phase.Run, env.Vars())

	cmd := exec.CommandContext(ctx, "bash", "-c", expanded)
	cmd.Dir = env.WorkDir
	cmd.Env = BuildEnv(env)

	logFile, err := os.Create(state.LogPath(env.ArtifactsDir, env.PhaseIndex))
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile, &captured)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile, &captured)

	code, err := exitCode(cmd.Run())
	if err != nil {
		return nil, err
	}

	return &Result{ExitCode: code, Output: captured.String()}, nil
}
