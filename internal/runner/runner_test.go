package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// mockDispatcher records calls and returns configurable results.
type mockDispatcher struct {
	mu      sync.Mutex
	calls   []string
	envs    []*dispatch.Environment
	results map[string]*dispatch.Result
	errors  map[string]error
	delays  map[string]time.Duration
}

func newMock() *mockDispatcher {
	return &mockDispatcher{
		results: make(map[string]*dispatch.Result),
		errors:  make(map[string]error),
		delays:  make(map[string]time.Duration),
	}
}

func (m *mockDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	m.mu.Lock()
	m.calls = append(m.calls, phase.Name)
	m.envs = append(m.envs, env)
	m.mu.Unlock()

	if d, ok := m.delays[phase.Name]; ok {
		time.Sleep(d)
	}

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

func (m *mockDispatcher) capturedEnvs() []*dispatch.Environment {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*dispatch.Environment, len(m.envs))
	copy(cp, m.envs)
	return cp
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
		Dispatcher:   mock,
		HistoryLimit: 10,
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
	}
	if r.State.GetPhaseIndex() != 3 {
		t.Fatalf("PhaseIndex = %d", r.State.GetPhaseIndex())
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("calls = %v", calls)
	}
	if got := r.State.GetFailureCategory(); got != "" {
		t.Fatalf("FailureCategory = %q, want empty for successful run", got)
	}
}

func TestRun_QuietMode_EmitsJSONLines(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	origStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	defer wp.Close() // safety net: close writer even if Run() fails
	t.Cleanup(func() {
		os.Stdout = origStdout
		ux.QuietMode = false
	})

	ux.QuietMode = true
	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	wp.Close()
	var buf bytes.Buffer
	io.Copy(&buf, rp)

	lines := strings.Split(buf.String(), "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) != 4 {
		t.Fatalf("expected 4 JSON lines, got %d:\n%s", len(nonEmpty), buf.String())
	}

	type event struct {
		Phase  string                 `json:"phase"`
		Status string                 `json:"status"`
		Extra  map[string]interface{} `json:"-"`
	}

	parseEvent := func(line string) map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("failed to parse JSON line %q: %v", line, err)
		}
		return m
	}

	e0 := parseEvent(nonEmpty[0])
	if e0["phase"] != "a" || e0["status"] != "started" {
		t.Fatalf("line 0: got %v, want phase=a status=started", e0)
	}

	e1 := parseEvent(nonEmpty[1])
	if e1["phase"] != "a" || e1["status"] != "complete" {
		t.Fatalf("line 1: got %v, want phase=a status=complete", e1)
	}
	if _, ok := e1["duration_s"]; !ok {
		t.Fatalf("line 1: missing duration_s key: %v", e1)
	}

	e2 := parseEvent(nonEmpty[2])
	if e2["phase"] != "b" || e2["status"] != "started" {
		t.Fatalf("line 2: got %v, want phase=b status=started", e2)
	}

	e3 := parseEvent(nonEmpty[3])
	if e3["phase"] != "b" || e3["status"] != "complete" {
		t.Fatalf("line 3: got %v, want phase=b status=complete", e3)
	}
	if _, ok := e3["duration_s"]; !ok {
		t.Fatalf("line 3: missing duration_s key: %v", e3)
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
	assertExitCode(t, err, ExitPhaseFailure)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q", r.State.GetStatus())
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryScriptFailure {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryScriptFailure)
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	assertExitCode(t, err, ExitPhaseFailure)
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

	histEntries, err := os.ReadDir(filepath.Join(r.Env.ArtifactsDir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(r.Env.ArtifactsDir, "history", histEntries[len(histEntries)-1].Name())
	counts, err := state.LoadLoopCounts(runDir)
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
	}

	histEntries, err := os.ReadDir(filepath.Join(r.Env.ArtifactsDir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(r.Env.ArtifactsDir, "history", histEntries[len(histEntries)-1].Name())
	counts, err := state.LoadLoopCounts(runDir)
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
	assertExitCode(t, err, ExitPhaseFailure)
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
	r.State.SetPhase(2) // resume from phase c

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
	assertExitCode(t, err, ExitInterrupted)
	if r.State.GetStatus() != state.StatusInterrupted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q", r.State.GetStatus())
	}
}

func readTestMeta(t *testing.T, artifactsDir string, phaseIdx int) *state.PhaseMetadata {
	t.Helper()
	// Metadata is archived into history/ at the end of Run(); look there.
	histDir, err := state.LatestHistoryDir(artifactsDir)
	if err != nil || histDir == "" {
		t.Fatalf("finding latest history dir for phase %d: err=%v histDir=%q", phaseIdx, err, histDir)
	}
	path := state.MetaPath(histDir, phaseIdx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading meta for phase %d: %v", phaseIdx, err)
	}
	var meta state.PhaseMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parsing meta for phase %d: %v", phaseIdx, err)
	}
	return &meta
}

func TestRun_ParallelTimingPerPhase(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "fast", Type: "script", Run: "echo", ParallelWith: "slow"},
			{Name: "slow", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.delays["slow"] = 200 * time.Millisecond
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	fastMeta := readTestMeta(t, r.Env.ArtifactsDir, 0)
	slowMeta := readTestMeta(t, r.Env.ArtifactsDir, 1)

	if slowMeta.DurationSecs <= fastMeta.DurationSecs {
		t.Fatalf("slow duration (%f) should be > fast duration (%f)",
			slowMeta.DurationSecs, fastMeta.DurationSecs)
	}
	if !fastMeta.EndTime.Before(slowMeta.EndTime) {
		t.Fatalf("fast EndTime (%v) should be before slow EndTime (%v)",
			fastMeta.EndTime, slowMeta.EndTime)
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
	}
	// Both parallel phases + c should have been called
	if mock.callCount() != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", mock.callCount(), mock.callNames())
	}
}

func TestRun_ParallelNonAdjacentIntermediateSkipped(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "c"},
			{Name: "b", Type: "script", Run: "echo"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "d", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
	}
	// a, c, d dispatched; b skipped
	if mock.callCount() != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", mock.callCount(), mock.callNames())
	}
	for _, name := range mock.callNames() {
		if name == "b" {
			t.Fatal("phase b should not be dispatched (intermediate skipped)")
		}
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Phases) != 4 {
		t.Fatalf("len(phases) = %d, want 4", len(result.Phases))
	}
	cases := []struct {
		name   string
		status string
	}{
		{"a", "completed"},
		{"b", "skipped"},
		{"c", "completed"},
		{"d", "completed"},
	}
	for i, tc := range cases {
		if result.Phases[i].Name != tc.name {
			t.Fatalf("phases[%d].name = %q, want %q", i, result.Phases[i].Name, tc.name)
		}
		if result.Phases[i].Status != tc.status {
			t.Fatalf("phases[%d].status = %q, want %q", i, result.Phases[i].Status, tc.status)
		}
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
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q", r.State.GetStatus())
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

	histEntries, err := os.ReadDir(filepath.Join(r.Env.ArtifactsDir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(r.Env.ArtifactsDir, "history", histEntries[len(histEntries)-1].Name())
	loaded, err := state.Load(runDir)
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
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
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

func TestDryRunPrint_ExpandsORCPrefixedVars(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "check", Type: "script", Run: "echo",
				Condition: "test -f $ORC_ARTIFACTS_DIR/run-mode.txt"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

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

	// $ORC_ARTIFACTS_DIR must expand to the real artifacts dir, not empty string.
	if !strings.Contains(output, r.Env.ArtifactsDir) {
		t.Errorf("expected expanded ORC_ARTIFACTS_DIR %q in output:\n%s", r.Env.ArtifactsDir, output)
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
	assertExitCode(t, err, ExitPhaseFailure)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
	}
}

func TestRun_TimeoutExitCode(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Timeout: 1},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1, TimedOut: true}
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for timeout")
	}
	assertExitCode(t, err, ExitTimeout)
	if r.State.GetFailureCategory() != state.FailCategoryTimeout {
		t.Fatalf("failure_category = %q, want %q", r.State.GetFailureCategory(), state.FailCategoryTimeout)
	}
}

func TestRun_ParallelTimeoutExitCode(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", ParallelWith: "b", Timeout: 1},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1, TimedOut: true}
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for parallel timeout")
	}
	assertExitCode(t, err, ExitTimeout)
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
	assertExitCode(t, err, ExitCostLimit)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
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
	assertExitCode(t, err, ExitCostLimit)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
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
	assertExitCode(t, err, ExitCostLimit)
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
	assertExitCode(t, err, ExitCostLimit)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
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
	assertExitCode(t, err, ExitCostLimit)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want failed", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	assertExitCode(t, err, ExitPhaseFailure)
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
	assertExitCode(t, err, ExitPhaseFailure)

	// Failed runs are not archived; load loop counts directly from artifacts dir.
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
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q", r.State.GetStatus())
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
	histEntries, err := os.ReadDir(filepath.Join(r.Env.ArtifactsDir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(r.Env.ArtifactsDir, "history", histEntries[len(histEntries)-1].Name())
	counts, err := state.LoadLoopCounts(runDir)
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

func TestRun_AttemptCountsSurviveResume(t *testing.T) {
	// Phase a succeeds, phase b fails — simulates an interrupted run.
	// On "resume" (second Runner with same dirs), attempt counts should persist
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
	if r.State.GetSessionID() != "" {
		t.Fatalf("PhaseSessionID = %q after success, want empty", r.State.GetSessionID())
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

	// Load state from disk and verify session ID was persisted.
	// Failed runs are not archived; load directly from artifacts dir.
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

	if r.State.GetSessionID() != "" {
		t.Fatalf("PhaseSessionID = %q for script phase, want empty", r.State.GetSessionID())
	}
}

func TestRun_ParallelAgentSessionIDWarning(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "test.md", ParallelWith: "b"},
			{Name: "b", Type: "agent", Prompt: "test.md"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, SessionID: "session-parallel-a"}
	mock.results["b"] = &dispatch.Result{ExitCode: 0, SessionID: "session-parallel-b"}
	r := newTestRunner(t, cfg, mock)

	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	stderr := buf.String()
	if !strings.Contains(stderr, "session ID from parallel phase") {
		t.Fatalf("stderr should contain 'session ID from parallel phase', got: %q", stderr)
	}
	if !strings.Contains(stderr, `"a"`) {
		t.Fatalf("stderr should contain phase name \"a\", got: %q", stderr)
	}
	if !strings.Contains(stderr, `"b"`) {
		t.Fatalf("stderr should contain phase name \"b\", got: %q", stderr)
	}
}

func TestRun_ParallelScriptNoSessionIDWarning(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "session ID from parallel phase") {
		t.Fatalf("stderr should NOT contain 'session ID from parallel phase', got: %q", buf.String())
	}
}

func TestRun_PreRunSuccess(t *testing.T) {
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PreRun: "true"}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected dispatch called once, got %d", mock.callCount())
	}
}

func TestRun_PreRunFailure_SkipsDispatch(t *testing.T) {
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PreRun: "false"}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected failure")
	}
	if mock.callCount() != 0 {
		t.Fatalf("dispatch should not have been called, got %d calls", mock.callCount())
	}
}

func TestRun_PreRunFailure_PostRunStillRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post-ran")
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PreRun: "false", PostRun: "touch " + marker}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	_ = r.Run(context.Background()) // expected to fail
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("post-run should have run even after pre-run failure")
	}
}

func TestRun_PostRunFailure_OverridesSuccess(t *testing.T) {
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PostRun: "false"}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	assertExitCode(t, err, ExitPhaseFailure)
}

func TestRun_PostRunWarning_OnDispatchFailure(t *testing.T) {
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PostRun: "false"}},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected failure")
	}
	// Just verify it fails — the warning goes to stderr, which is tested by the behavior
}

func TestRun_HooksVarExpansion(t *testing.T) {
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", PreRun: "test -d $ARTIFACTS_DIR"}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	if err := state.EnsureDir(r.Env.ArtifactsDir); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRun_HooksNotRunOnConditionSkip(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "hook-ran")
	cfg := &config.Config{
		Name:   "test",
		Phases: []config.Phase{{Name: "a", Type: "script", Run: "echo", Condition: "false", PreRun: "touch " + marker}},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("pre-run hook should not run when condition skips phase")
	}
}

func TestRun_HooksRunEveryLoopIteration(t *testing.T) {
	markerDir := t.TempDir()
	marker := filepath.Join(markerDir, "pre-marker")
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo",
				PreRun: "echo x >> " + marker,
				Loop:   &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}
	callCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			mu.Lock()
			callCount++
			n := callCount
			mu.Unlock()
			if n == 1 {
				return &dispatch.Result{ExitCode: 1, Output: "retry"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker file not created: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 pre-run iterations, got %d: %q", len(lines), string(data))
	}
}

func TestRun_StepModeContinue(t *testing.T) {
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
	r.StepMode = true
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("calls = %v, want [a b c]", calls)
	}
}

func TestRun_StepModeAbort(t *testing.T) {
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
	r.StepMode = true
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "b" {
			return ux.StepAction{Type: "abort"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertExitCode(t, err, ExitInterrupted)
	if r.State.GetStatus() != state.StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", r.State.GetStatus())
	}
	for _, c := range mock.callNames() {
		if c == "c" {
			t.Fatal("phase c should not have been dispatched")
		}
	}
}

func TestRun_StepModeRewind(t *testing.T) {
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
	r.StepMode = true
	rewound := false
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "c" && !rewound {
			rewound = true
			return ux.StepAction{Type: "rewind", Target: "2"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	want := []string{"a", "b", "c", "b", "c"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i, c := range calls {
		if c != want[i] {
			t.Fatalf("calls[%d] = %q, want %q (calls=%v)", i, c, want[i], calls)
		}
	}
}

func TestRun_StepModeInvalidRewindReprompts(t *testing.T) {
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
	r.StepMode = true
	firstCall := true
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "a" && firstCall {
			firstCall = false
			return ux.StepAction{Type: "rewind", Target: "nonexistent"}
		}
		return ux.StepAction{Type: "continue"}
	}

	// Capture stderr to verify error message
	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want [a b c]", calls)
	}
	if !strings.Contains(buf.String(), "invalid rewind target") {
		t.Fatalf("stderr should contain 'invalid rewind target', got: %q", buf.String())
	}
}

func TestRun_StepModeRewindForwardRejected(t *testing.T) {
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
	r.StepMode = true
	firstCall := true
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "a" && firstCall {
			firstCall = false
			return ux.StepAction{Type: "rewind", Target: "3"} // forward: phase a is index 0, target 3 is index 2
		}
		return ux.StepAction{Type: "continue"}
	}

	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want [a b c]", calls)
	}
	if !strings.Contains(buf.String(), "rewind only jumps backward") {
		t.Fatalf("stderr should contain 'rewind only jumps backward', got: %q", buf.String())
	}
}

func TestRun_StepModeParallelRewindForwardRejected(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", ParallelWith: "c"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "d", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true
	rejected := false
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "b + c" && !rejected {
			rejected = true
			return ux.StepAction{Type: "rewind", Target: "4"} // forward: after b+c state index is 3, target 4 is index 3 >= 3
		}
		return ux.StepAction{Type: "continue"}
	}

	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	if !strings.Contains(buf.String(), "rewind only jumps backward") {
		t.Fatalf("stderr should contain 'rewind only jumps backward', got: %q", buf.String())
	}
}

func TestRun_RePromptRecordsCostAndArchive(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet",
				Outputs: []string{"result.md"}},
		},
	}

	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		// Write log so archivePhaseFiles can copy it for iter-1
		logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
		os.MkdirAll(filepath.Dir(logPath), 0755)
		os.WriteFile(logPath, []byte("main dispatch"), 0644)
		// Succeed but do NOT write result.md — triggers re-prompt
		return &dispatch.Result{
			ExitCode:     0,
			CostUSD:      0.10,
			InputTokens:  1000,
			OutputTokens: 500,
			Turns:        1,
		}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	r.RePromptFn = func(ctx context.Context, phase config.Phase, env *dispatch.Environment, prompt, sessionID string) (*dispatch.Result, error) {
		// Write log so archivePhaseFiles can copy it for iter-2
		logPath := state.LogPath(env.ArtifactsDir, env.PhaseIndex)
		os.MkdirAll(filepath.Dir(logPath), 0755)
		os.WriteFile(logPath, []byte("re-prompt dispatch"), 0644)
		// Create the missing output file so re-prompt "succeeds"
		if err := os.WriteFile(filepath.Join(env.ArtifactsDir, "result.md"), []byte("done"), 0644); err != nil {
			return nil, err
		}
		return &dispatch.Result{
			ExitCode:     0,
			CostUSD:      0.25,
			InputTokens:  5000,
			OutputTokens: 3000,
			Turns:        1,
		}, nil
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)

	// Both dispatches should be recorded in costs
	costs, err := state.LoadCosts(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.Phases) != 2 {
		t.Fatalf("expected 2 cost entries (main + re-prompt), got %d", len(costs.Phases))
	}
	const wantTotal = 0.10 + 0.25
	if costs.TotalCostUSD != wantTotal {
		t.Fatalf("TotalCostUSD = %f, want %f", costs.TotalCostUSD, wantTotal)
	}

	// attemptCount for phase 0 should be 2
	if r.attemptCount[0] != 2 {
		t.Fatalf("attemptCount[0] = %d, want 2", r.attemptCount[0])
	}

	// Audit log files must exist for both iter-1 and iter-2
	if _, err := os.Stat(state.AuditLogPath(auditDir, 0, 1)); err != nil {
		t.Fatalf("audit log iter-1 missing: %v", err)
	}
	if _, err := os.Stat(state.AuditLogPath(auditDir, 0, 2)); err != nil {
		t.Fatalf("audit log iter-2 missing: %v", err)
	}
}

func TestRun_RePromptFnError(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet",
				Outputs: []string{"result.md"}},
		},
	}

	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		// Succeed but do NOT write result.md — triggers re-prompt
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	r.RePromptFn = func(ctx context.Context, phase config.Phase, env *dispatch.Environment, prompt, sessionID string) (*dispatch.Result, error) {
		return nil, fmt.Errorf("agent crashed")
	}

	oldStderr := os.Stderr
	pr, pw, _ := os.Pipe()
	os.Stderr = pw

	err := r.Run(context.Background())

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, pr)
	os.Stderr = oldStderr

	if err == nil {
		t.Fatal("expected run to fail due to missing outputs")
	}
	stderr := buf.String()
	if !strings.Contains(stderr, "warning: re-prompt for missing outputs failed: agent crashed") {
		t.Fatalf("stderr should contain re-prompt warning, got: %q", stderr)
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryOutputMissing {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryOutputMissing)
	}
	assertExitCode(t, err, ExitPhaseFailure)
}

func TestRun_StepMode_PreRunHookFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", PreRun: "false"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true

	var prompts []string
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		prompts = append(prompts, phaseName)
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())

	if err == nil {
		t.Fatal("expected error from pre-run hook failure, got nil")
	}
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want %q", r.State.GetStatus(), state.StatusFailed)
	}
	if len(prompts) != 1 || prompts[0] != "a" {
		t.Fatalf("step prompts = %v, want [a]", prompts)
	}
	calls := mock.callNames()
	if len(calls) != 1 || calls[0] != "a" {
		t.Fatalf("dispatch calls = %v, want [a]", calls)
	}
}

func TestRun_StepMode_PostRunHookFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", PostRun: "false"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true

	var prompts []string
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		prompts = append(prompts, phaseName)
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())

	if err == nil {
		t.Fatal("expected error from post-run hook failure, got nil")
	}
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want %q", r.State.GetStatus(), state.StatusFailed)
	}
	if len(prompts) != 1 || prompts[0] != "a" {
		t.Fatalf("step prompts = %v, want [a]", prompts)
	}
	calls := mock.callNames()
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("dispatch calls = %v, want [a b]", calls)
	}
}

func TestRun_StepMode_HooksSuccess(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", PreRun: "true"},
			{Name: "b", Type: "script", Run: "echo", PreRun: "true", PostRun: "true"},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true

	var prompts []struct {
		idx  int
		name string
	}
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		prompts = append(prompts, struct {
			idx  int
			name string
		}{phaseIdx, phaseName})
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 3 || calls[0] != "a" || calls[1] != "b" || calls[2] != "c" {
		t.Fatalf("dispatch calls = %v, want [a b c]", calls)
	}
	if len(prompts) != 3 {
		t.Fatalf("got %d step prompts, want 3", len(prompts))
	}
	for i, p := range prompts {
		wantName := cfg.Phases[i].Name
		if p.idx != i || p.name != wantName {
			t.Fatalf("prompt[%d] = {%d, %q}, want {%d, %q}", i, p.idx, p.name, i, wantName)
		}
	}
}

func TestRun_PreRunHookGoError_Propagates(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root to enforce directory permissions")
	}

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", PreRun: "true"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	// Pre-create the artifacts directory structure so Run()'s EnsureDir is a no-op.
	// Then make the logs/ subdirectory read-only so runHookWithLog's os.OpenFile fails
	// with a permission error (a Go error, not a non-zero exit code).
	if err := state.EnsureDir(r.Env.ArtifactsDir); err != nil {
		t.Fatal(err)
	}
	logsDir := filepath.Join(r.Env.ArtifactsDir, "logs")
	if err := os.Chmod(logsDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(logsDir, 0755) })

	err := r.Run(context.Background())

	// The Go error from runHookWithLog must propagate through dispatchWithHooks → Run
	if err == nil {
		t.Fatal("expected error from pre-run hook Go error, got nil")
	}
	assertExitCode(t, err, ExitPhaseFailure)
	if r.State.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want %q", r.State.GetStatus(), state.StatusFailed)
	}
	// The dispatcher should NOT have been called — the pre-run hook Go error
	// short-circuits before dispatch (runner.go:878-879).
	if mock.callCount() != 0 {
		t.Fatalf("dispatch calls = %d, want 0 (pre-run Go error should skip dispatch)", mock.callCount())
	}
}

func TestRun_StepModeRewindClearsLoopCounts(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 2, Max: 3,
			}},
			{Name: "c", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true
	rewound := false
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "c" && !rewound {
			rewound = true
			return ux.StepAction{Type: "rewind", Target: "1"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	want := []string{"a", "b", "a", "b", "c", "a", "b", "a", "b", "c"}
	calls := mock.callNames()
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i, c := range calls {
		if c != want[i] {
			t.Fatalf("calls[%d] = %q, want %q (calls=%v)", i, c, want[i], calls)
		}
	}
}

func TestRun_StepModeParallelRewindClearsLoopCounts(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Loop: &config.Loop{
				Goto: "a", Min: 2, Max: 3,
			}},
			{Name: "b", Type: "script", Run: "echo", ParallelWith: "c"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "d", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true
	rewound := false
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "b + c" && !rewound {
			rewound = true
			return ux.StepAction{Type: "rewind", Target: "1"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 9 {
		t.Fatalf("len(calls) = %d, want 9 (calls=%v)", len(calls), calls)
	}
	// First 2 calls must be "a","a" (before first parallel)
	for i, want := range []string{"a", "a"} {
		if calls[i] != want {
			t.Fatalf("calls[%d] = %q, want %q (calls=%v)", i, calls[i], want, calls)
		}
	}
	// calls[2] and calls[3] are "b" and "c" in any order (parallel)
	pair1 := []string{calls[2], calls[3]}
	sort.Strings(pair1)
	if pair1[0] != "b" || pair1[1] != "c" {
		t.Fatalf("calls[2:4] = %v, want b+c in any order", calls[2:4])
	}
	// After rewind: next 2 must be "a","a"
	for i, want := range []string{"a", "a"} {
		if calls[4+i] != want {
			t.Fatalf("calls[%d] = %q, want %q (calls=%v)", 4+i, calls[4+i], want, calls)
		}
	}
	// calls[6] and calls[7] are "b" and "c" in any order
	pair2 := []string{calls[6], calls[7]}
	sort.Strings(pair2)
	if pair2[0] != "b" || pair2[1] != "c" {
		t.Fatalf("calls[6:8] = %v, want b+c in any order", calls[6:8])
	}
	// Last call must be "d"
	if calls[8] != "d" {
		t.Fatalf("calls[8] = %q, want \"d\" (calls=%v)", calls[8], calls)
	}
}

func TestRun_StepModeParallelRewindReturnsSentinel(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "pre", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", ParallelWith: "c"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "post", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true
	rewound := false
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "b + c" && !rewound {
			rewound = true
			return ux.StepAction{Type: "rewind", Target: "1"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.State.GetStatus() != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", r.State.GetStatus())
	}
	calls := mock.callNames()
	if len(calls) != 7 {
		t.Fatalf("len(calls) = %d, want 7 (calls=%v)", len(calls), calls)
	}
	// pre, b+c (parallel), pre again, b+c again, post
	if calls[0] != "pre" {
		t.Fatalf("calls[0] = %q, want \"pre\"", calls[0])
	}
	pair1 := []string{calls[1], calls[2]}
	sort.Strings(pair1)
	if pair1[0] != "b" || pair1[1] != "c" {
		t.Fatalf("calls[1:3] = %v, want b+c in any order", calls[1:3])
	}
	if calls[3] != "pre" {
		t.Fatalf("calls[3] = %q, want \"pre\"", calls[3])
	}
	pair2 := []string{calls[4], calls[5]}
	sort.Strings(pair2)
	if pair2[0] != "b" || pair2[1] != "c" {
		t.Fatalf("calls[4:6] = %v, want b+c in any order", calls[4:6])
	}
	if calls[6] != "post" {
		t.Fatalf("calls[6] = %q, want \"post\"", calls[6])
	}
}

func TestRun_ParallelAttemptCountInvariant(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", ParallelWith: "b"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}

	// Barrier ensures both dispatches are truly concurrent, maximising the
	// window for the race detector to catch any concurrent map write.
	var barrier sync.WaitGroup
	barrier.Add(2)
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		barrier.Done()
		barrier.Wait()
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Each parallel phase must have exactly 1 attempt recorded.
	// The increment lives in the sequential drain loop (runner.go), not in the
	// goroutines — so no mutex is needed and no race should fire.
	if r.attemptCount[0] != 1 {
		t.Fatalf("attemptCount[0] = %d, want 1", r.attemptCount[0])
	}
	if r.attemptCount[1] != 1 {
		t.Fatalf("attemptCount[1] = %d, want 1", r.attemptCount[1])
	}
}

func TestRun_StepModeParallelAbort(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "pre", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", ParallelWith: "c"},
			{Name: "c", Type: "script", Run: "echo"},
			{Name: "post", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.StepMode = true
	r.StepPromptFn = func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction {
		if phaseName == "pre" {
			return ux.StepAction{Type: "continue"}
		}
		if phaseName == "b + c" {
			return ux.StepAction{Type: "abort"}
		}
		return ux.StepAction{Type: "continue"}
	}

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from parallel abort, got nil")
	}
	assertExitCode(t, err, ExitInterrupted)
	if r.State.GetStatus() != state.StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", r.State.GetStatus())
	}
	for _, c := range mock.callNames() {
		if c == "post" {
			t.Fatal("phase post should not have been dispatched after parallel abort")
		}
	}
	calls := mock.callNames()
	if len(calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 (pre + b + c); calls=%v", len(calls), calls)
	}
	if calls[0] != "pre" {
		t.Fatalf("calls[0] = %q, want \"pre\"", calls[0])
	}
	pair := []string{calls[1], calls[2]}
	sort.Strings(pair)
	if pair[0] != "b" || pair[1] != "c" {
		t.Fatalf("calls[1:3] = %v, want b+c in any order", calls[1:3])
	}
}

func TestRun_ArchivesOnCompletion(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	artDir := r.Env.ArtifactsDir

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// history/ should exist with exactly one entry
	histDir := filepath.Join(artDir, "history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatalf("reading history dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}

	runDir := filepath.Join(histDir, entries[0].Name())

	// state.json should exist with status "completed"
	st, err := state.Load(runDir)
	if err != nil {
		t.Fatalf("loading archived state: %v", err)
	}
	if st.GetStatus() != state.StatusCompleted {
		t.Fatalf("archived status = %q, want %q", st.GetStatus(), state.StatusCompleted)
	}

	// timing.json and costs.json should be present
	if _, err := os.Stat(filepath.Join(runDir, "timing.json")); err != nil {
		t.Fatalf("timing.json missing from archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "costs.json")); err != nil {
		t.Fatalf("costs.json missing from archive: %v", err)
	}

	// top-level state.json should be gone
	if _, err := os.Stat(filepath.Join(artDir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("top-level state.json should have been archived away")
	}
}

func TestRun_ArchivesOnFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)
	artDir := r.Env.ArtifactsDir

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected failure")
	}

	// state.json should still exist in the artifacts root (not archived)
	st, loadErr := state.Load(artDir)
	if loadErr != nil {
		t.Fatalf("loading state: %v", loadErr)
	}
	if st.GetStatus() != state.StatusFailed {
		t.Fatalf("status = %q, want %q", st.GetStatus(), state.StatusFailed)
	}

	// history/ should NOT exist — failed runs are not archived
	histDir := filepath.Join(artDir, "history")
	if _, err := os.Stat(histDir); err == nil {
		entries, _ := os.ReadDir(histDir)
		t.Fatalf("expected no history dir after failure, but found %d entries", len(entries))
	}
}

func TestRun_DoesNotArchiveStaleOnStart(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	artDir := r.Env.ArtifactsDir

	// Pre-create artifacts dir with a stale state.json
	if err := os.MkdirAll(artDir, 0755); err != nil {
		t.Fatal(err)
	}
	staleState := &state.State{Status: state.StatusRunning, PhaseIndex: 1}
	if err := staleState.Save(artDir); err != nil {
		t.Fatal(err)
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Runner no longer archives stale state (moved to main.go).
	// history/ should have exactly 1 entry (from successful completion only).
	histDir := filepath.Join(artDir, "history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatalf("reading history dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry (completion only), got %d", len(entries))
	}
}

func TestRun_FailureCategory_AgentError(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryAgentError {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryAgentError)
	}
}

func TestRun_FailureCategory_GateRejection(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "approve", Type: "gate"},
		},
	}
	mock := newMock()
	mock.results["approve"] = &dispatch.Result{ExitCode: 1, Output: "no"}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for gate denial")
	}
	assertExitCode(t, err, ExitPhaseFailure)
	if got := r.State.GetFailureCategory(); got != state.FailCategoryGateRejection {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryGateRejection)
	}
}

func TestRun_FailureCategory_LoopExhaustion(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}
	alwaysFail := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			return &dispatch.Result{ExitCode: 1, Output: "fail"}, nil
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, alwaysFail)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected loop exhaustion error")
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryLoopExhaustion {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryLoopExhaustion)
	}
}

func TestRun_FailureCategory_CostOverrun(t *testing.T) {
	cfg := &config.Config{
		Name:    "test",
		MaxCost: 0.01,
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 0, CostUSD: 1.0, InputTokens: 1, OutputTokens: 1, Turns: 1}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected cost overrun error")
	}
	assertExitCode(t, err, ExitCostLimit)
	if got := r.State.GetFailureCategory(); got != state.FailCategoryCostOverrun {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryCostOverrun)
	}
}

func TestRun_FailureCategory_Timeout(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", Timeout: 1},
		},
	}
	mock := newMock()
	mock.results["a"] = &dispatch.Result{ExitCode: 1, TimedOut: true}
	r := newTestRunner(t, cfg, mock)
	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	assertExitCode(t, err, ExitTimeout)
	if got := r.State.GetFailureCategory(); got != state.FailCategoryTimeout {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryTimeout)
	}
}

func TestRun_FailureCategory_OutputMissing(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "agent", Prompt: "unused.md", Model: "sonnet", Outputs: []string{"result.md"}},
		},
	}
	mock := newMock()
	// Agent succeeds but doesn't create result.md
	mock.results["a"] = &dispatch.Result{ExitCode: 0}
	r := newTestRunner(t, cfg, mock)
	// Override RePromptFn to also not create the file
	r.RePromptFn = func(ctx context.Context, phase config.Phase, env *dispatch.Environment, prompt, sessionID string) (*dispatch.Result, error) {
		return &dispatch.Result{ExitCode: 0}, nil
	}

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected missing outputs error")
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryOutputMissing {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryOutputMissing)
	}
}

func TestRun_FailureCategory_Interrupted(t *testing.T) {
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
	if err == nil {
		t.Fatal("expected interrupted error")
	}
	if got := r.State.GetFailureCategory(); got != state.FailCategoryInterrupted {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryInterrupted)
	}
}

func TestRun_FinalStateSaveFailure_InfraExitCode(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root to enforce directory permissions")
	}

	cfg := &config.Config{Name: "test", Phases: nil}
	r := newTestRunner(t, cfg, newMock())

	// Pre-create all dirs EnsureDir expects so MkdirAll is a no-op on them
	artDir := r.Env.ArtifactsDir
	for _, sub := range []string{"", "prompts", "logs", "feedback"} {
		os.MkdirAll(filepath.Join(artDir, sub), 0755)
	}
	os.Chmod(artDir, 0555)
	t.Cleanup(func() { os.Chmod(artDir, 0755) })

	err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	assertExitCode(t, err, ExitInfraError)
	if got := r.State.GetFailureCategory(); got != state.FailCategoryStateSave {
		t.Fatalf("FailureCategory = %q, want %q", got, state.FailCategoryStateSave)
	}
}

func TestRun_WritesPhaseMetadata(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "plan", Type: "agent", Prompt: "unused.md", Model: "sonnet"},
		},
	}
	mock := newMock()
	mock.results["plan"] = &dispatch.Result{
		ExitCode:    0,
		ToolsUsed:   []string{"Read", "Edit"},
		ToolsDenied: []string{"Bash"},
	}
	r := newTestRunner(t, cfg, mock)

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// After Run completes, ArchiveRun moves logs/ into history/.
	// The audit dir has a stable copy of the meta file.
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditMetaPath := state.AuditMetaPath(auditDir, 0, 1)
	meta, err := state.LoadMetadata(auditMetaPath)
	if err != nil {
		t.Fatalf("LoadMetadata from audit: %v", err)
	}
	if meta == nil {
		t.Fatal("audit meta file not found at", auditMetaPath)
	}
	if meta.PhaseName != "plan" {
		t.Errorf("PhaseName = %q, want %q", meta.PhaseName, "plan")
	}
	if meta.PhaseType != "agent" {
		t.Errorf("PhaseType = %q, want %q", meta.PhaseType, "agent")
	}
	if len(meta.ToolsUsed) != 2 || meta.ToolsUsed[0] != "Read" || meta.ToolsUsed[1] != "Edit" {
		t.Errorf("ToolsUsed = %v, want [Read Edit]", meta.ToolsUsed)
	}
	if len(meta.ToolsDenied) != 1 || meta.ToolsDenied[0] != "Bash" {
		t.Errorf("ToolsDenied = %v, want [Bash]", meta.ToolsDenied)
	}
}

func TestRun_AutoModePassedToDispatcher(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "gate1", Type: "gate"},
			{Name: "a", Type: "script", Run: "echo ok"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)
	r.Env.AutoMode = true

	err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	calls := mock.callNames()
	if len(calls) != 2 || calls[0] != "gate1" || calls[1] != "a" {
		t.Fatalf("expected [gate1 a], got %v", calls)
	}

	envs := mock.capturedEnvs()
	for i, env := range envs {
		if !env.AutoMode {
			t.Errorf("call %d (%s): AutoMode = false, want true", i, calls[i])
		}
	}
}

func TestRun_RunResultOnSuccess(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Status != state.StatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", result.ExitCode)
	}
	if result.PhasesCompleted != 2 {
		t.Fatalf("phases_completed = %d, want 2", result.PhasesCompleted)
	}
	if result.PhasesTotal != 2 {
		t.Fatalf("phases_total = %d, want 2", result.PhasesTotal)
	}
	if result.FailedPhase != nil {
		t.Fatalf("failed_phase should be nil, got %q", *result.FailedPhase)
	}
	// Also verify audit dir copy
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditData, err := os.ReadFile(state.RunResultPath(auditDir))
	if err != nil {
		t.Fatalf("run-result.json not written to audit dir: %v", err)
	}
	var auditResult state.RunResult
	if err := json.Unmarshal(auditData, &auditResult); err != nil {
		t.Fatalf("invalid audit JSON: %v", err)
	}
	if auditResult.Status != state.StatusCompleted {
		t.Fatalf("audit status = %q, want completed", auditResult.Status)
	}
	// Verify artifacts and audit copies are identical (cached result reused, not rebuilt)
	if result.ExitCode != auditResult.ExitCode {
		t.Errorf("exit_code mismatch: artifacts=%d audit=%d", result.ExitCode, auditResult.ExitCode)
	}
	if result.PhasesCompleted != auditResult.PhasesCompleted {
		t.Errorf("phases_completed mismatch: artifacts=%d audit=%d", result.PhasesCompleted, auditResult.PhasesCompleted)
	}
	if result.PhasesTotal != auditResult.PhasesTotal {
		t.Errorf("phases_total mismatch: artifacts=%d audit=%d", result.PhasesTotal, auditResult.PhasesTotal)
	}
}

func TestRun_RunResultOnFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Status != state.StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.ExitCode != ExitPhaseFailure {
		t.Fatalf("exit_code = %d, want %d", result.ExitCode, ExitPhaseFailure)
	}
	if result.FailedPhase == nil || *result.FailedPhase != "b" {
		t.Fatalf("failed_phase = %v, want 'b'", result.FailedPhase)
	}
	if result.PhasesCompleted != 1 {
		t.Fatalf("phases_completed = %d, want 1", result.PhasesCompleted)
	}
	// Also verify audit dir copy
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditData, err := os.ReadFile(state.RunResultPath(auditDir))
	if err != nil {
		t.Fatalf("run-result.json not written to audit dir: %v", err)
	}
	var auditResult state.RunResult
	if err := json.Unmarshal(auditData, &auditResult); err != nil {
		t.Fatalf("invalid audit JSON: %v", err)
	}
	if auditResult.Status != state.StatusFailed {
		t.Fatalf("audit status = %q, want failed", auditResult.Status)
	}
	if auditResult.FailedPhase == nil || *auditResult.FailedPhase != "b" {
		t.Fatalf("audit failed_phase = %v, want 'b'", auditResult.FailedPhase)
	}
}

func TestRun_RunResultOnInterrupt(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "b" {
			cancel()
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(ctx); err == nil {
		t.Fatal("expected error")
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Status != state.StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", result.Status)
	}
	if result.ExitCode != ExitInterrupted {
		t.Fatalf("exit_code = %d, want %d", result.ExitCode, ExitInterrupted)
	}
	// Also verify audit dir copy
	auditDir := state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
	auditData, err := os.ReadFile(state.RunResultPath(auditDir))
	if err != nil {
		t.Fatalf("run-result.json not written to audit dir: %v", err)
	}
	var auditResult state.RunResult
	if err := json.Unmarshal(auditData, &auditResult); err != nil {
		t.Fatalf("invalid audit JSON: %v", err)
	}
	if auditResult.Status != state.StatusInterrupted {
		t.Fatalf("audit status = %q, want interrupted", auditResult.Status)
	}
}

func TestRun_RunResultPhasesOnSuccess(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Phases) != 2 {
		t.Fatalf("len(phases) = %d, want 2", len(result.Phases))
	}
	if result.Phases[0].Name != "a" {
		t.Fatalf("phases[0].name = %q, want a", result.Phases[0].Name)
	}
	if result.Phases[0].Status != "completed" {
		t.Fatalf("phases[0].status = %q, want completed", result.Phases[0].Status)
	}
	if result.Phases[0].DurationSeconds < 0 {
		t.Fatalf("phases[0].duration_seconds = %f, want >= 0", result.Phases[0].DurationSeconds)
	}
	if result.Phases[1].Name != "b" {
		t.Fatalf("phases[1].name = %q, want b", result.Phases[1].Name)
	}
	if result.Phases[1].Status != "completed" {
		t.Fatalf("phases[1].status = %q, want completed", result.Phases[1].Status)
	}
	if result.Phases[1].DurationSeconds < 0 {
		t.Fatalf("phases[1].duration_seconds = %f, want >= 0", result.Phases[1].DurationSeconds)
	}
}

func TestRun_RunResultPhasesOnFailure(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	mock.results["b"] = &dispatch.Result{ExitCode: 1}
	r := newTestRunner(t, cfg, mock)

	_ = r.Run(context.Background()) // expect error; don't fatal

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Phases[0].Name != "a" {
		t.Fatalf("phases[0].name = %q, want a", result.Phases[0].Name)
	}
	if result.Phases[0].Status != "completed" {
		t.Fatalf("phases[0].status = %q, want completed", result.Phases[0].Status)
	}
	if result.Phases[1].Name != "b" {
		t.Fatalf("phases[1].name = %q, want b", result.Phases[1].Name)
	}
	if result.Phases[1].Status != "failed" {
		t.Fatalf("phases[1].status = %q, want failed", result.Phases[1].Status)
	}
}

func TestRun_RunResultPhasesWithSkip(t *testing.T) {
	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo", Condition: "false"},
			{Name: "b", Type: "script", Run: "echo"},
		},
	}
	mock := newMock()
	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Phases[0].Status != "skipped" {
		t.Fatalf("phases[0].status = %q, want skipped", result.Phases[0].Status)
	}
	if result.Phases[1].Status != "completed" {
		t.Fatalf("phases[1].status = %q, want completed", result.Phases[1].Status)
	}
}

func TestRun_RunResultPhasesLoopReEntryOverridesSkip(t *testing.T) {
	markerFile := filepath.Join(t.TempDir(), "marker")

	cfg := &config.Config{
		Name: "test",
		Phases: []config.Phase{
			{Name: "a", Type: "script", Run: "echo"},
			{Name: "b", Type: "script", Run: "echo", Condition: "test -f " + markerFile},
			{Name: "c", Type: "script", Run: "echo", Loop: &config.Loop{Goto: "a", Min: 1, Max: 3}},
		},
	}

	cCount := 0
	mu := sync.Mutex{}
	mock := &funcDispatcher{fn: func(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
		if phase.Name == "c" {
			mu.Lock()
			cCount++
			n := cCount
			mu.Unlock()
			if n == 1 {
				// First pass: create marker so b's condition passes on re-entry, then fail c to trigger loop back
				os.WriteFile(markerFile, []byte("ok"), 0644)
				return &dispatch.Result{ExitCode: 1, Output: "c failed"}, nil
			}
		}
		return &dispatch.Result{ExitCode: 0}, nil
	}}

	r := newTestRunner(t, cfg, mock)

	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(state.RunResultPath(r.Env.ArtifactsDir))
	if err != nil {
		t.Fatalf("run-result.json not written: %v", err)
	}
	var result state.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Phases) != 3 {
		t.Fatalf("len(phases) = %d, want 3", len(result.Phases))
	}
	// Phase b was skipped on first pass but ran on second pass after loop re-entry.
	// It must be reported as "completed", not "skipped".
	if result.Phases[0].Status != "completed" {
		t.Fatalf("phases[0].status = %q, want completed", result.Phases[0].Status)
	}
	if result.Phases[1].Status != "completed" {
		t.Fatalf("phases[1].status = %q, want completed", result.Phases[1].Status)
	}
	if result.Phases[2].Status != "completed" {
		t.Fatalf("phases[2].status = %q, want completed", result.Phases[2].Status)
	}
}
