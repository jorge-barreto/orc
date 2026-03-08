#!/usr/bin/env bash
set -euo pipefail

# Resolve bead ID from ticket. orc-* tickets are bead IDs directly;
# R-\d+ tickets need a search to find the corresponding bead.
if [[ "$TICKET" == orc-* ]]; then
  BEAD_ID="$TICKET"
else
  BEAD_ID=$(bd search "$TICKET" | grep " - $TICKET:" | head -1 | cut -d' ' -f1)
fi

if [[ -n "$BEAD_ID" ]]; then
  bd update "$BEAD_ID" --status=in_progress
  echo "Claimed bead $BEAD_ID"
else
  echo "No bead found for $TICKET (continuing anyway)"
fi
