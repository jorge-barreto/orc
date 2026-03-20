package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestParseToolCalls(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "phase-1.log")
	content := "some regular text\n⚡ Read CLAUDE.md\n⚡ Bash make test\n\nmid⚡line\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	calls, err := parseToolCalls(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "Read" || calls[0].Summary != "CLAUDE.md" {
		t.Errorf("calls[0] = %+v, want {Read, CLAUDE.md}", calls[0])
	}
	if calls[1].Name != "Bash" || calls[1].Summary != "make test" {
		t.Errorf("calls[1] = %+v, want {Bash, make test}", calls[1])
	}
}

func TestParseToolCalls_EmptyLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "phase-1.log")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	calls, err := parseToolCalls(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil slice, got %v", calls)
	}
}

func TestParseToolCalls_NoToolCalls(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "phase-1.log")
	content := "regular text\nno tool calls here\nanother line\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	calls, err := parseToolCalls(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil slice, got %v", calls)
	}
}

func TestFindMostRecentTicket(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".orc", "artifacts")

	// Create TICK-1/state.json
	tick1Dir := filepath.Join(base, "TICK-1")
	if err := os.MkdirAll(tick1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tick1Dir, "state.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create TICK-2/state.json
	tick2Dir := filepath.Join(base, "TICK-2")
	if err := os.MkdirAll(tick2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tick2Dir, "state.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Set TICK-2's state.json mtime 1 hour later
	tick2StatePath := filepath.Join(tick2Dir, "state.json")
	laterTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(tick2StatePath, laterTime, laterTime); err != nil {
		t.Fatal(err)
	}

	ticket, err := FindMostRecentTicket(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket != "TICK-2" {
		t.Errorf("expected TICK-2, got %s", ticket)
	}
}

func TestFindMostRecentTicket_NoTickets(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".orc", "artifacts")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := FindMostRecentTicket(dir, "")
	if err == nil || !strings.Contains(err.Error(), "no tickets found") {
		t.Errorf("expected 'no tickets found' error, got %v", err)
	}
}

func TestFindMostRecentTicket_WithWorkflow(t *testing.T) {
	dir := t.TempDir()
	tickDir := filepath.Join(dir, ".orc", "artifacts", "bugfix", "TICK-1")
	if err := os.MkdirAll(tickDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tickDir, "state.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	ticket, err := FindMostRecentTicket(dir, "bugfix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket != "TICK-1" {
		t.Errorf("expected TICK-1, got %s", ticket)
	}
}

func TestFormatTokenCount(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1K"},
		{1499, "1K"},
		{1500, "2K"},
		{15000, "15K"},
		{1200000, "1.2M"},
	}
	for _, tc := range cases {
		got := formatTokenCount(tc.input)
		if got != tc.expected {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func writeStateJSON(t *testing.T, dir string, phaseIndex int, status string) {
	t.Helper()
	type stateJSON struct {
		PhaseIndex int    `json:"phase_index"`
		Status     string `json:"status"`
	}
	data, err := json.Marshal(stateJSON{PhaseIndex: phaseIndex, Status: status})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestExitStatusFromLog(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		writeStateJSON(t, dir, 1, "running")
		logPath := filepath.Join(dir, "phase.log")
		if err := os.WriteFile(logPath, []byte("no markers\n"), 0644); err != nil {
			t.Fatal(err)
		}
		status := determineExitStatus(logPath, dir, 0, "test")
		if status != "success" {
			t.Errorf("expected success, got %q", status)
		}
	})

	t.Run("failed with log reason", func(t *testing.T) {
		dir := t.TempDir()
		writeStateJSON(t, dir, 0, "failed")
		logPath := filepath.Join(dir, "phase.log")
		logContent := `some output
[orc] phase "test" failed: exit code 1
`
		if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
			t.Fatal(err)
		}
		status := determineExitStatus(logPath, dir, 0, "test")
		if status != "failed: exit code 1" {
			t.Errorf("expected 'failed: exit code 1', got %q", status)
		}
	})

	t.Run("interrupted", func(t *testing.T) {
		dir := t.TempDir()
		writeStateJSON(t, dir, 0, "interrupted")
		logPath := filepath.Join(dir, "phase.log")
		if err := os.WriteFile(logPath, []byte("some output\n"), 0644); err != nil {
			t.Fatal(err)
		}
		status := determineExitStatus(logPath, dir, 0, "test")
		if status != "interrupted" {
			t.Errorf("expected interrupted, got %q", status)
		}
	})
}

func TestRun_MissingLogFile(t *testing.T) {
	dir := t.TempDir()

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := `name: test
phases:
  - name: build
    type: script
    run: make build
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Create artifacts dir but no log file
	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		t.Fatal(err)
	}

	err = Run(dir, cfg, 0, "TEST-1", "")
	if err == nil || !strings.Contains(err.Error(), "no log file found") {
		t.Errorf("expected 'no log file found' error, got %v", err)
	}
}

func TestRun_AgentPhase(t *testing.T) {
	dir := t.TempDir()

	// Write prompt file (relative path — Validate joins projectRoot + phase.Prompt)
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Build the feature.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write config.yaml
	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: prompt.md\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Create artifacts dir structure
	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	logsDir := filepath.Join(artifactsDir, "logs")
	promptsDir := filepath.Join(artifactsDir, "prompts")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write log file with tool calls
	logContent := "⚡ Read file.go\nsome output\n"
	if err := os.WriteFile(filepath.Join(logsDir, "phase-1.log"), []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write rendered prompt
	if err := os.WriteFile(filepath.Join(promptsDir, "phase-1.md"), []byte("rendered prompt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write state.json
	stateJSON := `{"phase_index":1,"ticket":"TEST-1","status":"completed"}`
	if err := os.WriteFile(filepath.Join(artifactsDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Write timing.json
	timingJSON := `{"entries":[{"phase":"plan","start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:01:00Z","duration":"1m 00s"}]}`
	if err := os.WriteFile(filepath.Join(artifactsDir, "timing.json"), []byte(timingJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Write costs.json
	costsJSON := `{"phases":[{"name":"plan","phase_index":0,"cost_usd":0.05,"input_tokens":1000,"output_tokens":500}],"total_cost_usd":0.05}`
	if err := os.WriteFile(filepath.Join(artifactsDir, "costs.json"), []byte(costsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(dir, cfg, 0, "TEST-1", ""); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}
