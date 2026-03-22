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
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte("plan content"), 0644)
	os.WriteFile(filepath.Join(dir, "ticket.txt"), []byte("ticket"), 0644)

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
}
