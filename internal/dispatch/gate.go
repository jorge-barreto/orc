package dispatch

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

// RunGate executes a gate phase, prompting for human approval.
func RunGate(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	logFile, err := os.Create(state.LogPath(env.ArtifactsDir, env.PhaseIndex))
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

	// Show gate description
	if phase.Description != "" {
		fmt.Printf("\n  %s\n\n", phase.Description)
	}

	// Prompt user
	fmt.Printf("  [y to continue / feedback to revise]: ")
	reader := bufio.NewReader(os.Stdin)

	// Use a channel to handle context cancellation during read
	type readResult struct {
		input string
		err   error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- readResult{input: strings.TrimSpace(line), err: err}
	}()

	select {
	case <-ctx.Done():
		msg := "Gate cancelled\n"
		logFile.WriteString(msg)
		return &Result{ExitCode: 1, Output: msg}, nil
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		input := r.input
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
