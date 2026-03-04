package improve

import (
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
	if strings.Contains(result, "Previous Run Data") {
		t.Error("context should not contain 'Previous Run Data' when no audit data provided")
	}
}

func TestBuildInteractiveContext_WithAudit(t *testing.T) {
	result := buildInteractiveContext(
		"name: test",
		map[string]string{},
		"Phase timing: plan (2m 15s)",
	)
	if !strings.Contains(result, "Previous Run Data") {
		t.Error("context should contain 'Previous Run Data'")
	}
	if !strings.Contains(result, "Phase timing: plan (2m 15s)") {
		t.Error("context should contain audit timing data")
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
