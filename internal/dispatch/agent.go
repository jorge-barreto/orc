package dispatch

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// runAgent is the shared implementation for agent dispatch.
func runAgent(ctx context.Context, phase config.Phase, env *Environment, prompt string, logFlags int) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--model", phase.Model)
	cmd.Dir = env.WorkDir
	cmd.Env = BuildEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), logFlags, 0644)
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

// RunAgent executes an agent phase by rendering a prompt and invoking claude -p.
func RunAgent(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	// Read and render the prompt template
	promptData, err := os.ReadFile(filepath.Join(env.ProjectRoot, phase.Prompt))
	if err != nil {
		return nil, err
	}
	rendered := ExpandVars(string(promptData), env.Vars())

	// Save rendered prompt for inspection
	if err := os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(rendered), 0644); err != nil {
		return nil, err
	}

	return runAgent(ctx, phase, env, rendered, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
}

// RunAgentWithPrompt invokes claude -p with an explicit prompt string (for output re-prompting).
func RunAgentWithPrompt(ctx context.Context, phase config.Phase, env *Environment, prompt string) (*Result, error) {
	return runAgent(ctx, phase, env, prompt, os.O_APPEND|os.O_CREATE|os.O_WRONLY)
}
