package dispatch

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RunHook executes a hook command (pre-run or post-run) via bash.
// The caller is responsible for providing logWriter; RunHook does not open any files.
func RunHook(ctx context.Context, command string, phase config.Phase, env *Environment, logWriter io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = PhaseWorkDir(phase, env)
	cmd.Env = BuildEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	mw := io.MultiWriter(os.Stdout, logWriter)
	cmd.Stdout = mw
	cmd.Stderr = mw

	return exitCode(cmd.Run())
}

// DispatchFunc is the signature for phase dispatch. Both Dispatcher.Dispatch
// and the package-level Dispatch function match this signature.
type DispatchFunc func(ctx context.Context, phase config.Phase, env *Environment) (*Result, error)

// RunHookWithLog opens the phase log file, writes a label header, and calls RunHook.
// This is the standard way to execute a hook with log capture.
func RunHookWithLog(ctx context.Context, hookCmd, label string, phase config.Phase, env *Environment) (int, error) {
	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening phase log for %s hook: %w", label, err)
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "\n[orc] %s: %s\n", label, hookCmd)
	return RunHook(ctx, hookCmd, phase, env, logFile)
}

// DispatchWithHooks runs pre-run hook, dispatches the phase, then runs post-run hook.
// Pre-run failure skips dispatch. Post-run always runs (cleanup semantics).
// Post-run failure overrides a successful dispatch result.
func DispatchWithHooks(ctx context.Context, phase config.Phase, env *Environment, dispatchFn DispatchFunc) (*Result, error) {
	var preRunFailed bool
	var preRunCode int
	var preRunErr error

	if phase.PreRun != "" {
		code, err := RunHookWithLog(ctx, phase.PreRun, "pre-run", phase, env)
		if err != nil {
			preRunErr = err
			preRunFailed = true
		} else if code != 0 {
			preRunFailed = true
			preRunCode = code
		}
	}

	var result *Result
	var dispatchErr error
	if !preRunFailed {
		result, dispatchErr = dispatchFn(ctx, phase, env)
	}

	if phase.PostRun != "" {
		code, err := RunHookWithLog(ctx, phase.PostRun, "post-run", phase, env)
		if err != nil {
			if !preRunFailed && dispatchErr == nil {
				return result, fmt.Errorf("post-run hook: %w", err)
			}
			fmt.Fprintf(os.Stderr, "warning: post-run hook error: %v\n", err)
		} else if code != 0 {
			if !preRunFailed && dispatchErr == nil && result != nil && result.ExitCode == 0 {
				result.ExitCode = code
			} else {
				fmt.Fprintf(os.Stderr, "warning: post-run hook failed (exit %d) but phase already failed\n", code)
			}
		}
	}

	if preRunErr != nil {
		return nil, preRunErr
	}

	if preRunFailed && result == nil {
		result = &Result{ExitCode: preRunCode}
	}

	return result, dispatchErr
}
