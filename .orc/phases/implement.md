You are implementing changes for a single work item.

## Context

- Bead plan: read $ARTIFACTS_DIR/bead-plan.md
- Bead detail: read $ARTIFACTS_DIR/current-bead-detail.txt
- Current work item: read $ARTIFACTS_DIR/current-ticket.txt (use for commit context)
- Review feedback (if looping): check $ARTIFACTS_DIR/quick-review-findings.md
- Project root: $PROJECT_ROOT

## Instructions

1. Read the bead plan at $ARTIFACTS_DIR/bead-plan.md. This is your SOLE specification.
2. If $ARTIFACTS_DIR/quick-review-findings.md exists and contains "Issues Found", read it and address every issue listed.
3. Check `$ARTIFACTS_DIR/feedback/` for feedback from previous attempts (test failures, build errors). If feedback exists, read it and fix the specific failures.
4. Read `$PROJECT_ROOT/CLAUDE.md` — project conventions.
5. Implement the changes described in the bead plan.
6. Write or update tests as specified in the bead plan.
7. Run `make build` to verify compilation.
8. Run `make test` to verify no regressions.
9. If any test fails, fix it before finishing. Do NOT leave failing tests.

## Scope Rules

Your ONLY task is what is described in `$ARTIFACTS_DIR/bead-plan.md`. Nothing else.

- **Do NOT read `$ARTIFACTS_DIR/plan.md`** unless the bead plan explicitly tells you to consult it for context on a specific point. The overall plan contains work for OTHER beads that is NOT your responsibility.
- **Do NOT implement work belonging to other beads.** If the bead plan says "add field X to struct Y", add field X. Do not also add fields A, B, C that you think will be needed later.
- **Do NOT modify files outside the bead plan's scope.** The only exception is updating imports or fixing compile errors directly caused by your changes.
- **If you discover something that needs to be done but is NOT in the bead plan:**
  1. Note it in stdout so the wrap-up phase can capture it as a new bead.
  2. Do NOT implement it.
- **Do NOT add abstractions, utilities, or helpers** that the bead plan does not call for.
- **Do NOT refactor adjacent code** unless the bead plan explicitly says to.

## Quality Rules

- Follow existing code patterns. Read neighboring code before writing new code.
- Keep dependencies minimal — only `gopkg.in/yaml.v3`, `github.com/urfave/cli/v3`, and `github.com/google/uuid` beyond stdlib.
- Wrap errors with `%w` for error chains.
- State files use atomic writes (write to `.tmp`, fsync, rename).
- Every new exported function needs a test. Follow the testing patterns in the package's existing `_test.go` file.
- Use the Edit tool for targeted changes — don't rewrite entire files unless the plan calls for a new file.
- Match the existing code style in each file you touch.
- Run `make test` as your final action. If it fails, fix it.
