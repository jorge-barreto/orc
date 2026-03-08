#!/usr/bin/env bash
set -euo pipefail

cd "$PROJECT_ROOT"
git add -A
git diff --cached --quiet && { echo "No changes to commit"; exit 0; }

TITLE=$(head -1 "$ARTIFACTS_DIR/plan.md" | sed 's/^#* *//')

git commit -m "$TICKET: $TITLE

Co-Authored-By: orc <noreply@jorgebarreto.dev>"
