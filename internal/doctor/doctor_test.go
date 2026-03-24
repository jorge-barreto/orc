package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestGatherLog_Short(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, ".orc", "artifacts")
	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)

	logPath := state.LogPath(artifactsDir, 0)
	os.WriteFile(logPath, []byte("line 1\nline 2\nline 3"), 0644)

	result := gatherLog(artifactsDir, 0)
	if result != "line 1\nline 2\nline 3" {
		t.Errorf("expected full content, got %q", result)
	}
}

func TestGatherLog_Long(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, ".orc", "artifacts")
	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)

	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "log line")
	}
	logPath := state.LogPath(artifactsDir, 0)
	os.WriteFile(logPath, []byte(strings.Join(lines, "\n")), 0644)

	result := gatherLog(artifactsDir, 0)
	if !strings.HasPrefix(result, "... (truncated to last 200 lines)") {
		t.Errorf("expected truncation prefix, got %q", result[:60])
	}
	// Count lines in output (prefix line + 200 content lines)
	outLines := strings.Split(result, "\n")
	// The truncated output: 1 prefix line + 200 content lines
	if len(outLines) < 200 {
		t.Errorf("expected at least 200 lines, got %d", len(outLines))
	}
}

func TestGatherLog_Missing(t *testing.T) {
	dir := t.TempDir()
	result := gatherLog(dir, 0)
	if result != "(no log file found)" {
		t.Errorf("expected missing placeholder, got %q", result)
	}
}

func TestGatherPhaseConfig_Agent(t *testing.T) {
	phase := config.Phase{
		Name:   "implement",
		Type:   "agent",
		Prompt: ".orc/prompts/implement.md",
		Model:  "opus",
	}
	result := gatherPhaseConfig(phase)
	if !strings.Contains(result, "Name: implement") {
		t.Error("missing name")
	}
	if !strings.Contains(result, "Type: agent") {
		t.Error("missing type")
	}
	if !strings.Contains(result, "Prompt file: .orc/prompts/implement.md") {
		t.Error("missing prompt")
	}
	if !strings.Contains(result, "Model: opus") {
		t.Error("missing model")
	}
}

func TestGatherPhaseConfig_Script(t *testing.T) {
	phase := config.Phase{
		Name: "build",
		Type: "script",
		Run:  "make build",
	}
	result := gatherPhaseConfig(phase)
	if !strings.Contains(result, "Run: make build") {
		t.Error("missing run command")
	}
	if strings.Contains(result, "Prompt") {
		t.Error("should not contain prompt for script phase")
	}
}

func TestGatherPhaseConfig_WithLoop(t *testing.T) {
	phase := config.Phase{
		Name: "test",
		Type: "script",
		Run:  "make test",
		Loop: &config.Loop{
			Goto: "implement",
			Min:  1,
			Max:  3,
		},
	}
	result := gatherPhaseConfig(phase)
	if !strings.Contains(result, "Loop: goto implement (min 1, max 3)") {
		t.Error("missing loop info")
	}
}

func TestGatherPhaseConfig_WithLoopOnExhaust(t *testing.T) {
	phase := config.Phase{
		Name: "test",
		Type: "script",
		Run:  "make test",
		Loop: &config.Loop{
			Goto: "implement",
			Min:  1,
			Max:  3,
			OnExhaust: &config.OnExhaust{
				Goto: "plan",
				Max:  2,
			},
		},
	}
	result := gatherPhaseConfig(phase)
	if !strings.Contains(result, "Loop: goto implement (min 1, max 3)") {
		t.Error("missing loop info")
	}
	if !strings.Contains(result, "on-exhaust: goto plan (max 2)") {
		t.Error("missing on-exhaust info")
	}
}

func TestGatherFeedback_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	fbDir := filepath.Join(dir, "feedback")
	os.MkdirAll(fbDir, 0755)
	os.WriteFile(filepath.Join(fbDir, "from-test.md"), []byte("test failed"), 0644)
	os.WriteFile(filepath.Join(fbDir, "from-build.md"), []byte("build error"), 0644)

	result := gatherFeedback(dir)
	if !strings.Contains(result, "from-test.md") {
		t.Error("missing test feedback file")
	}
	if !strings.Contains(result, "test failed") {
		t.Error("missing test feedback content")
	}
	if !strings.Contains(result, "from-build.md") {
		t.Error("missing build feedback file")
	}
	if !strings.Contains(result, "build error") {
		t.Error("missing build feedback content")
	}
}

func TestGatherFeedback_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "feedback"), 0755)

	result := gatherFeedback(dir)
	if result != "" {
		t.Errorf("expected empty string for empty dir, got %q", result)
	}
}

func TestGatherFeedback_MissingDir(t *testing.T) {
	dir := t.TempDir()
	result := gatherFeedback(dir)
	if result != "" {
		t.Errorf("expected empty string for missing dir, got %q", result)
	}
}

func TestGatherTiming_WithData(t *testing.T) {
	dir := t.TempDir()
	timing := state.NewTiming([]state.TimingEntry{
		{Phase: "build", Duration: "1m 30s"},
		{Phase: "test", Duration: "0m 45s"},
	})
	timing.Flush(dir)

	result := gatherTiming(dir)
	if !strings.Contains(result, "test") {
		t.Error("missing phase name")
	}
	if !strings.Contains(result, "0m 45s") {
		t.Error("missing duration")
	}
}

func TestGatherTiming_MissingEnd(t *testing.T) {
	dir := t.TempDir()
	timing := state.NewTiming([]state.TimingEntry{
		{Phase: "build"},
	})
	timing.Flush(dir)

	result := gatherTiming(dir)
	if !strings.Contains(result, "did not complete") {
		t.Errorf("expected 'did not complete', got %q", result)
	}
}

func TestGatherTiming_NoData(t *testing.T) {
	dir := t.TempDir()
	result := gatherTiming(dir)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestRun_NotFailed(t *testing.T) {
	st := &state.State{Status: state.StatusCompleted}
	cfg := &config.Config{Phases: []config.Phase{{Name: "test"}}}
	err := Run(context.Background(), t.TempDir(), t.TempDir(), cfg, st)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRun_PhaseIndexOutOfRange(t *testing.T) {
	st := &state.State{Status: state.StatusFailed, PhaseIndex: 5}
	cfg := &config.Config{Phases: []config.Phase{{Name: "test"}}}
	err := Run(context.Background(), t.TempDir(), t.TempDir(), cfg, st)
	if err == nil {
		t.Error("expected error for out of range phase index")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' error, got %v", err)
	}
}

func TestGatherTimingWithFallback_WorkflowNamespaced(t *testing.T) {
	projectRoot := t.TempDir()

	auditDir := state.AuditDirForWorkflow(projectRoot, "bugfix", "T-001")
	os.MkdirAll(auditDir, 0755)

	timing := state.NewTiming([]state.TimingEntry{
		{Phase: "plan", Duration: "1m 10s"},
		{Phase: "implement", Duration: "3m 20s"},
	})
	timing.Flush(auditDir)

	artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, "bugfix", "T-001")
	os.MkdirAll(artifactsDir, 0755)

	result := gatherTimingWithFallback(auditDir, artifactsDir)
	if !strings.Contains(result, "plan") {
		t.Error("missing plan phase in timing")
	}
	if !strings.Contains(result, "1m 10s") {
		t.Error("missing plan duration")
	}
	if !strings.Contains(result, "implement") {
		t.Error("missing implement phase in timing")
	}
	if !strings.Contains(result, "3m 20s") {
		t.Error("missing implement duration")
	}
}

func TestGatherIterationLogs_WorkflowNamespaced(t *testing.T) {
	projectRoot := t.TempDir()

	auditDir := state.AuditDirForWorkflow(projectRoot, "bugfix", "T-001")
	logsDir := filepath.Join(auditDir, "logs")
	os.MkdirAll(logsDir, 0755)

	os.WriteFile(state.AuditLogPath(auditDir, 1, 1), []byte("iter 1 wf output"), 0644)
	os.WriteFile(state.AuditLogPath(auditDir, 1, 2), []byte("iter 2 wf output"), 0644)

	result := gatherIterationLogs(auditDir, 1)
	if !strings.Contains(result, "iter 1 wf output") {
		t.Error("missing iteration 1 from workflow-namespaced audit dir")
	}
	if !strings.Contains(result, "iter 2 wf output") {
		t.Error("missing iteration 2 from workflow-namespaced audit dir")
	}
	if !strings.Contains(result, "phase-2.iter-1.log") {
		t.Error("missing iteration 1 header")
	}
}

func TestRun_WorkflowNamespaced_GathersData(t *testing.T) {
	projectRoot := t.TempDir()
	workflow := "bugfix"
	ticket := "T-001"

	auditDir := state.AuditDirForWorkflow(projectRoot, workflow, ticket)
	artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflow, ticket)
	state.EnsureDir(artifactsDir)
	os.MkdirAll(filepath.Join(auditDir, "logs"), 0755)

	os.WriteFile(state.LogPath(artifactsDir, 0), []byte("build failed: exit 1"), 0644)

	timing := state.NewTiming([]state.TimingEntry{
		{Phase: "build", Duration: "0m 15s"},
	})
	timing.Flush(auditDir)

	os.WriteFile(state.AuditLogPath(auditDir, 0, 1), []byte("prev attempt output"), 0644)

	st := &state.State{
		Status:     state.StatusFailed,
		PhaseIndex: 0,
		Ticket:     ticket,
		Workflow:   workflow,
	}

	cfg := &config.Config{
		Phases: []config.Phase{
			{Name: "build", Type: "script", Run: "make build"},
		},
	}

	// Clear PATH so claude binary is not findable — prevents real API calls.
	// t.Setenv automatically restores the original PATH when the test completes.
	t.Setenv("PATH", t.TempDir())

	err := Run(context.Background(), auditDir, artifactsDir, cfg, st)
	if err == nil {
		t.Fatal("expected error from runClaude (no claude binary), got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "claude") {
		t.Errorf("expected claude-related error, got: %v", err)
	}
}

func TestGatherAllLogs_MultiplePhases(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, ".orc", "artifacts")
	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)

	// Write logs for 3 phases
	os.WriteFile(state.LogPath(artifactsDir, 0), []byte("plan output"), 0644)
	os.WriteFile(state.LogPath(artifactsDir, 1), []byte("implement output"), 0644)
	os.WriteFile(state.LogPath(artifactsDir, 2), []byte("review output"), 0644)

	phases := []config.Phase{
		{Name: "plan"},
		{Name: "implement"},
		{Name: "review"},
	}

	// failedIdx=1 (implement), so we should get plan and review logs
	result := gatherAllLogs(artifactsDir, phases, 1)
	if !strings.Contains(result, "plan output") {
		t.Error("missing plan log")
	}
	if strings.Contains(result, "implement output") {
		t.Error("should not contain failed phase log")
	}
	if !strings.Contains(result, "review output") {
		t.Error("missing review log")
	}
	if !strings.Contains(result, "Phase 1: plan") {
		t.Error("missing phase header for plan")
	}
	if !strings.Contains(result, "Phase 3: review") {
		t.Error("missing phase header for review")
	}
}

func TestGatherAllLogs_MissingLogs(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, ".orc", "artifacts")
	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)

	// Only write log for phase 0
	os.WriteFile(state.LogPath(artifactsDir, 0), []byte("plan output"), 0644)

	phases := []config.Phase{
		{Name: "plan"},
		{Name: "implement"},
		{Name: "review"},
	}

	result := gatherAllLogs(artifactsDir, phases, 2)
	if !strings.Contains(result, "plan output") {
		t.Error("missing plan log")
	}
	// implement has no log — should be silently skipped
	if strings.Contains(result, "implement") {
		t.Error("should not mention phase with no log")
	}
}

func TestGatherIterationLogs_WithHistory(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, ".orc", "audit", "TEST-1")
	logsDir := filepath.Join(auditDir, "logs")
	os.MkdirAll(logsDir, 0755)

	// Write archived iteration logs for phase 2 (index 1)
	os.WriteFile(state.AuditLogPath(auditDir, 1, 1), []byte("iteration 1 output"), 0644)
	os.WriteFile(state.AuditLogPath(auditDir, 1, 2), []byte("iteration 2 output"), 0644)

	result := gatherIterationLogs(auditDir, 1)
	if !strings.Contains(result, "iteration 1 output") {
		t.Error("missing iteration 1")
	}
	if !strings.Contains(result, "iteration 2 output") {
		t.Error("missing iteration 2")
	}
	if !strings.Contains(result, "phase-2.iter-1.log") {
		t.Error("missing iteration 1 header")
	}
	if !strings.Contains(result, "phase-2.iter-2.log") {
		t.Error("missing iteration 2 header")
	}
}

func TestGatherIterationLogs_NoHistory(t *testing.T) {
	dir := t.TempDir()
	result := gatherIterationLogs(dir, 0)
	if result != "" {
		t.Errorf("expected empty string for no history, got %q", result)
	}
}

func TestTruncateLines_BelowThreshold(t *testing.T) {
	content := "line 1\nline 2\nline 3"
	result := truncateLines(content, 10)
	if result != content {
		t.Errorf("expected pass-through, got %q", result)
	}
}

func TestTruncateLines_AboveThreshold(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "log line")
	}
	content := strings.Join(lines, "\n")

	result := truncateLines(content, 10)
	if !strings.HasPrefix(result, "... (truncated to last 10 lines)") {
		t.Errorf("expected truncation prefix, got %q", result[:40])
	}
	outLines := strings.Split(result, "\n")
	// 1 prefix line + 10 content lines = 11
	if len(outLines) != 11 {
		t.Errorf("expected 11 lines, got %d", len(outLines))
	}
}
