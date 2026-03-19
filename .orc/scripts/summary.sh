#!/usr/bin/env bash
set -euo pipefail

cd "$PROJECT_ROOT"

WORK_ITEM=$(cat "$ORC_ARTIFACTS_DIR/current-ticket.txt" 2>/dev/null || echo "$TICKET")
BASE=$(cat "$ORC_ARTIFACTS_DIR/base-commit.txt" 2>/dev/null || echo "main")
TITLE=$(head -1 "$ORC_ARTIFACTS_DIR/plan.md" 2>/dev/null | sed 's/^#* *//' || echo "unknown")
COMMITS=$(git log --oneline "$BASE..HEAD" 2>/dev/null || echo "(no commits)")
STATS=$(git diff --stat "$BASE..HEAD" 2>/dev/null || echo "(no changes)")

cat > "$ORC_ARTIFACTS_DIR/summary.md" <<EOF
# $WORK_ITEM: $TITLE

## Commits
$COMMITS

## Changes
$STATS
EOF

cat "$ORC_ARTIFACTS_DIR/summary.md"
