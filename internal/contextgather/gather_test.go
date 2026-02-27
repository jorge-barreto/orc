package contextgather

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGather_DirTree(t *testing.T) {
	dir := t.TempDir()

	// Create some files and directories
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if !strings.Contains(pc.DirTree, "src/") {
		t.Fatal("expected DirTree to contain src/")
	}
	if !strings.Contains(pc.DirTree, "  main.go") {
		t.Fatal("expected DirTree to contain nested main.go")
	}
	if !strings.Contains(pc.DirTree, "README.md") {
		t.Fatal("expected DirTree to contain README.md")
	}
}

func TestGather_SkipsDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if strings.Contains(pc.DirTree, ".git") {
		t.Fatal("DirTree should not contain .git")
	}
	if strings.Contains(pc.DirTree, "node_modules") {
		t.Fatal("DirTree should not contain node_modules")
	}
	if !strings.Contains(pc.DirTree, "src/") {
		t.Fatal("DirTree should contain src/")
	}
}

func TestGather_WellKnownFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if pc.Files["README.md"] != "# Hello" {
		t.Fatalf("expected README.md content, got %q", pc.Files["README.md"])
	}
	if pc.Files["go.mod"] != "module test" {
		t.Fatalf("expected go.mod content, got %q", pc.Files["go.mod"])
	}
}

func TestGather_MissingFilesOmitted(t *testing.T) {
	dir := t.TempDir()

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if len(pc.Files) != 0 {
		t.Fatalf("expected no files, got %d", len(pc.Files))
	}
}

func TestGather_GlobPatterns(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	os.MkdirAll(wfDir, 0755)
	os.WriteFile(filepath.Join(wfDir, "ci.yml"), []byte("name: CI"), 0644)

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	key := filepath.Join(".github", "workflows", "ci.yml")
	if pc.Files[key] != "name: CI" {
		t.Fatalf("expected workflow file content, got %q", pc.Files[key])
	}
}

func TestGather_NonGitDir(t *testing.T) {
	dir := t.TempDir()

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if pc.GitLog != "" {
		t.Fatalf("expected empty GitLog for non-git dir, got %q", pc.GitLog)
	}
}

func TestRender_IncludesSections(t *testing.T) {
	pc := &ProjectContext{
		DirTree: "src/\n  main.go\nREADME.md\n",
		Files:   map[string]string{"README.md": "# Hello"},
		GitLog:  "abc123 Initial commit",
	}

	rendered := pc.Render()

	if !strings.Contains(rendered, "## Project Directory Structure") {
		t.Fatal("rendered should contain directory structure section")
	}
	if !strings.Contains(rendered, "## Key Files") {
		t.Fatal("rendered should contain key files section")
	}
	if !strings.Contains(rendered, "## Recent Git History") {
		t.Fatal("rendered should contain git history section")
	}
	if !strings.Contains(rendered, "# Hello") {
		t.Fatal("rendered should contain file content")
	}
}

func TestRender_NoGitLog(t *testing.T) {
	pc := &ProjectContext{
		DirTree: "src/\n",
		Files:   map[string]string{},
		GitLog:  "",
	}

	rendered := pc.Render()

	if strings.Contains(rendered, "## Recent Git History") {
		t.Fatal("rendered should not contain git history when empty")
	}
}

func TestGather_LargeFileTruncated(t *testing.T) {
	dir := t.TempDir()
	largeContent := strings.Repeat("x", maxFileSize+100)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(largeContent), 0644)

	pc, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	content := pc.Files["README.md"]
	if !strings.HasSuffix(content, "... (truncated)") {
		t.Fatal("large file should be truncated")
	}
	if len(content) > maxFileSize+50 {
		t.Fatalf("truncated content too large: %d", len(content))
	}
}
