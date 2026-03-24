package debug

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func makeToolCalls(n int) []ToolCall {
	calls := make([]ToolCall, n)
	for i := range calls {
		calls[i] = ToolCall{
			Name:    fmt.Sprintf("Tool%d", i+1),
			Summary: fmt.Sprintf("summary%d", i+1),
		}
	}
	return calls
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

func TestRun_ArchivedRun(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Build the feature.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: prompt.md\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath, dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Create artifacts in history subdir; live dir is empty (no state.json)
	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	histDir := filepath.Join(artifactsDir, "history", "2026-01-01T00-00-00.000")
	logsDir := filepath.Join(histDir, "logs")
	promptsDir := filepath.Join(histDir, "prompts")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logContent := "⚡ Read file.go\nsome output\n"
	if err := os.WriteFile(filepath.Join(logsDir, "phase-1.log"), []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "phase-1.md"), []byte("rendered prompt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(histDir, "state.json"),
		[]byte(`{"phase_index":1,"ticket":"TEST-1","status":"completed"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(histDir, "timing.json"),
		[]byte(`{"entries":[{"phase":"plan","start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:01:00Z","duration":"1m 00s"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(histDir, "costs.json"),
		[]byte(`{"phases":[{"name":"plan","phase_index":0,"cost_usd":0.05,"input_tokens":1000,"output_tokens":500}],"total_cost_usd":0.05}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(dir, cfg, 0, "TEST-1", ""); err != nil {
		t.Errorf("Run returned error for archived run: %v", err)
	}
}

func TestRun_PartialArchive(t *testing.T) {
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

	// Create artifacts dir with a history entry that has NO state.json (partial archive)
	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	histDir := filepath.Join(artifactsDir, "history", "2026-01-01T00-00-00.000")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatal(err)
	}
	// history entry exists but has no state.json — simulates partial archive

	err = Run(dir, cfg, 0, "TEST-1", "")
	if err == nil {
		t.Fatal("expected error for partial archive, got nil")
	}
	if !strings.Contains(err.Error(), "no log file found") {
		t.Errorf("expected 'no log file found' error, got: %v", err)
	}
}

func TestRun_ScriptPhase(t *testing.T) {
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

	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	logsDir := filepath.Join(artifactsDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	logContent := "building...\nok\n"
	if err := os.WriteFile(filepath.Join(logsDir, "phase-1.log"), []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	stateJSON := `{"phase_index":1,"status":"completed"}`
	if err := os.WriteFile(filepath.Join(artifactsDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}

	timingJSON := `{"entries":[{"phase":"build","start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:02:00Z","duration":"2m 00s"}]}`
	if err := os.WriteFile(filepath.Join(artifactsDir, "timing.json"), []byte(timingJSON), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(dir, cfg, 0, "TEST-1", ""); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestRender_ScriptPhase(t *testing.T) {
	var buf bytes.Buffer
	info := &PhaseInfo{
		Name:       "build",
		Type:       "script",
		Index:      0,
		Total:      1,
		Duration:   2 * time.Minute,
		RunCommand: "make build",
		ExitStatus: "success",
	}
	render(&buf, info)
	out := buf.String()

	for _, want := range []string{
		"Phase:", "build", "(script)",
		"Duration:", "2m",
		"Command: make build",
		"Artifacts written:", "none declared",
		"Feedback:", "none",
		"Exit:", "success",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	for _, absent := range []string{"Cost:", "Tokens:", "Rendered prompt:", "Tool calls"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q in script output:\n%s", absent, out)
		}
	}
}

func TestRender_Hooks(t *testing.T) {
	t.Run("both hooks", func(t *testing.T) {
		var buf bytes.Buffer
		render(&buf, &PhaseInfo{
			Type:    "script",
			PreRun:  "echo setup",
			PostRun: "echo cleanup",
		})
		out := buf.String()
		if !strings.Contains(out, "Pre-run:  echo setup") {
			t.Errorf("missing pre-run; got:\n%s", out)
		}
		if !strings.Contains(out, "Post-run: echo cleanup") {
			t.Errorf("missing post-run; got:\n%s", out)
		}
	})

	t.Run("pre-run only", func(t *testing.T) {
		var buf bytes.Buffer
		render(&buf, &PhaseInfo{Type: "script", PreRun: "make deps"})
		out := buf.String()
		if !strings.Contains(out, "Pre-run:  make deps") {
			t.Errorf("missing pre-run; got:\n%s", out)
		}
		if strings.Contains(out, "Post-run:") {
			t.Errorf("unexpected post-run in output:\n%s", out)
		}
	})

	t.Run("post-run only", func(t *testing.T) {
		var buf bytes.Buffer
		render(&buf, &PhaseInfo{Type: "script", PostRun: "rm -rf tmp/"})
		out := buf.String()
		if !strings.Contains(out, "Post-run: rm -rf tmp/") {
			t.Errorf("missing post-run; got:\n%s", out)
		}
		if strings.Contains(out, "Pre-run:") {
			t.Errorf("unexpected pre-run in output:\n%s", out)
		}
	})

	t.Run("no hooks", func(t *testing.T) {
		var buf bytes.Buffer
		render(&buf, &PhaseInfo{Type: "script"})
		out := buf.String()
		if strings.Contains(out, "Pre-run:") {
			t.Errorf("unexpected pre-run in output:\n%s", out)
		}
		if strings.Contains(out, "Post-run:") {
			t.Errorf("unexpected post-run in output:\n%s", out)
		}
	})

	t.Run("long hook truncated", func(t *testing.T) {
		// 70-char string: first 57 chars + "..." should appear; full string should not
		long := strings.Repeat("a", 57) + strings.Repeat("b", 13) // 70 chars total
		var buf bytes.Buffer
		render(&buf, &PhaseInfo{Type: "script", PreRun: long})
		out := buf.String()
		want := strings.Repeat("a", 57) + "..."
		if !strings.Contains(out, want) {
			t.Errorf("missing truncated form %q; got:\n%s", want, out)
		}
		if strings.Contains(out, long) {
			t.Errorf("full 70-char string should not appear in output:\n%s", out)
		}
	})
}

func TestRender_ToolCallTruncation(t *testing.T) {
	t.Run("n=20 no truncation", func(t *testing.T) {
		var buf bytes.Buffer
		info := PhaseInfo{
			Type:      "agent",
			ToolCalls: makeToolCalls(20),
		}
		render(&buf, &info)
		out := buf.String()

		if !strings.Contains(out, "Tool calls (20):") {
			t.Errorf("missing header; got:\n%s", out)
		}
		for i := 1; i <= 20; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in output", want)
			}
		}
		if strings.Contains(out, "...") {
			t.Errorf("unexpected '...' in output for n=20")
		}
	})

	t.Run("n=21 first truncation", func(t *testing.T) {
		var buf bytes.Buffer
		info := PhaseInfo{
			Type:      "agent",
			ToolCalls: makeToolCalls(21),
		}
		render(&buf, &info)
		out := buf.String()

		if !strings.Contains(out, "Tool calls (21):") {
			t.Errorf("missing header; got:\n%s", out)
		}
		for i := 1; i <= 15; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if !strings.Contains(out, want) {
				t.Errorf("missing first-15 entry %q", want)
			}
		}
		if !strings.Contains(out, "... (21 total)") {
			t.Errorf("missing separator '... (21 total)'")
		}
		for i := 17; i <= 21; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if !strings.Contains(out, want) {
				t.Errorf("missing last-5 entry %q", want)
			}
		}
		if strings.Contains(out, "Tool16 summary16") {
			t.Errorf("truncated entry Tool16 should not appear")
		}
	})

	t.Run("n=30 large N", func(t *testing.T) {
		var buf bytes.Buffer
		info := PhaseInfo{
			Type:      "agent",
			ToolCalls: makeToolCalls(30),
		}
		render(&buf, &info)
		out := buf.String()

		if !strings.Contains(out, "Tool calls (30):") {
			t.Errorf("missing header; got:\n%s", out)
		}
		for i := 1; i <= 15; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if !strings.Contains(out, want) {
				t.Errorf("missing first-15 entry %q", want)
			}
		}
		if !strings.Contains(out, "... (30 total)") {
			t.Errorf("missing separator '... (30 total)'")
		}
		for i := 26; i <= 30; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if !strings.Contains(out, want) {
				t.Errorf("missing last-5 entry %q", want)
			}
		}
		for i := 16; i <= 25; i++ {
			want := fmt.Sprintf("Tool%d summary%d", i, i)
			if strings.Contains(out, want) {
				t.Errorf("truncated entry %q should not appear", want)
			}
		}
	})
}

func TestReadFeedbackFiles(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, dir string)
		pathFn func(dir string) string
		want   []FeedbackFile
	}{
		{
			name:   "dir does not exist",
			setup:  func(t *testing.T, dir string) {},
			pathFn: func(dir string) string { return filepath.Join(dir, "nonexistent") },
			want:   nil,
		},
		{
			name: "empty dir",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
			},
			want: nil,
		},
		{
			name: "zero size files skipped",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-build.md"), []byte{}, 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: nil,
		},
		{
			name: "non-matching files filtered",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "notes.txt"), []byte("content"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-build.txt"), []byte("content"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: nil,
		},
		{
			name: "subdirectories skipped",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback", "from-build.md"), 0755); err != nil {
					t.Fatal(err)
				}
			},
			want: nil,
		},
		{
			name: "single valid file",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-build.md"), []byte("fix this"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: []FeedbackFile{{FromPhase: "build", Size: 8}},
		},
		{
			name: "multiple files sorted",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-review.md"), []byte("0123456789"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-build.md"), []byte("01234"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: []FeedbackFile{{FromPhase: "build", Size: 5}, {FromPhase: "review", Size: 10}},
		},
		{
			name: "mixed valid and invalid",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "feedback"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-build.md"), []byte("fix this"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "from-empty.md"), []byte{}, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "feedback", "readme.txt"), []byte("hello"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: []FeedbackFile{{FromPhase: "build", Size: 8}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			path := dir
			if tc.pathFn != nil {
				path = tc.pathFn(dir)
			}
			got := readFeedbackFiles(path)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].FromPhase != w.FromPhase {
					t.Errorf("[%d].FromPhase = %q, want %q", i, got[i].FromPhase, w.FromPhase)
				}
				if got[i].Size != w.Size {
					t.Errorf("[%d].Size = %d, want %d", i, got[i].Size, w.Size)
				}
			}
		})
	}
}

func TestScanLogForStatus(t *testing.T) {
	tests := []struct {
		name       string
		logContent string
		create     bool
		phaseName  string
		want       string
	}{
		{
			name:      "file does not exist",
			create:    false,
			phaseName: "build",
			want:      "",
		},
		{
			name:       "empty file",
			logContent: "",
			create:     true,
			phaseName:  "build",
			want:       "",
		},
		{
			name:       "no status markers",
			logContent: "line1\nline2\nline3\n",
			create:     true,
			phaseName:  "build",
			want:       "",
		},
		{
			name:       "failed status near end",
			logContent: "output\nmore output\n[orc] phase \"build\" failed: exit code 1\n",
			create:     true,
			phaseName:  "build",
			want:       "failed: exit code 1",
		},
		{
			name:       "interrupted status",
			logContent: "output\n[orc] phase interrupted: SIGINT\n",
			create:     true,
			phaseName:  "build",
			want:       "interrupted",
		},
		{
			name:       "status on last line no trailing newline",
			logContent: "output\n[orc] phase \"deploy\" failed: timeout",
			create:     true,
			phaseName:  "deploy",
			want:       "failed: timeout",
		},
		{
			name:       "status on first line",
			logContent: "[orc] phase \"test\" failed: oom\nmore output\n",
			create:     true,
			phaseName:  "test",
			want:       "failed: oom",
		},
		{
			name:       "multiple markers last wins",
			logContent: "[orc] phase \"test\" failed: exit code 1\nmiddle\n[orc] phase \"test\" failed: exit code 2\n",
			create:     true,
			phaseName:  "test",
			want:       "failed: exit code 2",
		},
		{
			name:       "phase name mismatch",
			logContent: "[orc] phase \"other\" failed: exit code 1\n",
			create:     true,
			phaseName:  "build",
			want:       "",
		},
		{
			name:       "interrupted last beats failed",
			logContent: "[orc] phase \"build\" failed: exit code 1\n[orc] phase interrupted: SIGINT\n",
			create:     true,
			phaseName:  "build",
			want:       "interrupted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "phase.log")
			if tc.create {
				if err := os.WriteFile(logPath, []byte(tc.logContent), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got := scanLogForStatus(logPath, tc.phaseName)
			if got != tc.want {
				t.Errorf("scanLogForStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}
