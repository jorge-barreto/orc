package state

import (
	"os"
	"path/filepath"
	"strings"
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

func TestReadAllFeedback_SingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeedback(dir, "build", "build failed: exit code 1"); err != nil {
		t.Fatal(err)
	}
	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "--- Feedback from build ---") {
		t.Fatalf("missing feedback header; got:\n%s", result)
	}
	if !strings.Contains(result, "build failed: exit code 1") {
		t.Fatalf("missing feedback content; got:\n%s", result)
	}
}

func TestReadAllFeedback_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeedback(dir, "build", "build failed"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeedback(dir, "test", "tests failed"); err != nil {
		t.Fatal(err)
	}
	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "--- Feedback from build ---") {
		t.Fatalf("missing build feedback header; got:\n%s", result)
	}
	if !strings.Contains(result, "--- Feedback from test ---") {
		t.Fatalf("missing test feedback header; got:\n%s", result)
	}
	if !strings.Contains(result, "build failed") {
		t.Fatalf("missing build feedback content; got:\n%s", result)
	}
	if !strings.Contains(result, "tests failed") {
		t.Fatalf("missing test feedback content; got:\n%s", result)
	}
}

func TestReadAllFeedback_NoDir(t *testing.T) {
	dir := t.TempDir()
	// Don't call EnsureDir — feedback/ subdir does not exist
	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestReadAllFeedback_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestReadAllFeedback_SkipsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeedback(dir, "build", "real feedback"); err != nil {
		t.Fatal(err)
	}
	// Write a whitespace-only feedback file directly
	emptyPath := filepath.Join(dir, "feedback", "from-empty.md")
	if err := os.WriteFile(emptyPath, []byte("   \n  "), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "--- Feedback from build ---") {
		t.Fatalf("missing build feedback header; got:\n%s", result)
	}
	if !strings.Contains(result, "real feedback") {
		t.Fatalf("missing build feedback content; got:\n%s", result)
	}
	if strings.Contains(result, "--- Feedback from empty ---") {
		t.Fatalf("should not contain empty feedback; got:\n%s", result)
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

func TestStreamLogPath(t *testing.T) {
	got := StreamLogPath("/art", 0)
	want := filepath.Join("/art", "logs", "phase-1.stream.jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = StreamLogPath("/art", 4)
	want = filepath.Join("/art", "logs", "phase-5.stream.jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
