package dispatch

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
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

	cmd := exec.CommandContext(ctx, "bash", "-c", phase.Run)
	cmd.Dir = PhaseWorkDir(phase, env)
	cmd.Env = BuildEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	captured := newTailWriter(1 << 20) // 1 MB tail buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile, captured)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile, captured)

	code, err := exitCode(cmd.Run())
	if err != nil {
		return nil, err
	}

	res := &Result{ExitCode: code, Output: captured.String()}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	return res, nil
}
