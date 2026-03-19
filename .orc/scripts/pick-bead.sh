#!/usr/bin/env bash
set -euo pipefail

PARENT_ID=$(cat "$ORC_ARTIFACTS_DIR/epic-id.txt" 2>/dev/null || true)

if [[ -z "$PARENT_ID" ]]; then
  echo "No epic-id.txt found — bead-claim must run first"
  exit 1
fi

# Get the next ready bead under the parent
TASK_JSON=$(bdv next "$PARENT_ID" --json 2>/dev/null || echo '{"total":0}')
TOTAL=$(echo "$TASK_JSON" | jq -r '.total // 0')

if [[ "$TOTAL" -eq 0 ]]; then
  echo "No ready beads remaining"
  exit 1
fi

ID=$(echo "$TASK_JSON" | jq -r '.epics[0].tasks[0].id')
if [[ "$ID" == "null" || -z "$ID" ]]; then
  echo "No ready beads remaining"
  exit 1
fi

echo "$ID" > "$ORC_ARTIFACTS_DIR/current-bead.txt"
bd show "$ID" > "$ORC_ARTIFACTS_DIR/current-bead-detail.txt"
bd update "$ID" --status=in_progress
echo "Picked bead $ID"
