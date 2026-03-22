package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

// writeFile creates a file with the given content, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- DiscoverCases ---

func TestDiscoverCases(t *testing.T) {
	dir := t.TempDir()
	evalsDir := filepath.Join(dir, ".orc", "evals")
	os.MkdirAll(filepath.Join(evalsDir, "bug-fix"), 0o755)
	os.MkdirAll(filepath.Join(evalsDir, "new-feature"), 0o755)
	writeFile(t, filepath.Join(evalsDir, "not-a-dir.txt"), "hello")

	cases, err := DiscoverCases(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d: %v", len(cases), cases)
	}
	if cases[0] != "bug-fix" || cases[1] != "new-feature" {
		t.Errorf("wrong order or names: %v", cases)
	}
}

func TestDiscoverCases_NoEvalsDir(t *testing.T) {
	dir := t.TempDir()
	_, err := DiscoverCases(dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- LoadFixture ---

func TestLoadFixture(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), `
ref: abc123f
ticket: T-001
description: A test case
vars:
  FOO: bar
  BAZ: qux
`)
	f, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Ref != "abc123f" {
		t.Errorf("Ref = %q, want abc123f", f.Ref)
	}
	if f.Ticket != "T-001" {
		t.Errorf("Ticket = %q, want T-001", f.Ticket)
	}
	if f.Description != "A test case" {
		t.Errorf("Description = %q, want 'A test case'", f.Description)
	}
	if f.Vars["FOO"] != "bar" || f.Vars["BAZ"] != "qux" {
		t.Errorf("Vars = %v, want FOO=bar BAZ=qux", f.Vars)
	}
}

func TestLoadFixture_MissingRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ticket: T-001\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for missing ref")
	}
	if !strings.Contains(err.Error(), "ref is required") {
		t.Errorf("error = %v, want message containing 'ref is required'", err)
	}
}

func TestLoadFixture_MissingTicket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: abc123\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for missing ticket")
	}
	if !strings.Contains(err.Error(), "ticket is required") {
		t.Errorf("error = %v, want message containing 'ticket is required'", err)
	}
}

func TestLoadFixture_InvalidRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: \"abc; rm -rf /\"\nticket: T-001\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
	if !strings.Contains(err.Error(), "invalid characters") {
		t.Errorf("error = %v, want message containing 'invalid characters'", err)
	}
}

func TestLoadFixture_DotDotTicket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: abc123\nticket: \"..\"\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for ticket '..'")
	}
	if !strings.Contains(err.Error(), "is not allowed") {
		t.Errorf("error = %v, want message containing 'is not allowed'", err)
	}
}

func TestLoadFixture_DotTicket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: abc123\nticket: \".\"\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for ticket '.'")
	}
	if !strings.Contains(err.Error(), "is not allowed") {
		t.Errorf("error = %v, want message containing 'is not allowed'", err)
	}
}

func TestLoadFixture_PathSeparatorTicket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: abc123\nticket: \"foo/bar\"\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for ticket with path separator")
	}
	if !strings.Contains(err.Error(), "must not contain path separators") {
		t.Errorf("error = %v, want message containing 'must not contain path separators'", err)
	}
}

// --- LoadRubric ---

func TestLoadRubric(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, ".orc", "prompts", "quality.md"), "Rate the quality.")
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: tests-pass
    check: "exit 0"
    weight: 3
    expect: "exit 0"
  - name: quality
    judge: true
    prompt: ".orc/prompts/quality.md"
    weight: 2
    expect: ">= 7"
`)
	r, err := LoadRubric(dir, projectRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(r.Criteria))
	}
	if r.Criteria[0].Name != "tests-pass" {
		t.Errorf("first criterion name = %q, want tests-pass", r.Criteria[0].Name)
	}
	if !r.Criteria[1].Judge {
		t.Error("second criterion should be judge=true")
	}
}

func TestLoadRubric_InvalidCriterion(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: broken
    weight: 1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for criterion missing check and judge")
	}
	if !strings.Contains(err.Error(), "must have check or judge") {
		t.Errorf("error = %v, want message containing 'must have check or judge'", err)
	}
}

func TestLoadRubric_BothCheckAndJudge(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, ".orc", "prompts", "p.md"), "prompt")
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: conflict
    check: "exit 0"
    judge: true
    prompt: ".orc/prompts/p.md"
    weight: 1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for criterion with both check and judge")
	}
	if !strings.Contains(err.Error(), "cannot have both") {
		t.Errorf("error = %v, want message containing 'cannot have both'", err)
	}
}

func TestLoadRubric_JudgePromptTraversal(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: escape
    judge: true
    prompt: "../../etc/passwd"
    weight: 1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for traversal prompt path")
	}
	if !strings.Contains(err.Error(), "escapes project root") {
		t.Errorf("error = %v, want message containing 'escapes project root'", err)
	}
}

// --- ConfigFingerprint ---

func TestConfigFingerprint(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	promptRelPath := ".orc/prompts/phase.md"
	writeFile(t, configPath, "phases:\n  - name: p\n    prompt: .orc/prompts/phase.md\n")
	writeFile(t, filepath.Join(dir, promptRelPath), "original prompt")

	cfg := &config.Config{
		Phases: []config.Phase{{Name: "p", Prompt: promptRelPath}},
	}

	fp1, err := ConfigFingerprint(configPath, cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modify prompt file — fingerprint must change
	writeFile(t, filepath.Join(dir, promptRelPath), "changed prompt")
	fp2, err := ConfigFingerprint(configPath, cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error after modification: %v", err)
	}

	if fp1 == fp2 {
		t.Error("fingerprints should differ after prompt change")
	}
}

func TestConfigFingerprint_Deterministic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".orc", "config.yaml")
	writeFile(t, configPath, "phases:\n  - name: p\n    run: echo hello\n")

	cfg := &config.Config{
		Phases: []config.Phase{{Name: "p", Run: "echo hello"}},
	}

	fp1, err := ConfigFingerprint(configPath, cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fp2, err := ConfigFingerprint(configPath, cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprints should be identical: %q vs %q", fp1, fp2)
	}
}

// --- ComputeScore ---

func TestComputeScore(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "a", Weight: 5},
		{Name: "b", Weight: 2},
	}}
	results := []CriterionResult{
		{Name: "a", Score: 1.0},
		{Name: "b", Score: 0.7},
	}
	// (5*1.0 + 2*0.7) / 7 * 100 = 6.4/7*100 = 91.43 → 91
	got := ComputeScore(results, rubric)
	if got != 91 {
		t.Errorf("ComputeScore = %d, want 91", got)
	}
}

func TestComputeScore_AllPass(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "a", Weight: 1},
		{Name: "b", Weight: 2},
	}}
	results := []CriterionResult{
		{Name: "a", Score: 1.0},
		{Name: "b", Score: 1.0},
	}
	got := ComputeScore(results, rubric)
	if got != 100 {
		t.Errorf("ComputeScore = %d, want 100", got)
	}
}

func TestComputeScore_AllFail(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "a", Weight: 1},
		{Name: "b", Weight: 2},
	}}
	results := []CriterionResult{
		{Name: "a", Score: 0.0},
		{Name: "b", Score: 0.0},
	}
	got := ComputeScore(results, rubric)
	if got != 0 {
		t.Errorf("ComputeScore = %d, want 0", got)
	}
}

// --- parseExpect ---

func TestComputeScore_ZeroWeight(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{}}
	results := []CriterionResult{}
	got := ComputeScore(results, rubric)
	if got != 0 {
		t.Errorf("ComputeScore with zero weight = %d, want 0", got)
	}
}

func TestParseExpect_ExitZero(t *testing.T) {
	if !parseExpect("exit 0", 0, 0, false) {
		t.Error("exit 0, exitCode=0 should pass")
	}
	if parseExpect("exit 0", 1, 0, false) {
		t.Error("exit 0, exitCode=1 should fail")
	}
	if !parseExpect("exit 1", 1, 0, false) {
		t.Error("exit 1, exitCode=1 should pass")
	}
	if parseExpect("exit 1", 0, 0, false) {
		t.Error("exit 1, exitCode=0 should fail")
	}
}

func TestParseExpect_JudgeComparisons(t *testing.T) {
	tests := []struct {
		expect     string
		judgeScore float64
		want       bool
	}{
		{">= 7", 7, true},
		{">= 7", 6, false},
		{">= 7", 8, true},
		{"> 6", 7, true},
		{"> 6", 6, false},
		{"<= 8", 8, true},
		{"<= 8", 9, false},
		{"< 9", 8, true},
		{"< 9", 9, false},
		{"== 7", 7, true},
		{"== 7", 8, false},
	}
	for _, tt := range tests {
		got := parseExpect(tt.expect, 0, tt.judgeScore, true)
		if got != tt.want {
			t.Errorf("parseExpect(%q, _, %v, true) = %v, want %v",
				tt.expect, tt.judgeScore, got, tt.want)
		}
	}
}

// --- History ---

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	h := &History{Runs: []HistoryEntry{{
		Timestamp:         "2026-01-01T00:00:00Z",
		ConfigFingerprint: "abc123",
		Cases: map[string]CaseHistoryEntry{
			"test-case": {
				Score:           85,
				CostUSD:         1.2,
				DurationSeconds: 492,
				Details:         map[string]float64{"quality": 0.9},
			},
		},
	}}}
	if err := SaveHistory(dir, h); err != nil {
		t.Fatal(err)
	}
	h2, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h2.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(h2.Runs))
	}
	run := h2.Runs[0]
	if run.ConfigFingerprint != "abc123" {
		t.Errorf("fingerprint = %q, want abc123", run.ConfigFingerprint)
	}
	if run.Timestamp != "2026-01-01T00:00:00Z" {
		t.Errorf("timestamp = %q, want 2026-01-01T00:00:00Z", run.Timestamp)
	}
	c, ok := run.Cases["test-case"]
	if !ok {
		t.Fatal("missing test-case in history")
	}
	if c.Score != 85 {
		t.Errorf("score = %d, want 85", c.Score)
	}
	if c.CostUSD != 1.2 {
		t.Errorf("cost = %f, want 1.2", c.CostUSD)
	}
}

// --- RenderScoreReport ---

func TestRenderScoreReport(t *testing.T) {
	cases := []CaseResult{
		{Name: "bug-fix", Score: 85, CostUSD: 1.20, DurationSeconds: 492, PassCount: 5, TotalCount: 5},
		{Name: "new-feature", Score: 62, CostUSD: 4.80, DurationSeconds: 1323, PassCount: 3, TotalCount: 5,
			Failures: []string{"tests-pass: exit 1"}},
	}
	var buf bytes.Buffer
	RenderScoreReport(&buf, "a1b2c3", cases)
	out := buf.String()

	for _, want := range []string{"bug-fix", "new-feature", "85/100", "62/100", "a1b2c3", "Totals:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// --- RenderCaseList ---

func TestRenderCaseList(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "evals", "bug-fix"), 0o755)
	writeFile(t, filepath.Join(dir, ".orc", "evals", "bug-fix", "fixture.yaml"),
		"ref: abc123\nticket: T-001\ndescription: Fix the bug\n")
	os.MkdirAll(filepath.Join(dir, ".orc", "evals", "feature"), 0o755)
	writeFile(t, filepath.Join(dir, ".orc", "evals", "feature", "fixture.yaml"),
		"ref: def456\nticket: T-002\n")

	var buf bytes.Buffer
	if err := RenderCaseList(&buf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"bug-fix", "Fix the bug", "feature"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// --- ValidateCaseName ---

// validateCaseName checks that a case name is a simple directory name without path separators.
func validateCaseName(name string) bool {
	return name != "" && filepath.Base(name) == name && name != "." && name != ".."
}

func TestValidateCaseName(t *testing.T) {
	valid := []string{"bug-fix", "feature-123", "my_case"}
	for _, name := range valid {
		if !validateCaseName(name) {
			t.Errorf("%q should be a valid case name", name)
		}
	}
	invalid := []string{"../escape", ".", "..", "foo/bar", ""}
	for _, name := range invalid {
		if validateCaseName(name) {
			t.Errorf("%q should be an invalid case name", name)
		}
	}
}

// --- ValidateRef ---

func TestValidateRef(t *testing.T) {
	dir := t.TempDir()

	validRefs := []string{"abc123f", "main", "v1.0", "HEAD~3"}
	for _, ref := range validRefs {
		writeFile(t, filepath.Join(dir, "fixture.yaml"),
			"ref: "+ref+"\nticket: T-001\n")
		_, err := LoadFixture(dir)
		if err != nil {
			t.Errorf("ref %q should be valid, got error: %v", ref, err)
		}
	}

	invalidRefs := []string{";", "|", "`", "$("}
	for _, ref := range invalidRefs {
		writeFile(t, filepath.Join(dir, "fixture.yaml"),
			"ref: \""+ref+"\"\nticket: T-001\n")
		_, err := LoadFixture(dir)
		if err == nil {
			t.Errorf("ref %q should be invalid but got no error", ref)
		}
	}
}

// --- CopyOrcDir tests ---

func TestCopyOrcDir_SingleConfig(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	configPath := filepath.Join(src, ".orc", "config.yaml")
	promptRelPath := ".orc/prompts/phase.md"
	writeFile(t, configPath, "phases:\n  - name: p\n    prompt: .orc/prompts/phase.md\n")
	writeFile(t, filepath.Join(src, promptRelPath), "do the thing")

	cfg := &config.Config{
		Phases: []config.Phase{{Name: "p", Prompt: promptRelPath}},
	}

	if err := copyOrcDir(src, dst, configPath, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".orc", "config.yaml")); err != nil {
		t.Error("config.yaml not copied to worktree")
	}
	if _, err := os.Stat(filepath.Join(dst, promptRelPath)); err != nil {
		t.Error("prompt file not copied to worktree")
	}
}

func TestCopyOrcDir_MultiWorkflow(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	configPath := filepath.Join(src, ".orc", "workflows", "review.yaml")
	promptRelPath := ".orc/prompts/review.md"
	writeFile(t, configPath, "phases:\n  - name: p\n    prompt: .orc/prompts/review.md\n")
	writeFile(t, filepath.Join(src, promptRelPath), "review prompt")

	cfg := &config.Config{
		Phases: []config.Phase{{Name: "p", Prompt: promptRelPath}},
	}

	if err := copyOrcDir(src, dst, configPath, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".orc", "workflows", "review.yaml")); err != nil {
		t.Error("workflow config not copied")
	}
	if _, err := os.Stat(filepath.Join(dst, promptRelPath)); err != nil {
		t.Error("prompt file not copied")
	}
}

func TestCopyOrcDir_MissingPromptFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	configPath := filepath.Join(src, ".orc", "config.yaml")
	writeFile(t, configPath, "phases:\n  - name: p\n    prompt: .orc/prompts/missing.md\n")

	cfg := &config.Config{
		Phases: []config.Phase{{Name: "p", Prompt: ".orc/prompts/missing.md"}},
	}

	err := copyOrcDir(src, dst, configPath, cfg)
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
}

func TestLoadFixture_StrictYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"),
		"ref: abc123\nticket: T-001\nunknown_field: oops\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for unknown YAML field")
	}
	if !strings.Contains(err.Error(), "parsing fixture.yaml") {
		t.Errorf("error = %v, want message containing 'parsing fixture.yaml'", err)
	}
}

func TestLoadFixture_VarShadowsBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"),
		"ref: abc123\nticket: T-001\nvars:\n  TICKET: override\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for var overriding built-in")
	}
	if !strings.Contains(err.Error(), "overrides a built-in variable") {
		t.Errorf("error = %v, want message containing 'overrides a built-in variable'", err)
	}
}

func TestLoadFixture_VarShadowsArtifactsDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"),
		"ref: abc123\nticket: T-001\nvars:\n  ARTIFACTS_DIR: x\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for var overriding built-in")
	}
	if !strings.Contains(err.Error(), "overrides a built-in variable") {
		t.Errorf("error = %v, want message containing 'overrides a built-in variable'", err)
	}
}

func TestLoadRubric_JudgePromptNotFound(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: test
    judge: true
    weight: 1
    prompt: .orc/prompts/nonexistent.md
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want message containing 'does not exist'", err)
	}
}

func TestRunEval_CaseNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".orc", "evals", "real-case"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmpDir, ".orc", "config.yaml")
	writeFile(t, configPath, "phases: []\n")
	_, _, err := RunEval(context.Background(), tmpDir, configPath, "", &config.Config{}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent case")
	}
	if !strings.Contains(err.Error(), "case not found") {
		t.Errorf("error = %v, want message containing 'case not found'", err)
	}
	if !strings.Contains(err.Error(), "real-case") {
		t.Errorf("error = %v, want message containing 'real-case'", err)
	}
}

func TestLoadRubric_StrictYAML(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: test
    check: "exit 0"
    weight: 1
    weigth: 2
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for unknown YAML field (weigth typo)")
	}
	if !strings.Contains(err.Error(), "parsing rubric.yaml") {
		t.Errorf("error = %v, want message containing 'parsing rubric.yaml'", err)
	}
}

func TestComputeScore_ClampsAbove100(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "a", Weight: 1},
	}}
	results := []CriterionResult{
		{Name: "a", Score: 1.5}, // >1.0 should be clamped to 100
	}
	got := ComputeScore(results, rubric)
	if got != 100 {
		t.Errorf("ComputeScore = %d, want 100 (clamped)", got)
	}
}

func TestParseExpect_Defaults(t *testing.T) {
	if !parseExpect("", 0, 0, false) {
		t.Error("empty expect, exitCode=0 should pass (default)")
	}
	if parseExpect("", 1, 0, false) {
		t.Error("empty expect, exitCode=1 should fail (default)")
	}
	if !parseExpect("", 0, 7, true) {
		t.Error("empty expect, judgeScore=7 should pass (default >= 7)")
	}
	if parseExpect("", 0, 6, true) {
		t.Error("empty expect, judgeScore=6 should fail (default >= 7)")
	}
	if !parseExpect("garbage", 0, 0, false) {
		t.Error("garbage expect, exitCode=0 should pass (default)")
	}
	if !parseExpect("garbage", 0, 8, true) {
		t.Error("garbage expect, judgeScore=8 should pass (default >= 7)")
	}
}

func TestAppendResult(t *testing.T) {
	h := &History{}
	cases := []CaseResult{
		{
			Name:            "bug-fix",
			Score:           85,
			CostUSD:         1.20,
			DurationSeconds: 492,
			Details:         map[string]float64{"quality": 0.9},
		},
	}
	h.AppendResult("abc123", cases)
	if len(h.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(h.Runs))
	}
	run := h.Runs[0]
	if run.ConfigFingerprint != "abc123" {
		t.Errorf("fingerprint = %q, want abc123", run.ConfigFingerprint)
	}
	c, ok := run.Cases["bug-fix"]
	if !ok {
		t.Fatal("missing bug-fix case")
	}
	if c.Score != 85 {
		t.Errorf("score = %d, want 85", c.Score)
	}
	if c.CostUSD != 1.20 {
		t.Errorf("cost = %f, want 1.20", c.CostUSD)
	}
	if c.DurationSeconds != 492 {
		t.Errorf("duration = %f, want 492", c.DurationSeconds)
	}
	if c.Details["quality"] != 0.9 {
		t.Errorf("quality detail = %f, want 0.9", c.Details["quality"])
	}
}

func TestRenderHistoryReport(t *testing.T) {
	h := &History{Runs: []HistoryEntry{
		{
			Timestamp:         "2026-01-15T10:30:00Z",
			ConfigFingerprint: "abc123",
			Cases: map[string]CaseHistoryEntry{
				"test-case": {Score: 85, CostUSD: 1.20, DurationSeconds: 492},
			},
		},
	}}
	var buf bytes.Buffer
	RenderHistoryReport(&buf, h)
	out := buf.String()
	for _, want := range []string{"abc123", "85/100", "Jan 15"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderHistoryReport_Empty(t *testing.T) {
	h := &History{}
	var buf bytes.Buffer
	RenderHistoryReport(&buf, h)
	// Should not panic on empty history
}

func TestRenderJSON(t *testing.T) {
	cases := []CaseResult{
		{Name: "bug-fix", Score: 85, CostUSD: 1.20, DurationSeconds: 492, PassCount: 3, TotalCount: 3},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, "abc123", cases); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"abc123", "bug-fix", "85"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// Verify snake_case JSON keys (not PascalCase)
	for _, want := range []string{`"name"`, `"score"`, `"cost_usd"`, `"duration_seconds"`, `"pass_count"`, `"total_count"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing snake_case key %s", want)
		}
	}
}

func TestLoadRubric_DuplicateCriterionName(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: tests-pass
    check: "exit 0"
    weight: 1
  - name: tests-pass
    check: "exit 0"
    weight: 2
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for duplicate criterion name")
	}
	if !strings.Contains(err.Error(), "duplicate criterion") {
		t.Errorf("error = %v, want message containing 'duplicate criterion'", err)
	}
}

func TestLoadRubric_InvalidExpect(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, ".orc", "prompts", "p.md"), "prompt")
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: quality
    judge: true
    prompt: ".orc/prompts/p.md"
    weight: 1
    expect: "=> 7"
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for invalid expect operator")
	}
	if !strings.Contains(err.Error(), "invalid expect") {
		t.Errorf("error = %v, want message containing 'invalid expect'", err)
	}
}

func TestLoadRubric_WeightZero(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: tests-pass
    check: "exit 0"
    weight: 0
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for weight 0")
	}
	if !strings.Contains(err.Error(), "weight must be > 0") {
		t.Errorf("error = %v, want message containing 'weight must be > 0'", err)
	}
}

func TestLoadRubric_WeightNegative(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: tests-pass
    check: "exit 0"
    weight: -1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for negative weight")
	}
	if !strings.Contains(err.Error(), "weight must be > 0") {
		t.Errorf("error = %v, want message containing 'weight must be > 0'", err)
	}
}

func TestLoadRubric_MissingName(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - check: "exit 0"
    weight: 1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for missing criterion name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error = %v, want message containing 'name is required'", err)
	}
}

func TestLoadRubric_JudgeWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: quality
    judge: true
    weight: 1
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for judge criterion missing prompt")
	}
	if !strings.Contains(err.Error(), "require a prompt file") {
		t.Errorf("error = %v, want message containing 'require a prompt file'", err)
	}
}

func TestLoadRubric_InvalidScriptExpect(t *testing.T) {
	dir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(dir, "rubric.yaml"), `
criteria:
  - name: tests-pass
    check: "exit 0"
    weight: 1
    expect: "pass"
`)
	_, err := LoadRubric(dir, projectRoot)
	if err == nil {
		t.Fatal("expected error for invalid script expect")
	}
	if !strings.Contains(err.Error(), "invalid expect") {
		t.Errorf("error = %v, want message containing 'invalid expect'", err)
	}
}
