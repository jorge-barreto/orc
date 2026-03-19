package dispatch

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
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
