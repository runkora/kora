#!/usr/bin/env bash
# Verifies the five contract boundary rules are identical in their three
# homes: frontend/src/packages/hades/CONTRACT.md (delivered with the package),
# docs/prd/design-system.md and the design system versioning ADR. The
# numbered list is extracted from each and diffed.
set -euo pipefail

cd "$(dirname "$0")/../.."

CONTRACT=frontend/src/packages/hades/CONTRACT.md
PRD=docs/prd/design-system.md
ADR=docs/adr/2026-10-11-design-system-semantic-versioning.md

# The five numbered items that follow the "Contract boundary rules" heading.
rules() {
  awk '
    /^\*\*The contract boundary rules\.\*\*|^\*\*Contract boundary rules\.\*\*|^# Hades theme contract boundary rules/ { in_block = 1; next }
    in_block && /^[1-5]\. / { print; count++; if (count == 5) exit }
  ' "$1"
}

status=0
for other in "$PRD" "$ADR"; do
  if ! diff <(rules "$CONTRACT") <(rules "$other") >/dev/null; then
    echo "✗ $CONTRACT and $other disagree on the contract boundary rules:"
    diff <(rules "$CONTRACT") <(rules "$other") || true
    status=1
  fi
done

if [ "$(rules "$CONTRACT" | wc -l | tr -d ' ')" != "5" ]; then
  echo "✗ $CONTRACT does not carry five numbered rules"
  status=1
fi

if [ "$status" = "0" ]; then
  echo "✓ contract boundary rules match in CONTRACT.md, the Hades PRD and the versioning ADR"
fi
exit "$status"
