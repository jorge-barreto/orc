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
go test ./internal/config/ -run TestValidate_ParallelWithOnFail -count=1

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

**Data flow:** `main.go` loads config, creates state, builds a `Runner`, and calls `runner.Run()`. The runner iterates phases, calling `dispatch.Dispatch()` for each. Dispatch routes to `RunScript`, `RunAgent`, or `RunGate` based on phase type. State is persisted to `.artifacts/` after each phase.

**Key interfaces:** `dispatch.Dispatcher` is the only interface — the runner depends on it, and tests substitute a `mockDispatcher`.

### Phase Types

- **script** — Executes a bash command via `exec.CommandContext("bash", "-c", run)`. Requires `run` field.
- **agent** — Reads a prompt template file, expands variables (`$TICKET`, `$ARTIFACTS_DIR`, `$WORK_DIR`, `$PROJECT_ROOT`), invokes `claude -p` with the rendered prompt. Requires `prompt` field pointing to an existing file.
- **gate** — Prompts human for y/n approval. Auto-skipped with `--auto`.

### Runner State Machine

The runner loop handles: condition checks (skip phase if shell command exits non-zero), parallel execution (`parallel-with` runs two phases concurrently via goroutines + WaitGroup), on-fail backward jumps (loops back to an earlier phase with feedback written to `.artifacts/feedback/`), and output validation (re-prompts agent once if declared outputs are missing).

### Config Validation Rules

Validation (`internal/config/validate.go`) enforces: unique phase names, `on-fail.goto` must reference an earlier phase, `parallel-with` must reference an existing phase, agent phases need a `prompt` file that exists on disk, model must be `opus`/`sonnet`/`haiku`/empty, and ticket patterns are anchored for full-match semantics.

## Conventions

- Go 1.22+ required (for urfave/cli/v3)
- Dependencies: only `gopkg.in/yaml.v3` and `github.com/urfave/cli/v3` beyond stdlib
- State files are written atomically (write to `.tmp`, fsync, rename)
- Child processes inherit the parent env plus `ORC_*` variables; `CLAUDECODE` is stripped so `claude -p` can run
- Variable substitution uses `os.Expand()` with a custom map + env fallback (`internal/dispatch/expand.go`)
- Errors are wrapped with `%w` for error chains
- The binary refuses to run if `CLAUDECODE` env var is set (prevents nesting inside Claude Code)
