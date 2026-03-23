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

func TestCollectCommits_EmptyBase(t *testing.T) {
	commits := CollectCommits(t.TempDir(), "")
	if commits != nil {
		t.Errorf("expected nil, got %v", commits)
	}
}
