package dispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
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

func TestRunHookWithLog_WritesLabel(t *testing.T) {
	env := scriptEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ArtifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	phase := config.Phase{Name: "test", Type: "script"}
	code, err := RunHookWithLog(context.Background(), "echo hello", "pre-run", phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(state.LogPath(env.ArtifactsDir, env.PhaseIndex))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[orc] pre-run: echo hello") {
		t.Fatalf("log = %q, want label '[orc] pre-run: echo hello'", string(data))
	}
}

func TestRunHookWithLog_FailureExitCode(t *testing.T) {
	env := scriptEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ArtifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	phase := config.Phase{Name: "test", Type: "script"}
	code, err := RunHookWithLog(context.Background(), "exit 1", "pre-run", phase, env)
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunHookWithLog_LogFileOpenError(t *testing.T) {
	env := scriptEnv(t)
	// ArtifactsDir points to a path whose logs/ subdir does not exist — OpenFile will fail.
	env.ArtifactsDir = filepath.Join(t.TempDir(), "nonexistent", "artifacts")
	phase := config.Phase{Name: "test", Type: "script"}
	_, err := RunHookWithLog(context.Background(), "echo hello", "pre-run", phase, env)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestDispatchWithHooks_NoHooks(t *testing.T) {
	env := scriptEnv(t)
	phase := config.Phase{Name: "test", Type: "script"}
	called := false
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		called = true
		return &Result{ExitCode: 0}, nil
	}
	result, err := DispatchWithHooks(context.Background(), phase, env, fn)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("dispatchFn was not called")
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestDispatchWithHooks_PreRunFail_SkipsDispatch(t *testing.T) {
	env := scriptEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ArtifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	phase := config.Phase{Name: "test", Type: "script", PreRun: "exit 1"}
	called := false
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		called = true
		return &Result{ExitCode: 0}, nil
	}
	result, err := DispatchWithHooks(context.Background(), phase, env, fn)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("dispatchFn was called despite pre-run failure")
	}
	if result == nil || result.ExitCode != 1 {
		t.Fatalf("ExitCode = %v, want 1", result)
	}
}

func TestDispatchWithHooks_PostRunFail_OverridesSuccess(t *testing.T) {
	env := scriptEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ArtifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	phase := config.Phase{Name: "test", Type: "script", PostRun: "exit 7"}
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		return &Result{ExitCode: 0}, nil
	}
	result, err := DispatchWithHooks(context.Background(), phase, env, fn)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7 (post-run override)", result.ExitCode)
	}
}

func TestDispatchWithHooks_PreRunFail_PostRunStillRuns(t *testing.T) {
	env := scriptEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ArtifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "post-run-ran")
	phase := config.Phase{
		Name:    "test",
		Type:    "script",
		PreRun:  "exit 1",
		PostRun: "touch " + sentinel,
	}
	called := false
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		called = true
		return &Result{ExitCode: 0}, nil
	}
	_, err := DispatchWithHooks(context.Background(), phase, env, fn)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("dispatchFn was called despite pre-run failure")
	}
	if _, statErr := os.Stat(sentinel); os.IsNotExist(statErr) {
		t.Fatal("post-run hook did not run (sentinel file missing)")
	}
}

func TestDispatchWithHooks_PostRunInfraError_ReturnsError(t *testing.T) {
	env := scriptEnv(t)
	env.ArtifactsDir = filepath.Join(t.TempDir(), "nonexistent", "artifacts")

	phase := config.Phase{Name: "test", Type: "script", PostRun: "echo cleanup"}
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		return &Result{ExitCode: 0}, nil
	}
	_, err := DispatchWithHooks(context.Background(), phase, env, fn)
	if err == nil {
		t.Fatal("expected error from post-run hook infrastructure failure")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist wrapped", err)
	}
	if !strings.Contains(err.Error(), "post-run hook") {
		t.Fatalf("err = %v, want 'post-run hook' prefix", err)
	}
}

func TestDispatchWithHooks_PostRunInfraError_DispatchFailed_Warning(t *testing.T) {
	env := scriptEnv(t)
	env.ArtifactsDir = filepath.Join(t.TempDir(), "nonexistent", "artifacts")

	phase := config.Phase{Name: "test", Type: "script", PostRun: "echo cleanup"}
	dispatchOrigErr := fmt.Errorf("dispatch failed")
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		return nil, dispatchOrigErr
	}

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := DispatchWithHooks(context.Background(), phase, env, fn)

	w.Close()
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, r)
	r.Close()
	os.Stderr = origStderr

	if err != dispatchOrigErr {
		t.Fatalf("err = %v, want original dispatch error", err)
	}
	if !strings.Contains(stderrBuf.String(), "warning: post-run hook error") {
		t.Fatalf("stderr = %q, want post-run hook warning", stderrBuf.String())
	}
}

func TestDispatchWithHooks_PreRunInfraError_PostRunStillAttempted(t *testing.T) {
	env := scriptEnv(t)
	// Point to nonexistent logs dir so RunHookWithLog fails with fs.ErrNotExist.
	env.ArtifactsDir = filepath.Join(t.TempDir(), "nonexistent", "artifacts")

	phase := config.Phase{
		Name:    "test",
		Type:    "script",
		PreRun:  "echo pre",
		PostRun: "echo post",
	}

	called := false
	fn := func(ctx context.Context, p config.Phase, e *Environment) (*Result, error) {
		called = true
		return &Result{ExitCode: 0}, nil
	}

	// Capture stderr to verify post-run was attempted.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := DispatchWithHooks(context.Background(), phase, env, fn)

	w.Close()
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, r)
	r.Close()
	os.Stderr = origStderr

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if called {
		t.Fatal("dispatchFn was called despite pre-run infrastructure error")
	}
	if !strings.Contains(stderrBuf.String(), "warning: post-run hook error") {
		t.Fatalf("stderr = %q, want post-run hook warning (proves post-run was attempted)", stderrBuf.String())
	}
}
