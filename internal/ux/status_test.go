package ux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func TestRenderStatusAll_EmptyConfig(t *testing.T) {
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 2, Status: "running"}, Costs: &state.CostData{}},
		{Ticket: "PROJ-2", State: &state.State{PhaseIndex: 4, Status: "completed"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, tickets)
	})
	if strings.Contains(out, "0/0") {
		t.Errorf("expected no '0/0' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "phase 3") {
		t.Errorf("expected 'phase 3', got:\n%s", out)
	}
	if !strings.Contains(out, "phase 5") {
		t.Errorf("expected 'phase 5', got:\n%s", out)
	}
}

func TestRenderStatusAll_WithPhases(t *testing.T) {
	cfg := &config.Config{
		Phases: []config.Phase{
			{Name: "plan"},
			{Name: "implement"},
			{Name: "test"},
			{Name: "review"},
			{Name: "ship"},
		},
	}
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 1, Status: "running"}, Costs: &state.CostData{}},
		{Ticket: "PROJ-2", State: &state.State{PhaseIndex: 5, Status: "completed"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(cfg, tickets)
	})
	if !strings.Contains(out, "2/5 (implement)") {
		t.Errorf("expected '2/5 (implement)', got:\n%s", out)
	}
	if !strings.Contains(out, "5/5") {
		t.Errorf("expected '5/5', got:\n%s", out)
	}
}

func TestRenderStatusAll_WorkflowColumn(t *testing.T) {
	tickets := []state.TicketSummary{
		{Ticket: "PROJ-1", State: &state.State{PhaseIndex: 0, Status: "running", Workflow: "bugfix"}, Costs: &state.CostData{}},
	}
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, tickets)
	})
	if !strings.Contains(out, "WORKFLOW") {
		t.Errorf("expected 'WORKFLOW' header, got:\n%s", out)
	}
	if !strings.Contains(out, "bugfix") {
		t.Errorf("expected 'bugfix' in output, got:\n%s", out)
	}
}

func TestRenderStatusAll_Empty(t *testing.T) {
	out := captureOutput(func() {
		RenderStatusAll(&config.Config{}, nil)
	})
	if !strings.Contains(out, "No tickets found.") {
		t.Errorf("expected 'No tickets found.', got:\n%s", out)
	}
}

func writeTiming(t *testing.T, dir string, timing *state.Timing) {
	t.Helper()
	data, err := json.Marshal(timing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "timing.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeCosts(t *testing.T, dir string, costs *state.CostData) {
	t.Helper()
	data, err := json.Marshal(costs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "costs.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *config.Config
		st              *state.State
		setupAudit      func(t *testing.T, dir string)
		setupArt        func(t *testing.T, dir string)
		artDirOverride  string
		wantContains    []string
		wantNotContains []string
		customAssert    func(t *testing.T, out string)
	}{
		{
			name: "a completed workflow with timing and costs",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
				{Name: "implement", Type: "agent"},
				{Name: "test", Type: "script"},
			}},
			st: &state.State{PhaseIndex: 3, Ticket: "PROJ-1", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(72 * time.Second), Duration: "1m 12s"},
					{Phase: "implement", Start: now, End: now.Add(120 * time.Second), Duration: "2m 00s"},
					{Phase: "test", Start: now, End: now.Add(5 * time.Second), Duration: "5s"},
				}})
				writeCosts(t, dir, &state.CostData{
					Phases: []state.CostEntry{
						{Name: "plan", CostUSD: 0.05, InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 500, CacheCreationInputTokens: 300},
						{Name: "implement", CostUSD: 0.10, InputTokens: 2000, OutputTokens: 400},
						{Name: "test", CostUSD: 0.02, InputTokens: 500, OutputTokens: 100},
					},
					TotalCostUSD: 0.17,
				})
			},
			setupArt: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantContains: []string{
				"PROJ-1", "completed",
				"#", "PHASE", "TIME", "COST", "TOKENS IN/OUT", "CACHE R/W",
				"plan", "implement", "test",
				"$0.05", "1000/200", "500/300", "$0.17",
				"Artifacts:", "plan.md",
			},
			wantNotContains: []string{"Remaining:"},
		},
		{
			name: "b in-progress with empty duration fallback",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
				{Name: "implement", Type: "agent"},
				{Name: "test", Type: "script"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "TEST2", Status: state.StatusRunning},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
					{Phase: "implement", Start: now, End: now.Add(30 * time.Second), Duration: ""},
				}})
			},
			wantContains: []string{"TEST2", "2/3 (implement)", "running", "plan", "Remaining:", "test"},
			customAssert: func(t *testing.T, out string) {
				if !strings.Contains(out, "-") {
					t.Errorf("expected dash from empty-duration fallback, got:\n%s", out)
				}
			},
		},
		{
			name: "c no timing data — fallback to phase list",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
				{Name: "implement", Type: "agent"},
				{Name: "test", Type: "script"},
			}},
			st:              &state.State{PhaseIndex: 2, Ticket: "PROJ-3", Status: state.StatusRunning},
			wantContains:    []string{"Completed:", "plan", "implement", "done", "Remaining:", "test"},
			wantNotContains: []string{"#"},
		},
		{
			name: "d empty state — no phases completed",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
				{Name: "implement", Type: "agent"},
			}},
			st:              &state.State{PhaseIndex: 0, Ticket: "PROJ-4", Status: state.StatusRunning},
			wantContains:    []string{"1/2 (plan)", "Remaining:", "plan", "implement"},
			wantNotContains: []string{"Completed:"},
		},
		{
			name: "e timing fallback to artifactsDir",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
				{Name: "implement", Type: "agent"},
			}},
			st: &state.State{PhaseIndex: 2, Ticket: "PROJ-5", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "timing.json"), []byte("{invalid"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			setupArt: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
					{Phase: "implement", Start: now, End: now.Add(90 * time.Second), Duration: "1m 30s"},
				}})
			},
			wantContains: []string{"plan", "implement", "#"},
		},
		{
			name: "f phase ran multiple times — shows run number",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "implement", Type: "agent"},
				{Name: "test", Type: "script"},
			}},
			st: &state.State{PhaseIndex: 2, Ticket: "PROJ-6", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "implement", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
					{Phase: "implement", Start: now, End: now.Add(50 * time.Second), Duration: "50s"},
					{Phase: "test", Start: now, End: now.Add(5 * time.Second), Duration: "5s"},
					{Phase: "test", Start: now, End: now.Add(3 * time.Second), Duration: "3s"},
				}})
				writeCosts(t, dir, &state.CostData{
					Phases: []state.CostEntry{
						{Name: "implement", CostUSD: 0.05},
						{Name: "implement", CostUSD: 0.04},
						{Name: "test", CostUSD: 0.01},
						{Name: "test", CostUSD: 0.01},
					},
					TotalCostUSD: 0.11,
				})
			},
			wantContains: []string{"PROJ-6", "RUN"},
			customAssert: func(t *testing.T, out string) {
				if c := strings.Count(out, "implement"); c < 2 {
					t.Errorf("expected 'implement' at least 2 times, got %d\nfull output:\n%s", c, out)
				}
				if c := strings.Count(out, "test"); c < 2 {
					t.Errorf("expected 'test' at least 2 times, got %d\nfull output:\n%s", c, out)
				}
			},
		},
		{
			name: "g costs fallback to artifactsDir",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "PROJ-7", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
				}})
				if err := os.WriteFile(filepath.Join(dir, "costs.json"), []byte("{invalid"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			setupArt: func(t *testing.T, dir string) {
				writeCosts(t, dir, &state.CostData{
					Phases:       []state.CostEntry{{Name: "plan", CostUSD: 0.10}},
					TotalCostUSD: 0.10,
				})
			},
			wantContains: []string{"$0.10"},
		},
		{
			name: "h artifacts subdirectory with multiple files",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "PROJ-8", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
				}})
			},
			setupArt: func(t *testing.T, dir string) {
				feedbackDir := filepath.Join(dir, "feedback")
				if err := os.MkdirAll(feedbackDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(feedbackDir, "round-1.md"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(feedbackDir, "round-2.md"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantContains: []string{"feedback/", "round-1.md", "round-2.md", " .. "},
		},
		{
			name: "i artifacts subdirectory with single file",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "PROJ-9", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
				}})
			},
			setupArt: func(t *testing.T, dir string) {
				logsDir := filepath.Join(dir, "logs")
				if err := os.MkdirAll(logsDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(logsDir, "phase-1.log"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantContains:    []string{"logs/", "phase-1.log"},
			wantNotContains: []string{" .. "},
		},
		{
			name: "k loop iteration counts in header and remaining",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "implement", Type: "agent"},
				{Name: "review", Type: "agent", Loop: &config.Loop{Goto: "implement", Max: 5}},
				{Name: "ship", Type: "script"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "LOOP-1", Status: state.StatusRunning},
			setupArt: func(t *testing.T, dir string) {
				data, _ := json.Marshal(map[string]int{"review": 3})
				if err := os.WriteFile(filepath.Join(dir, "loop-counts.json"), data, 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantContains: []string{"Loops:", "review: 3/5", "[iter 3/5]"},
		},
		{
			name: "j non-existent artifacts dir shows none",
			cfg: &config.Config{Phases: []config.Phase{
				{Name: "plan", Type: "agent"},
			}},
			st: &state.State{PhaseIndex: 1, Ticket: "PROJ-10", Status: state.StatusCompleted},
			setupAudit: func(t *testing.T, dir string) {
				now := time.Now()
				writeTiming(t, dir, &state.Timing{Entries: []state.TimingEntry{
					{Phase: "plan", Start: now, End: now.Add(60 * time.Second), Duration: "1m 00s"},
				}})
			},
			artDirOverride:  filepath.Join(t.TempDir(), "nonexistent"),
			wantContains:    []string{"(none)"},
			wantNotContains: []string{"Remaining:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditDir := t.TempDir()
			artDir := tt.artDirOverride
			if artDir == "" {
				artDir = t.TempDir()
			}
			if tt.setupAudit != nil {
				tt.setupAudit(t, auditDir)
			}
			if tt.setupArt != nil {
				tt.setupArt(t, artDir)
			}
			out := captureOutput(func() {
				RenderStatus(tt.cfg, tt.st, artDir, auditDir)
			})
			for _, want := range tt.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(out, notWant) {
					t.Errorf("output should not contain %q\nfull output:\n%s", notWant, out)
				}
			}
			if tt.customAssert != nil {
				tt.customAssert(t, out)
			}
		})
	}
}
