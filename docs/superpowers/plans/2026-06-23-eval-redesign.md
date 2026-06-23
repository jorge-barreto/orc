# orc eval redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `orc eval` produce a trustworthy quality signal by running the live workflow against a clean, grader-free git stage, holding the grader out of the agent's environment, and grading as a separate re-runnable step.

**Architecture:** The agent's stage becomes a `git worktree add <ref>` plus one *curation commit* (copy the case spec to a known path, `rm -rf .orc/evals/`, write live workflow/prompt files, `.gitignore` orc's runtime dirs, then commit) — so the tree is git-clean and the grader is physically absent. The grader (rubric + checks + judge prompts + fixture vars) is read from the *live* project at grade time, never from the worktree. Because grading is a pure function of `(artifacts, grader)`, a new `--regrade` path re-scores a saved run without re-running the workflow.

**Tech Stack:** Go 1.22+, stdlib + `gopkg.in/yaml.v3` + `github.com/urfave/cli/v3`. Package under test: `internal/eval/`. CLI: `cmd/orc/eval.go`.

## Global Constraints

- Go 1.22+; tabs for indentation, never spaces (a PostToolUse hook runs `gofmt -w`, but write gofmt-correct Go from the start).
- Dependencies limited to stdlib, `gopkg.in/yaml.v3`, `github.com/urfave/cli/v3`, `github.com/google/uuid`. Do not add new dependencies.
- Errors wrapped with `%w`; all error strings for this package are prefixed `eval: `.
- State files written atomically via `state.WriteFileAtomic`.
- Subprocesses use `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`, `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }`, `cmd.WaitDelay = 5 * time.Second` (match existing `eval.go`/`rubric.go`).
- Test command for one package: `go test ./internal/eval/ -count=1`. Single test: `go test ./internal/eval/ -run TestName -count=1`. Build: `make build`.
- `spec:` is a **required** fixture field (hard migration — no fallback to "spec lives in the ref").
- The `$ORC_EVAL`/`$ORC_SPEC_FILE` contract is **opt-in QOL** for the workflow; orc neither enforces nor needs it.
- Grader path-resolution base (decided): all grader paths (`check:` relative paths, judge `prompt:`) resolve from the **live project root**; grader subprocesses run with `cmd.Dir = projectRoot`.
- Spec stage path (decided): `.orc/eval-spec/spec.md` inside the worktree; `$ORC_SPEC_FILE` is its absolute path.
- Re-grade history (decided): **append** a new row (never replace); mark it with `regraded_from: <run-id>`.

---

## File Structure

- `internal/eval/eval.go` — Fixture struct (+ `Spec` field), loaders, fingerprints (workflow + rubric), worktree/stage build, `RunWorkflow`, history, `RunEval`, new `RegradeEval`.
- `internal/eval/stage.go` *(new)* — stage construction: `BuildStage` (worktree + curation commit). Extracts the stage-building responsibility out of the `copyOrcDir`/`CreateWorktree` pair so it has one clear home and is unit-testable.
- `internal/eval/rubric.go` — `EvaluateRubric` (+ fixture vars in env, live-project cwd), `ComputeScore`, expect helpers. Signature of `EvaluateRubric` changes to accept fixture vars and resolve from project root.
- `internal/eval/render.go` — report/JSON rendering; history report gains the rubric-fingerprint column and a re-grade marker.
- `cmd/orc/eval.go` — new `--regrade` flag and wiring.
- `internal/eval/eval_test.go`, `internal/eval/stage_test.go` *(new)*, `internal/eval/rubric_test.go` — tests.
- `internal/docs/content.go` — eval doc section updates (spec field, $ORC_SPEC_FILE contract, --regrade, two fingerprints).

---

## Task 1: Add required `spec:` field to Fixture

**Files:**
- Modify: `internal/eval/eval.go` (Fixture struct ~line 28; LoadFixture ~line 66)
- Test: `internal/eval/eval_test.go`

**Interfaces:**
- Produces: `Fixture.Spec string` (yaml `spec`); `LoadFixture` errors `eval: fixture.yaml: spec is required` when empty and `eval: fixture.yaml: spec %q must not contain path separators` when not a base name.

- [ ] **Step 1: Write the failing tests**

Add to `internal/eval/eval_test.go`:

```go
func TestLoadFixture_Spec(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"),
		"ref: abc123\nticket: T-001\nspec: spec.md\n")
	f, err := LoadFixture(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Spec != "spec.md" {
		t.Errorf("Spec = %q, want spec.md", f.Spec)
	}
}

func TestLoadFixture_MissingSpec(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"), "ref: abc123\nticket: T-001\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for missing spec")
	}
	if !strings.Contains(err.Error(), "spec is required") {
		t.Errorf("error = %v, want message containing 'spec is required'", err)
	}
}

func TestLoadFixture_SpecPathSeparator(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fixture.yaml"),
		"ref: abc123\nticket: T-001\nspec: \"sub/spec.md\"\n")
	_, err := LoadFixture(dir)
	if err == nil {
		t.Fatal("expected error for spec with path separator")
	}
	if !strings.Contains(err.Error(), "must not contain path separators") {
		t.Errorf("error = %v, want message containing 'must not contain path separators'", err)
	}
}
```

Also update the *existing* tests that build a fixture without `spec:` so they keep passing. Add `spec: spec.md` to the fixture YAML in: `TestLoadFixture`, `TestValidateRef` (both valid and invalid blocks), and `TestRenderCaseList` (both cases). For `TestLoadFixture_MissingRef`/`MissingTicket`/`InvalidRef`/`DotDotTicket`/`DotTicket`/`PathSeparatorTicket`/`StrictYAML`/`VarShadows*` leave them as-is (they assert a *different* earlier error, or assert on ref/ticket which are validated before spec — keep spec-validation ordered AFTER ticket validation, see Step 3).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/eval/ -run TestLoadFixture_Spec -count=1`
Expected: FAIL — `f.Spec` undefined (compile error) then assertion failures.

- [ ] **Step 3: Implement**

In `internal/eval/eval.go`, add to the `Fixture` struct:

```go
type Fixture struct {
	Ref         string            `yaml:"ref"`
	Ticket      string            `yaml:"ticket"`
	Spec        string            `yaml:"spec"`
	Vars        map[string]string `yaml:"vars"`
	Description string            `yaml:"description"`
}
```

In `LoadFixture`, AFTER the existing ticket path-separator check (after `eval.go:89`) and BEFORE the ref regex check, add:

```go
	if f.Spec == "" {
		return nil, fmt.Errorf("eval: fixture.yaml: spec is required")
	}
	if f.Spec != filepath.Base(f.Spec) {
		return nil, fmt.Errorf("eval: fixture.yaml: spec %q must not contain path separators", f.Spec)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/eval/ -count=1`
Expected: PASS (all eval tests, including the updated existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/eval/eval.go internal/eval/eval_test.go
git commit -m "eval: add required spec field to fixture"
```

---

## Task 2: Stage builder — worktree + curation commit

**Files:**
- Create: `internal/eval/stage.go`
- Create: `internal/eval/stage_test.go`
- Modify: `internal/eval/eval.go` (these tests require `git`; the new code reuses `CreateWorktree`/`RemoveWorktree`)

**Interfaces:**
- Consumes: `CreateWorktree(ctx, projectRoot, ref, caseName) (string, error)` and `RemoveWorktree(projectRoot, worktreePath) error` (existing, `eval.go`); `Fixture` (Task 1); `*config.Config`.
- Produces: `BuildStage(ctx, projectRoot, worktreePath, configPath string, cfg *config.Config, fixture *Fixture, caseName string) (specPathAbs string, err error)`. It (a) copies `<caseDir>/<spec>` to `<worktree>/.orc/eval-spec/spec.md`, (b) removes `<worktree>/.orc/evals`, (c) writes live workflow + prompt files (reusing `copyOrcDir`), (d) appends `.orc/artifacts/` and `.orc/audit/` to `<worktree>/.gitignore`, (e) `git add -A && git commit`. Returns the absolute path of the spec on the stage. Also produces the constant `SpecStagePath = ".orc/eval-spec/spec.md"`.
- Produces: `caseDirFor(projectRoot, caseName string) string` helper. (Named `caseDirFor`, not `caseDir`, to avoid colliding with the existing local variable `caseDir` inside `runCase`.)

- [ ] **Step 1: Write the failing tests**

Create `internal/eval/stage_test.go`:

```go
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
		filepath.Join(repo, ".orc", "config.yaml"), cfg, fixture)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/eval/ -run TestBuildStage -count=1`
Expected: FAIL — `BuildStage` and `SpecStagePath` undefined.

- [ ] **Step 3: Implement**

Create `internal/eval/stage.go`:

```go
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
		"commit", "-q", "-m", "orc eval stage",
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

var _ = config.Config{} // keep config import if cfg type referenced only in signature
```

Note: the test in Step 1 already calls `BuildStage(context.Background(), repo, wt, filepath.Join(repo, ".orc", "config.yaml"), cfg, fixture)` — that is 6 args, but the signature has 7 (trailing `caseName`). Before running, append `"bug-fix"` as the final argument to that call so it reads `BuildStage(..., cfg, fixture, "bug-fix")`.

Delete the `var _ = config.Config{}` line shown above — `config` is already referenced by the `cfg *config.Config` parameter, so the import is used; the line is only an explanatory guard, not real code.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/eval/ -run TestBuildStage -count=1`
Expected: PASS.

- [ ] **Step 5: Run the full package and commit**

Run: `go test ./internal/eval/ -count=1`
Expected: PASS.

```bash
git add internal/eval/stage.go internal/eval/stage_test.go
git commit -m "eval: add BuildStage — clean, grader-free curation commit"
```

---

## Task 3: Wire fixture vars + live-project cwd into rubric grading

**Files:**
- Modify: `internal/eval/rubric.go` (`EvaluateRubric` ~line 65; `filteredEnv` ~line 43)
- Modify: `internal/eval/eval.go` (the `EvaluateRubric` call in `runCase` ~line 489)
- Test: `internal/eval/rubric_test.go`

**Interfaces:**
- Consumes: `dispatch.ExpandConfigVars` is NOT used here (fixture vars are a plain map already); instead expand var-in-var with `dispatch.ExpandVars`.
- Produces: new signature `EvaluateRubric(ctx context.Context, rubric *Rubric, artifactsDir, projectRoot string, vars map[string]string) ([]CriterionResult, error)`. The `worktreePath` parameter is **removed** — grader subprocesses now run with `cmd.Dir = projectRoot` and resolve paths from the live project. Fixture `vars` are expanded (var-in-var) and injected into both `check:` and judge subprocess envs as bare `KEY` and `ORC_KEY`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/eval/rubric_test.go` (create the file's package header if the file does not yet exist — it does):

```go
func TestEvaluateRubric_CheckSeesVars(t *testing.T) {
	projectRoot := t.TempDir()
	artifactsDir := t.TempDir()
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "uses-var", Check: `test "$MYVAR" = "hello"`, Expect: "exit 0", Weight: 1},
	}}
	vars := map[string]string{"MYVAR": "hello"}

	results, err := EvaluateRubric(context.Background(), rubric, artifactsDir, projectRoot, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || !results[0].Pass {
		t.Errorf("check should see MYVAR=hello and pass; got %+v", results)
	}
}

func TestEvaluateRubric_CheckSeesORCPrefixedVar(t *testing.T) {
	projectRoot := t.TempDir()
	artifactsDir := t.TempDir()
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "orc-prefixed", Check: `test "$ORC_MYVAR" = "hi"`, Expect: "exit 0", Weight: 1},
	}}
	results, err := EvaluateRubric(context.Background(), rubric, artifactsDir, projectRoot,
		map[string]string{"MYVAR": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Pass {
		t.Errorf("check should see ORC_MYVAR=hi; got %+v", results)
	}
}

func TestEvaluateRubric_VarInVarExpansion(t *testing.T) {
	projectRoot := t.TempDir()
	artifactsDir := t.TempDir()
	// SECOND references FIRST; it must arrive expanded.
	vars := expandFixtureVars(map[string]string{
		"FIRST":  "/base",
		"SECOND": "$FIRST/leaf",
	}, []string{"FIRST", "SECOND"})
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "expanded", Check: `test "$SECOND" = "/base/leaf"`, Expect: "exit 0", Weight: 1},
	}}
	results, err := EvaluateRubric(context.Background(), rubric, artifactsDir, projectRoot, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Pass {
		t.Errorf("SECOND should expand to /base/leaf; got %+v", results)
	}
}

func TestEvaluateRubric_CheckRunsFromProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	artifactsDir := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "marker.txt"), "x")
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "cwd", Check: "test -f marker.txt", Expect: "exit 0", Weight: 1},
	}}
	results, err := EvaluateRubric(context.Background(), rubric, artifactsDir, projectRoot, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Pass {
		t.Errorf("check should run with cwd=projectRoot; got %+v", results)
	}
}
```

Note `expandFixtureVars(vars map[string]string, order []string) map[string]string` is a new helper defined in Step 3. Because Go maps have no order and fixture YAML var ordering isn't preserved by `map[string]string`, the helper takes an explicit key order. For the test we pass the order directly; for production, see the caveat in Step 3.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/eval/ -run TestEvaluateRubric -count=1`
Expected: FAIL — new `EvaluateRubric` signature / `expandFixtureVars` undefined.

- [ ] **Step 3: Implement**

In `internal/eval/rubric.go`, change `EvaluateRubric`'s signature and the two subprocess env constructions. Replace the function header and both `cmd.Dir`/`cmd.Env` lines:

```go
// EvaluateRubric runs all criteria against the run's artifacts. Grader
// subprocesses run with cwd = projectRoot and receive the fixture vars
// (as KEY and ORC_KEY); paths resolve from the live project.
func EvaluateRubric(ctx context.Context, rubric *Rubric, artifactsDir, projectRoot string, vars map[string]string) ([]CriterionResult, error) {
	varEnv := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		varEnv = append(varEnv, k+"="+v, "ORC_"+k+"="+v)
	}
	baseExtras := append([]string{
		"ARTIFACTS_DIR=" + artifactsDir,
		"WORK_DIR=" + projectRoot,
		"PROJECT_ROOT=" + projectRoot,
	}, varEnv...)
	...
}
```

Then in the `c.Check != ""` branch, replace:

```go
			cmd.Dir = worktreePath
			cmd.Env = filteredEnv("ARTIFACTS_DIR="+artifactsDir, "WORK_DIR="+worktreePath, "PROJECT_ROOT="+projectRoot)
```

with:

```go
			cmd.Dir = projectRoot
			cmd.Env = filteredEnv(baseExtras...)
```

And in the `c.Judge` branch, replace the same two lines (currently using `worktreePath`) with:

```go
			cmd.Dir = projectRoot
			cmd.Env = filteredEnv(baseExtras...)
```

Add the `expandFixtureVars` helper to `internal/eval/rubric.go`:

```go
// expandFixtureVars expands var-in-var references in declaration order, so a
// later var like "$FIRST/leaf" resolves against earlier vars. Order is the
// fixture's var key order.
func expandFixtureVars(vars map[string]string, order []string) map[string]string {
	result := make(map[string]string, len(vars))
	lookup := make(map[string]string, len(vars))
	for _, k := range order {
		v, ok := vars[k]
		if !ok {
			continue
		}
		expanded := dispatch.ExpandVars(v, lookup)
		result[k] = expanded
		lookup[k] = expanded
	}
	// Include any keys not present in order (defensive), unexpanded.
	for k, v := range vars {
		if _, done := result[k]; !done {
			result[k] = v
		}
	}
	return result
}
```

`dispatch` is already imported in `rubric.go`. Remove the now-unused `overridden` entries referencing `WORK_DIR`/`PROJECT_ROOT`? No — leave `filteredEnv` as is; it correctly strips `ORC_*` and the overridable keys so our explicit `baseExtras` (which include `ORC_*` fixture vars) win by being appended last.

**Caveat on var order (production):** `Fixture.Vars` is a `map[string]string`, which loses YAML order, so `expandFixtureVars` cannot know declaration order from it alone. Task 3 establishes the *mechanism and signature*; Task 4 supplies the order. For now, production callers (Task 4) will pass `sortedKeys(vars)` until/unless fixture vars are changed to an ordered type. Document this limitation in the doc task (Task 7): "var-in-var expansion in fixture vars resolves in alphabetical key order." Add helper:

```go
import "sort"

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

(If `sort` is already imported in the file, don't re-import.)

- [ ] **Step 4: Update the caller in eval.go so the package compiles**

In `internal/eval/eval.go` `runCase` (~line 489), replace:

```go
	criterionResults, rubricErr := EvaluateRubric(ctx, rubric, runResult.ArtifactsDir, worktreePath, e.projectRoot)
```

with:

```go
	expandedVars := expandFixtureVars(fixture.Vars, sortedKeys(fixture.Vars))
	criterionResults, rubricErr := EvaluateRubric(ctx, rubric, runResult.ArtifactsDir, e.projectRoot, expandedVars)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/eval/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/eval/rubric.go internal/eval/eval.go internal/eval/rubric_test.go
git commit -m "eval: grader checks see fixture vars; resolve from project root"
```

---

## Task 4: Use BuildStage in runCase and set the eval-mode contract

**Files:**
- Modify: `internal/eval/eval.go` (`runCase` ~line 466; `RunWorkflow` ~line 334)
- Test: `internal/eval/eval_test.go`

**Interfaces:**
- Consumes: `BuildStage` (Task 2), `EvaluateRubric` new signature (Task 3).
- Produces: `RunWorkflow` gains a `specFile string` parameter; it sets `ORC_EVAL=1` and `ORC_SPEC_FILE=<specFile>` in the child env (in addition to fixture vars). New signature: `RunWorkflow(ctx, worktreePath, ticket, workflowName, specFile string, vars map[string]string) (*RunResult, error)`. `runCase` calls `CreateWorktree` then `BuildStage` (replacing the bare `copyOrcDir` call) and threads the returned spec path into `RunWorkflow`.

- [ ] **Step 1: Write the failing test**

Add to `internal/eval/eval_test.go`:

```go
func TestRunWorkflow_SetsEvalEnv(t *testing.T) {
	// We can't run a real `orc run` here, but we can assert the env-building
	// helper sets ORC_EVAL and ORC_SPEC_FILE. Extracted as buildEvalEnv.
	env := buildEvalEnv("/stage/.orc/eval-spec/spec.md", map[string]string{"K": "v"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"ORC_EVAL=1",
		"ORC_SPEC_FILE=/stage/.orc/eval-spec/spec.md",
		"K=v",
		"ORC_K=v",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eval/ -run TestRunWorkflow_SetsEvalEnv -count=1`
Expected: FAIL — `buildEvalEnv` undefined.

- [ ] **Step 3: Implement**

In `internal/eval/eval.go`, extract the env construction in `RunWorkflow` into a helper and add the eval-mode vars. Replace the inline env-building block (lines ~349–366) with a call to `buildEvalEnv`, and add the helper:

```go
// buildEvalEnv returns the child env for an eval run: inherited env minus
// CLAUDECODE/ORC_*, plus fixture vars (KEY and ORC_KEY), plus the eval-mode
// contract (ORC_EVAL, ORC_SPEC_FILE).
func buildEvalEnv(specFile string, vars map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(vars)*2+2)
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		if strings.HasPrefix(key, "CLAUDECODE") || strings.HasPrefix(key, "ORC_") {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range vars {
		env = append(env, k+"="+v, "ORC_"+k+"="+v)
	}
	env = append(env, "ORC_EVAL=1", "ORC_SPEC_FILE="+specFile)
	return env
}
```

Change `RunWorkflow`'s signature to accept `specFile`:

```go
func RunWorkflow(ctx context.Context, worktreePath, ticket, workflowName, specFile string, vars map[string]string) (*RunResult, error) {
```

and replace the env block with:

```go
	cmd.Env = buildEvalEnv(specFile, vars)
```

In `runCase`, replace the `copyOrcDir` + `RunWorkflow` calls:

```go
	specFile, err := BuildStage(ctx, e.projectRoot, worktreePath, e.configPath, e.cfg, fixture, caseName)
	if err != nil {
		return CaseResult{Name: caseName}, fmt.Errorf("eval: building stage for %q: %w", caseName, err)
	}

	runResult, _ := RunWorkflow(ctx, worktreePath, fixture.Ticket, e.workflowName, specFile, fixture.Vars)
```

(Delete the old `copyOrcDir(...)` block in `runCase`. Keep `copyOrcDir` itself — `BuildStage` calls it.)

Update the existing `TestRunWorkflow_FallbackToHistory` if it calls `RunWorkflow` directly — it does NOT (it only exercises state helpers), so no change needed there. Search for any other `RunWorkflow(` callers: only `runCase`. Good.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/eval/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/eval/eval.go internal/eval/eval_test.go
git commit -m "eval: runCase uses BuildStage; set ORC_EVAL/ORC_SPEC_FILE contract"
```

---

## Task 5: Two fingerprints — add rubric fingerprint to history

**Files:**
- Modify: `internal/eval/eval.go` (`HistoryEntry` ~line 312, `ConfigFingerprint` ~line 167, `AppendResult` ~line 441, `runCase`/`RunEval`)
- Modify: `internal/eval/render.go` (`RenderHistoryReport` ~line 66)
- Test: `internal/eval/eval_test.go`

**Interfaces:**
- Produces: `RubricFingerprint(rubric *Rubric, caseDir, projectRoot string) (string, error)` — hashes `rubric.yaml` content plus the contents of each judge `prompt:` file (resolved from projectRoot) plus each `check:` string, returning 8 hex chars. `HistoryEntry` gains `RubricFingerprints map[string]string json:"rubric_fingerprints"` (per-case, since each case has its own rubric). `CaseHistoryEntry` gains `RegradedFrom string json:"regraded_from,omitempty"`. `AppendResult` signature unchanged but reads a new `CaseResult.RubricFingerprint string` and `CaseResult.RegradedFrom string` field.

- [ ] **Step 1: Write the failing tests**

Add to `internal/eval/eval_test.go`:

```go
func TestRubricFingerprint_ChangesWithRubric(t *testing.T) {
	caseDir := t.TempDir()
	projectRoot := t.TempDir()
	r1 := &Rubric{Criteria: []Criterion{{Name: "a", Check: "exit 0", Weight: 1}}}
	fp1, err := RubricFingerprint(r1, caseDir, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	r2 := &Rubric{Criteria: []Criterion{{Name: "a", Check: "exit 1", Weight: 1}}}
	fp2, err := RubricFingerprint(r2, caseDir, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp2 {
		t.Error("rubric fingerprint should change when a check changes")
	}
}

func TestRubricFingerprint_ChangesWithJudgePrompt(t *testing.T) {
	caseDir := t.TempDir()
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, ".orc", "p.md"), "v1")
	r := &Rubric{Criteria: []Criterion{{Name: "q", Judge: true, Prompt: ".orc/p.md", Weight: 1}}}
	fp1, err := RubricFingerprint(r, caseDir, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(projectRoot, ".orc", "p.md"), "v2")
	fp2, err := RubricFingerprint(r, caseDir, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp2 {
		t.Error("rubric fingerprint should change when judge prompt content changes")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/eval/ -run TestRubricFingerprint -count=1`
Expected: FAIL — `RubricFingerprint` undefined.

- [ ] **Step 3: Implement**

Add to `internal/eval/eval.go`:

```go
// RubricFingerprint hashes the rubric's grading inputs: each check string and
// the contents of each judge prompt file (resolved from projectRoot). Returns
// 8 hex chars, stable and order-independent across criteria.
func RubricFingerprint(rubric *Rubric, caseDir, projectRoot string) (string, error) {
	h := sha256.New()
	// Sort criteria by name for determinism.
	names := make([]string, len(rubric.Criteria))
	byName := make(map[string]Criterion, len(rubric.Criteria))
	for i, c := range rubric.Criteria {
		names[i] = c.Name
		byName[c.Name] = c
	}
	sort.Strings(names)
	for _, name := range names {
		c := byName[name]
		fmt.Fprintf(h, "name:%s\nweight:%g\nexpect:%s\ncheck:%s\njudge:%t\n",
			c.Name, c.Weight, c.Expect, c.Check, c.Judge)
		if c.Judge && c.Prompt != "" {
			data, err := os.ReadFile(filepath.Join(projectRoot, c.Prompt))
			if err != nil {
				return "", fmt.Errorf("eval: rubric fingerprint: reading %s: %w", c.Prompt, err)
			}
			fmt.Fprintf(h, "prompt:%s:", c.Prompt)
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}
```

Add fields. To `CaseResult` (in `rubric.go`):

```go
	RubricFingerprint string `json:"rubric_fingerprint,omitempty"`
	RegradedFrom      string `json:"regraded_from,omitempty"`
```

To `HistoryEntry` (in `eval.go`), add a per-case rubric fingerprint map:

```go
	RubricFingerprints map[string]string `json:"rubric_fingerprints,omitempty"`
```

To `CaseHistoryEntry`:

```go
	RegradedFrom string `json:"regraded_from,omitempty"`
```

In `AppendResult`, populate them:

```go
func (h *History) AppendResult(fingerprint string, cases []CaseResult) {
	entry := HistoryEntry{
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		ConfigFingerprint:  fingerprint,
		Cases:              make(map[string]CaseHistoryEntry),
		RubricFingerprints: make(map[string]string),
	}
	for _, c := range cases {
		entry.Cases[c.Name] = CaseHistoryEntry{
			Score:           c.Score,
			CostUSD:         c.CostUSD,
			DurationSeconds: c.DurationSeconds,
			Details:         c.Details,
			RegradedFrom:    c.RegradedFrom,
		}
		if c.RubricFingerprint != "" {
			entry.RubricFingerprints[c.Name] = c.RubricFingerprint
		}
	}
	h.Runs = append(h.Runs, entry)
}
```

In `runCase`, compute and set the rubric fingerprint on the result:

```go
	rubricFP, _ := RubricFingerprint(rubric, caseDir, e.projectRoot)
	result.RubricFingerprint = rubricFP
```

(Add this right before `return result, nil`. `caseDir` is the local var already computed at the top of `runCase`.)

In `render.go` `RenderHistoryReport`, add a RUBRIC column. Change the header and row format:

```go
	fmt.Fprintf(w, "  %s%-12s %-10s %-13s %-10s %-11s %s%s\n",
		ux.Dim, "FINGERPRINT", "RUBRIC", "DATE", "AVG SCORE", "TOTAL COST", "TOTAL TIME", ux.Reset)
```

and in the loop, derive a single rubric fingerprint string for the row (join distinct per-case values, or show the lone value):

```go
		rubricFP := summarizeRubricFPs(entry.RubricFingerprints)
		...
		fmt.Fprintf(w, "  %-12s %-10s %-13s %d/100      $%-10.2f %s\n",
			entry.ConfigFingerprint, rubricFP, dateStr,
			int(math.Round(avgScore)), totalCost, state.FormatDuration(totalDuration))
```

Add the helper to `render.go`:

```go
// summarizeRubricFPs renders the rubric fingerprints for a history row: the
// single value if all cases share it, "-" if none, else "mixed".
func summarizeRubricFPs(m map[string]string) string {
	if len(m) == 0 {
		return "-"
	}
	var first string
	for _, v := range m {
		if first == "" {
			first = v
		} else if v != first {
			return "mixed"
		}
	}
	return first
}
```

- [ ] **Step 4: Update existing history/report tests**

`TestRenderHistoryReport` asserts presence of `abc123`, `85/100`, `Jan 15` — still present. Add `"RUBRIC"` to its want-list to lock the new column header:

```go
	for _, want := range []string{"abc123", "85/100", "Jan 15", "RUBRIC"} {
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/eval/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/eval/eval.go internal/eval/rubric.go internal/eval/render.go internal/eval/eval_test.go
git commit -m "eval: track rubric fingerprint per case in history + report"
```

---

## Task 6: `--regrade` — re-score a saved run without re-running

**Files:**
- Modify: `internal/eval/eval.go` (add `RegradeEval`, a `regradeCase` helper on `evalRunner` or standalone)
- Modify: `cmd/orc/eval.go` (add `--regrade` flag + wiring)
- Test: `internal/eval/eval_test.go`

**Interfaces:**
- Consumes: `state.ListHistory(artifactsDir) ([]state.HistoryEntry, error)`, `state.LatestHistoryDir`, `state.ArtifactsDirForWorkflow`, `EvaluateRubric` (Task 3), `RubricFingerprint` (Task 5), `ComputeScore`, `LoadFixture`, `LoadRubric`.
- Produces: `RegradeEval(ctx context.Context, projectRoot, configPath, workflowName string, cfg *config.Config, caseName, runID string) (string, []CaseResult, error)`. Loads the saved run's artifacts dir (named `runID`, or the latest if `runID==""`), runs only the grade step against the *current* rubric, sets `CaseResult.RegradedFrom = <runID>`, appends a history row, returns results. `caseName` is required for regrade (can't regrade "all").

- [ ] **Step 1: Write the failing test**

Add to `internal/eval/eval_test.go`:

```go
func TestResolveRunArtifactsDir(t *testing.T) {
	// Build an artifacts tree with two history runs; resolveRunArtifactsDir
	// returns the named one, and the latest when runID is empty.
	base := t.TempDir()
	mk := func(runID string) {
		d := filepath.Join(base, "history", runID)
		writeFile(t, filepath.Join(d, "state.json"),
			`{"ticket":"T-1","status":"completed","phases":{}}`)
	}
	mk("2026-01-01T10-00-00.000")
	mk("2026-01-02T10-00-00.000")

	got, err := resolveRunArtifactsDir(base, "2026-01-01T10-00-00.000")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "2026-01-01T10-00-00.000" {
		t.Errorf("named run = %q, want the 01-01 dir", got)
	}

	latest, err := resolveRunArtifactsDir(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(latest) != "2026-01-02T10-00-00.000" {
		t.Errorf("latest run = %q, want the 01-02 dir", latest)
	}

	_, err = resolveRunArtifactsDir(base, "nonexistent-run")
	if err == nil {
		t.Fatal("expected error for nonexistent run id")
	}
	if !strings.Contains(err.Error(), "run not found") {
		t.Errorf("error = %v, want 'run not found'", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eval/ -run TestResolveRunArtifactsDir -count=1`
Expected: FAIL — `resolveRunArtifactsDir` undefined.

- [ ] **Step 3: Implement the resolver and RegradeEval**

Add to `internal/eval/eval.go`:

```go
// resolveRunArtifactsDir returns the artifacts dir for a saved run. If runID is
// empty, returns the most recent history entry. Errors if the named run or any
// history is absent.
func resolveRunArtifactsDir(artifactsDir, runID string) (string, error) {
	if runID == "" {
		latest, err := state.LatestHistoryDir(artifactsDir)
		if err != nil {
			return "", fmt.Errorf("eval: locating latest run: %w", err)
		}
		if latest == "" {
			return "", fmt.Errorf("eval: no saved runs found under %s", state.HistoryDir(artifactsDir))
		}
		return latest, nil
	}
	dir := filepath.Join(state.HistoryDir(artifactsDir), runID)
	if !state.HasState(dir) {
		return "", fmt.Errorf("eval: run not found: %q under %s", runID, state.HistoryDir(artifactsDir))
	}
	return dir, nil
}

// RegradeEval re-scores a saved run's artifacts against the current rubric,
// without re-running the workflow. Appends a history row marked RegradedFrom.
func RegradeEval(ctx context.Context, projectRoot, configPath, workflowName string, cfg *config.Config, caseName, runID string) (string, []CaseResult, error) {
	if caseName == "" {
		return "", nil, fmt.Errorf("eval: --regrade requires a case name")
	}
	cdir := caseDirFor(projectRoot, caseName)
	fixture, err := LoadFixture(cdir)
	if err != nil {
		return "", nil, fmt.Errorf("eval: loading fixture for %q: %w", caseName, err)
	}
	rubric, err := LoadRubric(cdir, projectRoot)
	if err != nil {
		return "", nil, fmt.Errorf("eval: loading rubric for %q: %w", caseName, err)
	}

	baseArtifacts := state.ArtifactsDirForWorkflow(projectRoot, workflowName, fixture.Ticket)
	runDir, err := resolveRunArtifactsDir(baseArtifacts, runID)
	if err != nil {
		return "", nil, err
	}

	expandedVars := expandFixtureVars(fixture.Vars, sortedKeys(fixture.Vars))
	criterionResults, rubricErr := EvaluateRubric(ctx, rubric, runDir, projectRoot, expandedVars)
	if rubricErr != nil {
		fmt.Fprintf(os.Stderr, "  warning: rubric evaluation error for %q: %v\n", caseName, rubricErr)
	}
	score := ComputeScore(criterionResults, rubric)

	details := make(map[string]float64)
	var failures []string
	passCount := 0
	for _, cr := range criterionResults {
		details[cr.Name] = cr.Score
		if cr.Pass {
			passCount++
		} else {
			failures = append(failures, cr.Name+": "+cr.Detail)
		}
	}
	rubricFP, _ := RubricFingerprint(rubric, cdir, projectRoot)

	result := CaseResult{
		Name:              caseName,
		Score:             score,
		PassCount:         passCount,
		TotalCount:        len(criterionResults),
		Failures:          failures,
		Details:           details,
		RubricFingerprint: rubricFP,
		RegradedFrom:      filepath.Base(runDir),
	}

	fingerprint, err := ConfigFingerprint(configPath, cfg, projectRoot)
	if err != nil {
		return "", nil, fmt.Errorf("eval: computing fingerprint: %w", err)
	}
	results := []CaseResult{result}

	history, err := LoadHistory(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: cannot load eval history, skipping save: %v\n", err)
	} else {
		history.AppendResult(fingerprint, results)
		if err := SaveHistory(projectRoot, history); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to save eval history: %v\n", err)
		}
	}
	return fingerprint, results, nil
}
```

- [ ] **Step 4: Wire the CLI flag**

In `cmd/orc/eval.go`, add to `Flags`:

```go
			&cli.StringFlag{Name: "regrade", Usage: "Re-grade a saved run against the current rubric without re-running (optionally pass a run id)"},
```

After config load and `caseName` validation (after `eval.go` line ~86), branch before `RunEval`:

```go
			if cmd.IsSet("regrade") {
				if caseName == "" {
					return cfgErr(fmt.Errorf("--regrade requires a case name: orc eval <case> --regrade [run-id]"))
				}
				runID := cmd.String("regrade")
				fingerprint, cases, err := eval.RegradeEval(ctx, projectRoot, configPath, workflowName, cfg, caseName, runID)
				if err != nil {
					return fmt.Errorf("re-grading eval: %w", err)
				}
				if cmd.Bool("json") {
					return eval.RenderJSON(os.Stdout, fingerprint, cases)
				}
				eval.RenderScoreReport(os.Stdout, fingerprint, cases)
				return nil
			}
```

Note: `--regrade` takes an *optional* value. With urfave/cli a `StringFlag` whose value is empty when passed as a bare `--regrade` works via `cmd.IsSet("regrade")` true + `cmd.String("regrade")==""`. Document usage as `orc eval <case> --regrade` (latest) or `orc eval <case> --regrade <run-id>`.

- [ ] **Step 5: Run tests + build to verify**

Run: `go test ./internal/eval/ -count=1 && make build`
Expected: PASS and a successful build.

- [ ] **Step 6: Commit**

```bash
git add internal/eval/eval.go cmd/orc/eval.go internal/eval/eval_test.go
git commit -m "eval: add --regrade to re-score a saved run without re-running"
```

---

## Task 7: Update documentation

**Files:**
- Modify: `internal/docs/content.go` (eval topic ~lines 1570–1693)

**Interfaces:**
- Consumes: nothing (doc strings only).
- Produces: doc reflects `spec:` required field, `$ORC_EVAL`/`$ORC_SPEC_FILE` contract, the clean-stage/held-out-grader model, `--regrade`, two fingerprints, var-in-var alphabetical-order caveat, grader paths resolving from project root.

- [ ] **Step 1: Update the eval doc string**

In `internal/docs/content.go`, in the eval topic:

1. Under "Eval Case Structure", add the spec file and note the holdout model:

```
  .orc/evals/
  └── bug-fix/
      ├── fixture.yaml     Git ref + ticket + spec to replay
      ├── spec.md          Agent-visible input (the "ticket body")
      └── rubric.yaml      Scoring criteria (HELD OUT from the agent)

The whole .orc/evals/ directory is removed from the agent's worktree before the
workflow runs, so the grader (rubric, judge prompts, held-out tests) is never
visible to the workflow under test. orc reads the grader from your live project
at grade time only.
```

2. In the `fixture.yaml` fields list, add:

```
  spec       Required. Filename (in the case dir) of the agent-visible spec —
             the stand-in for a real ticket body. orc copies it into the stage
             at .orc/eval-spec/spec.md and exposes its absolute path as
             $ORC_SPEC_FILE (with $ORC_EVAL=1) so the workflow can read it
             instead of fetching from an external store. This branch is an
             optional convenience; a self-contained workflow needs no change.
```

3. Add a new section after `rubric.yaml`:

```
Eval Mode Contract
------------------

When a workflow runs under orc eval, orc sets two env vars:

  ORC_EVAL=1                       Signals eval mode.
  ORC_SPEC_FILE=<abs path>         Path to the case spec on the stage.

A workflow whose ticket-fetch phase normally calls an external store (Jira,
etc.) can branch on these:

  if [ -n "$ORC_EVAL" ]; then
    cat "$ORC_SPEC_FILE"
  else
    jira-cli get "$TICKET"
  fi

This is opt-in: orc neither enforces nor requires it. The agent's worktree is a
clean git checkout of the fixture ref plus one curation commit (spec copied in,
.orc/evals removed, live workflow written, .orc/artifacts and .orc/audit
gitignored), so clean-tree guards in your workflow pass.

Grader checks and judge prompts run from your live project root, receive the
fixture vars (as KEY and ORC_KEY), and resolve relative paths from the project
root. Fixture var-in-var references (e.g. SECOND: $FIRST/leaf) expand in
alphabetical key order.
```

4. Update the command list (top) and "Typical Workflow" to mention `--regrade`:

```
  orc eval <case> --regrade            Re-grade the latest run of <case>
  orc eval <case> --regrade <run-id>   Re-grade a specific saved run
```

Add to "Typical Workflow for Prompt Iteration" a second loop:

```
Iterating on the rubric (no workflow re-run):

1. Run the case once:        orc eval bug-fix
2. Edit rubric.yaml
3. Re-grade the saved run:    orc eval bug-fix --regrade
4. Repeat 2-3 — seconds each, no agent turns or build cost.
```

5. Update the "Config Fingerprint" section to mention the rubric fingerprint:

```
History rows also record a rubric fingerprint per case (rubric.yaml + judge
prompt contents + check strings). The --report RUBRIC column lets you tell a
score change caused by a workflow edit (FINGERPRINT changed) apart from one
caused by a rubric edit (RUBRIC changed) — a re-grade shows the same
FINGERPRINT with a new RUBRIC value.
```

- [ ] **Step 2: Build to verify the doc compiles**

Run: `make build`
Expected: successful build (doc strings are plain Go string literals).

- [ ] **Step 3: Commit**

```bash
git add internal/docs/content.go
git commit -m "docs: document eval held-out grader, ORC_SPEC_FILE, --regrade, two fingerprints"
```

---

## Task 8: Full suite + end-to-end smoke verification

**Files:**
- No new code; this is the integration gate.

- [ ] **Step 1: Run the entire test suite**

Run: `make test`
Expected: PASS across all packages. If `internal/eval` git-using tests fail in CI without git identity, confirm `initGitRepo` sets `user.email`/`user.name`/`commit.gpgsign=false` (it does) and that `gitCommitStage` passes `-c user.email=... -c user.name=...` (it does).

- [ ] **Step 2: Build and check the binary**

Run: `make build && ./orc eval --help`
Expected: help text lists `--regrade`.

- [ ] **Step 3: Manual smoke (optional, requires a project with an eval case)**

In a scratch project that already uses orc with a `.orc/evals/<case>/` containing `fixture.yaml` (with `spec:`), `spec.md`, and `rubric.yaml`:

```bash
orc eval <case>                 # full run, then grade; note the run id in --report
orc eval <case> --regrade       # re-grade latest, no workflow run
orc eval --report               # see FINGERPRINT + RUBRIC columns and the regraded row
```

Expected: the second command completes in seconds (no agent turns); `--report` shows a new row with the same FINGERPRINT and a RUBRIC value, distinguishable as a re-grade.

- [ ] **Step 4: Final commit (if any doc/test tweaks were needed)**

```bash
git add -A
git commit -m "eval: integration verification for redesign"
```

---

## Self-Review

**Spec coverage:**

- Held-out grader (#9 P1) → Task 2 (`.orc/evals` removed in curation commit) + Task 3 (grader read from live project). ✓
- Clean stage / dirty-tree (#9 P3) → Task 2 (curation commit + gitignore artifacts/audit; test asserts `git status` clean). ✓
- Checks see vars (#9 P2) → Task 3 (`baseExtras` includes fixture vars as KEY/ORC_KEY; tests). ✓
- Var-in-var (#9 P4) → Task 3 (`expandFixtureVars`; test). ✓
- Spec injection seam / Jira-without-Jira (#9 Q1) → Task 4 (`ORC_EVAL`/`ORC_SPEC_FILE`) + Task 1 (`spec:` field). ✓
- Re-grade without re-run (#8) → Task 6 (`RegradeEval`, `--regrade`). ✓
- Two fingerprints → Task 5. ✓
- Path-resolution base unified (spec Open Q3) → Task 3 (project root for both check + judge). ✓
- Spec stage path (Open Q1) → Task 2 (`SpecStagePath` constant). ✓
- Re-grade append + marker (Open Q5) → Task 5 (`RegradedFrom`) + Task 6 (sets it). ✓
- Hard migration (Open Q4) → Task 1 (`spec` required). ✓
- Adversarial reachability (Open Q6) → accepted out of scope; no task (intentional). ✓
- Docs → Task 7. ✓

**Placeholder scan:** No TBD/TODO; all code shown. The one design note — fixture var ordering is alphabetical because `Vars` is a `map` — is called out explicitly in Task 3 and documented in Task 7, not left vague. ✓

**Type consistency:**
- `EvaluateRubric(ctx, rubric, artifactsDir, projectRoot, vars)` — defined Task 3, called Task 3 (eval.go), Task 6 (RegradeEval). Consistent (5 params, no `worktreePath`). ✓
- `RunWorkflow(ctx, worktreePath, ticket, workflowName, specFile, vars)` — defined Task 4, called Task 4. Consistent. ✓
- `BuildStage(ctx, projectRoot, worktreePath, configPath, cfg, fixture, caseName)` — signature note in Task 2 reconciles the test (the test call must pass `caseName`); flagged inline. ✓
- `RubricFingerprint(rubric, caseDir, projectRoot)` — defined Task 5, called Task 5 (runCase) + Task 6. Consistent. ✓
- `CaseResult.RubricFingerprint` / `.RegradedFrom` — added Task 5, set Task 5/6, rendered Task 5. ✓
- `caseDirFor(projectRoot, caseName)` helper — defined Task 2, used Task 2 (`BuildStage`) and Task 6 (`RegradeEval`). Named `caseDirFor` (not `caseDir`) so it does not collide with the existing local variable `caseDir` inside `runCase`. Applied consistently in all task bodies. ✓
