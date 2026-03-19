#!/usr/bin/env bash
set -euo pipefail

cd "$PROJECT_ROOT"

WAVE_BASE=$(cat "$ORC_ARTIFACTS_DIR/wave-base-commit.txt" 2>/dev/null || echo "main")
COMMITS=$(git log --oneline $WAVE_BASE..HEAD 2>/dev/null || echo "(no commits)")
COMMIT_COUNT=$(git log --oneline $WAVE_BASE..HEAD 2>/dev/null | wc -l || echo "0")
STATS=$(git diff --stat $WAVE_BASE..HEAD 2>/dev/null || echo "(no changes)")
FILES_CHANGED=$(git diff --name-only $WAVE_BASE..HEAD 2>/dev/null | wc -l || echo "0")

# Show wave status
WAVE_STATUS=$(bdv show "$TICKET" 2>/dev/null || echo "(unable to fetch wave status)")

cat > "$ORC_ARTIFACTS_DIR/wave-summary.md" <<EOF
# Wave Summary: $TICKET

## Wave Status
$WAVE_STATUS

## Stats
- Commits: $COMMIT_COUNT
- Files changed: $FILES_CHANGED

## All Commits
$COMMITS

## Diff Summary
$STATS
EOF

cat "$ORC_ARTIFACTS_DIR/wave-summary.md"
