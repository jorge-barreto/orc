package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

// initGitRepo creates a git repo at dir with one commit and returns nothing.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitCommitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func gitStatusPorcelain(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return string(out)
}

// TestBuildStage_SkipsGitHooks proves W1: a project pre-commit hook (shared by
// worktrees via .git/hooks) must NOT abort the throwaway curation commit, so
// BuildStage passes --no-verify.
func TestBuildStage_SkipsGitHooks(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)

	writeFile(t, filepath.Join(repo, ".orc", "config.yaml"),
		"phases:\n  - name: p\n    prompt: .orc/prompts/p.md\n")
	writeFile(t, filepath.Join(repo, ".orc", "prompts", "p.md"), "REF VERSION")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "fixture.yaml"),
		"ref: HEAD\nticket: T-1\nspec: spec.md\n")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "spec.md"), "BUILD THE THING")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "rubric.yaml"),
		"criteria:\n  - name: c\n    check: \"exit 0\"\n    weight: 1\n")
	ref := gitCommitAll(t, repo, "init")

	// Install a pre-commit hook that always fails. Worktrees share .git/hooks,
	// so without --no-verify this would abort the curation commit.
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'hook says no' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Phases: []config.Phase{{Name: "p", Prompt: ".orc/prompts/p.md"}}}
	fixture := &Fixture{Ref: ref, Ticket: "T-1", Spec: "spec.md"}

	wt, err := CreateWorktree(context.Background(), repo, ref, "bug-fix")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer RemoveWorktree(repo, wt)

	if _, err := BuildStage(context.Background(), repo, wt,
		filepath.Join(repo, ".orc", "config.yaml"), cfg, fixture, "bug-fix"); err != nil {
		t.Fatalf("BuildStage failed (pre-commit hook should be skipped): %v", err)
	}
	if s := gitStatusPorcelain(t, wt); s != "" {
		t.Errorf("worktree not clean after BuildStage:\n%s", s)
	}
}

func TestBuildStage(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)

	// Project files at the ref: a workflow config, a prompt, an eval case (with grader).
	writeFile(t, filepath.Join(repo, ".orc", "config.yaml"),
		"phases:\n  - name: p\n    prompt: .orc/prompts/p.md\n")
	writeFile(t, filepath.Join(repo, ".orc", "prompts", "p.md"), "REF VERSION")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "fixture.yaml"),
		"ref: HEAD\nticket: T-1\nspec: spec.md\n")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "spec.md"), "BUILD THE THING")
	writeFile(t, filepath.Join(repo, ".orc", "evals", "bug-fix", "rubric.yaml"),
		"criteria:\n  - name: c\n    check: \"exit 0\"\n    weight: 1\n")
	ref := gitCommitAll(t, repo, "init")

	// Live edit: the prompt differs from the committed ref version (this is what
	// today's injection makes "dirty"; the curation commit must absorb it).
	writeFile(t, filepath.Join(repo, ".orc", "prompts", "p.md"), "LIVE VERSION")

	cfg := &config.Config{Phases: []config.Phase{{Name: "p", Prompt: ".orc/prompts/p.md"}}}
	fixture := &Fixture{Ref: ref, Ticket: "T-1", Spec: "spec.md"}

	wt, err := CreateWorktree(context.Background(), repo, ref, "bug-fix")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer RemoveWorktree(repo, wt)

	specAbs, err := BuildStage(context.Background(), repo, wt,
		filepath.Join(repo, ".orc", "config.yaml"), cfg, fixture, "bug-fix")
	if err != nil {
		t.Fatalf("BuildStage: %v", err)
	}

	// (1) tree is clean
	if s := gitStatusPorcelain(t, wt); s != "" {
		t.Errorf("worktree not clean after BuildStage:\n%s", s)
	}
	// (2) grader/evals dir is gone
	if _, err := os.Stat(filepath.Join(wt, ".orc", "evals")); !os.IsNotExist(err) {
		t.Errorf(".orc/evals should be absent from stage, stat err = %v", err)
	}
	// (3) spec copied to the known path; returned path matches; contents correct
	wantSpec := filepath.Join(wt, SpecStagePath)
	if specAbs != wantSpec {
		t.Errorf("specAbs = %q, want %q", specAbs, wantSpec)
	}
	data, err := os.ReadFile(specAbs)
	if err != nil || string(data) != "BUILD THE THING" {
		t.Errorf("spec contents = %q (err %v), want 'BUILD THE THING'", data, err)
	}
	// (4) live prompt present (not the ref version)
	pd, _ := os.ReadFile(filepath.Join(wt, ".orc", "prompts", "p.md"))
	if string(pd) != "LIVE VERSION" {
		t.Errorf("prompt = %q, want LIVE VERSION", pd)
	}
	// (5) artifacts/audit are gitignored: creating them keeps the tree clean
	writeFile(t, filepath.Join(wt, ".orc", "artifacts", "x"), "y")
	writeFile(t, filepath.Join(wt, ".orc", "audit", "z"), "w")
	if s := gitStatusPorcelain(t, wt); s != "" {
		t.Errorf("artifacts/audit should be gitignored, got dirty:\n%s", s)
	}
}
