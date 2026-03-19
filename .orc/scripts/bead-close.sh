#!/usr/bin/env bash
set -euo pipefail

# Close the current work item's bead (from epic-id.txt, not $TICKET).
# In wave mode, $TICKET is the wave — epic-id.txt has the child bead.
BEAD_ID=$(cat "$ORC_ARTIFACTS_DIR/epic-id.txt" 2>/dev/null || true)

if [[ -z "$BEAD_ID" ]]; then
  # Fallback: resolve from $TICKET directly
  if [[ "$TICKET" == orc-* ]]; then
    BEAD_ID="$TICKET"
  else
    BEAD_ID=$(bd search "$TICKET" | grep " - $TICKET:" | head -1 | cut -d' ' -f1)
  fi
fi

if [[ -n "$BEAD_ID" ]]; then
  bd close "$BEAD_ID"
  echo "Closed bead $BEAD_ID"
else
  echo "No bead found to close (continuing anyway)"
fi
