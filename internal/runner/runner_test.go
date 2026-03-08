package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
)

// mockDispatcher records calls and returns configurable results.
type mockDispatcher struct {
	mu      sync.Mutex
	calls   []string
	results map[string]*dispatch.Result
	errors  map[string]error
}

func newMock() *mockDispatcher {
	return &mockDispatcher{
		results: make(map[string]*dispatch.Result),
		errors:  make(map[string]error),
	}
}

func (m *mockDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	m.mu.Lock()
	m.calls = append(m.calls, phase.Name)
	m.mu.Unlock()

	if err, ok := m.errors[phase.Name]; ok {
		res := m.results[phase.Name]
		return res, err
	}
	if res, ok := m.results[phase.Name]; ok {
		return res, nil
	}
	return &dispatch.Result{ExitCode: 0}, nil
}

func (m *mockDispatcher) callNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := make([]string, len(m.calls))
	copy(c, m.calls)
	return c
}

func (m *mockDispatcher) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func newTestRunner(t *testing.T, cfg *config.Config, mock dispatch.Dispatcher) *Runner {
	t.Helper()
	artDir := filepath.Join(t.TempDir(), "artifacts")
	workDir := t.TempDir()
	return &Runner{
		Config: cfg,
		State:  &state.State{Status: state.StatusRunning},
		Env: &dispatch.Environment{
			ProjectRoot:  workDir,
			WorkDir:      workDir,
			ArtifactsDir: artDir,
			Ticket:       "TEST-1",
			PhaseCount:   len(cfg.Phases),
		},
		Dispatcher: mock,
	}
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	got := ExitCodeFrom(err)
	if got != want {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, want, err)
	}
}

func TestRun_AllPhasesSucceed(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
	if r.State.PhaseIndex != 3 {
		t.Fatalf("PhaseIndex = %d", r.State.PhaseIndex)
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestRun_FailNoLoop(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), `phase "b" failed`) {
		t.Fatalf("expected phase b failure, got %v", err)
	}
	assertExitCode(t, err, ExitRetryable)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q", r.State.Status)
	}
	calls := mock.callNames()
	// c should never be called
	for _, c := range calls {
		if c == "c" {
			t.Fatal("phase c should not have been called")
		}
	}
}

func TestRun_LoopBasicConvergence(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}

	// c fails on first call, then succeeds
	cCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "c" {
			mu.Lock()
			cCount++
			n := cCount
			mu.Unlock()
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "c failed"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}

	// Feedback should be cleared after loop exit
	fb, err := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Verify feedback was archived to audit dir
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 2, 2, "c")
	data, err := os.ReadFile(auditFb)
	if err != nil {
		t.Fatalf("archived feedback not found: %v", err)
	}
	if !strings.Contains(string(data), "c failed") {
		t.Fatalf("archived feedback = %q", string(data))
	}
}

func TestRun_LoopMaxExceeded(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}
	// b always fails
	alwaysFail := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, alwaysFail)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed after 3 iterations") {
		t.Fatalf("expected loop exhaustion error, got %v", err)
	}
	assertExitCode(t, err, ExitRetryable)
}

func TestRun_LoopCounterPersisted(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 5}},
		},
	}
	bCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	counts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	// 1 fail iteration + 1 pass iteration = 2 total
	if counts["b"] != 2 {
		t.Fatalf("loop count for b = %d, want 2", counts["b"])
	}
}

func TestRun_LoopMinForcesIteration(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 3, Max: 5}},
		},
	}
	bCount := 0
	mu := sync.Mutex{}
	// b always succeeds
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			mu.Unlock()
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
	// b should be called 3 times (min=3 forced iterations)
	mu.Lock()
	n := bCount
	mu.Unlock()
	if n != 3 {
		t.Fatalf("b called %d times, want 3 (min forced)", n)
	}
}

func TestRun_LoopOnExhaust(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 2,
				OnExhaust: &config.OnExhaust{Goto: "a", Max: 2},
			}},
		},
	}
	bCount := 0
	mu := sync.Mutex{}
	// b fails twice (exhausts loop), then succeeds on recovery
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			if n <= 2 {
				return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}

	counts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	// on-exhaust counter should be 1
	if counts["b:exhaust"] != 1 {
		t.Fatalf("exhaust count for b = %d, want 1", counts["b:exhaust"])
	}
}

func TestRun_LoopOnExhaustExhausted(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 1,
				OnExhaust: &config.OnExhaust{Goto: "a", Max: 1},
			}},
		},
	}
	// b always fails
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "recovery exhausted") {
		t.Fatalf("expected recovery exhausted error, got %v", err)
	}
	assertExitCode(t, err, ExitRetryable)
}

func TestRun_LoopExhaustFeedbackHeader(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 1,
				OnExhaust: &config.OnExhaust{Goto: "a", Max: 2},
			}},
		},
	}
	bCount := 0
	mu := sync.Mutex{}
	// b fails once (exhausts loop at max=1), succeeds on recovery
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "original failure"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Feedback should be cleared after loop exit
	fb, err := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Check archived feedback has convergence-failed header
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	data, err := os.ReadFile(auditFb)
	if err != nil {
		t.Fatalf("archived feedback not found: %v", err)
	}
	if !strings.Contains(string(data), "Convergence failed") {
		t.Fatalf("expected convergence-failed header, got: %q", string(data))
	}
	if !strings.Contains(string(data), "original failure") {
		t.Fatalf("expected original output in feedback, got: %q", string(data))
	}
}

func TestRun_LoopFeedbackOnPass(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 2, Max: 5}},
		},
	}
	// b always succeeds with output
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 0, Output: "b output"}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Feedback should be cleared after loop exit
	fb, err := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Archived feedback should contain the forced loop-back output
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	data, err := os.ReadFile(auditFb)
	if err != nil {
		t.Fatalf("archived feedback not found: %v", err)
	}
	if !strings.Contains(string(data), "b output") {
		t.Fatalf("archived feedback = %q", string(data))
	}
}

// TestRun_FeedbackClearedOnLoopExit verifies that downstream phases don't see
// stale loop feedback. Phase b loops (fails once, then succeeds), then phase c runs.
// After b's loop exits, the feedback directory must be empty.
func TestRun_FeedbackClearedOnLoopExit(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}

	bCount := 0
	mu := sync.Mutex{}
	var cSawFeedback string
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "b failed"}, nil
			}
		}
		if phase.Name == "c" {
			// Capture what feedback c would see
			fb, _ := state.ReadAllFeedback(env.ArtifactsDir)
			cSawFeedback = fb
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Phase c must NOT have seen b's feedback
	if cSawFeedback != "" {
		t.Fatalf("phase c should not see loop feedback from b, got: %q", cSawFeedback)
	}

	// Feedback directory should be empty after run
	fb, err := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Feedback was archived to audit dir
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	if _, err := os.Stat(auditFb); os.IsNotExist(err) {
		t.Fatalf("expected archived feedback at %s", auditFb)
	}
}

func TestRun_ConditionFalse(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Condition: "false"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	calls := mock.callNames()
	for _, c := range calls {
		if c == "b" {
			t.Fatal("phase b should have been skipped")
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (a, c), got %v", calls)
	}
}

func TestRun_ConditionTrue(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Condition: "true"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.callCount())
	}
}

func TestRun_ResumeFromState(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "d", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.State.PhaseIndex = 2 // resume from phase c

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	calls := mock.callNames()
	if len(calls) != 2 || calls[0] != "c" || calls[1] != "d" {
		t.Fatalf("expected [c, d], got %v", calls)
	}
}

func TestRun_ContextCancelled(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := r.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got %v", err)
	}
	assertExitCode(t, err, ExitSignal)
	if r.State.Status != state.StatusInterrupted {
		t.Fatalf("status = %q", r.State.Status)
	}
}

func TestRun_OutputCheckPass(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Outputs: []string{"result.md"}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	// Create the expected output file before running
	state.EnsureDir(r.Env.ArtifactsDir)
	os.WriteFile(filepath.Join(r.Env.ArtifactsDir, "result.md"), []byte("done"), 0644)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
}

func TestRun_OutputCheckFail(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Outputs: []string{"missing.md"}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing outputs") {
		t.Fatalf("expected missing outputs error, got %v", err)
	}
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q", r.State.Status)
	}
}

func TestRun_ParallelBothSucceed(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
	// Both parallel phases + c should have been called
	if mock.callCount() != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", mock.callCount(), mock.callNames())
	}
}

func TestRun_ParallelOneFails(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q", r.State.Status)
	}
	// c should not have been called
	for _, c := range mock.callNames() {
		if c == "c" {
			t.Fatal("phase c should not run after parallel failure")
		}
	}
}

func TestRun_SavesStatePersistently(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := state.Load(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PhaseIndex != 2 {
		t.Fatalf("persisted PhaseIndex = %d, want 2", loaded.PhaseIndex)
	}
	if loaded.Status != state.StatusCompleted {
		t.Fatalf("persisted Status = %q", loaded.Status)
	}
}

func TestRun_ParallelOutputCheck(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b", Outputs: []string{"a-output.md"}},
			{Name: "b", Type: "script", Run: "echo", Outputs: []string{"b-output.md"}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	// Don't create output files — both should be missing
	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing outputs") {
		t.Fatalf("expected missing outputs error, got %v", err)
	}
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
}

func TestRun_ParallelOutputCheckPass(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b", Outputs: []string{"a-output.md"}},
			{Name: "b", Type: "script", Run: "echo", Outputs: []string{"b-output.md"}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	// Create expected output files
	state.EnsureDir(r.Env.ArtifactsDir)
	os.WriteFile(filepath.Join(r.Env.ArtifactsDir, "a-output.md"), []byte("done"), 0644)
	os.WriteFile(filepath.Join(r.Env.ArtifactsDir, "b-output.md"), []byte("done"), 0644)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.Status)
	}
}

func TestRun_CustomVarsPassedToDispatch(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	var capturedVars map[string]string
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		capturedVars = env.Vars()
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)
	r.Env.CustomVars = map[string]string{"MY_DIR": "/custom/path"}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capturedVars["MY_DIR"] != "/custom/path" {
		t.Fatalf("MY_DIR = %q", capturedVars["MY_DIR"])
	}
	if capturedVars["TICKET"] != "TEST-1" {
		t.Fatalf("TICKET = %q", capturedVars["TICKET"])
	}
}

func TestRun_ConditionRespectsPhase(t *testing.T) {
	// Condition "test -f marker" should use the phase's cwd
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "marker"), []byte("ok"), 0644)

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Condition: "test -f marker", Cwd: dir},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Phase a should have run (condition true because marker exists in dir)
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.callCount())
	}
}

// funcDispatcher is a Dispatcher backed by a function, for flexible test scenarios.
type funcDispatcher struct {
	fn func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error)
}

func (f *funcDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	return f.fn(ctx, phase, env)
}

// Fix 1: Condition strings expanded through ExpandVars

func TestRun_ConditionVarExpanded(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "mydir"), 0755)

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Condition: "test -d $MY_DIR"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.Env.CustomVars = map[string]string{"MY_DIR": filepath.Join(dir, "mydir")}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 call (condition true after expansion), got %d", mock.callCount())
	}
}

func TestRun_ConditionVarExpandedFalse(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Condition: "test -d $MY_DIR"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.Env.CustomVars = map[string]string{"MY_DIR": "/nonexistent/path/that/does/not/exist"}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mock.callCount() != 0 {
		t.Fatalf("expected 0 calls (condition false after expansion), got %d", mock.callCount())
	}
}

// Fix 5: DryRunPrint vars sorted

func TestDryRunPrint_VarsAreSorted(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.Env.CustomVars = map[string]string{
		"ZEBRA":  "z",
		"ALPHA":  "a",
		"MIDDLE": "m",
	}

	// Capture stdout
	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	r.DryRunPrint()

	pw.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, pr)
	output := buf.String()

	// Verify new flow diagram header format
	if !strings.Contains(output, "Workflow: test") {
		t.Fatalf("expected 'Workflow: test' in output, got:\n%s", output)
	}

	alphaIdx := strings.Index(output, "ALPHA")
	middleIdx := strings.Index(output, "MIDDLE")
	zebraIdx := strings.Index(output, "ZEBRA")

	if alphaIdx < 0 || middleIdx < 0 || zebraIdx < 0 {
		t.Fatalf("expected all vars in output, got:\n%s", output)
	}
	if !(alphaIdx < middleIdx && middleIdx < zebraIdx) {
		t.Fatalf("vars not sorted: ALPHA@%d MIDDLE@%d ZEBRA@%d\noutput:\n%s",
			alphaIdx, middleIdx, zebraIdx, output)
	}
}

func TestRun_CostsTrackedForAgentPhases(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{
		ExitCode:     0,
		Output:       "done",
		CostUSD:      0.5,
		InputTokens:  15000,
		OutputTokens: 8000,
		Turns:        1,
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify costs.json was created
	costs, err := state.LoadCosts(state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket))
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.Phases) != 1 {
		t.Fatalf("expected 1 cost entry, got %d", len(costs.Phases))
	}
	if costs.Phases[0].Name != "b" {
		t.Fatalf("phase name = %q, want 'b'", costs.Phases[0].Name)
	}
	if costs.Phases[0].CostUSD != 0.5 {
		t.Fatalf("CostUSD = %f, want 0.5", costs.Phases[0].CostUSD)
	}
	if costs.Phases[0].InputTokens != 15000 {
		t.Fatalf("InputTokens = %d, want 15000", costs.Phases[0].InputTokens)
	}
	if costs.TotalCostUSD != 0.5 {
		t.Fatalf("TotalCostUSD = %f, want 0.5", costs.TotalCostUSD)
	}
}

func TestRun_CostsTrackedZeroCost(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{
		ExitCode:     0,
		Output:       "done",
		CostUSD:      0,
		InputTokens:  0,
		OutputTokens: 0,
		Turns:        1,
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	costs, err := state.LoadCosts(state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket))
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.Phases) != 1 {
		t.Fatalf("expected 1 cost entry, got %d", len(costs.Phases))
	}
	if costs.Phases[0].CostUSD != 0 {
		t.Fatalf("CostUSD = %f, want 0", costs.Phases[0].CostUSD)
	}
}

func TestRun_NoCostsForScriptPhases(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	costs, err := state.LoadCosts(state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket))
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.Phases) != 0 {
		t.Fatalf("expected 0 cost entries for script-only run, got %d", len(costs.Phases))
	}
}

func TestRun_GateDenialExitCode(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "approve", Type: "gate"},
		},
	}
	mock := newMock()
	// Gate denial: ExitCode 1 from gate, same as RunGate returning "n"
	mock.results["approve"] = &dispatch.Result{ExitCode: 1, Output: "no"}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for gate denial")
	}
	assertExitCode(t, err, ExitHumanNeeded)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
}

func TestRun_ConfigErrorExitCode(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			// parallel-with references nonexistent phase — config error at runtime
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "nonexistent"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for config error")
	}
	assertExitCode(t, err, ExitConfigError)
}

func TestRun_SuccessExitCode(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	assertExitCode(t, err, ExitSuccess)
}

// Cost limit tests — sequential path

func TestRun_RunCostLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		Name:    "test",
		MaxCost: 1.0,
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{ExitCode: 0, CostUSD: 1.5, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "run exceeded cost limit") {
		t.Fatalf("expected run cost limit error, got %v", err)
	}
	assertExitCode(t, err, ExitHumanNeeded)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
	// Phase c should NOT have been dispatched
	for _, c := range mock.callNames() {
		if c == "c" {
			t.Fatal("phase c should not run after cost limit exceeded")
		}
	}
}

func TestRun_RunCostLimitNotExceeded(t *testing.T) {
	cfg := &config.Config{
		Name:    "test",
		MaxCost: 5.0,
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 2.0, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertExitCode(t, err, ExitSuccess)
}

func TestRun_PhaseCostLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", MaxCost: 1.0},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 1.5, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), `phase "a" exceeded cost limit`) {
		t.Fatalf("expected phase cost limit error, got %v", err)
	}
	assertExitCode(t, err, ExitHumanNeeded)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
	// Phase b should NOT have been dispatched
	for _, c := range mock.callNames() {
		if c == "b" {
			t.Fatal("phase b should not run after phase cost limit exceeded")
		}
	}
}

func TestRun_PhaseCostLimitNotExceeded(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", MaxCost: 5.0},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 2.0, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertExitCode(t, err, ExitSuccess)
	if mock.callCount() != 2 {
		t.Fatalf("expected 2 calls, got %d", mock.callCount())
	}
}

func TestRun_NoCostLimitSet(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 100.0, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertExitCode(t, err, ExitSuccess)
	if mock.callCount() != 2 {
		t.Fatalf("expected 2 calls, got %d", mock.callCount())
	}
}

func TestRun_LoopCostLimitInteraction(t *testing.T) {
	cfg := &config.Config{
		Name:    "test",
		MaxCost: 3.0,
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "agent", Prompt: "unused.md", Model: "sonnet", Loop: &config.Loop{Goto: "a", Min: 1, Max: 5}},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	// b always fails with CostUSD=2.0 — cost limit should stop the loop
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 1, Output: "fail", CostUSD: 2.0, InputTokens: 100, OutputTokens: 50, Turns: 1}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "run exceeded cost limit") {
		t.Fatalf("expected run cost limit error, got %v", err)
	}
	assertExitCode(t, err, ExitHumanNeeded)
}

// Cost limit tests — parallel path

func TestRun_ParallelRunCostLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		Name:    "test",
		MaxCost: 3.0,
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", ParallelWith: "b"},
			{Name: "b", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 2.0, InputTokens: 100, OutputTokens: 50, Turns: 1}
	mock.results["b"] = &dispatch.Result{ExitCode: 0, CostUSD: 2.0, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "run exceeded cost limit") {
		t.Fatalf("expected run cost limit error, got %v", err)
	}
	assertExitCode(t, err, ExitHumanNeeded)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
	// Phase c should NOT have been dispatched
	for _, c := range mock.callNames() {
		if c == "c" {
			t.Fatal("phase c should not run after parallel cost limit exceeded")
		}
	}
}

func TestRun_ParallelPhaseCostLimitExceeded(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", MaxCost: 1.0, ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 1.5, InputTokens: 100, OutputTokens: 50, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), `phase "a" exceeded cost limit`) {
		t.Fatalf("expected phase cost limit error, got %v", err)
	}
	assertExitCode(t, err, ExitHumanNeeded)
	if r.State.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.Status)
	}
}

// --- loop.check tests ---

func TestRun_LoopCheckFailTriggersLoopBack(t *testing.T) {
	tmpDir := t.TempDir()
	markerPath := filepath.Join(tmpDir, "check-marker")

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "agent", Prompt: "unused.md", Model: "sonnet", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 3,
				Check: "test -f " + markerPath,
			}},
		},
	}

	aCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "a" {
			mu.Lock()
			aCount++
			n := aCount
			mu.Unlock()
			if n == 2 {
				os.WriteFile(markerPath, []byte("pass"), 0644)
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}

	// Feedback should be cleared after loop exit
	fb, readErr := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Verify feedback was archived to audit dir
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	if _, statErr := os.Stat(auditFb); statErr != nil {
		t.Fatalf("archived feedback not found: %v", statErr)
	}
}

func TestRun_LoopCheckPassAdvances(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 3,
				Check: "true",
			}},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("calls = %v, want [a b c]", calls)
	}
}

func TestRun_LoopCheckVarExpansion(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 3,
				Check: "test -d $ARTIFACTS_DIR",
			}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
}

func TestRun_LoopCheckMaxExhausted(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 3,
				Check: "false",
			}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed after 3 iterations") {
		t.Fatalf("expected loop exhaustion error, got %v", err)
	}
	assertExitCode(t, err, ExitRetryable)
}

func TestRun_LoopCheckFeedbackWritten(t *testing.T) {
	tmpDir := t.TempDir()
	markerPath := filepath.Join(tmpDir, "check-marker")

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo",
				Outputs: []string{"review-findings.md"},
				Loop: &config.Loop{
					Goto: "a", Min: 1, Max: 3,
					Check: "test -f " + markerPath,
				}},
		},
	}

	bCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			// Create the declared output file (simulates agent writing review findings)
			os.WriteFile(filepath.Join(env.ArtifactsDir, "review-findings.md"),
				[]byte("Found 3 issues in handler.go"), 0644)
			if n == 2 {
				os.WriteFile(markerPath, []byte("pass"), 0644)
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Feedback should be cleared after loop exit
	fb, readErr := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Archived feedback should contain declared output content
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	data, readErr := os.ReadFile(auditFb)
	if readErr != nil {
		t.Fatalf("archived feedback not found: %v", readErr)
	}
	if !strings.Contains(string(data), "Found 3 issues in handler.go") {
		t.Fatalf("archived feedback should contain declared output content, got: %q", string(data))
	}
}

func TestRun_LoopCheckFeedbackUsesDeclaredOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	markerPath := filepath.Join(tmpDir, "check-marker")

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo",
				Outputs: []string{"review-findings.md"},
				Loop: &config.Loop{
					Goto: "a", Min: 1, Max: 3,
					Check: "echo 'check-sentinel-do-not-use' && test -f " + markerPath,
				}},
		},
	}

	bCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			os.WriteFile(filepath.Join(env.ArtifactsDir, "review-findings.md"),
				[]byte("Review: variable naming is inconsistent in handler.go"), 0644)
			if n == 2 {
				os.WriteFile(markerPath, []byte("pass"), 0644)
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Feedback should be cleared after loop exit
	fb, readErr := state.ReadAllFeedback(r.Env.ArtifactsDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if fb != "" {
		t.Fatalf("feedback should be cleared after loop exit, got %q", fb)
	}

	// Archived feedback must contain the declared output content
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditFb := state.AuditFeedbackPath(auditDir, 1, 2, "b")
	data, readErr := os.ReadFile(auditFb)
	if readErr != nil {
		t.Fatalf("archived feedback not found: %v", readErr)
	}

	feedback := string(data)
	if !strings.Contains(feedback, "variable naming is inconsistent") {
		t.Fatalf("archived feedback should contain declared output content, got: %q", feedback)
	}
	// Feedback must NOT contain the check command's stdout (the old buggy behavior)
	if strings.Contains(feedback, "check-sentinel-do-not-use") {
		t.Fatalf("archived feedback should NOT contain check stdout, got: %q", feedback)
	}
}

func TestRun_LoopCheckWithOnExhaust(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto:      "a",
				Min:       1,
				Max:       2,
				Check:     "false",
				OnExhaust: &config.OnExhaust{Goto: "a", Max: 2},
			}},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "recovery exhausted") {
		t.Fatalf("expected recovery exhausted error, got %v", err)
	}
	assertExitCode(t, err, ExitRetryable)

	counts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if counts["b:exhaust"] < 1 {
		t.Fatalf("expected exhaust counter >= 1, got %d", counts["b:exhaust"])
	}
}

func TestRun_LoopCheckOmittedPreservesBehavior(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 1, Max: 3,
			}},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("calls = %v, want [a b c]", calls)
	}
}

// callbackDispatcher allows per-call custom behavior in tests.
type callbackDispatcher struct {
	fn func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error)
}

func (d *callbackDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	return d.fn(ctx, phase, env)
}

func TestRun_LoopArchivesLogs(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}
	callCount := 0
	mock := &callbackDispatcher{
		fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
			callCount++
			// Write something to the log so we can verify archiving
			logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
			os.MkdirAll(filepath.Dir(logPath), 0755)
			os.WriteFile(logPath, []byte(fmt.Sprintf("log from call %d phase %s", callCount, phase.Name)), 0644)

			if phase.Name == "b" && callCount <= 2 {
				return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
			}
			return &dispatch.Result{ExitCode: 0}, nil
		},
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify audit dir has archived iteration logs for phase b (index 1)
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	iter1Log := state.AuditLogPath(auditDir, 1, 1)
	if _, err := os.Stat(iter1Log); os.IsNotExist(err) {
		t.Fatalf("expected archived log at %s", iter1Log)
	}
	data, err := os.ReadFile(iter1Log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "phase b") {
		t.Fatalf("archived log should contain phase b content, got %q", string(data))
	}
}

func TestRun_ArchivesOnFirstDispatch(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := &callbackDispatcher{
		fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
			logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
			os.MkdirAll(filepath.Dir(logPath), 0755)
			os.WriteFile(logPath, []byte("first dispatch log"), 0644)
			return &dispatch.Result{ExitCode: 0}, nil
		},
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	iter1Log := state.AuditLogPath(auditDir, 0, 1)
	data, err := os.ReadFile(iter1Log)
	if err != nil {
		t.Fatalf("expected archived log at %s: %v", iter1Log, err)
	}
	if string(data) != "first dispatch log" {
		t.Fatalf("archived log = %q, want %q", string(data), "first dispatch log")
	}
}

func TestRun_CostsWrittenToAuditDir(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{
		ExitCode:     0,
		CostUSD:      1.23,
		InputTokens:  5000,
		OutputTokens: 2000,
		Turns:        1,
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Costs should be in audit dir
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	costs, err := state.LoadCosts(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	if costs.TotalCostUSD != 1.23 {
		t.Fatalf("total cost = %f, want 1.23", costs.TotalCostUSD)
	}

	// Costs should NOT be in artifacts dir
	artCosts, err := state.LoadCosts(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(artCosts.Phases) != 0 {
		t.Fatalf("expected no costs in artifacts dir, got %d entries", len(artCosts.Phases))
	}
}

// TestRun_OuterLoopResetsIntermediateCounters verifies that when an outer loop
// jumps backward, loop counters for intermediate phases are reset to zero.
// This prevents counter bleed across outer-loop iterations.
func TestRun_OuterLoopResetsIntermediateCounters(t *testing.T) {
	// 4 phases: pick → work → review (loops to work, max=3) → next (loops to pick, max=3)
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "pick", Type: "script", Run: "echo"},
			{Name: "work", Type: "script", Run: "echo"},
			{Name: "review", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "work", Min: 1, Max: 3}},
			{Name: "next", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "pick", Min: 1, Max: 3}},
		},
	}

	var mu sync.Mutex
	reviewCount := 0
	nextCount := 0

	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		switch phase.Name {
		case "review":
			reviewCount++
			// Fail on first call of each outer iteration to exercise loop counter
			if reviewCount == 1 || reviewCount == 3 {
				return &dispatch.Result{ExitCode: 1, Output: "review fail"}, nil
			}
			return &dispatch.Result{ExitCode: 0}, nil
		case "next":
			nextCount++
			if nextCount == 1 {
				// First outer iteration: fail to trigger outer loop-back
				return &dispatch.Result{ExitCode: 1, Output: "more work"}, nil
			}
			return &dispatch.Result{ExitCode: 0}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// After run, review's loop counter should reflect only the second outer iteration.
	// Without the fix, it would accumulate across both iterations.
	counts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		t.Fatal(err)
	}

	// review ran: iter1(fail+pass=2 dispatches), iter2(fail+pass=2 dispatches) = 4 total
	if reviewCount != 4 {
		t.Fatalf("review dispatched %d times, want 4", reviewCount)
	}

	// The review counter should be 2 (1 fail + 1 pass from the second outer iteration).
	// Without the fix, it would be 4 (accumulated across both iterations).
	if counts["review"] != 2 {
		t.Fatalf("review loop count = %d, want 2", counts["review"])
	}
}

// TestRun_OuterLoopClearsFeedback verifies that feedback files from a previous
// outer-loop iteration are cleared when the outer loop jumps backward.
func TestRun_OuterLoopClearsFeedback(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "pick", Type: "script", Run: "echo"},
			{Name: "work", Type: "script", Run: "echo"},
			{Name: "next", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "pick", Min: 1, Max: 3}},
		},
	}

	var mu sync.Mutex
	nextCount := 0
	var artDir string

	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		artDir = env.ArtifactsDir
		mu.Lock()
		defer mu.Unlock()
		if phase.Name == "next" {
			nextCount++
			if nextCount == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "retry"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// After run completes, feedback directory should be empty.
	// The outer loop's backward jump should have cleared stale feedback.
	feedback, err := state.ReadAllFeedback(artDir)
	if err != nil {
		t.Fatal(err)
	}
	if feedback != "" {
		t.Fatalf("expected no feedback after outer loop, got: %s", feedback)
	}
}

func TestRun_AuditStateOnCompletion(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	loaded, err := state.Load(auditDir)
	if err != nil {
		t.Fatalf("audit state not found: %v", err)
	}
	if loaded.Status != state.StatusCompleted {
		t.Fatalf("audit status = %q, want completed", loaded.Status)
	}
}

func TestRun_AuditStateOnFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected failure")
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	loaded, loadErr := state.Load(auditDir)
	if loadErr != nil {
		t.Fatalf("audit state not found: %v", loadErr)
	}
	if loaded.Status != state.StatusFailed {
		t.Fatalf("audit status = %q, want failed", loaded.Status)
	}
}

func TestRun_ArchivesOutputsBeforeReDispatch(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo",
				Outputs: []string{"result.md"},
				Loop:    &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}

	var mu sync.Mutex
	bCount := 0
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			bCount++
			n := bCount
			mu.Unlock()
			os.WriteFile(filepath.Join(env.ArtifactsDir, "result.md"),
				[]byte(fmt.Sprintf("iteration %d", n)), 0644)
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "b failed"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	archived := state.AuditOutputPath(auditDir, 1, 1, "result.md")
	data, err := os.ReadFile(archived)
	if err != nil {
		t.Fatalf("archived output not found: %v", err)
	}
	if string(data) != "iteration 1" {
		t.Fatalf("archived output = %q, want %q", string(data), "iteration 1")
	}
}

func TestRun_DispatchCountsSurviveResume(t *testing.T) {
	// Phase a succeeds, phase b fails — simulates an interrupted run.
	// On "resume" (second Runner with same dirs), dispatch counts should persist
	// so iter-1 from the first run is not overwritten.
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}

	artDir := filepath.Join(t.TempDir(), "artifacts")
	workDir := t.TempDir()

	// First run: both phases succeed, writing logs
	callCount := 0
	mock1 := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		callCount++
		logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
		os.MkdirAll(filepath.Dir(logPath), 0755)
		os.WriteFile(logPath, []byte(fmt.Sprintf("run1 call %d", callCount)), 0644)
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r1 := &Runner{
		Config: cfg,
		State:  &state.State{Status: state.StatusRunning},
		Env: &dispatch.Environment{
			ProjectRoot:  workDir,
			WorkDir:      workDir,
			ArtifactsDir: artDir,
			Ticket:       "TEST-1",
			PhaseCount:   len(cfg.Phases),
		},
		Dispatcher: mock1,
	}
	r1.Run(context.Background()) // will fail at phase b

	// Verify iter-1 exists for both phases
	auditDir := state.AuditDir(workDir, "TEST-1")
	if _, err := os.Stat(state.AuditLogPath(auditDir, 0, 1)); err != nil {
		t.Fatalf("iter-1 for phase a missing after first run: %v", err)
	}
	if _, err := os.Stat(state.AuditLogPath(auditDir, 1, 1)); err != nil {
		t.Fatalf("iter-1 for phase b missing after first run: %v", err)
	}

	// Second run (simulating resume): re-dispatches from phase b (index 1)
	mock2 := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
		os.MkdirAll(filepath.Dir(logPath), 0755)
		os.WriteFile(logPath, []byte("run2 "+phase.Name), 0644)
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r2 := &Runner{
		Config: cfg,
		State:  &state.State{PhaseIndex: 1, Status: state.StatusRunning, Ticket: "TEST-1"},
		Env: &dispatch.Environment{
			ProjectRoot:  workDir,
			WorkDir:      workDir,
			ArtifactsDir: artDir,
			Ticket:       "TEST-1",
			PhaseCount:   len(cfg.Phases),
		},
		Dispatcher: mock2,
	}
	if err := r2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// iter-1 for phase b should still have run1 content (not overwritten)
	data, err := os.ReadFile(state.AuditLogPath(auditDir, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "run1") {
		t.Fatalf("iter-1 should have run1 content, got %q", string(data))
	}

	// iter-2 for phase b should have run2 content
	data, err = os.ReadFile(state.AuditLogPath(auditDir, 1, 2))
	if err != nil {
		t.Fatalf("iter-2 for phase b missing after resume: %v", err)
	}
	if !strings.Contains(string(data), "run2") {
		t.Fatalf("iter-2 should have run2 content, got %q", string(data))
	}
}

func TestRun_ParallelArchivesLogs(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}

	var mu sync.Mutex
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
		os.MkdirAll(filepath.Dir(logPath), 0755)
		os.WriteFile(logPath, []byte("parallel log "+phase.Name), 0644)
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	for _, pi := range []struct {
		name string
		idx  int
	}{{"a", 0}, {"b", 1}} {
		path := state.AuditLogPath(auditDir, pi.idx, 1)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("parallel phase %q iter-1 log missing: %v", pi.name, err)
		}
		if !strings.Contains(string(data), pi.name) {
			t.Fatalf("parallel phase %q log = %q, expected to contain phase name", pi.name, string(data))
		}
	}
}

func TestRun_SavesAndClearsSessionIDOnSuccess(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "impl", Type: "agent", Prompt: "test.md"},
		},
	}
	mock := newMock()
	mock.results["impl"] = &dispatch.Result{
		ExitCode:  0,
		SessionID: "session-abc-123",
	}
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// After successful completion, PhaseSessionID should be cleared by Advance()
	if r.State.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q after success, want empty", r.State.PhaseSessionID)
	}
}

func TestRun_PersistsSessionIDOnFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "impl", Type: "agent", Prompt: "test.md"},
		},
	}
	mock := newMock()
	mock.results["impl"] = &dispatch.Result{
		ExitCode:  1,
		SessionID: "session-abc-456",
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	// Load state from disk and verify session ID was persisted
	st, loadErr := state.Load(r.Env.ArtifactsDir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if st.PhaseSessionID != "session-abc-456" {
		t.Fatalf("PhaseSessionID = %q, want %q", st.PhaseSessionID, "session-abc-456")
	}
}

func TestRun_ClearsResumeSessionIDAfterDispatch(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "impl", Type: "agent", Prompt: "test.md"},
		},
	}
	mock := newMock()
	mock.results["impl"] = &dispatch.Result{
		ExitCode:  0,
		SessionID: "session-new",
	}
	r := newTestRunner(t, cfg, mock)
	r.Env.ResumeSessionID = "session-old"

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// ResumeSessionID should be cleared after dispatch
	if r.Env.ResumeSessionID != "" {
		t.Fatalf("ResumeSessionID = %q after dispatch, want empty", r.Env.ResumeSessionID)
	}
}

func TestRun_NoSessionIDForScriptPhase(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["build"] = &dispatch.Result{
		ExitCode:  0,
		SessionID: "should-be-ignored",
	}
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if r.State.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q for script phase, want empty", r.State.PhaseSessionID)
	}
}
