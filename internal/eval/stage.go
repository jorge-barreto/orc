package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
)

// SpecStagePath is the worktree-relative path where the case spec is written.
// The workflow finds it via $ORC_SPEC_FILE (its absolute form).
const SpecStagePath = ".orc/eval-spec/spec.md"

// caseDirFor returns the eval case directory for a given case name.
// Named caseDirFor (not caseDir) to avoid colliding with the local
// variable `caseDir` inside runCase.
func caseDirFor(projectRoot, caseName string) string {
	return filepath.Join(projectRoot, ".orc", "evals", caseName)
}

// BuildStage curates an existing worktree (already checked out at fixture.Ref)
// into the agent's stage and commits it so the tree is git-clean:
//   - copies the case spec to SpecStagePath,
//   - removes .orc/evals (holds the grader out of the agent's environment),
//   - writes the live workflow + prompt files over the ref's copies,
//   - gitignores orc's runtime dirs so the run doesn't re-dirty the tree,
//   - commits everything.
//
// Returns the absolute path of the spec on the stage.
func BuildStage(ctx context.Context, projectRoot, worktreePath, configPath string, cfg *config.Config, fixture *Fixture, caseName string) (string, error) {
	// (a) copy the spec onto the stage
	specSrc := filepath.Join(caseDirFor(projectRoot, caseName), fixture.Spec)
	specDst := filepath.Join(worktreePath, SpecStagePath)
	if err := os.MkdirAll(filepath.Dir(specDst), 0o755); err != nil {
		return "", fmt.Errorf("eval: BuildStage: creating eval-spec dir: %w", err)
	}
	if err := copyFile(specSrc, specDst); err != nil {
		return "", fmt.Errorf("eval: BuildStage: copying spec: %w", err)
	}

	// (b) remove .orc/evals — the grader must not be on the agent's stage
	if err := os.RemoveAll(filepath.Join(worktreePath, ".orc", "evals")); err != nil {
		return "", fmt.Errorf("eval: BuildStage: removing .orc/evals: %w", err)
	}

	// (c) write the live workflow + prompt files over the ref's copies
	if err := copyOrcDir(projectRoot, worktreePath, configPath, cfg); err != nil {
		return "", fmt.Errorf("eval: BuildStage: writing live workflow: %w", err)
	}

	// (d) gitignore orc's runtime dirs so the run stays clean
	if err := appendGitignore(worktreePath, ".orc/artifacts/", ".orc/audit/"); err != nil {
		return "", fmt.Errorf("eval: BuildStage: writing .gitignore: %w", err)
	}

	// (e) commit the curated stage
	if err := gitCommitStage(ctx, worktreePath); err != nil {
		return "", err
	}
	return specDst, nil
}

// appendGitignore appends entries to the worktree's root .gitignore, creating it
// if absent and avoiding duplicate lines.
func appendGitignore(worktreePath string, entries ...string) error {
	path := filepath.Join(worktreePath, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, line := range splitLines(string(existing)) {
		have[line] = true
	}
	out := string(existing)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out += "\n"
	}
	for _, e := range entries {
		if !have[e] {
			out += e + "\n"
		}
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// gitCommitStage runs `git add -A && git commit` in the worktree with an
// identity that does not depend on the user's git config.
func gitCommitStage(ctx context.Context, worktreePath string) error {
	add := exec.CommandContext(ctx, "git", "add", "-A")
	add.Dir = worktreePath
	setProcAttrs(add)
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("eval: BuildStage: git add: %w\n%s", err, out)
	}
	commit := exec.CommandContext(ctx, "git",
		"-c", "user.email=orc-eval@localhost",
		"-c", "user.name=orc eval",
		"-c", "commit.gpgsign=false",
		// --no-verify: the curation commit is disposable scaffolding; worktrees
		// share .git/hooks, so a project pre-commit/commit-msg hook would
		// otherwise fire on it and could abort the eval.
		// --allow-empty: tolerate the rare "nothing to commit" case (curation
		// produced no net change) instead of failing the whole eval.
		"commit", "--no-verify", "--allow-empty", "-q", "-m", "orc eval stage",
	)
	commit.Dir = worktreePath
	setProcAttrs(commit)
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("eval: BuildStage: git commit: %w\n%s", err, out)
	}
	return nil
}

// setProcAttrs applies the standard process-group settings used across eval.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
}
