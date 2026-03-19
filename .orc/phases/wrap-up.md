You are wrapping up work on a bead.

## Context

- Current bead ID: read from $ARTIFACTS_DIR/current-bead.txt
- Current work item: read from $ARTIFACTS_DIR/current-ticket.txt (use for commit messages)
- Project root: $PROJECT_ROOT

## Instructions

1. Read the bead ID from $ARTIFACTS_DIR/current-bead.txt.

2. **Commit the changes:**
   - Run `git status` to see what changed.
   - Stage relevant files with `git add <specific files>` — stage Go files, test files, markdown, yaml, and shell scripts that are part of this bead's work. Never use `git add .` or `git add -A`.
   - Read the bead ID from $ARTIFACTS_DIR/current-bead.txt.
   - Read the bead detail from $ARTIFACTS_DIR/current-bead-detail.txt to get the bead title.
   - Commit with the **bead ID only** (not the parent ticket):
     ```
     git commit -m "<bead-id>: <short description of what this bead accomplished>

     Co-Authored-By: orc <orc@jorgebarreto.dev>"
     ```
     Example: `orc-4ho.3.4: Update docs with recipe documentation`
     NOT: `orc-4ho.3: orc-4ho.3.4: Update docs...` (do not duplicate IDs)

4. **Close the bead:**
   ```bash
   bd close <bead-id>
   ```

5. **Create beads for discovered issues:**
   Check `$ARTIFACTS_DIR/quick-review-findings.md` for any non-blocking notes from the review. If any note describes an issue **directly related to this work item's scope** that must be addressed to complete it correctly, create a bead for it:
   ```bash
   EPIC=$(cat "$ARTIFACTS_DIR/epic-id.txt")
   bd create --title="<concise title>" --type=bug --priority=3 --parent="$EPIC" -d "<what's wrong and where>"
   ```
   Append each new bead ID to the bead list:
   ```bash
   echo "<new-bead-id>" >> "$ARTIFACTS_DIR/bead-ids.txt"
   ```

6. **Clean up** per-bead artifacts so the next bead starts fresh:
   ```bash
   rm -f "$ARTIFACTS_DIR/bead-plan.md" "$ARTIFACTS_DIR/quick-review-findings.md" "$ARTIFACTS_DIR/quick-review-pass.txt" "$ARTIFACTS_DIR/current-bead.txt" "$ARTIFACTS_DIR/current-bead-detail.txt"
   ```

## Rules

- Stage specific files, never `git add .` or `git add -A`.
- Only create new beads for issues within this ticket's scope — not general tech debt.
- The commit message should be concise and describe what this bead accomplished, not the whole ticket.
