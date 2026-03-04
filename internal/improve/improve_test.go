package improve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/fileblocks"
)

func TestReadOrcFiles_Success(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "phases"), 0755)
	os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: .orc/phases/plan.md\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".orc", "phases", "plan.md"), []byte("Plan prompt content"), 0644)

	configYAML, phaseFiles, err := readOrcFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(configYAML, "name: test") {
		t.Fatalf("configYAML missing 'name: test', got: %s", configYAML)
	}
	content, ok := phaseFiles[".orc/phases/plan.md"]
	if !ok {
		t.Fatalf("phaseFiles missing .orc/phases/plan.md, got keys: %v", phaseFiles)
	}
	if content != "Plan prompt content" {
		t.Fatalf("phaseFiles content = %q, want %q", content, "Plan prompt content")
	}
}

func TestReadOrcFiles_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, _, err := readOrcFiles(dir)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "orc init") {
		t.Fatalf("error should mention 'orc init', got: %v", err)
	}
}

func TestBuildOneShotPrompt(t *testing.T) {
	result := buildOneShotPrompt(
		"name: test\nphases: []",
		map[string]string{
			".orc/phases/a.md": "alpha prompt",
			".orc/phases/b.md": "beta prompt",
		},
		"",
		"add a lint phase",
	)

	checks := []string{
		"orc Config Schema Reference",
		"name: test",
		"alpha prompt",
		"beta prompt",
		"add a lint phase",
		"file=.orc/config.yaml",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("prompt missing %q", c)
		}
	}

	// Verify sorted order: a.md before b.md
	idxA := strings.Index(result, ".orc/phases/a.md")
	idxB := strings.Index(result, ".orc/phases/b.md")
	if idxA >= idxB {
		t.Errorf("a.md (at %d) should appear before b.md (at %d)", idxA, idxB)
	}

	// No audit data
	if strings.Contains(result, "Previous Run Data") {
		t.Error("prompt should not contain 'Previous Run Data' when no audit data provided")
	}
}

func TestBuildOneShotPrompt_WithAudit(t *testing.T) {
	result := buildOneShotPrompt(
		"name: test",
		map[string]string{},
		"Total cost: $2.50\nPhase: plan ($1.25)",
		"optimize costs",
	)
	if !strings.Contains(result, "Previous Run Data") {
		t.Error("prompt should contain 'Previous Run Data'")
	}
	if !strings.Contains(result, "Total cost: $2.50") {
		t.Error("prompt should contain audit cost data")
	}
}

func TestBuildInteractiveContext(t *testing.T) {
	result := buildInteractiveContext(
		"name: test",
		map[string]string{".orc/phases/plan.md": "prompt"},
		"",
	)
	if !strings.Contains(result, "You are orc") {
		t.Error("context missing orc identity")
	}
	if !strings.Contains(result, "orc Config Schema Reference") {
		t.Error("context missing schema reference")
	}
	if !strings.Contains(result, "name: test") {
		t.Error("context missing config")
	}
	if !strings.Contains(result, "prompt") {
		t.Error("context missing phase file content")
	}
	if strings.Contains(result, "Output Format") {
		t.Error("interactive context should not contain 'Output Format'")
	}
	if !strings.Contains(result, "No run data yet") {
		t.Error("context should contain no-data fallback when no audit data provided")
	}
}

func TestBuildInteractiveContext_WithAudit(t *testing.T) {
	result := buildInteractiveContext(
		"name: test",
		map[string]string{},
		"Phase timing: plan (2m 15s)",
	)
	if !strings.Contains(result, "My Run History") {
		t.Error("context should contain 'My Run History'")
	}
	if !strings.Contains(result, "Phase timing: plan (2m 15s)") {
		t.Error("context should contain audit timing data")
	}
	if !strings.Contains(result, "lead the conversation") {
		t.Error("context should contain proactive analysis directive")
	}
}

func TestWriteChanges_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "phases"), 0755)
	os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: .orc/phases/plan.md\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".orc", "phases", "plan.md"), []byte("plan prompt"), 0644)

	blocks := []fileblocks.FileBlock{
		{Path: ".orc/config.yaml", Content: "name: test-updated\nphases:\n  - name: plan\n    type: agent\n    prompt: .orc/phases/plan.md"},
	}
	if err := writeChanges(dir, blocks); err != nil {
		t.Fatalf("writeChanges failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orc", "config.yaml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "test-updated") {
		t.Fatalf("config not updated, got: %s", string(data))
	}
}

func TestWriteChanges_ValidConfig_ExistingPromptNotInBlocks(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "phases"), 0755)
	os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: .orc/phases/plan.md\n  - name: implement\n    type: agent\n    prompt: .orc/phases/implement.md\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".orc", "phases", "plan.md"), []byte("plan prompt"), 0644)
	os.WriteFile(filepath.Join(dir, ".orc", "phases", "implement.md"), []byte("implement prompt"), 0644)

	blocks := []fileblocks.FileBlock{
		{Path: ".orc/config.yaml", Content: "name: test\nphases:\n  - name: plan\n    type: agent\n    prompt: .orc/phases/plan.md\n  - name: implement\n    type: agent\n    prompt: .orc/phases/implement.md\n  - name: lint\n    type: script\n    run: make lint"},
	}
	if err := writeChanges(dir, blocks); err != nil {
		t.Fatalf("writeChanges should pass with existing prompt files, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orc", "config.yaml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "make lint") {
		t.Fatalf("config not updated with lint phase, got: %s", string(data))
	}
}

func TestWriteChanges_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "phases"), 0755)
	os.WriteFile(filepath.Join(dir, ".orc", "config.yaml"), []byte("name: test\nphases:\n  - name: plan\n    type: script\n    run: echo ok\n"), 0644)

	blocks := []fileblocks.FileBlock{
		{Path: ".orc/config.yaml", Content: "name: bad\nphases:\n  - name: x\n    type: agent\n    prompt: .orc/phases/nonexistent.md"},
	}
	err := writeChanges(dir, blocks)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("error should mention 'invalid config', got: %v", err)
	}

	// Original config should NOT be overwritten
	data, err := os.ReadFile(filepath.Join(dir, ".orc", "config.yaml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "name: test") {
		t.Fatalf("original config was overwritten, got: %s", string(data))
	}
}

func TestWriteChanges_OnlyOrcPaths(t *testing.T) {
	dir := t.TempDir()
	blocks := []fileblocks.FileBlock{
		{Path: "src/main.go", Content: "package main"},
		{Path: "README.md", Content: "# readme"},
	}
	err := writeChanges(dir, blocks)
	if err == nil {
		t.Fatal("expected error for non-.orc paths")
	}
	if !strings.Contains(err.Error(), "outside .orc/") {
		t.Fatalf("error should mention 'outside .orc/', got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "main.go")); err == nil {
		t.Fatal("src/main.go should not have been written")
	}
}

func TestWriteChanges_MixedPaths(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".orc", "phases"), 0755)

	blocks := []fileblocks.FileBlock{
		{Path: ".orc/phases/new.md", Content: "new prompt"},
		{Path: "src/main.go", Content: "package main"},
	}
	if err := writeChanges(dir, blocks); err != nil {
		t.Fatalf("writeChanges should succeed with mixed paths, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orc", "phases", "new.md"))
	if err != nil {
		t.Fatalf("reading new.md: %v", err)
	}
	if !strings.Contains(string(data), "new prompt") {
		t.Fatalf("new.md content wrong, got: %s", string(data))
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "main.go")); err == nil {
		t.Fatal("src/main.go should not have been written")
	}
}

func TestWriteChanges_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	blocks := []fileblocks.FileBlock{
		{Path: ".orc/../../traversal-test-canary.txt", Content: "pwned"},
	}
	err := writeChanges(dir, blocks)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "outside .orc/") {
		t.Fatalf("error should mention 'outside .orc/', got: %v", err)
	}
	// The canary file should not have been written
	if _, err := os.Stat(filepath.Join(dir, "..", "traversal-test-canary.txt")); err == nil {
		t.Fatal("traversal path should not have been written")
	}
}

func TestWriteChanges_CreatesSubdirs(t *testing.T) {
	dir := t.TempDir()
	blocks := []fileblocks.FileBlock{
		{Path: ".orc/phases/deep/nested.md", Content: "deep content"},
	}
	if err := writeChanges(dir, blocks); err != nil {
		t.Fatalf("writeChanges failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".orc", "phases", "deep", "nested.md"))
	if err != nil {
		t.Fatalf("reading nested.md: %v", err)
	}
	if !strings.Contains(string(data), "deep content") {
		t.Fatalf("nested.md content wrong, got: %s", string(data))
	}
}

func TestReadAuditSummary_NoAuditDir(t *testing.T) {
	dir := t.TempDir()
	result := readAuditSummary(dir)
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestReadAuditSummary_WithData(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, ".orc", "audit", "TEST-1")
	os.MkdirAll(auditDir, 0755)

	costsJSON := `{"phases":[{"name":"plan","phase_index":1,"cost_usd":1.23,"input_tokens":10,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"turns":1}],"total_cost_usd":1.23,"total_input_tokens":10,"total_output_tokens":100,"total_cache_creation_input_tokens":0,"total_cache_read_input_tokens":0}`
	os.WriteFile(filepath.Join(auditDir, "costs.json"), []byte(costsJSON), 0644)

	timingJSON := `{"entries":[{"phase":"plan","start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:02:15Z","duration":"2m 15s"}]}`
	os.WriteFile(filepath.Join(auditDir, "timing.json"), []byte(timingJSON), 0644)

	// Add phase logs, feedback, state, and artifacts
	os.MkdirAll(filepath.Join(auditDir, "logs"), 0755)
	os.WriteFile(filepath.Join(auditDir, "logs", "phase-1.iter-1.log"), []byte("plan output content"), 0644)

	os.MkdirAll(filepath.Join(auditDir, "feedback"), 0755)
	os.WriteFile(filepath.Join(auditDir, "feedback", "phase-3.iter-2.from-review.md"), []byte("review feedback content"), 0644)

	artifactsDir := filepath.Join(dir, ".orc", "artifacts", "TEST-1")
	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)
	os.WriteFile(filepath.Join(artifactsDir, "logs", "phase-1.log"), []byte("final plan log"), 0644)

	os.MkdirAll(filepath.Join(artifactsDir, "feedback"), 0755)
	os.WriteFile(filepath.Join(artifactsDir, "feedback", "from-review.md"), []byte("current feedback"), 0644)

	os.WriteFile(filepath.Join(artifactsDir, "state.json"), []byte(`{"phase_index": 3, "ticket": "TEST-1", "status": "completed"}`), 0644)

	result := readAuditSummary(dir)
	if !strings.Contains(result, "TEST-1") {
		t.Errorf("result should contain ticket name, got: %s", result)
	}
	if !strings.Contains(result, "$1.23") {
		t.Errorf("result should contain cost, got: %s", result)
	}
	if !strings.Contains(result, "2m 15s") {
		t.Errorf("result should contain timing, got: %s", result)
	}
	if !strings.Contains(result, "plan output content") {
		t.Errorf("result should contain archived log, got: %s", result)
	}
	if !strings.Contains(result, "review feedback content") {
		t.Errorf("result should contain archived feedback, got: %s", result)
	}
	if !strings.Contains(result, "final plan log") {
		t.Errorf("result should contain artifacts log, got: %s", result)
	}
	if !strings.Contains(result, "Run status: completed") {
		t.Errorf("result should contain run status, got: %s", result)
	}
}

func TestReadAuditSummary_SortsByRecency(t *testing.T) {
	dir := t.TempDir()
	auditBase := filepath.Join(dir, ".orc", "audit")

	// Create 3 audit dirs with different timing starts.
	// OLD-RUN started earliest, NEW-RUN started latest.
	runs := []struct {
		name  string
		start string
	}{
		{"A-OLD-RUN", "2026-01-01T00:00:00Z"},
		{"B-NEW-RUN", "2026-03-01T00:00:00Z"},
		{"C-MID-RUN", "2026-02-01T00:00:00Z"},
	}
	for _, r := range runs {
		ad := filepath.Join(auditBase, r.name)
		os.MkdirAll(ad, 0755)
		timing := fmt.Sprintf(`{"entries":[{"phase":"plan","start":"%s","end":"%s","duration":"1m 00s"}]}`, r.start, r.start)
		os.WriteFile(filepath.Join(ad, "timing.json"), []byte(timing), 0644)
		os.WriteFile(filepath.Join(ad, "costs.json"), []byte(`{"total_cost_usd":0.10,"phases":[]}`), 0644)
	}

	result := readAuditSummary(dir)

	// B-NEW-RUN (March) should appear before C-MID-RUN (Feb) before A-OLD-RUN (Jan)
	newIdx := strings.Index(result, "B-NEW-RUN")
	midIdx := strings.Index(result, "C-MID-RUN")
	oldIdx := strings.Index(result, "A-OLD-RUN")

	if newIdx == -1 || midIdx == -1 || oldIdx == -1 {
		t.Fatalf("expected all runs in output, got:\n%s", result)
	}
	if newIdx >= midIdx {
		t.Errorf("NEW-RUN (at %d) should appear before MID-RUN (at %d)", newIdx, midIdx)
	}
	if midIdx >= oldIdx {
		t.Errorf("MID-RUN (at %d) should appear before OLD-RUN (at %d)", midIdx, oldIdx)
	}
}

func TestGatherPhaseLogs(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit", "TEST-1")
	artifactsDir := filepath.Join(dir, "artifacts", "TEST-1")

	os.MkdirAll(filepath.Join(auditDir, "logs"), 0755)
	os.WriteFile(filepath.Join(auditDir, "logs", "phase-1.iter-1.log"), []byte("iteration 1 log content"), 0644)
	os.WriteFile(filepath.Join(auditDir, "logs", "phase-2.iter-1.log"), []byte("phase 2 iter 1"), 0644)

	os.MkdirAll(filepath.Join(artifactsDir, "logs"), 0755)
	os.WriteFile(filepath.Join(artifactsDir, "logs", "phase-1.log"), []byte("final phase 1 log"), 0644)
	os.WriteFile(filepath.Join(artifactsDir, "logs", "phase-2.log"), []byte("final phase 2 log"), 0644)

	result := gatherPhaseLogs(auditDir, artifactsDir)
	for _, want := range []string{
		"iteration 1 log content",
		"phase 2 iter 1",
		"final phase 1 log",
		"final phase 2 log",
		"#### Log:",
		"```",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q, got: %s", want, result)
		}
	}
}

func TestGatherPhaseLogs_Empty(t *testing.T) {
	result := gatherPhaseLogs("/nonexistent/audit", "/nonexistent/artifacts")
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestGatherFeedback(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit", "TEST-1")
	artifactsDir := filepath.Join(dir, "artifacts", "TEST-1")

	os.MkdirAll(filepath.Join(auditDir, "feedback"), 0755)
	os.WriteFile(filepath.Join(auditDir, "feedback", "phase-3.iter-2.from-review-plan.md"), []byte("# Review feedback\nFix the bug"), 0644)

	os.MkdirAll(filepath.Join(artifactsDir, "feedback"), 0755)
	os.WriteFile(filepath.Join(artifactsDir, "feedback", "from-review.md"), []byte("# Current feedback\nNot done yet"), 0644)

	result := gatherFeedback(auditDir, artifactsDir)
	for _, want := range []string{
		"Review feedback",
		"Current feedback",
		"#### Feedback:",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q, got: %s", want, result)
		}
	}
}

func TestGatherFeedback_Empty(t *testing.T) {
	result := gatherFeedback("/nonexistent/audit", "/nonexistent/artifacts")
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestGatherRunStatus_Failed(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts", "TEST-1")
	os.MkdirAll(artifactsDir, 0755)
	os.WriteFile(filepath.Join(artifactsDir, "state.json"), []byte(`{"phase_index": 4, "ticket": "TEST-1", "status": "failed"}`), 0644)

	result := gatherRunStatus(filepath.Join(dir, "nonexistent"), artifactsDir)
	if !strings.Contains(result, "Run status: failed") {
		t.Errorf("result should contain 'Run status: failed', got: %q", result)
	}
	if !strings.Contains(result, "phase 5") {
		t.Errorf("result should contain 'phase 5' (1-indexed), got: %q", result)
	}
}

func TestGatherRunStatus_Completed(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts", "TEST-1")
	os.MkdirAll(artifactsDir, 0755)
	os.WriteFile(filepath.Join(artifactsDir, "state.json"), []byte(`{"phase_index": 3, "ticket": "TEST-1", "status": "completed"}`), 0644)

	result := gatherRunStatus(filepath.Join(dir, "nonexistent"), artifactsDir)
	if result != "- Run status: completed" {
		t.Errorf("expected '- Run status: completed', got: %q", result)
	}
	if strings.Contains(result, "phase") {
		t.Errorf("completed status should not contain 'phase', got: %q", result)
	}
}

func TestGatherRunStatus_NoState(t *testing.T) {
	dir := t.TempDir()
	result := gatherRunStatus(filepath.Join(dir, "nonexistent"), dir)
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestCopyOrcDir(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, ".orc", "phases"), 0755)
	os.MkdirAll(filepath.Join(srcDir, ".orc", "artifacts", "ticket"), 0755)
	os.MkdirAll(filepath.Join(srcDir, ".orc", "audit", "ticket"), 0755)
	os.WriteFile(filepath.Join(srcDir, ".orc", "config.yaml"), []byte("name: test"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".orc", "phases", "plan.md"), []byte("plan prompt"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".orc", "artifacts", "ticket", "state.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".orc", "audit", "ticket", "costs.json"), []byte("{}"), 0644)

	dstDir := t.TempDir()
	if err := copyOrcDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyOrcDir failed: %v", err)
	}

	// Config should be copied
	data, err := os.ReadFile(filepath.Join(dstDir, ".orc", "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml not copied: %v", err)
	}
	if string(data) != "name: test" {
		t.Fatalf("config.yaml content wrong, got: %s", string(data))
	}

	// Phase file should be copied
	data, err = os.ReadFile(filepath.Join(dstDir, ".orc", "phases", "plan.md"))
	if err != nil {
		t.Fatalf("plan.md not copied: %v", err)
	}
	if string(data) != "plan prompt" {
		t.Fatalf("plan.md content wrong, got: %s", string(data))
	}

	// Artifacts should NOT be copied
	if _, err := os.Stat(filepath.Join(dstDir, ".orc", "artifacts")); err == nil {
		t.Fatal("artifacts/ should not be copied")
	}

	// Audit should NOT be copied
	if _, err := os.Stat(filepath.Join(dstDir, ".orc", "audit")); err == nil {
		t.Fatal("audit/ should not be copied")
	}
}
