package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

func writeJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild_CompletedRun(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 3, "ticket": "KS-42", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
			{"phase": "implement", "start": now, "end": now.Add(120 * time.Second), "duration": "2m 00s"},
			{"phase": "test", "start": now, "end": now.Add(10 * time.Second), "duration": "10s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{
			{"name": "plan", "cost_usd": 0.25, "input_tokens": 10000, "output_tokens": 5000},
			{"name": "implement", "cost_usd": 0.50, "input_tokens": 20000, "output_tokens": 10000},
		},
		"total_cost_usd": 0.75, "total_input_tokens": 30000, "total_output_tokens": 15000,
	})
	writeJSON(t, dir, "loop-counts.json", map[string]int{})
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("plan content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ticket.txt"), []byte("ticket"), 0644); err != nil {
		t.Fatal(err)
	}

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.Status != "Completed" {
		t.Errorf("status = %q, want Completed", data.Status)
	}
	if data.Duration == "—" {
		t.Errorf("duration should not be —")
	}
	if data.TotalCostUSD == 0 {
		t.Errorf("cost should not be 0")
	}
	if len(data.Phases) != 3 {
		t.Errorf("phases = %d, want 3", len(data.Phases))
	}
	if data.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", data.SchemaVersion)
	}
	if len(data.Loops) != 0 {
		t.Errorf("loops should be empty")
	}

	artifactNames := map[string]bool{}
	for _, a := range data.Artifacts {
		artifactNames[a.Name] = true
	}
	if !artifactNames["plan.md"] {
		t.Error("expected plan.md in artifacts")
	}
	if !artifactNames["ticket.txt"] {
		t.Error("expected ticket.txt in artifacts")
	}
	if artifactNames["state.json"] {
		t.Error("state.json should be excluded")
	}
	if artifactNames["timing.json"] {
		t.Error("timing.json should be excluded")
	}
	if artifactNames["costs.json"] {
		t.Error("costs.json should be excluded")
	}
	if data.FailureCategory != "" {
		t.Errorf("FailureCategory = %q, want empty for completed run", data.FailureCategory)
	}
	if data.FailureDetail != "" {
		t.Errorf("FailureDetail = %q, want empty for completed run", data.FailureDetail)
	}
}

func TestBuild_FailedRun(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-10", "status": "failed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
			{"phase": "review", "start": now, "end": now.Add(10 * time.Second), "duration": "10s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases":             []map[string]any{},
		"total_cost_usd":     0.0,
		"total_input_tokens": 0,
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "review", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.Status != "Failed" {
		t.Errorf("status = %q, want Failed", data.Status)
	}
	if len(data.Phases) != 3 {
		t.Fatalf("phases = %d, want 3", len(data.Phases))
	}
	if data.Phases[0].Result != "Pass" {
		t.Errorf("phase[0] result = %q, want Pass", data.Phases[0].Result)
	}
	if data.Phases[1].Result != "Pass" {
		t.Errorf("phase[1] result = %q, want Pass", data.Phases[1].Result)
	}
	if data.Phases[2].Result != "Fail" {
		t.Errorf("phase[2] result = %q, want Fail", data.Phases[2].Result)
	}
}

func TestBuild_FailedRun_IncludesFailureCategory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index":      1,
		"ticket":           "KS-FC",
		"status":           "failed",
		"failure_category": "agent_error",
		"failure_detail":   "non-zero exit",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases":             []map[string]any{},
		"total_cost_usd":     0.0,
		"total_input_tokens": 0,
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.FailureCategory != "agent_error" {
		t.Errorf("FailureCategory = %q, want agent_error", data.FailureCategory)
	}
	if data.FailureDetail != "non-zero exit" {
		t.Errorf("FailureDetail = %q, want non-zero exit", data.FailureDetail)
	}
}

func TestBuild_FailedRunWithRetries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// phase_index=2 → phases[2].Name = "review" is the failed phase
	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-11", "status": "failed",
	})
	// 4 timing entries: plan, implement, review, review (review retried)
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
			{"phase": "review", "start": now, "end": now.Add(10 * time.Second), "duration": "10s"},
			{"phase": "review", "start": now, "end": now.Add(10 * time.Second), "duration": "10s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "review", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 4 {
		t.Fatalf("phases = %d, want 4", len(data.Phases))
	}
	// First review entry should NOT be Fail
	if data.Phases[2].Result == "Fail" {
		t.Errorf("phase[2] (first review) result = Fail, want Pass")
	}
	// Last review entry should be Fail
	if data.Phases[3].Result != "Fail" {
		t.Errorf("phase[3] (last review) result = %q, want Fail", data.Phases[3].Result)
	}
}

func TestBuild_InterruptedRun(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-20", "status": "interrupted",
	})
	// Last entry has no end/duration
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
			{"phase": "verify", "start": now},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "verify", Type: "script"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.Status != "Interrupted" {
		t.Errorf("status = %q, want Interrupted", data.Status)
	}
	if len(data.Phases) != 3 {
		t.Fatalf("phases = %d, want 3", len(data.Phases))
	}
	last := data.Phases[2]
	if last.Result != "Interrupted" {
		t.Errorf("last phase result = %q, want Interrupted", last.Result)
	}
	if last.Duration != "—" {
		t.Errorf("last phase duration = %q, want —", last.Duration)
	}
}

func TestBuild_MissingCosts(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-30", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
		},
	})
	// No costs.json

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD = %f, want 0", data.TotalCostUSD)
	}
	if data.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", data.TotalTokens)
	}
	if data.Cost != "—" {
		t.Errorf("Cost = %q, want —", data.Cost)
	}
	for _, p := range data.Phases {
		if p.Cost != "—" {
			t.Errorf("phase %s Cost = %q, want —", p.Name, p.Cost)
		}
	}
}

func TestBuild_MissingTiming(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 3, "ticket": "KS-31", "status": "completed",
	})
	// No timing.json

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
		{Name: "test", Type: "script"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 3 {
		t.Errorf("phases = %d, want 3", len(data.Phases))
	}
	for _, p := range data.Phases {
		if p.Duration != "—" {
			t.Errorf("phase %s Duration = %q, want —", p.Name, p.Duration)
		}
		if p.Cost != "—" {
			t.Errorf("phase %s Cost = %q, want —", p.Name, p.Cost)
		}
	}
}

func TestBuild_WithLoopActivity(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 0, "ticket": "KS-40", "status": "completed",
	})
	writeJSON(t, dir, "loop-counts.json", map[string]int{
		"implement": 3,
		"quality":   0,
	})

	phases := []config.Phase{}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Loops) != 1 {
		t.Fatalf("loops = %d, want 1", len(data.Loops))
	}
	if data.Loops[0].Phase != "implement" {
		t.Errorf("loop phase = %q, want implement", data.Loops[0].Phase)
	}
	if data.Loops[0].Iterations != 3 {
		t.Errorf("loop iterations = %d, want 3", data.Loops[0].Iterations)
	}
}

func TestBuild_GatePhaseApproved(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-50", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "approve", "start": now, "end": now.Add(5 * time.Second), "duration": "5s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "approve", Type: "gate"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(data.Phases))
	}
	if data.Phases[1].Result != "Approved" {
		t.Errorf("gate phase result = %q, want Approved", data.Phases[1].Result)
	}
}

func TestRenderMarkdown_Format(t *testing.T) {
	r := &ReportData{
		SchemaVersion: 1,
		Ticket:        "KS-99",
		Status:        "Completed",
		Duration:      "5m 00s",
		Cost:          "$1.00 (50,000 tokens)",
		TotalCostUSD:  1.0,
		TotalTokens:   50000,
		Phases: []PhaseResult{
			{Number: 1, Name: "plan", Type: "agent", Duration: "1m 00s", Cost: "$0.50", Result: "Pass"},
		},
		Loops: []LoopActivity{
			{Phase: "plan", Iterations: 2},
		},
		Artifacts: []ArtifactFile{
			{Name: "plan.md", Size: "512 bytes"},
		},
	}

	var buf bytes.Buffer
	RenderMarkdown(&buf, r)
	out := buf.String()

	checks := []string{
		"# Run Report:",
		"**Status:**",
		"**Duration:**",
		"**Cost:**",
		"## Phase Summary",
		"| # | Phase | Type | Duration | Cost | Result |",
		"## Loop Activity",
		"## Artifacts",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderMarkdown_FailureCategory(t *testing.T) {
	r := &ReportData{
		Ticket:          "KS-99",
		Status:          "Failed",
		FailureCategory: "agent_error",
		FailureDetail:   "non-zero exit",
		Phases:          []PhaseResult{},
		Loops:           []LoopActivity{},
		Artifacts:       []ArtifactFile{},
	}
	var buf bytes.Buffer
	RenderMarkdown(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "**Failure:** agent_error — non-zero exit") {
		t.Errorf("output missing failure line, got:\n%s", out)
	}
}

func TestRenderMarkdown_NoFailureCategory(t *testing.T) {
	r := &ReportData{
		Ticket:    "KS-99",
		Status:    "Completed",
		Phases:    []PhaseResult{},
		Loops:     []LoopActivity{},
		Artifacts: []ArtifactFile{},
	}
	var buf bytes.Buffer
	RenderMarkdown(&buf, r)
	out := buf.String()
	if strings.Contains(out, "**Failure:**") {
		t.Errorf("output should not contain **Failure:** for completed run, got:\n%s", out)
	}
}

func TestRenderJSON_Schema(t *testing.T) {
	r := &ReportData{
		SchemaVersion: 1,
		Ticket:        "KS-77",
		Status:        "Completed",
		Duration:      "3m 00s",
		Cost:          "$0.75",
		TotalCostUSD:  0.75,
		TotalTokens:   45000,
		Phases:        []PhaseResult{},
		Loops:         []LoopActivity{},
		Artifacts:     []ArtifactFile{},
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatal(err)
	}

	var got ReportData
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", got.SchemaVersion)
	}
	if got.Ticket != "KS-77" {
		t.Errorf("ticket = %q, want KS-77", got.Ticket)
	}
	if got.TotalCostUSD != 0.75 {
		t.Errorf("total_cost_usd = %f, want 0.75", got.TotalCostUSD)
	}
	if got.TotalTokens != 45000 {
		t.Errorf("total_tokens = %d, want 45000", got.TotalTokens)
	}
}

func TestRenderJSON_MissingData(t *testing.T) {
	r := &ReportData{
		SchemaVersion: 1,
		Ticket:        "KS-00",
		Status:        "Completed",
		Duration:      "—",
		Cost:          "—",
		Phases:        []PhaseResult{},
		Loops:         []LoopActivity{},
		Artifacts:     []ArtifactFile{},
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// Must be valid JSON
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// phases must be [] not null
	phasesRaw, ok := got["phases"]
	if !ok {
		t.Fatal("missing phases field")
	}
	phases, ok := phasesRaw.([]any)
	if !ok {
		t.Fatalf("phases is not array, got %T", phasesRaw)
	}
	if len(phases) != 0 {
		t.Errorf("phases len = %d, want 0", len(phases))
	}
	// total_cost_usd and total_tokens should be 0
	if v, _ := got["total_cost_usd"].(float64); v != 0 {
		t.Errorf("total_cost_usd = %f, want 0", v)
	}
	if v, _ := got["total_tokens"].(float64); v != 0 {
		t.Errorf("total_tokens = %f, want 0", v)
	}
	// loops and artifacts must serialize as [] not null
	if strings.Contains(out, `"loops": null`) {
		t.Error("loops must be [] not null in JSON output")
	}
	if strings.Contains(out, `"artifacts": null`) {
		t.Error("artifacts must be [] not null in JSON output")
	}
	loopsRaw, ok := got["loops"]
	if !ok {
		t.Fatal("missing loops field")
	}
	if _, ok := loopsRaw.([]any); !ok {
		t.Fatalf("loops is not array, got %T", loopsRaw)
	}
	artifactsRaw, ok := got["artifacts"]
	if !ok {
		t.Fatal("missing artifacts field")
	}
	if _, ok := artifactsRaw.([]any); !ok {
		t.Fatalf("artifacts is not array, got %T", artifactsRaw)
	}
}

func TestBuild_PhaseIndexOutOfBounds(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 5, "ticket": "KS-OOB", "status": "failed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if data.Status != "Failed" {
		t.Errorf("status = %q, want Failed", data.Status)
	}
	// No phase should have "Fail" result — phase_index is out of bounds
	for i, p := range data.Phases {
		if p.Result == "Fail" {
			t.Errorf("phase[%d] %s has result Fail, but phase_index is out of bounds", i, p.Name)
		}
	}
}

func TestBuild_ZeroCostWithTokens(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 1, "ticket": "KS-SUB", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{
			{"name": "plan", "cost_usd": 0, "input_tokens": 15000, "output_tokens": 8000},
		},
		"total_cost_usd": 0, "total_input_tokens": 15000, "total_output_tokens": 8000,
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	// Per-phase: cost should show token count, not "—"
	if data.Phases[0].Cost == "—" {
		t.Errorf("phase cost = %q, want token count string", data.Phases[0].Cost)
	}
	if data.Phases[0].Tokens != 23000 {
		t.Errorf("phase tokens = %d, want 23000", data.Phases[0].Tokens)
	}
	// Total: cost should show token count, not "—"
	if data.Cost == "—" {
		t.Errorf("total cost = %q, want token count string", data.Cost)
	}
}

func TestBuild_ParallelCostOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-PAR", "status": "completed",
	})
	// Timing in config order: plan, implement
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
		},
	})
	// Costs in reverse order (implement finished first in goroutine)
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{
			{"name": "implement", "cost_usd": 0.80, "input_tokens": 20000, "output_tokens": 10000},
			{"name": "plan", "cost_usd": 0.25, "input_tokens": 10000, "output_tokens": 5000},
		},
		"total_cost_usd": 1.05, "total_input_tokens": 30000, "total_output_tokens": 15000,
	})

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(data.Phases))
	}
	// plan should get plan's cost (0.25), not implement's (0.80)
	if data.Phases[0].CostUSD != 0.25 {
		t.Errorf("plan cost_usd = %f, want 0.25", data.Phases[0].CostUSD)
	}
	if data.Phases[1].CostUSD != 0.80 {
		t.Errorf("implement cost_usd = %f, want 0.80", data.Phases[1].CostUSD)
	}
}

func TestBuild_WithMetadata(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-META", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})

	// Write .meta.json files using state.SaveMetadata
	meta0 := &state.PhaseMetadata{
		PhaseName:   "plan",
		PhaseType:   "agent",
		PhaseIndex:  0,
		Model:       "sonnet",
		SessionID:   "sess-abc",
		ToolsUsed:   []string{"Read", "Edit"},
		ToolsDenied: []string{},
	}
	if err := state.SaveMetadata(state.MetaPath(dir, 0), meta0); err != nil {
		t.Fatal(err)
	}
	meta1 := &state.PhaseMetadata{
		PhaseName:   "implement",
		PhaseType:   "agent",
		PhaseIndex:  1,
		Model:       "opus",
		SessionID:   "sess-def",
		ToolsUsed:   []string{"Bash", "Read"},
		ToolsDenied: []string{"Write"},
	}
	if err := state.SaveMetadata(state.MetaPath(dir, 1), meta1); err != nil {
		t.Fatal(err)
	}

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(data.Phases))
	}

	p0 := data.Phases[0]
	if p0.Model != "sonnet" {
		t.Errorf("phases[0].Model = %q, want sonnet", p0.Model)
	}
	if p0.SessionID != "sess-abc" {
		t.Errorf("phases[0].SessionID = %q, want sess-abc", p0.SessionID)
	}
	if len(p0.ToolsUsed) != 2 || p0.ToolsUsed[0] != "Read" || p0.ToolsUsed[1] != "Edit" {
		t.Errorf("phases[0].ToolsUsed = %v, want [Read Edit]", p0.ToolsUsed)
	}

	p1 := data.Phases[1]
	if p1.Model != "opus" {
		t.Errorf("phases[1].Model = %q, want opus", p1.Model)
	}
	if len(p1.ToolsDenied) != 1 || p1.ToolsDenied[0] != "Write" {
		t.Errorf("phases[1].ToolsDenied = %v, want [Write]", p1.ToolsDenied)
	}
}

func TestBuild_MissingMetadata(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeJSON(t, dir, "state.json", map[string]any{
		"phase_index": 1, "ticket": "KS-NOMETA", "status": "completed",
	})
	writeJSON(t, dir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
		},
	})
	writeJSON(t, dir, "costs.json", map[string]any{
		"phases": []map[string]any{},
	})
	// No .meta.json files written

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
	}
	st, _ := state.Load(dir)
	data, err := Build(dir, dir, st, phases)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Phases) != 1 {
		t.Fatalf("phases = %d, want 1", len(data.Phases))
	}
	p := data.Phases[0]
	if p.Model != "" {
		t.Errorf("Model = %q, want empty (no metadata)", p.Model)
	}
	if p.SessionID != "" {
		t.Errorf("SessionID = %q, want empty (no metadata)", p.SessionID)
	}
	if p.ToolsUsed != nil {
		t.Errorf("ToolsUsed = %v, want nil (no metadata)", p.ToolsUsed)
	}
	if p.ToolsDenied != nil {
		t.Errorf("ToolsDenied = %v, want nil (no metadata)", p.ToolsDenied)
	}
}

func TestBuild_AuditDirMetadata(t *testing.T) {
	artifactsDir := t.TempDir()
	auditDir := t.TempDir()
	auditLogsDir := filepath.Join(auditDir, "logs")
	if err := os.MkdirAll(auditLogsDir, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// Write state.json in artifactsDir (required for state.Load)
	writeJSON(t, artifactsDir, "state.json", map[string]any{
		"phase_index": 2, "ticket": "KS-AUDIT", "status": "completed",
	})
	// Write timing.json and costs.json to auditDir (Build tries auditDir first; LoadTiming
	// returns empty+nil when file missing, so the artifactsDir fallback never triggers)
	writeJSON(t, auditDir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
			{"phase": "implement", "start": now, "end": now.Add(60 * time.Second), "duration": "1m 00s"},
		},
	})
	writeJSON(t, auditDir, "costs.json", map[string]any{"phases": []map[string]any{}})

	// Write dispatch-counts.json in auditDir: phase 0 ran once, phase 1 ran twice
	writeJSON(t, auditDir, "dispatch-counts.json", map[string]any{"0": 1, "1": 2})

	// Write metadata only in auditDir at AuditMetaPath paths (simulate purged artifacts)
	meta0 := &state.PhaseMetadata{
		PhaseName: "plan", PhaseType: "agent", PhaseIndex: 0,
		Model: "sonnet", SessionID: "audit-sess-0",
	}
	if err := state.SaveMetadata(state.AuditMetaPath(auditDir, 0, 1), meta0); err != nil {
		t.Fatal(err)
	}
	meta1 := &state.PhaseMetadata{
		PhaseName: "implement", PhaseType: "agent", PhaseIndex: 1,
		Model: "opus", SessionID: "audit-sess-1",
	}
	if err := state.SaveMetadata(state.AuditMetaPath(auditDir, 1, 2), meta1); err != nil {
		t.Fatal(err)
	}

	phases := []config.Phase{
		{Name: "plan", Type: "agent"},
		{Name: "implement", Type: "agent"},
	}
	st, _ := state.Load(artifactsDir)
	data, err := Build(artifactsDir, auditDir, st, phases)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(data.Phases))
	}
	if data.Phases[0].Model != "sonnet" {
		t.Errorf("phases[0].Model = %q, want sonnet", data.Phases[0].Model)
	}
	if data.Phases[0].SessionID != "audit-sess-0" {
		t.Errorf("phases[0].SessionID = %q, want audit-sess-0", data.Phases[0].SessionID)
	}
	if data.Phases[1].Model != "opus" {
		t.Errorf("phases[1].Model = %q, want opus", data.Phases[1].Model)
	}
	if data.Phases[1].SessionID != "audit-sess-1" {
		t.Errorf("phases[1].SessionID = %q, want audit-sess-1", data.Phases[1].SessionID)
	}
}

func TestBuild_AuditDirFallsBackToArtifacts(t *testing.T) {
	artifactsDir := t.TempDir()
	auditDir := t.TempDir()
	now := time.Now()

	writeJSON(t, artifactsDir, "state.json", map[string]any{
		"phase_index": 1, "ticket": "KS-FALLBACK", "status": "completed",
	})
	// Write timing.json and costs.json to auditDir (Build tries auditDir first)
	writeJSON(t, auditDir, "timing.json", map[string]any{
		"entries": []map[string]any{
			{"phase": "plan", "start": now, "end": now.Add(30 * time.Second), "duration": "30s"},
		},
	})
	writeJSON(t, auditDir, "costs.json", map[string]any{"phases": []map[string]any{}})

	// Metadata only in artifactsDir, not in auditDir
	if err := os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	meta0 := &state.PhaseMetadata{
		PhaseName: "plan", PhaseType: "agent", PhaseIndex: 0,
		Model: "haiku", SessionID: "fallback-sess-0",
	}
	if err := state.SaveMetadata(state.MetaPath(artifactsDir, 0), meta0); err != nil {
		t.Fatal(err)
	}

	// Empty dispatch-counts in auditDir (no attempts recorded)
	writeJSON(t, auditDir, "dispatch-counts.json", map[string]any{})

	phases := []config.Phase{{Name: "plan", Type: "agent"}}
	st, _ := state.Load(artifactsDir)
	data, err := Build(artifactsDir, auditDir, st, phases)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Phases) != 1 {
		t.Fatalf("phases = %d, want 1", len(data.Phases))
	}
	if data.Phases[0].Model != "haiku" {
		t.Errorf("phases[0].Model = %q, want haiku", data.Phases[0].Model)
	}
	if data.Phases[0].SessionID != "fallback-sess-0" {
		t.Errorf("phases[0].SessionID = %q, want fallback-sess-0", data.Phases[0].SessionID)
	}
}
