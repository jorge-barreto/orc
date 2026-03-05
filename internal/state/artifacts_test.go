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

func TestReadDeclaredOutputs_AllPresent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "findings.md"), []byte("issue A"), 0644)
	os.WriteFile(filepath.Join(dir, "summary.md"), []byte("summary B"), 0644)

	result := ReadDeclaredOutputs(dir, []string{"findings.md", "summary.md"})
	if !strings.Contains(result, "issue A") {
		t.Fatalf("missing findings content; got: %q", result)
	}
	if !strings.Contains(result, "summary B") {
		t.Fatalf("missing summary content; got: %q", result)
	}
}

func TestReadDeclaredOutputs_SomeMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "findings.md"), []byte("issue A"), 0644)

	result := ReadDeclaredOutputs(dir, []string{"findings.md", "missing.md"})
	if !strings.Contains(result, "issue A") {
		t.Fatalf("missing findings content; got: %q", result)
	}
	if strings.Contains(result, "missing") {
		t.Fatalf("should not contain missing file reference; got: %q", result)
	}
}

func TestReadDeclaredOutputs_Empty(t *testing.T) {
	dir := t.TempDir()
	result := ReadDeclaredOutputs(dir, []string{})
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestReadDeclaredOutputs_SkipsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "empty.md"), []byte("   \n  "), 0644)
	os.WriteFile(filepath.Join(dir, "real.md"), []byte("actual content"), 0644)

	result := ReadDeclaredOutputs(dir, []string{"empty.md", "real.md"})
	if result != "actual content" {
		t.Fatalf("expected only real content, got %q", result)
	}
}

func TestClearFeedback(t *testing.T) {
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

	if err := ClearFeedback(dir); err != nil {
		t.Fatal(err)
	}

	result, err := ReadAllFeedback(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("expected empty feedback after clear, got %q", result)
	}
}

func TestClearFeedback_NoDir(t *testing.T) {
	dir := t.TempDir()
	// Don't call EnsureDir — feedback/ does not exist
	if err := ClearFeedback(dir); err != nil {
		t.Fatalf("ClearFeedback on non-existent dir should return nil, got %v", err)
	}
}

func TestAuditFeedbackPath(t *testing.T) {
	got := AuditFeedbackPath("/audit", 0, 1, "review")
	want := filepath.Join("/audit", "feedback", "phase-1.iter-1.from-review.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = AuditFeedbackPath("/audit", 2, 3, "plan")
	want = filepath.Join("/audit", "feedback", "phase-3.iter-3.from-plan.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuditOutputPath(t *testing.T) {
	got := AuditOutputPath("/audit", 0, 1, "design.md")
	want := filepath.Join("/audit", "outputs", "phase-1.iter-1.design.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = AuditOutputPath("/audit", 2, 3, "report.md")
	want = filepath.Join("/audit", "outputs", "phase-3.iter-3.report.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// filepath.Base strips subdirectory prefix
	got = AuditOutputPath("/audit", 0, 1, "subdir/report.md")
	want = filepath.Join("/audit", "outputs", "phase-1.iter-1.report.md")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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

func TestAuditStreamLogPath(t *testing.T) {
	got := AuditStreamLogPath("/audit", 0, 1)
	want := filepath.Join("/audit", "logs", "phase-1.iter-1.stream.jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = AuditStreamLogPath("/audit", 4, 3)
	want = filepath.Join("/audit", "logs", "phase-5.iter-3.stream.jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDispatchCounts_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[int]int{0: 3, 9: 7}
	if err := SaveDispatchCounts(dir, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDispatchCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0] != 3 || loaded[9] != 7 {
		t.Fatalf("got %v, want map[0:3 9:7]", loaded)
	}
}

func TestDispatchCounts_NoFile(t *testing.T) {
	dir := t.TempDir()
	counts, err := LoadDispatchCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %v", counts)
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
