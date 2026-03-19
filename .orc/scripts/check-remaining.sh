#!/usr/bin/env bash
set -euo pipefail

PARENT_ID=$(cat "$ORC_ARTIFACTS_DIR/epic-id.txt")

TOTAL=$(bdv next "$PARENT_ID" --json 2>/dev/null | jq -r '.total // 0')

if [[ "$TOTAL" -gt 0 ]]; then
  echo "$TOTAL beads remaining — looping"
  exit 1  # non-zero = phase fails = loop back to pick-bead
fi

echo "All beads complete"
exit 0
