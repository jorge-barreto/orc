package dispatch

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RunGate executes a gate phase, prompting for human approval.
func RunGate(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	return runGate(ctx, phase, env, os.Stdin)
}

func runGate(ctx context.Context, phase config.Phase, env *Environment, stdin io.Reader) (*Result, error) {
	logFile, err := os.OpenFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	// Auto-approve if --auto mode
	if env.AutoMode {
		msg := fmt.Sprintf("Gate %q auto-approved (--auto mode)\n", phase.Name)
		fmt.Print(msg)
		logFile.WriteString(msg)
		return &Result{ExitCode: 0, Output: msg}, nil
	}

	// Run pre-prompt command if specified
	if phase.Run != "" {
		cmd := exec.CommandContext(ctx, "bash", "-c", phase.Run)
		cmd.Dir = PhaseWorkDir(phase, env)
		cmd.Env = BuildEnv(env)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		cmd.WaitDelay = 5 * time.Second
		cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
		cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
		if err := cmd.Run(); err != nil {
			logFile.WriteString(fmt.Sprintf("gate run command failed: %v\n", err))
		}
	}

	// Show gate description
	if phase.Description != "" {
		fmt.Printf("\n  %s\n\n", phase.Description)
	}

	// Prompt user
	fmt.Printf("  [y to continue / feedback to revise]: ")

	reader := NewStdinReader(stdin)
	defer reader.Stop()

	type lineResult struct {
		text string
		ok   bool
	}
	lineCh := make(chan lineResult, 1)
	go func() {
		text, ok := reader.ReadLineBlocking()
		lineCh <- lineResult{text, ok}
	}()

	select {
	case <-ctx.Done():
		msg := "Gate cancelled\n"
		logFile.WriteString(msg)
		return &Result{ExitCode: 1, Output: msg}, nil
	case lr := <-lineCh:
		if !lr.ok {
			return nil, io.EOF
		}
		input := strings.TrimSpace(lr.text)
		switch strings.ToLower(input) {
		case "y", "yes":
			msg := fmt.Sprintf("Gate %q approved\n", phase.Name)
			fmt.Print(msg)
			logFile.WriteString(msg)
			return &Result{ExitCode: 0, Output: msg}, nil
		default:
			msg := fmt.Sprintf("Gate %q — revision requested\n", phase.Name)
			fmt.Print(msg)
			logFile.WriteString(msg)
			logFile.WriteString(fmt.Sprintf("Feedback: %s\n", input))
			return &Result{ExitCode: 1, Output: input}, nil
		}
	}
}
