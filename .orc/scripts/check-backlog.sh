#!/usr/bin/env bash
set -euo pipefail

RUN_MODE=$(cat "$ORC_ARTIFACTS_DIR/run-mode.txt" 2>/dev/null || echo "single")

if [[ "$RUN_MODE" == "wave" ]]; then
  # Wave mode: check for remaining children of this wave
  TOTAL=$(bd ready "$TICKET" --json 2>/dev/null | jq -r '.total // 0')
else
  # Single-ticket mode: check global backlog
  TOTAL=$(bd ready --json 2>/dev/null | jq -r '.total // 0')
fi

if [[ "$TOTAL" -gt 0 ]]; then
  echo "$TOTAL beads remaining — looping"
  exit 1  # non-zero = phase fails = loop back to bead-claim
fi

echo "Backlog complete"
exit 0
