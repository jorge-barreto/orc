package state

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestWriteRunResult_Success(t *testing.T) {
	dir := t.TempDir()
	result := &RunResult{
		Ticket:               "TEST-1",
		Workflow:             "default",
		Status:               StatusCompleted,
		ExitCode:             0,
		PhasesCompleted:      5,
		PhasesTotal:          5,
		TotalCostUSD:         12.34,
		TotalDurationSeconds: 847,
		ArtifactsDir:         dir,
	}

	if err := WriteRunResult(dir, result); err != nil {
		t.Fatalf("WriteRunResult: %v", err)
	}

	data, err := os.ReadFile(RunResultPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Ticket != "TEST-1" {
		t.Errorf("Ticket: got %q, want %q", got.Ticket, "TEST-1")
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status: got %q, want %q", got.Status, StatusCompleted)
	}
	if got.FailedPhase != nil {
		t.Errorf("FailedPhase: got %v, want nil", got.FailedPhase)
	}
	if got.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", got.ExitCode)
	}
	if !bytes.Contains(data, []byte(`"commits": []`)) {
		t.Errorf("expected commits to serialize as [], got: %s", data)
	}
}

func TestWriteRunResult_WithFailedPhase(t *testing.T) {
	dir := t.TempDir()
	failed := "implement"
	result := &RunResult{
		Ticket:      "TEST-2",
		Status:      StatusFailed,
		ExitCode:    1,
		FailedPhase: &failed,
	}

	if err := WriteRunResult(dir, result); err != nil {
		t.Fatalf("WriteRunResult: %v", err)
	}

	data, err := os.ReadFile(RunResultPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.FailedPhase == nil {
		t.Fatal("FailedPhase: got nil, want non-nil")
	}
	if *got.FailedPhase != "implement" {
		t.Errorf("FailedPhase: got %q, want %q", *got.FailedPhase, "implement")
	}
}

func TestWriteRunResult_WithPhases(t *testing.T) {
	dir := t.TempDir()
	result := &RunResult{
		Status: StatusCompleted,
		Phases: []PhaseResult{
			{Name: "fetch", Status: "completed", DurationSeconds: 1.5, CostUSD: 0.0},
			{Name: "build", Status: "skipped", DurationSeconds: 0.0, CostUSD: 0.0},
			{Name: "deploy", Status: "failed", DurationSeconds: 3.2, CostUSD: 0.05},
		},
	}

	if err := WriteRunResult(dir, result); err != nil {
		t.Fatalf("WriteRunResult: %v", err)
	}

	data, err := os.ReadFile(RunResultPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.Phases) != 3 {
		t.Fatalf("Phases: got %d, want 3", len(got.Phases))
	}
	if got.Phases[0].Name != "fetch" || got.Phases[0].Status != "completed" {
		t.Errorf("Phases[0]: got %+v", got.Phases[0])
	}
	if got.Phases[1].Status != "skipped" {
		t.Errorf("Phases[1].Status: got %q, want skipped", got.Phases[1].Status)
	}
	if got.Phases[2].Status != "failed" || got.Phases[2].CostUSD != 0.05 {
		t.Errorf("Phases[2]: got %+v", got.Phases[2])
	}
	if !bytes.Contains(data, []byte(`"phases"`)) {
		t.Errorf("expected phases key in JSON, got: %s", data)
	}
}

func TestWriteRunResult_EmptyPhases(t *testing.T) {
	dir := t.TempDir()
	result := &RunResult{Status: StatusCompleted}

	if err := WriteRunResult(dir, result); err != nil {
		t.Fatalf("WriteRunResult: %v", err)
	}

	data, err := os.ReadFile(RunResultPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Contains(data, []byte(`"phases": []`)) {
		t.Errorf("expected phases to serialize as [], got: %s", data)
	}
}

func TestWriteRunResult_DoesNotMutateCaller(t *testing.T) {
	dir := t.TempDir()
	result := &RunResult{Status: StatusCompleted}
	// Commits and Phases are nil — not set

	if err := WriteRunResult(dir, result); err != nil {
		t.Fatalf("WriteRunResult: %v", err)
	}

	if result.Commits != nil {
		t.Errorf("Commits: expected nil after WriteRunResult, got %v", result.Commits)
	}
	if result.Phases != nil {
		t.Errorf("Phases: expected nil after WriteRunResult, got %v", result.Phases)
	}

	data, err := os.ReadFile(RunResultPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(`"commits": []`)) {
		t.Errorf("expected commits to serialize as [], got: %s", data)
	}
	if !bytes.Contains(data, []byte(`"phases": []`)) {
		t.Errorf("expected phases to serialize as [], got: %s", data)
	}
}

func TestCollectCommits_EmptyBase(t *testing.T) {
	commits := CollectCommits(t.TempDir(), "")
	if commits != nil {
		t.Errorf("expected nil, got %v", commits)
	}
}
