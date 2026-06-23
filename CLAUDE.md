# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**orc** is a deterministic agent orchestrator CLI written in Go. It runs project-defined AI workflows as a state machine — phases are dispatched deterministically, with context passing through artifact files on disk rather than conversational memory. Projects define workflows in `.orc/config.yaml` with phase prompts, and the engine runs them.

## Build & Test Commands

```bash
make build          # go build -o orc ./cmd/orc/
make install        # go install ./cmd/orc/
make test           # go test ./... -count=1
make clean          # rm -f orc

# Run a single test
go test ./internal/config/ -run TestValidate_ParallelWithLoop -count=1

# Run tests for one package
go test ./internal/runner/ -count=1
```

## Architecture

Core internal packages, each with a single responsibility:

```
cmd/orc/main.go           CLI entrypoint (urfave/cli/v3)
internal/config/           Config + Phase structs, YAML loading, validation
internal/state/            State persistence (JSON), timing, artifacts dir, atomic writes
internal/dispatch/         Phase executors: script (bash), agent (claude -p), gate (human y/n); workflow/branch dispatched by runner
internal/runner/           Main state machine loop — drives the workflow
internal/eval/             Eval cases: stage builder (held-out grader), rubric grading, --regrade (see Eval Subsystem below)
internal/ux/               ANSI-colored terminal output, phase headers, status rendering
```

(Other packages — `docs`, `doctor`, `improve`, `report`, `scaffold`, `stats`, etc. — back individual subcommands.)

**Data flow:** `main.go` loads config, creates state, builds a `Runner`, and calls `runner.Run()`. The runner iterates phases, calling `dispatch.Dispatch()` for each. Dispatch routes to `RunScript`, `RunAgent`, or `RunGate` based on phase type. `workflow` and `branch` phases are handled directly by the runner via inline child `Runner.Run()` calls. State is persisted to `.orc/artifacts/` after each phase.

**Key interfaces:** `dispatch.Dispatcher` is the only interface — the runner depends on it, and tests substitute a `mockDispatcher`.

### Phase Types

- **script** — Executes a bash command via `exec.CommandContext("bash", "-c", run)`. Requires `run` field.
- **agent** — Reads a prompt template file, expands variables (`$TICKET`, `$ARTIFACTS_DIR`, `$WORK_DIR`, `$PROJECT_ROOT`), invokes `claude -p` with the rendered prompt. Requires `prompt` field pointing to an existing file.
- **gate** — Prompts human for y/n approval. Auto-skipped with `--auto`.
- **workflow** — Runs a named sub-workflow inline via a child `Runner.Run()` call. Requires `workflow` field referencing a config in `.orc/workflows/`. Child gets its own state and artifacts dir; costs merge into parent.
- **branch** — N-way dispatch: runs a `check` script, matches stdout to `branches` keys, runs the corresponding workflow. Requires `check` and `branches` fields. Optional `default` fallback.

### Runner State Machine

The runner loop handles: condition checks (skip phase if shell command exits non-zero), parallel execution (`parallel-with` runs two phases concurrently via goroutines + WaitGroup), loop backward jumps (convergent iteration with min/max and optional on-exhaust recovery, feedback written to `.orc/artifacts/feedback/`), and output validation (re-prompts agent once if declared outputs are missing).

### Config Validation Rules

Validation (`internal/config/validate.go`) enforces: unique phase names, `loop.goto` must reference an earlier phase, `parallel-with` must reference an existing phase, `parallel-with` and `loop` cannot be combined, agent phases need a `prompt` file that exists on disk, model must be `opus`/`sonnet`/`haiku`/empty, ticket patterns are anchored for full-match semantics, workflow/branch phases must reference existing workflows in `.orc/workflows/`, and cross-workflow circular references are detected via DFS at load time. The deprecated `on-fail` field is rejected with a migration hint.

### Eval Subsystem

`internal/eval/` (driven by `cmd/orc/eval.go`) measures workflow quality. Cases live in `.orc/evals/<case>/`: a `fixture.yaml` (with a **required `spec:` field** naming the agent-visible spec file), the spec file, `rubric.yaml`, and any held-out judge prompts / test scripts.

**Held-out grader.** Before the workflow runs, `internal/eval/stage.go` creates a git worktree at the fixture `ref`, then makes ONE curation commit that copies the case spec to `.orc/eval-spec/spec.md`, removes the ENTIRE `.orc/evals/` dir (so the grader is never visible to the agent), writes the LIVE workflow/prompt files, and gitignores `.orc/artifacts/` and `.orc/audit/`. The worktree is thus git-clean (clean-tree guards pass). orc reads the rubric from the LIVE project at grade time, never from the worktree.

**Eval-mode contract (opt-in).** orc sets `ORC_EVAL=1` and `ORC_SPEC_FILE=<abs path>`; a workflow whose ticket-fetch hits an external store can branch on these to read the local spec instead. orc neither requires nor enforces it — a self-contained workflow needs no change.

**Re-grading.** `orc eval <case> --regrade` re-scores a SAVED run against the CURRENT rubric without re-running the workflow. `--regrade` is a BOOLEAN flag; the optional run-id is a SECOND positional argument (`orc eval <case> --regrade <run-id>`), never `--regrade=<id>`. History tracks two fingerprints — the workflow fingerprint (config + prompts) and a per-case rubric fingerprint — so `--report` distinguishes a score change from a workflow edit vs. a rubric edit.

## Conventions

- Go 1.22+ required (for urfave/cli/v3)
- Dependencies: only `gopkg.in/yaml.v3`, `github.com/urfave/cli/v3`, and `github.com/google/uuid` beyond stdlib
- State files are written atomically (write to `.tmp`, fsync, rename)
- Child processes inherit the parent env plus `ORC_*` variables; `CLAUDECODE` is stripped so `claude -p` can run
- Variable substitution uses `os.Expand()` with a custom map + env fallback (`internal/dispatch/expand.go`)
- Errors are wrapped with `%w` for error chains
- The binary refuses to run if `CLAUDECODE` env var is set (prevents nesting inside Claude Code)
- Go uses tabs for indentation (never spaces). A PostToolUse hook runs `gofmt -w` automatically after edits, but write idiomatic Go formatting from the start.

## Releases

orc ships as a single Go binary. There are two ways to cut a release; both end
in `.github/workflows/release.yml` → GoReleaser building the linux/darwin ×
amd64/arm64 binaries + checksums + a GitHub Release, and pushing a Homebrew
formula to `jorge-barreto/homebrew-tap`.

- **Version-file flow (preferred):** the root `VERSION` file (plain semver, no
  leading `v`) is the version of record. Bump it and merge to `main`;
  `.github/workflows/auto-tag.yml` validates the bump (strict semver, strictly
  greater than the latest tag, not already tagged), creates and pushes the
  matching `v<VERSION>` tag on the merge commit, then invokes `release.yml` via
  `workflow_call`. This avoids the GITHUB_TOKEN anti-recursion rule (a tag
  pushed by `GITHUB_TOKEN` does NOT fire `release.yml`'s `push: tags` trigger).
- **Manual flow (escape hatch):** `git tag -a v0.3.0 -m "..." && git push origin
  v0.3.0`. The pushed tag fires `release.yml`'s `push: tags` trigger directly.
  Keep `VERSION` in sync when you tag manually.
- `make build` reads `VERSION` first (stamping `v<VERSION>` plus a git-derived
  suffix so dev builds are marked dirty); it falls back to `git describe` when
  the file is absent. GoReleaser still reads the version from the tag.
- GoReleaser strips the leading `v`, so tags are plain semver. Archive names
  are `orc_<version-without-v>_<os>_<arch>.tar.gz` alongside a `checksums.txt`.
- Requires the `HOMEBREW_TAP_PAT` repo secret (shared with horde) for the
  formula push; without it the binaries/Release still publish.
- `scripts/install.sh` is the `curl | sh` installer documented in the README.
  It resolves the latest tag, downloads the matching tarball, verifies it
  against `checksums.txt`, and installs the binary. Keep its archive-name
  template in sync with `.goreleaser.yaml` if the naming ever changes.

## Beads: Work Tracking

Beads is a **work tracker** that persists across sessions. The orc roadmap (ROADMAP.md) has been decomposed into beads: each Wave is an epic, each roadmap item (R-NNN, P-NNN) is a task bead.

- **Use beads instead of TodoWrite** — beads persist across sessions
- **Valid types:** `task`, `bug`, `feature`, `chore`, `epic`
- **One tool:** `bd` for everything

| Use case | Command |
|----------|---------|
| Project dashboard (epics + children) | `bd` |
| What to work on next | `bd ready` |
| Epic dependency chain | `bd deps` |
| Bead details + notes | `bd show <id>` |
| Full metadata as JSON | `bd show <id> --json` |

### Search Beads BEFORE Planning

**Beads contain institutional knowledge from prior sessions.** You MUST mine this before planning or writing code — 3+ searches minimum.

```bash
bd search "R-004"              # The roadmap item
bd search "cost tracking"      # Domain area
bd search "stream parser"      # Components you'll touch
```

### Creating Beads

Every bead MUST have `--description` with: what/why, files involved, approach, acceptance criteria.

```bash
bd create --title="Fix stream parser edge case" --type=bug --priority=1 \
  -d "The stream parser drops the last token count...
Files: internal/dispatch/stream.go
Approach: Add buffered read at EOF
Acceptance: Token counts match expected in test"
```

### Capturing Decisions

Decisions belong on the work bead as notes. Cross-cutting decisions go on the wave epic.

```bash
bd update <bead-id> --append-notes="Decision: chose X because Y"
bd update <bead-id> --append-notes="Note: discovered Z behavior"
```

### Workflow: Roadmap Item → Implementation

1. `bd search "R-NNN"` — find the bead
2. `bd show <id>` — read full spec (description has everything)
3. `bd update <id> --status=in_progress` — claim
4. Implement the feature
5. `bd update <id> --append-notes="Decision: ..."` — record key decisions
6. Commit code, then `bd close <id>` (with user permission), then `bd sync`

### Connecting Beads

| Relationship | Command | Use when |
|-------------|---------|----------|
| Parent-child | `--parent=<id>` on create | Subtask of wave epic |
| Dependency | `bd dep add <blocked> <blocker>` | X can't start until Y completes |
| Related | `bd dep relate <a> <b>` | Cross-reference |

### Quick Reference

```bash
# Viewing
bd                                        # Dashboard
bd ready                                  # Ready tasks
bd show <id>                              # Bead details + notes
bd deps                                   # Dependency chain
bd search "keyword"                       # Full-text search

# Writing
bd create --title="..." --type=task --parent=<id> -d "..."
bd update <id> --status=in_progress       # Claim
bd update <id> --append-notes="info"      # Add notes
bd close <id>                             # Complete
bd dep add <blocked> <blocker>            # Add dependency
```

### Warnings

- Do NOT use `bd edit` — it opens $EDITOR which blocks agents
- Do NOT use TodoWrite, TaskCreate, or markdown for task tracking
- Priority: 0-4 (0=critical). NOT "high"/"medium"/"low"
- Do NOT close beads without explicit user permission
