#!/usr/bin/env bash
# Prints the make check groups (backend, go, docs, frontend) that the change since the merge base with main needs.
# A path outside the known groups, no merge base, or no changes selects every group.

set -euo pipefail
cd "$(dirname "$0")/.."

all() {
  echo "check scope: all ($1)" >&2
  echo "backend go docs frontend"
  exit 0
}

base=$(git merge-base origin/main HEAD 2>/dev/null || git merge-base main HEAD 2>/dev/null) \
  || all "no merge base with main"

changed=$({
  git diff --name-only --no-renames "$base"
  git ls-files --others --exclude-standard
} | sort -u)
deleted=$(git diff --name-only --no-renames --diff-filter=D "$base")

[ -z "$changed" ] && all "no changed files"

backend="" go="" docs="" frontend=""
while IFS= read -r path; do
  case "$path" in
    # A backend test reads this guide.
    docs/guides/declarative-resources.md) backend=$path ;;
    # docs-check compares the binary table with backend/cmd.
    backend/cmd/*) backend=$path; docs=$path ;;
    backend/* | rules/* | .golangci.yml) backend=$path ;;
    frontend/*) frontend=$path ;;
    docs/* | tools/diagrams/* | *.md) docs=$path ;;
    *) all "$path" ;;
  esac
  case "$path" in
    *.md) docs=$path ;;
  esac
  case "$path" in
    *.go | */go.mod | */go.sum) go=$path ;;
  esac
done <<< "$changed"

# A deleted file can break a link in any doc.
[ -n "$deleted" ] && docs=$(echo "$deleted" | head -1)

groups=""
for g in backend go docs frontend; do
  path=${!g}
  if [ -n "$path" ]; then
    echo "check scope: $g (e.g. $path)" >&2
    groups="$groups $g"
  else
    echo "check scope: skip $g (no changes)" >&2
  fi
done
echo "$groups"
