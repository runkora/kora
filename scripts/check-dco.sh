#!/usr/bin/env bash
# =============================================================================
# check-dco.sh — Check every commit in a range carries a DCO sign-off.
#
# Each commit needs a `Signed-off-by:` trailer with its author's email, the
# contributor's assertion under the Developer Certificate of Origin that they
# may submit the change under the project's licence. Commits authored by bots
# are exempt. Merge commits are skipped, since they carry no new content.
#
# Usage: scripts/check-dco.sh <base> <head>
# =============================================================================
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <base> <head>" >&2
  exit 2
fi

# Listed up front so a bad range fails the check instead of checking nothing.
commits="$(git rev-list --no-merges "$1..$2")"

missing=0
while IFS= read -r sha; do
  [ -n "$sha" ] || continue
  author_name="$(git log -1 --format='%an' "$sha")"
  author_email="$(git log -1 --format='%ae' "$sha")"
  case "$author_name" in
    *"[bot]") continue ;;
  esac
  if ! git log -1 --format='%(trailers:key=Signed-off-by,valueonly)' "$sha" \
    | grep -qiF "<$author_email>"; then
    echo "✗ $(git log -1 --format='%h %s' "$sha")"
    echo "    no Signed-off-by for $author_name <$author_email>"
    missing=$((missing + 1))
  fi
done <<< "$commits"

if [ "$missing" -gt 0 ]; then
  echo ""
  echo "$missing commit(s) are missing a DCO sign-off."
  echo "See https://github.com/runkora/kora/blob/main/CONTRIBUTING.md#developer-certificate-of-origin"
  exit 1
fi
echo "✓ every commit is signed off"
