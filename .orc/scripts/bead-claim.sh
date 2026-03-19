#!/usr/bin/env bash
set -euo pipefail

# Clean stale artifacts from previous iteration
rm -f "$ORC_ARTIFACTS_DIR/plan.md" \
      "$ORC_ARTIFACTS_DIR/plan-review.md" \
      "$ORC_ARTIFACTS_DIR/plan-approved.txt" \
      "$ORC_ARTIFACTS_DIR/deep-review-pass.txt" \
      "$ORC_ARTIFACTS_DIR/review-findings.md" \
      "$ORC_ARTIFACTS_DIR/bead-ids.txt" \
      "$ORC_ARTIFACTS_DIR/current-ticket.txt"

# Detect wave mode: is $TICKET a bead ID pointing to an epic?
WAVE_MODE=false
if [[ "$TICKET" == orc-* ]]; then
  BEAD_TYPE=$(bd show "$TICKET" --json 2>/dev/null | jq -r '.[0].issue_type // empty' || true)
  if [[ "$BEAD_TYPE" == "epic" ]]; then
    WAVE_MODE=true
  fi
fi

# Record base commit for this work item (deep-review diffs against this)
git rev-parse HEAD > "$ORC_ARTIFACTS_DIR/base-commit.txt"

# Record wave base commit once (wave-review diffs against this)
if [[ ! -f "$ORC_ARTIFACTS_DIR/wave-base-commit.txt" ]]; then
  cp "$ORC_ARTIFACTS_DIR/base-commit.txt" "$ORC_ARTIFACTS_DIR/wave-base-commit.txt"
fi

# Persist mode so other scripts can check without re-querying
if [[ "$WAVE_MODE" == "true" ]]; then
  echo "wave" > "$ORC_ARTIFACTS_DIR/run-mode.txt"
else
  echo "single" > "$ORC_ARTIFACTS_DIR/run-mode.txt"
fi

if [[ "$WAVE_MODE" == "true" ]]; then
  # Wave mode: pick next ready child of the wave epic
  TASK_JSON=$(bdv next "$TICKET" --json 2>/dev/null || echo '{"total":0}')
  TOTAL=$(echo "$TASK_JSON" | jq -r '.total // 0')

  if [[ "$TOTAL" -eq 0 ]]; then
    echo "No ready beads under wave $TICKET"
    exit 1
  fi

  BEAD_ID=$(echo "$TASK_JSON" | jq -r '.epics[0].tasks[0].id')
  if [[ "$BEAD_ID" == "null" || -z "$BEAD_ID" ]]; then
    echo "No ready beads under wave $TICKET"
    exit 1
  fi

  bd update "$BEAD_ID" --status=in_progress
  echo "$BEAD_ID" > "$ORC_ARTIFACTS_DIR/epic-id.txt"

  # Write the bead ID as the current ticket — the plan phase will
  # use bd show on it directly to get full context.
  echo "$BEAD_ID" > "$ORC_ARTIFACTS_DIR/current-ticket.txt"
  echo "Wave mode: claimed $BEAD_ID"
else
  # Single-ticket mode: $TICKET is the work item itself
  BEAD_ID=$(bd search "$TICKET" 2>/dev/null | grep -oP '^orc-\S+' | head -1 || true)

  if [[ -n "$BEAD_ID" ]]; then
    bd update "$BEAD_ID" --status=in_progress
    echo "$BEAD_ID" > "$ORC_ARTIFACTS_DIR/epic-id.txt"
  fi

  echo "$TICKET" > "$ORC_ARTIFACTS_DIR/current-ticket.txt"
  echo "Single-ticket mode: working on $TICKET"
fi
