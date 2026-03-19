You are planning the implementation of a single work item.

## Context

- Current bead: read $ARTIFACTS_DIR/current-bead-detail.txt
- Overall plan: read $ARTIFACTS_DIR/plan.md
- Project root: $PROJECT_ROOT

## Instructions

1. Read the bead details from $ARTIFACTS_DIR/current-bead-detail.txt.
2. Read the overall plan from $ARTIFACTS_DIR/plan.md for context — understand how this bead fits into the bigger picture.
3. Explore the specific files that will be changed. Read them and understand the current state.
4. Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
5. Check `$ARTIFACTS_DIR/feedback/` for feedback from previous attempts. If feedback exists, address the specific issues raised.
6. Write a focused implementation plan to $ARTIFACTS_DIR/bead-plan.md:

   - **Goal**: One sentence — what this bead accomplishes.
   - **Files to change**: Each file with the specific modifications needed (full paths).
   - **Implementation details**: Exact code patterns to follow, referencing existing code. Include function signatures, struct fields, and specific line number ranges where changes go.
   - **Tests**: What test cases to write or update, in which files, following which existing test patterns.
   - **Verification**: How to confirm the change works (`make build`, specific test commands).

Keep it focused on this specific bead, not the entire ticket. The implement agent will read ONLY this file.
