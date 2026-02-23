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
	artifactsDir := filepath.Join(dir, ".artifacts")
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
	artifactsDir := filepath.Join(dir, ".artifacts")
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

func TestGatherPhaseConfig_WithOnFail(t *testing.T) {
	phase := config.Phase{
		Name: "test",
		Type: "script",
		Run:  "make test",
		OnFail: &config.OnFail{
			Goto: "implement",
			Max:  3,
		},
	}
	result := gatherPhaseConfig(phase)
	if !strings.Contains(result, "On-fail: goto implement (max 3)") {
		t.Error("missing on-fail info")
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
	timing := &state.Timing{
		Entries: []state.TimingEntry{
			{Phase: "build", Duration: "1m 30s"},
			{Phase: "test", Duration: "0m 45s"},
		},
	}
	timing.Flush(dir)

	result := gatherTiming(dir, "test")
	if !strings.Contains(result, "test") {
		t.Error("missing phase name")
	}
	if !strings.Contains(result, "0m 45s") {
		t.Error("missing duration")
	}
}

func TestGatherTiming_MissingEnd(t *testing.T) {
	dir := t.TempDir()
	timing := &state.Timing{
		Entries: []state.TimingEntry{
			{Phase: "build"},
		},
	}
	timing.Flush(dir)

	result := gatherTiming(dir, "build")
	if !strings.Contains(result, "did not complete") {
		t.Errorf("expected 'did not complete', got %q", result)
	}
}

func TestGatherTiming_NoData(t *testing.T) {
	dir := t.TempDir()
	result := gatherTiming(dir, "nonexistent")
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
