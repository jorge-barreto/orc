You are triaging findings from the wave review into actionable beads.

## Context

- Wave epic: $TICKET
- Findings: read `$ARTIFACTS_DIR/wave-review-findings.md`

## Instructions

1. Read `$ARTIFACTS_DIR/wave-review-findings.md`.

2. **Classify each finding** as either **critical** or **backlog** using this rule:

   **Critical** (goes in current wave — gets worked immediately):
   - Bugs that cause incorrect runtime behavior: crashes, data races, data loss, security issues, silent wrong results
   - Test gaps for code that handles concurrency, state persistence, external input, or error recovery

   **Backlog** (orphan bead — saved for human review later):
   - Improvements, refactors, and style issues
   - Test gaps for rendering, formatting, simple getters, or deterministic pure functions
   - Anything where the worst case is "ugly" rather than "wrong"

3. **For each critical finding** — create a bead as a child of this wave so it gets picked up:
   ```bash
   bd create --title="Fix: <concise title>" --type=bug --priority=1 --parent="$TICKET" \
     -d "<what's wrong, which file/line, how to fix>"
   ```

4. **For each backlog finding** — create an orphan bead (no parent) so it's saved but not auto-scheduled:
   ```bash
   bd create --title="<concise title>" --type=task --priority=3 \
     -d "<description and rationale>"
   ```

5. Sync beads:
   ```bash
   bd sync
   ```

6. Write a summary to `$ARTIFACTS_DIR/triage-results.md`:
   ```markdown
   # Triage Results: $TICKET

   ## Critical (current wave)
   - <bead-id>: <title> — why it's critical

   ## Backlog (orphan — for human review)
   - <bead-id>: <title> — classification reason

   ## Stats
   - Critical: N
   - Backlog: N
   - Total beads created: N
   ```

## Rules

- **Critical beads** get `--parent="$TICKET"` so they enter the wave's work loop.
- **Backlog beads** get NO parent — they're saved for the human to review and prioritize later.
- Do NOT create beads for style preferences or minor nits.
- Each bead description must be specific enough for an agent to implement without additional context.
- If the wave review found no findings, write an empty triage-results.md noting "No findings to triage."
