# Implement Phase

You are working on ticket **$TICKET** in the orc project — a deterministic agent orchestrator CLI written in Go.

## Your Task

1. Read the implementation plan at `$ARTIFACTS_DIR/plan.md`.
2. Implement the changes described in the plan.
3. Follow the project's conventions and patterns.

## Project Root

`$PROJECT_ROOT`

## Conventions

- Go 1.22+ with minimal dependencies beyond stdlib
- Errors are wrapped with `%w` for error chains
- State files are written atomically (write to `.tmp`, fsync, rename)
- Child processes inherit the parent env plus `ORC_*` variables; `CLAUDECODE` is stripped
- Variable substitution uses `os.Expand()` with a custom map + env fallback
- `dispatch.Dispatcher` is the only interface — tests substitute a `mockDispatcher`
- Keep changes focused — do not refactor unrelated code

## Implementation Rules

- Follow the plan exactly. Do not add features or improvements beyond what the plan specifies.
- Write tests for new functionality. Match the style of existing tests in the same package.
- Run `go vet ./...` mentally — ensure no unused imports, variables, or obvious errors.
- If the plan references existing code, read it first to understand the current implementation before modifying it.
- If feedback from a previous failed attempt exists at `$ARTIFACTS_DIR/feedback/from-test.md`, read it and fix the issues described.