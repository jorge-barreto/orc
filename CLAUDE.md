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

Five internal packages, each with a single responsibility:

```
cmd/orc/main.go           CLI entrypoint (urfave/cli/v3)
internal/config/           Config + Phase structs, YAML loading, validation
internal/state/            State persistence (JSON), timing, artifacts dir, atomic writes
internal/dispatch/         Phase executors: script (bash), agent (claude -p), gate (human y/n)
internal/runner/           Main state machine loop — drives the workflow
internal/ux/               ANSI-colored terminal output, phase headers, status rendering
```

**Data flow:** `main.go` loads config, creates state, builds a `Runner`, and calls `runner.Run()`. The runner iterates phases, calling `dispatch.Dispatch()` for each. Dispatch routes to `RunScript`, `RunAgent`, or `RunGate` based on phase type. State is persisted to `.orc/artifacts/` after each phase.

**Key interfaces:** `dispatch.Dispatcher` is the only interface — the runner depends on it, and tests substitute a `mockDispatcher`.

### Phase Types

- **script** — Executes a bash command via `exec.CommandContext("bash", "-c", run)`. Requires `run` field.
- **agent** — Reads a prompt template file, expands variables (`$TICKET`, `$ARTIFACTS_DIR`, `$WORK_DIR`, `$PROJECT_ROOT`), invokes `claude -p` with the rendered prompt. Requires `prompt` field pointing to an existing file.
- **gate** — Prompts human for y/n approval. Auto-skipped with `--auto`.

### Runner State Machine

The runner loop handles: condition checks (skip phase if shell command exits non-zero), parallel execution (`parallel-with` runs two phases concurrently via goroutines + WaitGroup), loop backward jumps (convergent iteration with min/max and optional on-exhaust recovery, feedback written to `.orc/artifacts/feedback/`), and output validation (re-prompts agent once if declared outputs are missing).

### Config Validation Rules

Validation (`internal/config/validate.go`) enforces: unique phase names, `loop.goto` must reference an earlier phase, `parallel-with` must reference an existing phase, `parallel-with` and `loop` cannot be combined, agent phases need a `prompt` file that exists on disk, model must be `opus`/`sonnet`/`haiku`/empty, and ticket patterns are anchored for full-match semantics. The deprecated `on-fail` field is rejected with a migration hint.

## Conventions

- Go 1.22+ required (for urfave/cli/v3)
- Dependencies: only `gopkg.in/yaml.v3`, `github.com/urfave/cli/v3`, and `github.com/google/uuid` beyond stdlib
- State files are written atomically (write to `.tmp`, fsync, rename)
- Child processes inherit the parent env plus `ORC_*` variables; `CLAUDECODE` is stripped so `claude -p` can run
- Variable substitution uses `os.Expand()` with a custom map + env fallback (`internal/dispatch/expand.go`)
- Errors are wrapped with `%w` for error chains
- The binary refuses to run if `CLAUDECODE` env var is set (prevents nesting inside Claude Code)
- Go uses tabs for indentation (never spaces). A PostToolUse hook runs `gofmt -w` automatically after edits, but write idiomatic Go formatting from the start.

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
