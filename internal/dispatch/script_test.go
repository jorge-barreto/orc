package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func scriptEnv(t *testing.T) *Environment {
	t.Helper()
	artDir := filepath.Join(t.TempDir(), "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}
	return &Environment{
		ProjectRoot:  t.TempDir(),
		WorkDir:      t.TempDir(),
		ArtifactsDir: artDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}
}

func TestRunScript_Success(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script", Run: "echo hello"}
	result, err := RunScript(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestRunScript_Failure(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script", Run: "exit 1"}
	result, err := RunScript(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestRunScript_VarExpansion(t *testing.T) {
	env := scriptEnv(t)
	env.Ticket = "EXPAND-42"
	phase := config.Phase{Name: "test", Type: "script", Run: "echo $TICKET"}
	result, err := RunScript(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "EXPAND-42") {
		t.Fatalf("output = %q, expected EXPAND-42", result.Output)
	}
}

func TestRunScript_LogFile(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script", Run: "echo logged-output"}
	_, err := RunScript(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	logPath := state.LogPath(env.ArtifactsDir, 0)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "logged-output") {
		t.Fatalf("log = %q", string(data))
	}
}

func TestRunScript_Stderr(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script", Run: "echo err >&2"}
	result, err := RunScript(context.Background(), phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "err") {
		t.Fatalf("output = %q, expected stderr captured", result.Output)
	}
}
