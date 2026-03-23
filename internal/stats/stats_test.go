package stats

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jorge-barreto/orc/internal/state"
)

func writeStateFile(t *testing.T, dir, status, ticket, workflow, failCat string) {
	t.Helper()
	os.MkdirAll(dir, 0755)
	st := &state.State{}
	st.SetStatus(status)
	st.SetTicket(ticket)
	st.SetWorkflow(workflow)
	st.SetFailure(failCat, "")
	if err := st.Save(dir); err != nil {
		t.Fatal(err)
	}
}

func TestCollectRuns_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runs))
	}
}

func TestCollectRuns_NonexistentDir(t *testing.T) {
	runs, err := CollectRuns("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runs != nil {
		t.Fatalf("expected nil runs, got %v", runs)
	}
}

func TestCollectRuns_FlatLayout(t *testing.T) {
	tmpDir := t.TempDir()
	ticketDir := filepath.Join(tmpDir, "T-1")
	writeStateFile(t, ticketDir, "completed", "T-1", "", "")

	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Ticket != "T-1" {
		t.Errorf("expected Ticket=T-1, got %q", runs[0].Ticket)
	}
	if runs[0].Status != "completed" {
		t.Errorf("expected Status=completed, got %q", runs[0].Status)
	}
}

func TestCollectRuns_WorkflowNamespaced(t *testing.T) {
	tmpDir := t.TempDir()
	ticketDir := filepath.Join(tmpDir, "myflow", "T-2")
	writeStateFile(t, ticketDir, "completed", "T-2", "myflow", "")

	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Ticket != "T-2" {
		t.Errorf("expected Ticket=T-2, got %q", runs[0].Ticket)
	}
	if runs[0].Workflow != "myflow" {
		t.Errorf("expected Workflow=myflow, got %q", runs[0].Workflow)
	}
}

func TestCollectRuns_RotatedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	rotatedDir := filepath.Join(tmpDir, "T-3-260322-143000")
	writeStateFile(t, rotatedDir, "interrupted", "T-3", "", "")

	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Ticket != "T-3" {
		t.Errorf("expected Ticket=T-3, got %q", runs[0].Ticket)
	}
	if runs[0].Status != "interrupted" {
		t.Errorf("expected Status=interrupted, got %q", runs[0].Status)
	}
}

func TestCollectRuns_SparseData(t *testing.T) {
	tmpDir := t.TempDir()
	ticketDir := filepath.Join(tmpDir, "T-4")
	writeStateFile(t, ticketDir, "completed", "T-4", "", "")
	// No timing.json, no costs.json

	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].CostUSD != 0 {
		t.Errorf("expected CostUSD=0, got %f", runs[0].CostUSD)
	}
	if runs[0].Duration != 0 {
		t.Errorf("expected Duration=0, got %v", runs[0].Duration)
	}
}

func TestCollectRuns_MultipleLayouts(t *testing.T) {
	tmpDir := t.TempDir()

	// Flat layout
	writeStateFile(t, filepath.Join(tmpDir, "T-5"), "completed", "T-5", "", "")

	// Workflow-namespaced layout
	writeStateFile(t, filepath.Join(tmpDir, "wf", "T-6"), "failed", "T-6", "wf", "")

	runs, err := CollectRuns(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestFilterRuns_ByTicket(t *testing.T) {
	runs := []RunData{
		{Ticket: "T-1"},
		{Ticket: "T-2"},
		{Ticket: "T-1"},
	}
	result := FilterRuns(runs, "T-1", 0)
	if len(result) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(result))
	}
	for _, r := range result {
		if r.Ticket != "T-1" {
			t.Errorf("expected Ticket=T-1, got %q", r.Ticket)
		}
	}
}

func TestFilterRuns_ByLast(t *testing.T) {
	now := time.Now()
	runs := []RunData{
		{Ticket: "T-1", StartTime: now.Add(-4 * time.Hour)},
		{Ticket: "T-1", StartTime: now.Add(-3 * time.Hour)},
		{Ticket: "T-1", StartTime: now.Add(-2 * time.Hour)},
		{Ticket: "T-1", StartTime: now.Add(-1 * time.Hour)},
		{Ticket: "T-1", StartTime: now},
	}
	result := FilterRuns(runs, "", 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(result))
	}
	// Should be the 3 most recent, in chronological (ascending) order
	if !result[0].StartTime.Before(result[1].StartTime) {
		t.Errorf("expected chronological order: result[0] should be before result[1]")
	}
	if !result[1].StartTime.Before(result[2].StartTime) {
		t.Errorf("expected chronological order: result[1] should be before result[2]")
	}
	// The most recent 3 are at -2h, -1h, now
	if result[2].StartTime != now {
		t.Errorf("expected most recent entry last, got StartTime %v", result[2].StartTime)
	}
}

func TestFilterRuns_Combined(t *testing.T) {
	t1 := time.Now().Add(-4 * time.Hour)
	t2 := time.Now().Add(-3 * time.Hour)
	t3 := time.Now().Add(-2 * time.Hour)
	t4 := time.Now().Add(-1 * time.Hour)

	runs := []RunData{
		{Ticket: "T-1", StartTime: t1},
		{Ticket: "T-2", StartTime: t2},
		{Ticket: "T-1", StartTime: t3},
		{Ticket: "T-1", StartTime: t4},
	}

	result := FilterRuns(runs, "T-1", 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(result))
	}
	// Should be the 2 most recent T-1 runs (t3, t4), in chronological order
	for _, r := range result {
		if r.Ticket != "T-1" {
			t.Errorf("expected Ticket=T-1, got %q", r.Ticket)
		}
	}
	if result[0].StartTime != t3 {
		t.Errorf("expected result[0].StartTime=%v, got %v", t3, result[0].StartTime)
	}
	if result[1].StartTime != t4 {
		t.Errorf("expected result[1].StartTime=%v, got %v", t4, result[1].StartTime)
	}
}

func TestAggregate_BasicMetrics(t *testing.T) {
	now := time.Now()
	runs := make([]RunData, 10)
	for i := 0; i < 8; i++ {
		runs[i] = RunData{
			Status:    "completed",
			CostUSD:   float64(i+1) * 1.0,
			Duration:  time.Duration(i+1) * time.Minute,
			StartTime: now.Add(time.Duration(i) * time.Hour),
		}
	}
	runs[8] = RunData{Status: "failed", CostUSD: 9.0, Duration: 9 * time.Minute, StartTime: now.Add(8 * time.Hour)}
	runs[9] = RunData{Status: "failed", CostUSD: 10.0, Duration: 10 * time.Minute, StartTime: now.Add(9 * time.Hour)}

	s := Aggregate(runs)

	if s.TotalRuns != 10 {
		t.Errorf("expected TotalRuns=10, got %d", s.TotalRuns)
	}
	if s.SuccessCount != 8 {
		t.Errorf("expected SuccessCount=8, got %d", s.SuccessCount)
	}
	if s.SuccessRate != 80.0 {
		t.Errorf("expected SuccessRate=80, got %f", s.SuccessRate)
	}

	// All 10 runs have cost > 0
	expectedAvgCost := (1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 + 9 + 10.0) / 10
	if math.Abs(s.AvgCostUSD-expectedAvgCost) > 1e-9 {
		t.Errorf("expected AvgCostUSD=%f, got %f", expectedAvgCost, s.AvgCostUSD)
	}

	// P95: ceil(0.95*10)-1 = ceil(9.5)-1 = 10-1 = 9 → sorted[9] = 10.0
	if s.P95CostUSD != 10.0 {
		t.Errorf("expected P95CostUSD=10.0, got %f", s.P95CostUSD)
	}

	// sum(1..10)=55, count=10 → 55/10 minutes = 5m30s
	expectedAvgDur := 55 * time.Minute / 10
	if s.AvgDuration != expectedAvgDur {
		t.Errorf("expected AvgDuration=%v, got %v", expectedAvgDur, s.AvgDuration)
	}

	// P95 duration: sorted[9] = 10min
	if s.P95Duration != 10*time.Minute {
		t.Errorf("expected P95Duration=10m, got %v", s.P95Duration)
	}
}

func TestAggregate_PhaseBreakdown(t *testing.T) {
	runs := []RunData{
		{
			Status:          "completed",
			PhaseIterations: map[string]int{"implement": 1},
			PhaseCosts:      map[string]float64{},
			PhaseDurations:  map[string]time.Duration{},
		},
		{
			Status:          "completed",
			PhaseIterations: map[string]int{"implement": 3},
			PhaseCosts:      map[string]float64{},
			PhaseDurations:  map[string]time.Duration{},
		},
		{
			Status:          "completed",
			PhaseIterations: map[string]int{"implement": 2},
			PhaseCosts:      map[string]float64{},
			PhaseDurations:  map[string]time.Duration{},
		},
	}

	s := Aggregate(runs)

	if len(s.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(s.Phases))
	}
	ps := s.Phases[0]
	if ps.Name != "implement" {
		t.Errorf("expected phase Name=implement, got %q", ps.Name)
	}
	if ps.RunCount != 3 {
		t.Errorf("expected RunCount=3, got %d", ps.RunCount)
	}
	// LoopRate: 2 out of 3 runs had iterations > 1
	expectedLoopRate := 2.0 / 3.0
	if math.Abs(ps.LoopRate-expectedLoopRate) > 1e-9 {
		t.Errorf("expected LoopRate≈%f, got %f", expectedLoopRate, ps.LoopRate)
	}
	// AvgIters: (3+2)/2 = 2.5 (only runs with iters > 1)
	if math.Abs(ps.AvgIters-2.5) > 1e-9 {
		t.Errorf("expected AvgIters=2.5, got %f", ps.AvgIters)
	}
}

func TestAggregate_FailureBreakdown(t *testing.T) {
	runs := []RunData{
		{Status: "failed", FailureCategory: "loop_exhaustion"},
		{Status: "failed", FailureCategory: "loop_exhaustion"},
		{Status: "interrupted", FailureCategory: "script_failure"},
		{Status: "failed", FailureCategory: ""},
	}

	s := Aggregate(runs)

	if len(s.Failures) != 3 {
		t.Fatalf("expected 3 failure categories, got %d", len(s.Failures))
	}

	// First entry should be loop_exhaustion (count=2)
	if s.Failures[0].Category != "loop_exhaustion" {
		t.Errorf("expected first category=loop_exhaustion, got %q", s.Failures[0].Category)
	}
	if s.Failures[0].Count != 2 {
		t.Errorf("expected loop_exhaustion Count=2, got %d", s.Failures[0].Count)
	}
	if math.Abs(s.Failures[0].Percent-50.0) > 1e-9 {
		t.Errorf("expected loop_exhaustion Percent=50, got %f", s.Failures[0].Percent)
	}

	// Find script_failure and unclassified
	cats := make(map[string]FailureStat)
	for _, f := range s.Failures {
		cats[f.Category] = f
	}
	sf, ok := cats["script_failure"]
	if !ok {
		t.Fatal("expected script_failure in Failures")
	}
	if sf.Count != 1 {
		t.Errorf("expected script_failure Count=1, got %d", sf.Count)
	}
	if math.Abs(sf.Percent-25.0) > 1e-9 {
		t.Errorf("expected script_failure Percent=25, got %f", sf.Percent)
	}
	uc, ok := cats["unclassified"]
	if !ok {
		t.Fatal("expected unclassified in Failures")
	}
	if uc.Count != 1 {
		t.Errorf("expected unclassified Count=1, got %d", uc.Count)
	}
	if math.Abs(uc.Percent-25.0) > 1e-9 {
		t.Errorf("expected unclassified Percent=25, got %f", uc.Percent)
	}
}

func TestAggregate_WeeklyTrend(t *testing.T) {
	// Create 6 weeks of runs (6 distinct Mondays)
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC) // a Monday
	runs := make([]RunData, 6)
	for i := 0; i < 6; i++ {
		runs[i] = RunData{
			Status:    "completed",
			CostUSD:   float64(i+1) * 2.0,
			StartTime: base.Add(time.Duration(i) * 7 * 24 * time.Hour),
		}
	}

	s := Aggregate(runs)

	if len(s.Weeks) != 5 {
		t.Fatalf("expected 5 weeks (last 5), got %d", len(s.Weeks))
	}

	// Verify chronological order
	for i := 1; i < len(s.Weeks); i++ {
		if !s.Weeks[i].WeekStart.After(s.Weeks[i-1].WeekStart) {
			t.Errorf("weeks not in chronological order at index %d", i)
		}
	}

	// The last week (index 4) corresponds to runs[5]: cost=12.0
	lastWeek := s.Weeks[4]
	if math.Abs(lastWeek.AvgCost-12.0) > 1e-9 {
		t.Errorf("expected last week AvgCost=12.0, got %f", lastWeek.AvgCost)
	}
}

func TestAggregate_WeeklyTrend_ZeroCostRuns(t *testing.T) {
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC) // a Monday
	runs := []RunData{
		{Status: "completed", CostUSD: 0, StartTime: base},
		{Status: "completed", CostUSD: 0, StartTime: base.Add(24 * time.Hour)},
		{Status: "completed", CostUSD: 0, StartTime: base.Add(48 * time.Hour)},
	}

	s := Aggregate(runs)

	if len(s.Weeks) != 1 {
		t.Fatalf("expected 1 week, got %d", len(s.Weeks))
	}
	if s.Weeks[0].RunCount != 3 {
		t.Errorf("expected RunCount=3, got %d", s.Weeks[0].RunCount)
	}
	if s.Weeks[0].AvgCost != 0 {
		t.Errorf("expected AvgCost=0, got %f", s.Weeks[0].AvgCost)
	}
}

func TestAggregate_WeeklyTrend_MixedCostRuns(t *testing.T) {
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC) // a Monday
	runs := []RunData{
		{Status: "completed", CostUSD: 0, StartTime: base},
		{Status: "completed", CostUSD: 0, StartTime: base.Add(24 * time.Hour)},
		{Status: "completed", CostUSD: 6.0, StartTime: base.Add(48 * time.Hour)},
	}

	s := Aggregate(runs)

	if len(s.Weeks) != 1 {
		t.Fatalf("expected 1 week, got %d", len(s.Weeks))
	}
	if s.Weeks[0].RunCount != 3 {
		t.Errorf("expected RunCount=3, got %d", s.Weeks[0].RunCount)
	}
	if math.Abs(s.Weeks[0].AvgCost-6.0) > 1e-9 {
		t.Errorf("expected AvgCost=6.0, got %f", s.Weeks[0].AvgCost)
	}
}

func TestAggregate_SingleRun(t *testing.T) {
	runs := []RunData{
		{
			Status:    "completed",
			CostUSD:   5.0,
			Duration:  10 * time.Minute,
			StartTime: time.Now(),
		},
	}

	s := Aggregate(runs)

	if s.TotalRuns != 1 {
		t.Errorf("expected TotalRuns=1, got %d", s.TotalRuns)
	}
	if s.SuccessRate != 100.0 {
		t.Errorf("expected SuccessRate=100, got %f", s.SuccessRate)
	}
	if s.AvgCostUSD != 5.0 {
		t.Errorf("expected AvgCostUSD=5.0, got %f", s.AvgCostUSD)
	}
	if s.P95CostUSD != 5.0 {
		t.Errorf("expected P95CostUSD=5.0, got %f", s.P95CostUSD)
	}
	if s.AvgDuration != 10*time.Minute {
		t.Errorf("expected AvgDuration=10m, got %v", s.AvgDuration)
	}
	if s.P95Duration != 10*time.Minute {
		t.Errorf("expected P95Duration=10m, got %v", s.P95Duration)
	}
}

func TestAggregate_NoCompletedRuns(t *testing.T) {
	runs := []RunData{
		{Status: "failed", FailureCategory: "script_failure"},
		{Status: "failed", FailureCategory: "loop_exhaustion"},
		{Status: "interrupted", FailureCategory: "script_failure"},
	}

	s := Aggregate(runs)

	if s.TotalRuns != 3 {
		t.Errorf("expected TotalRuns=3, got %d", s.TotalRuns)
	}
	if s.SuccessCount != 0 {
		t.Errorf("expected SuccessCount=0, got %d", s.SuccessCount)
	}
	if s.SuccessRate != 0.0 {
		t.Errorf("expected SuccessRate=0, got %f", s.SuccessRate)
	}
	if len(s.Failures) == 0 {
		t.Error("expected non-empty Failures")
	}
}

func TestAggregate_SkipsRunning(t *testing.T) {
	runs := []RunData{
		{Status: "completed"},
		{Status: "running"},
		{Status: "running"},
		{Status: "failed"},
	}

	s := Aggregate(runs)

	// Only completed + failed = 2 terminal runs
	if s.TotalRuns != 2 {
		t.Errorf("expected TotalRuns=2 (excluding running), got %d", s.TotalRuns)
	}
}

func TestRenderJSON_RoundTrips(t *testing.T) {
	s := &Stats{
		TotalRuns:    5,
		TotalTickets: 3,
		DateRange:    "Mar 1 – Mar 22, 2026",
		SuccessCount: 4,
		SuccessRate:  80.0,
		AvgCostUSD:   3.50,
		P95CostUSD:   7.00,
		AvgDuration:  10 * time.Minute,
		P95Duration:  20 * time.Minute,
		Phases: []PhaseStat{
			{Name: "implement", AvgCostUSD: 1.5, AvgDuration: 5 * time.Minute, LoopRate: 0.4, AvgIters: 2.1, RunCount: 5},
		},
		Failures: []FailureStat{
			{Category: "loop_exhaustion", Count: 1, Percent: 100.0},
		},
		Weeks: []WeekStat{
			{WeekStart: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC), AvgCost: 3.5, RunCount: 5},
		},
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, s); err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if got := out["total_runs"]; got != float64(5) {
		t.Errorf("expected total_runs=5, got %v", got)
	}
	if got := out["success_rate"]; got != float64(80.0) {
		t.Errorf("expected success_rate=80, got %v", got)
	}
	// Duration should be a string, not a number
	if dur, ok := out["avg_duration"].(string); !ok || dur == "" {
		t.Errorf("expected avg_duration to be a non-empty string, got %v", out["avg_duration"])
	}
	phases, ok := out["phases"].([]interface{})
	if !ok || len(phases) != 1 {
		t.Fatalf("expected 1 phase in JSON, got %v", out["phases"])
	}
	ph := phases[0].(map[string]interface{})
	if ph["name"] != "implement" {
		t.Errorf("expected phase name=implement, got %v", ph["name"])
	}
	if _, ok := ph["avg_duration"].(string); !ok {
		t.Errorf("expected phase avg_duration to be a string, got %T", ph["avg_duration"])
	}
}

func TestRenderJSON_EmptyStats_ArraysNotNull(t *testing.T) {
	s := &Stats{}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, s); err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	for _, field := range []string{"phases", "failures", "weeks"} {
		val, exists := out[field]
		if !exists {
			t.Errorf("expected %q field in JSON output", field)
			continue
		}
		arr, ok := val.([]interface{})
		if !ok {
			t.Errorf("expected %q to be an array, got %T (value: %v)", field, val, val)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("expected %q to be empty array, got %d elements", field, len(arr))
		}
	}
}

func TestRenderJSON_AggregateEmpty_ArraysNotNull(t *testing.T) {
	s := Aggregate(nil)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, s); err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	for _, field := range []string{"phases", "failures", "weeks"} {
		val, exists := out[field]
		if !exists {
			t.Errorf("expected %q field in JSON output", field)
			continue
		}
		arr, ok := val.([]interface{})
		if !ok {
			t.Errorf("expected %q to be an array, got %T (value: %v)", field, val, val)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("expected %q to be empty array, got %d elements", field, len(arr))
		}
	}
}

func TestRenderText_NoRuns(t *testing.T) {
	// Stats with zero runs should produce output with "—" placeholders (no panic)
	s := &Stats{} // TotalRuns == 0

	var buf bytes.Buffer
	RenderText(&buf, s)
	output := buf.String()

	if !strings.Contains(output, "No audited runs found") {
		t.Errorf("expected 'No audited runs found' in output, got:\n%s", output)
	}
}

func TestRenderText_EmptyFailureCategory(t *testing.T) {
	s := &Stats{
		TotalRuns:    1,
		TotalTickets: 1,
		SuccessRate:  0,
		Failures: []FailureStat{
			{Category: "", Count: 1, Percent: 100.0},
		},
	}
	var buf bytes.Buffer
	RenderText(&buf, s)
	output := buf.String()
	if !strings.Contains(output, "Unknown") {
		t.Errorf("expected 'Unknown' fallback for empty category, got:\n%s", output)
	}
}
