package dispatch

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestRunHook_Success(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	var buf bytes.Buffer
	code, err := RunHook(context.Background(), "echo hello", phase, env, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("logWriter = %q, expected 'hello'", buf.String())
	}
}

func TestRunHook_Failure(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	var buf bytes.Buffer
	code, err := RunHook(context.Background(), "exit 1", phase, env, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunHook_VarExpansion(t *testing.T) {
	env := scriptEnv(t)
	env.Ticket = "HOOK-99"
	phase := config.Phase{Name: "test", Type: "script"}
	var buf bytes.Buffer
	code, err := RunHook(context.Background(), "echo $TICKET", phase, env, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(buf.String(), "HOOK-99") {
		t.Fatalf("logWriter = %q, expected 'HOOK-99'", buf.String())
	}
}

func TestRunHook_LogCapture(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	var buf bytes.Buffer
	_, err := RunHook(context.Background(), "echo captured", phase, env, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "captured") {
		t.Fatalf("logWriter = %q, expected stdout written to logWriter", buf.String())
	}
}

func TestRunHook_StderrCapture(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	var buf bytes.Buffer
	_, err := RunHook(context.Background(), "echo err >&2", phase, env, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "err") {
		t.Fatalf("logWriter = %q, expected stderr written to logWriter", buf.String())
	}
}
