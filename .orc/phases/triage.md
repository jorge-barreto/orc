You are triaging findings from the wave review into actionable beads.

## Context

- Wave epic: $TICKET
- Findings: read `$ARTIFACTS_DIR/wave-review-findings.md`

## Instructions

1. Read `$ARTIFACTS_DIR/wave-review-findings.md`.

2. **For each Bug finding** — create a bead as a child of this wave so it gets picked up:
   ```bash
   bd create --title="Fix: <concise title>" --type=bug --priority=1 --parent="$TICKET" \
     -d "<what's wrong, which file/line, how to fix>"
   ```

3. **For each Improvement finding** — create a bead under the current wave for future prioritization:
   ```bash
   bd create --title="<concise title>" --type=task --priority=3 --parent="$TICKET" \
     -d "<description and rationale>"
   ```

4. **For each Test Gap** — create a bead under the current wave (test gaps are bugs in coverage):
   ```bash
   bd create --title="Test: <what to test>" --type=task --priority=2 --parent="$TICKET" \
     -d "<scenario to test, which package, suggested test approach>"
   ```

5. Sync beads:
   ```bash
   bd sync
   ```

6. Write a summary to `$ARTIFACTS_DIR/triage-results.md`:
   ```markdown
   # Triage Results: $TICKET

   ## Beads Created in Current Wave
   - <bead-id>: <title> (bug/test)

   ## Beads Created for Future Work
   - <bead-id>: <title> (improvement)

   ## Stats
   - Bugs: N
   - Test gaps: N
   - Improvements: N
   - Total beads created: N
   ```

## Rules

- ALL beads go under the current wave (`--parent="$TICKET"`) — bugs and test gaps for follow-up, improvements for future prioritization.
- Do NOT create beads for style preferences or minor nits.
- Each bead description must be specific enough for an agent to implement without additional context.
- If the wave review found no findings, write an empty triage-results.md noting "No findings to triage."
