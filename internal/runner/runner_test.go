package runner

import (
	"context"
	"fmt"
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

func TestRun_FailNoOnFail(t *testing.T) {
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

func TestRun_OnFailLoopsBack(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo", OnFail: &config.OnFail{Goto: "a", Max: 2}},
		},
	}
	mock := newMock()

	// c fails on first call, then succeeds
	failCount := 0
	mu := sync.Mutex{}
	mock.results["c"] = nil // will be handled by custom logic below

	// Override dispatch to track c's behavior
	callNum := 0
	customMock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		mock.mu.Lock()
		mock.calls = append(mock.calls, phase.Name)
		mock.mu.Unlock()

		if phase.Name == "c" {
			mu.Lock()
			failCount++
			n := failCount
			mu.Unlock()
			_ = callNum
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "c failed"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, customMock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.Status != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.Status)
	}

	// Verify feedback file was written
	fbPath := filepath.Join(r.Env.ArtifactsDir, "feedback", "from-c.md")
	data, err := os.ReadFile(fbPath)
	if err != nil {
		t.Fatalf("feedback file not written: %v", err)
	}
	if !strings.Contains(string(data), "c failed") {
		t.Fatalf("feedback = %q", string(data))
	}
}

func TestRun_OnFailExceedsMax(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", OnFail: &config.OnFail{Goto: "a", Max: 2}},
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
	if err == nil || !strings.Contains(err.Error(), "exceeded max retries") {
		t.Fatalf("expected max retries error, got %v", err)
	}
}

func TestRun_OnFailLoopCountPersisted(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", OnFail: &config.OnFail{Goto: "a", Max: 3}},
		},
	}
	failCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			failCount++
			n := failCount
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
	if counts["b"] != 1 {
		t.Fatalf("loop count for b = %d, want 1", counts["b"])
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
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
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

// funcDispatcher is a Dispatcher backed by a function, for flexible test scenarios.
type funcDispatcher struct {
	fn func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error)
}

func (f *funcDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	return f.fn(ctx, phase, env)
}

// Suppress ux output during tests by ensuring it doesn't panic.
// The ux package writes to stdout which is fine in test output.
func init() {
	// No-op: ux functions write to stdout, which is captured by `go test`.
	_ = fmt.Sprintf // avoid unused import
}
