You are breaking an implementation plan into trackable beads (work items).

**CRITICAL: Your ONLY job is to create beads. Do NOT implement code, edit files, write code, run tests, or make any changes to the codebase. You create beads and write bead-ids.txt. That is ALL.**

## Context

- Epic bead ID: read from $ARTIFACTS_DIR/epic-id.txt
- Plan: read from $ARTIFACTS_DIR/plan.md

## Instructions

1. Read the epic bead ID from $ARTIFACTS_DIR/epic-id.txt. This is the PARENT_ID for all child beads.
2. Read the plan at $ARTIFACTS_DIR/plan.md.
3. Break the plan into discrete, implementable tasks. Each task should be:
   - Small enough to implement in one focused session (1-5 files)
   - Clear about what files to change and what the change is
   - Self-contained (can be verified independently after dependencies are met)
   - **Compilable** — after implementing this bead, `make build` must pass

4. For each task, create a bead as a child of the epic:
   ```bash
   bd create --title="<concise task title>" --type=task --priority=2 --parent=$(cat $ARTIFACTS_DIR/epic-id.txt) -d "<specific details: files to change, what to do, test strategy>"
   ```
   Capture the bead ID from the output.

5. Add dependencies between beads where order matters:
   ```bash
   bd dep add <later-bead> <earlier-bead>
   ```
   The first argument depends on the second (second must complete first).

6. Write ALL bead IDs to $ARTIFACTS_DIR/bead-ids.txt, one per line, in dependency order (no-dependency tasks first).

## Rules

### Compilability (HARD RULE)

**Every bead must leave the codebase in a compilable state.** After implementing a bead, `make build && go vet ./...` must pass. A bead that introduces compile errors is broken — even if a later bead would fix them.

This means:
- **Never split a type change from the code that uses it.** If you add a new field to a struct, the same bead must update all code that constructs that struct.
- **Never split an interface change from its implementations.** If you change an interface method signature, the same bead must update all implementations.
- **Group related changes together** when they form a single compilable unit.

### Sizing

- Typically 2–6 beads per ticket. More for large features, fewer for simple changes.
- Group related changes (e.g., "Add new config field + validation + tests" is one bead if they must compile together).
- Include tests alongside implementation, not as separate beads.

### Ordering

- Core types/structs before code that uses them.
- Internal packages before the runner/dispatch code that calls them.
- New functions before callers.

### Description Quality

Each bead description MUST include:
- Which files to create or modify (full paths)
- What specifically changes in each file
- What tests to add or update
