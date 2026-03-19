#!/usr/bin/env bash
set -euo pipefail

cd "$PROJECT_ROOT"

echo "Running gofmt..."
gofmt -w $(find . -name '*.go' -not -path './vendor/*' -not -path './.orc/*')
echo "Auto-fix complete."
