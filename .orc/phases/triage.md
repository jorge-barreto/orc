You are triaging findings from the wave review into actionable beads.

## Context

- Wave epic: $TICKET
- Findings: read `$ARTIFACTS_DIR/wave-review-findings.md`

## Instructions

1. Read `$ARTIFACTS_DIR/wave-review-findings.md`.

2. **Classify each finding** as either **critical** or **backlog** using this decision test:

   For each finding, ask: **"Can I describe a concrete scenario where this bug ships to a user and causes wrong behavior at runtime?"**

   If yes → **Critical**. If the answer is hypothetical ("if someone later refactors...", "if a future caller...") → **Backlog**.

   **Critical** (goes in current wave — gets worked immediately):
   - Bugs in code that runs today and produces wrong results: crashes, data races, data loss, security issues
   - A missing test is critical ONLY if: (1) the code it covers has zero existing test coverage, AND (2) the untested code path can produce a wrong result in a scenario that can actually happen today (not hypothetically after a future refactor)

   **Backlog** (orphan bead — saved for human review later):
   - Everything else: improvements, refactors, style, missing assertions on already-tested code, hypothetical future risks, defensive hardening
   - A missing test where the code already has some coverage (even indirect) is always backlog
   - A test that would only catch a bug introduced by a future change that hasn't happened is always backlog

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
