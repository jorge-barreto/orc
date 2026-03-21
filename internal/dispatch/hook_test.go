package dispatch

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
)

type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

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

func TestRunHook_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	var sb safeBuf

	type result struct {
		code int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		code, err := RunHook(ctx, "echo ready && sleep 60", phase, env, &sb)
		ch <- result{code, err}
	}()

	// Wait for the process to signal readiness before cancelling.
	deadline := time.After(5 * time.Second)
	for !strings.Contains(sb.String(), "ready") {
		select {
		case <-deadline:
			t.Fatal("hook process did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("expected nil error (exitCode extracts ExitError), got: %v", r.err)
		}
		if r.code != -1 {
			t.Fatalf("expected exit code -1 (signal kill), got %d", r.code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunHook did not return after context cancellation")
	}
}
