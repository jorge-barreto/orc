package stats

import (
	"os"
	"path/filepath"
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
