package dispatch

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RunAgent executes an agent phase by rendering a prompt and invoking claude -p.
func RunAgent(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

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

	// Invoke claude non-interactively
	cmd := exec.CommandContext(ctx, "claude", "-p", rendered,
		"--model", phase.Model, "--dangerously-skip-permissions")
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

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &Result{ExitCode: exitCode, Output: captured.String()}, nil
}

// RunAgentWithPrompt invokes claude -p with an explicit prompt string (for output re-prompting).
func RunAgentWithPrompt(ctx context.Context, phase config.Phase, env *Environment, prompt string) (*Result, error) {
	if phase.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--model", phase.Model, "--dangerously-skip-permissions")
	cmd.Dir = env.WorkDir
	cmd.Env = BuildEnv(env)

	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile, &captured)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile, &captured)

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &Result{ExitCode: exitCode, Output: captured.String()}, nil
}
