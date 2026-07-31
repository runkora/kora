#!/usr/bin/env bash
# Reports Go functions no main package can reach.
#
# golangci-lint's `unused` only sees within a package, so it catches an unused
# unexported helper and nothing else. What it cannot see is an EXPORTED function
# with no callers anywhere, which is how a platform helper ends up written,
# reviewed, merged and then hand-rolled around by every caller that needed it:
# storage.WithTransaction sat with zero callers while three repositories wrote
# their own Begin/Rollback/Commit, and both dependency health checks sit unused
# while every readiness probe returns a static 200.
#
# This uses reachability from every main package, so it answers "does anything
# actually call this", which is the question `unused` cannot ask.
#
# -test counts test code as a caller. A helper used only by tests is not dead,
# it is test support, and flagging it would train people to ignore the output.
set -euo pipefail

cd "$(dirname "$0")/.."

BACKEND_DIR=backend
DEADCODE_PKG="golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION:-v0.48.0}"

# The only exemptions. Everything else unreachable gets deleted, which is why
# this list is two lines rather than a page.
#
# Both are dependency health checks that should be WIRED UP, not deleted. Every
# readiness probe in the repo currently returns a static 200, so a pod whose
# database is unreachable still receives traffic. These are the checks a real
# probe would call. See issue #173.
#
# Adding anything here needs a reason on the same terms: kept because it is
# about to be used, not because deleting it is inconvenient.
ALLOWED=(
  "storage/db.go.*DB.Health"
  "storage/db.go.*DB.Stats"
)

echo "Checking for unreachable Go functions..."

raw=$(cd "$BACKEND_DIR" && go run "$DEADCODE_PKG" -test ./... 2>/dev/null || true)

findings=""
while IFS= read -r line; do
  [ -z "$line" ] && continue
  allowed=false
  for pat in "${ALLOWED[@]}"; do
    if printf '%s' "$line" | grep -qE "$pat"; then allowed=true; break; fi
  done
  $allowed || findings+="$line"$'\n'
done <<< "$raw"

if [ -n "$findings" ]; then
  echo "✗ unreachable functions (no main package can reach these):"
  printf '%s' "$findings" | sed 's/^/    /'
  echo
  echo "  Delete it, wire it up, or add it to ALLOWED in $0 with a reason."
  exit 1
fi

echo "✓ no new unreachable functions"
