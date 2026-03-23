package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestArtifacts(t *testing.T, dir, status, ticket string) {
	t.Helper()
	st := &State{Status: status, Ticket: ticket}
	if err := st.Save(dir); err != nil {
		t.Fatal(err)
	}
	timing := &Timing{}
	if err := timing.Flush(dir); err != nil {
		t.Fatal(err)
	}
	costs := &CostData{}
	if err := costs.Flush(dir); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRun(t *testing.T) {
	tempDir := t.TempDir()
	writeTestArtifacts(t, tempDir, "completed", "T-1")
	if err := os.MkdirAll(filepath.Join(tempDir, "prompts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "prompts", "phase-1.md"), []byte("prompt"), 0644); err != nil {
		t.Fatal(err)
	}

	runID, err := ArchiveRun(tempDir)
	if err != nil {
		t.Fatalf("ArchiveRun: %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty runID")
	}

	histDir := filepath.Join(tempDir, "history", runID)
	if _, err := os.Stat(histDir); err != nil {
		t.Fatalf("history dir missing: %v", err)
	}

	for _, name := range []string{"state.json", "timing.json", "costs.json"} {
		if _, err := os.Stat(filepath.Join(histDir, name)); err != nil {
			t.Errorf("expected %s in history: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(histDir, "prompts", "phase-1.md")); err != nil {
		t.Errorf("expected prompts/phase-1.md in history: %v", err)
	}

	// Originals removed
	if _, err := os.Stat(filepath.Join(tempDir, "state.json")); err == nil {
		t.Error("state.json should have been removed from artifacts root")
	}

	// history/ dir still exists
	if _, err := os.Stat(filepath.Join(tempDir, "history")); err != nil {
		t.Errorf("history dir should still exist: %v", err)
	}
}

func TestArchiveRun_Empty(t *testing.T) {
	tempDir := t.TempDir()
	// Only history/ subdir, no other files
	if err := os.MkdirAll(filepath.Join(tempDir, "history"), 0755); err != nil {
		t.Fatal(err)
	}

	runID, err := ArchiveRun(tempDir)
	if err != nil {
		t.Fatalf("ArchiveRun on empty dir: %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty runID")
	}
}

func TestArchiveRun_PartialFailure(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	histDir := filepath.Join(tempDir, "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Make history/ read-only so MkdirAll inside it fails
	if err := os.Chmod(histDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(histDir, 0755)

	_, err := ArchiveRun(tempDir)
	if err == nil {
		t.Fatal("expected error when history/ is read-only")
	}

	// Originals must still be intact
	if _, err := os.Stat(filepath.Join(tempDir, "state.json")); err != nil {
		t.Errorf("state.json should still exist after failed archive: %v", err)
	}
}

func TestPruneHistory(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")

	names := []string{
		"2026-01-01T00-00-00.001",
		"2026-01-01T00-00-00.002",
		"2026-01-01T00-00-00.003",
		"2026-01-01T00-00-00.004",
		"2026-01-01T00-00-00.005",
	}
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(histDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := PruneHistory(tempDir, 3); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	remaining := map[string]bool{}
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	for _, name := range []string{"2026-01-01T00-00-00.003", "2026-01-01T00-00-00.004", "2026-01-01T00-00-00.005"} {
		if !remaining[name] {
			t.Errorf("expected %s to remain", name)
		}
	}
	for _, name := range []string{"2026-01-01T00-00-00.001", "2026-01-01T00-00-00.002"} {
		if remaining[name] {
			t.Errorf("expected %s to be pruned", name)
		}
	}
}

func TestPruneHistory_NoDir(t *testing.T) {
	tempDir := t.TempDir()
	if err := PruneHistory(tempDir, 3); err != nil {
		t.Fatalf("expected nil error for missing history dir, got: %v", err)
	}
}

func TestPruneHistory_ZeroLimit(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")
	for _, name := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(histDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneHistory(tempDir, 0); err != nil {
		t.Fatalf("PruneHistory with limit=0: %v", err)
	}
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune with limit=0, got %d", len(entries))
	}
	if entries[0].Name() != "c" {
		t.Errorf("expected remaining entry to be \"c\", got %q", entries[0].Name())
	}
}

func TestPruneHistory_NegativeLimit(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")
	for _, name := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(histDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneHistory(tempDir, -5); err != nil {
		t.Fatalf("PruneHistory with limit=-5: %v", err)
	}
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune with limit=-5, got %d", len(entries))
	}
	if entries[0].Name() != "c" {
		t.Errorf("expected remaining entry to be \"c\", got %q", entries[0].Name())
	}
}

func TestPruneHistory_IgnoresFiles(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")

	// Create 3 history directories
	dirNames := []string{
		"2026-01-01T00-00-00.001",
		"2026-01-01T00-00-00.002",
		"2026-01-01T00-00-00.003",
	}
	for _, name := range dirNames {
		if err := os.MkdirAll(filepath.Join(histDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create 2 stray files
	for _, name := range []string{".DS_Store", "README.txt"} {
		if err := os.WriteFile(filepath.Join(histDir, name), []byte("stray"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Prune with limit=3 — exactly matches directory count, so no dirs should be removed
	if err := PruneHistory(tempDir, 3); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatal(err)
	}
	// 3 dirs + 2 stray files = 5 total entries
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (3 dirs + 2 files), got %d", len(entries))
	}

	// All 3 directories must survive
	for _, name := range dirNames {
		if _, err := os.Stat(filepath.Join(histDir, name)); err != nil {
			t.Errorf("expected dir %s to survive: %v", name, err)
		}
	}

	// Both stray files must be untouched
	for _, name := range []string{".DS_Store", "README.txt"} {
		if _, err := os.Stat(filepath.Join(histDir, name)); err != nil {
			t.Errorf("expected stray file %s to be untouched: %v", name, err)
		}
	}
}

func TestListHistory(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")

	entry1 := filepath.Join(histDir, "2026-01-01T00-00-00.001")
	if err := os.MkdirAll(entry1, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestArtifacts(t, entry1, "completed", "T-1")
	// Set costs
	costs1 := &CostData{TotalCostUSD: 1.23}
	if err := costs1.Flush(entry1); err != nil {
		t.Fatal(err)
	}
	timing1 := &Timing{}
	timing1.AddStart("phase-1")
	time.Sleep(time.Millisecond)
	timing1.AddEnd("phase-1")
	if err := timing1.Flush(entry1); err != nil {
		t.Fatal(err)
	}

	entry2 := filepath.Join(histDir, "2026-01-02T00-00-00.001")
	if err := os.MkdirAll(entry2, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestArtifacts(t, entry2, "failed", "T-1")
	timing2 := &Timing{}
	timing2.AddStart("phase-1")
	time.Sleep(time.Millisecond)
	timing2.AddEnd("phase-1")
	if err := timing2.Flush(entry2); err != nil {
		t.Fatal(err)
	}

	entries, err := ListHistory(tempDir)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].RunID != "2026-01-02T00-00-00.001" {
		t.Errorf("expected newest first, got %s", entries[0].RunID)
	}
	if entries[1].RunID != "2026-01-01T00-00-00.001" {
		t.Errorf("expected oldest second, got %s", entries[1].RunID)
	}
	if entries[1].CostUSD != 1.23 {
		t.Errorf("expected CostUSD 1.23, got %f", entries[1].CostUSD)
	}
	if entries[1].Status != "completed" {
		t.Errorf("expected status completed, got %s", entries[1].Status)
	}
}

func TestListHistory_Empty(t *testing.T) {
	tempDir := t.TempDir()
	entries, err := ListHistory(tempDir)
	if entries != nil || err != nil {
		t.Errorf("expected (nil, nil), got (%v, %v)", entries, err)
	}
}

func TestListHistory_MissingStateJSON(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")

	// Entry with state.json
	entry1 := filepath.Join(histDir, "2026-01-01T00-00-00.001")
	if err := os.MkdirAll(entry1, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestArtifacts(t, entry1, "completed", "T-1")

	// Entry without state.json (empty dir)
	entry2 := filepath.Join(histDir, "2026-01-02T00-00-00.001")
	if err := os.MkdirAll(entry2, 0755); err != nil {
		t.Fatal(err)
	}

	entries, err := ListHistory(tempDir)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (skipping dir without state.json), got %d", len(entries))
	}
	if entries[0].RunID != "2026-01-01T00-00-00.001" {
		t.Errorf("expected entry 2026-01-01T00-00-00.001, got %s", entries[0].RunID)
	}
}

func TestCopyEntry_PreservesPermissions(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "exec.sh")
	if err := os.WriteFile(src, []byte("#!/bin/bash"), 0755); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dstDir, "exec.sh")
	if err := copyEntry(src, dst); err != nil {
		t.Fatalf("copyEntry: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	// Check executable bit is preserved (accounting for umask)
	if info.Mode().Perm()&0100 == 0 {
		t.Errorf("expected executable permission preserved, got %o", info.Mode().Perm())
	}
}

func TestLatestHistoryDir(t *testing.T) {
	tempDir := t.TempDir()
	histDir := filepath.Join(tempDir, "history")
	for _, name := range []string{
		"2026-01-01T00-00-00.001",
		"2026-01-02T00-00-00.001",
		"2026-01-03T00-00-00.001",
	} {
		if err := os.MkdirAll(filepath.Join(histDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LatestHistoryDir(tempDir)
	if err != nil {
		t.Fatalf("LatestHistoryDir: %v", err)
	}
	if filepath.Base(got) != "2026-01-03T00-00-00.001" {
		t.Errorf("expected newest dir, got %s", got)
	}
}

func TestLatestHistoryDir_Empty(t *testing.T) {
	tempDir := t.TempDir()
	got, err := LatestHistoryDir(tempDir)
	if got != "" || err != nil {
		t.Errorf("expected (\"\", nil), got (%q, %v)", got, err)
	}
}

func TestLatestHistoryDir_EmptyHistoryDir(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "history"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := LatestHistoryDir(tempDir)
	if got != "" || err != nil {
		t.Errorf("expected (\"\", nil), got (%q, %v)", got, err)
	}
}

func TestArchiveRun_MillisecondPrecision(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir1, "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "state.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	runID1, err := ArchiveRun(dir1)
	if err != nil {
		t.Fatalf("ArchiveRun dir1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	runID2, err := ArchiveRun(dir2)
	if err != nil {
		t.Fatalf("ArchiveRun dir2: %v", err)
	}

	if runID1 == "" || runID2 == "" {
		t.Fatal("expected non-empty run IDs")
	}
	if runID1 == runID2 {
		t.Errorf("expected distinct run IDs, both were %s", runID1)
	}
}
