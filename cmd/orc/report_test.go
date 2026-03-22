package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStateJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"status":"completed"}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveStateDir_LiveArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeStateJSON(t, dir)

	got, err := resolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %q, got %q", dir, got)
	}
}

func TestResolveStateDir_ArchivedRun(t *testing.T) {
	dir := t.TempDir()
	histEntry := filepath.Join(dir, "history", "20260322T120000")
	if err := os.MkdirAll(histEntry, 0755); err != nil {
		t.Fatal(err)
	}
	writeStateJSON(t, histEntry)

	got, err := resolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != histEntry {
		t.Fatalf("expected %q, got %q", histEntry, got)
	}
}

func TestResolveStateDir_NoRun(t *testing.T) {
	dir := t.TempDir()

	_, err := resolveStateDir(dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveStateDir_PreferLiveOverHistory(t *testing.T) {
	dir := t.TempDir()
	// Live state exists
	writeStateJSON(t, dir)
	// History entry also exists
	histEntry := filepath.Join(dir, "history", "20260322T120000")
	if err := os.MkdirAll(histEntry, 0755); err != nil {
		t.Fatal(err)
	}
	writeStateJSON(t, histEntry)

	got, err := resolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected live dir %q, got %q", dir, got)
	}
}
