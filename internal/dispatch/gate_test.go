package dispatch

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestRunGate_AutoApprove(t *testing.T) {
	env := scriptEnv(t)
	env.AutoMode = true
	phase := config.Phase{Name: "test", Type: "gate"}
	result, err := RunGate(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Output, "auto-approved") {
		t.Fatalf("output = %q, want 'auto-approved'", result.Output)
	}
}

func TestRunGate_Approve(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("y\n")
	w.Close()
	result, err := runGate(context.Background(), phase, env, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Output, "approved") {
		t.Fatalf("output = %q, want 'approved'", result.Output)
	}
}

func TestRunGate_Feedback(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("please fix the title\n")
	w.Close()
	result, err := runGate(context.Background(), phase, env, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Output, "please fix the title") {
		t.Fatalf("output = %q, want feedback text", result.Output)
	}
}

func TestRunGate_ContextCancellation(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before call
	result, err := runGate(ctx, phase, env, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Output, "cancelled") {
		t.Fatalf("output = %q, want 'cancelled'", result.Output)
	}
}

func TestRunGate_EmptyInput(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("\ny\n")
	w.Close()
	result, err := runGate(context.Background(), phase, env, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (empty line skipped, y processed)", result.ExitCode)
	}
	if !strings.Contains(result.Output, "approved") {
		t.Fatalf("output = %q, want 'approved'", result.Output)
	}
}

func TestRunGate_EOF(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close() // immediate EOF
	_, err = runGate(context.Background(), phase, env, r)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestRunGate_PrePromptRun(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "gate", Run: "echo pre-prompt"}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("y\n")
	w.Close()
	result, err := runGate(context.Background(), phase, env, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	logPath := state.LogPath(env.ArtifactsDir, 0)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "pre-prompt") {
		t.Fatalf("log = %q, want 'pre-prompt'", string(data))
	}
}
