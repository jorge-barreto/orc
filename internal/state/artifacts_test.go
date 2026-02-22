package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"prompts", "logs", "feedback"} {
		path := filepath.Join(artDir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s not created: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", sub)
		}
	}
}

func TestLoopCounts_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[string]int{"build": 2, "test": 1}
	if err := SaveLoopCounts(dir, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLoopCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["build"] != 2 || loaded["test"] != 1 {
		t.Fatalf("got %v", loaded)
	}
}

func TestLoopCounts_NoFile(t *testing.T) {
	dir := t.TempDir()
	counts, err := LoadLoopCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %v", counts)
	}
}

func TestWriteFeedback(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeedback(dir, "build", "something broke"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "feedback", "from-build.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "something broke" {
		t.Fatalf("got %q", string(data))
	}
}

func TestCheckOutputs_AllPresent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "design.md"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "spec.md"), []byte("y"), 0644)

	missing := CheckOutputs(dir, []string{"design.md", "spec.md"})
	if len(missing) != 0 {
		t.Fatalf("expected no missing, got %v", missing)
	}
}

func TestCheckOutputs_SomeMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "design.md"), []byte("x"), 0644)

	missing := CheckOutputs(dir, []string{"design.md", "spec.md", "plan.md"})
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %v", missing)
	}
	if missing[0] != "spec.md" || missing[1] != "plan.md" {
		t.Fatalf("got %v", missing)
	}
}

func TestPromptPath(t *testing.T) {
	got := PromptPath("/art", 0)
	want := filepath.Join("/art", "prompts", "phase-1.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = PromptPath("/art", 4)
	want = filepath.Join("/art", "prompts", "phase-5.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLogPath(t *testing.T) {
	got := LogPath("/art", 0)
	want := filepath.Join("/art", "logs", "phase-1.log")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
