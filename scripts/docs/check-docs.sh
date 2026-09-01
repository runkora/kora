#!/usr/bin/env bash
# =============================================================================
# check-docs.sh. Fail the build when the docs no longer match the code.
#
#   links     Every relative markdown link resolves to a file on disk.
#   indexes   Every ADR/PRD on disk appears in its index, and vice versa.
#   targets   Every `make <target>` named in a doc exists in the Makefile.
#   binaries  The binary table and backend/cmd/ agree, in both directions.
#   counts    No doc hardcodes "N binaries"/"N services".
#   status    No build-status/roadmap claims in living docs (ADRs/PRDs exempt).
#   prose     No em dashes in living docs. ADRs and PRDs are exempt: they are
#             point-in-time records, not prose we keep rewriting.
#
# Usage: scripts/docs/check-docs.sh [links|indexes|targets|binaries|counts|status|prose]
#        (no argument runs every check)
# =============================================================================

set -uo pipefail

if [[ "${BASH_VERSINFO[0]}" -lt 4 ]]; then
  echo "ERROR: check-docs.sh requires bash 4+. You have bash ${BASH_VERSION}." >&2
  echo "       Run: brew install bash" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

C_RESET=$'\033[0m'; C_RED=$'\033[31m'; C_GREEN=$'\033[32m'
C_YELLOW=$'\033[33m'; C_DIM=$'\033[2m'

FAILURES=0
fail() { printf "%s\n" "  ${C_RED}✗${C_RESET} $*"; FAILURES=$((FAILURES + 1)); }
ok()   { printf "%s\n" "${C_GREEN}✓${C_RESET} $*"; }
head_() { printf "\n%s\n" "${C_YELLOW}$*${C_RESET}"; }

# Markdown files we own. Excludes vendored/generated trees and the archive,
# which is a deliberate record of what we used to believe.
md_files() {
  git ls-files '*.md' \
    | grep -vE '^(frontend/node_modules/|docs/archive/)' \
    | grep -vE '/(node_modules|dist|\.next)/'
}

# Strip fenced code blocks so Go slices (`[]string{...}(x)`) and shell
# snippets aren't parsed as markdown links or make invocations.
strip_fences() {
  awk '/^[[:space:]]*(```|~~~)/ { infence = !infence; next } !infence'
}

# Strip inline `code` spans. Without this, Go signatures like `func f(path string)`
# parse as a markdown link to "path string".
strip_inline_code() {
  sed -E 's/`[^`]*`//g'
}

# Emit only the code in a file: fenced blocks plus inline `code` spans. Prose
# such as "make informed decisions" must never be read as a make invocation.
code_only() {
  awk '/^[[:space:]]*(```|~~~)/ { infence = !infence; next } infence { print }' "$1"
  grep -oE '`[^`]*`' "$1" | tr -d '`'
}

# ── links ────────────────────────────────────────────────────────────────────
check_links() {
  head_ "Checking markdown links resolve..."
  local found=0
  while IFS= read -r file; do
    # Templates link to placeholder filenames by design (YYYY-MM-DD-slug.md).
    [ "$(basename "$file")" = "template.md" ] && continue
    local dir; dir="$(dirname "$file")"
    while IFS= read -r target; do
      [ -z "$target" ] && continue
      case "$target" in
        http://*|https://*|mailto:*|\#*) continue ;;
      esac
      # Only path-shaped targets. Prose captured by the regex ("url", "link",
      # "path string") has neither a separator nor a file extension.
      case "$target" in
        */*|*.md|*.yaml|*.yml|*.go|*.sh|*.png|*.excalidraw) ;;
        *) continue ;;
      esac
      # Drop any #anchor and ?query, then resolve relative to the file.
      local path="${target%%#*}"; path="${path%%\?*}"
      [ -z "$path" ] && continue
      if [ ! -e "$dir/$path" ] && [ ! -e "$path" ]; then
        local line; line=$(grep -nF "($target)" "$file" | head -1 | cut -d: -f1)
        fail "$file:${line:-?} → broken link: $target"
        found=1
      fi
    done < <(strip_fences < "$file" | strip_inline_code | grep -oE '\]\([^)]+\)' | sed -E 's/^\]\(//; s/\)$//')
  done < <(md_files)
  [ "$found" -eq 0 ] && ok "all markdown links resolve"
}

# ── indexes ──────────────────────────────────────────────────────────────────
# Every decision record must be discoverable from its index, or it may as well
# not exist. Checks both directions: orphans on disk, and dangling entries.
check_index_dir() {
  local dir="$1" label="$2"
  local index="$dir/README.md"
  if [ ! -f "$index" ]; then fail "$label index missing: $index"; return; fi

  local missing=0
  while IFS= read -r doc; do
    local base; base="$(basename "$doc")"
    case "$base" in README.md|template.md) continue ;; esac
    if ! grep -qF "$base" "$index"; then
      fail "$label not listed in $index: $base"
      missing=1
    fi
  done < <(find "$dir" -maxdepth 1 -name '*.md' | sort)

  # Dangling: index links to a file that no longer exists.
  while IFS= read -r target; do
    [ -z "$target" ] && continue
    case "$target" in http*|\#*) continue ;; esac
    local path="${target%%#*}"
    [ -e "$dir/$path" ] || { fail "$label index links to missing file: $path"; missing=1; }
  done < <(strip_fences < "$index" | grep -oE '\]\([^)]+\.md[^)]*\)' | sed -E 's/^\]\(//; s/\)$//')

  [ "$missing" -eq 0 ] && ok "$label index complete (both directions)"
}

check_indexes() {
  head_ "Checking ADR/PRD indexes..."
  check_index_dir docs/adr "ADR"
  check_index_dir docs/prd "PRD"
}

# ── targets ──────────────────────────────────────────────────────────────────
# A doc that tells you to run a target that does not exist is worse than no doc.
#
# Scoped to *living* docs. ADRs and PRDs are point-in-time records: an ADR that
# cites `make cluster-up` is faithfully recording the decision as it was made,
# and rewriting it to match today's Makefile would falsify the record.
living_docs() {
  md_files | grep -vE '^docs/(adr|prd)/'
  # Excludes this script: its own messages name targets it must not resolve.
  git ls-files 'scripts/*.sh' 'scripts/**/*.sh' | grep -v '^scripts/docs/check-docs.sh$'
}

check_targets() {
  head_ "Checking 'make <target>' references exist (living docs only)..."
  local found=0
  local targets; targets="$(grep -oE '^\.?[a-zA-Z0-9_/-]+:' Makefile | tr -d ':' | sort -u)"
  while IFS= read -r file; do
    while IFS= read -r target; do
      [ -z "$target" ] && continue
      # Skip variable assignments (make FOO=bar), flags, and glob placeholders
      # such as `make frontend-*` used to refer to a family of targets.
      case "$target" in -*|*=*|*\**|*-) continue ;; esac
      if ! printf '%s\n' "$targets" | grep -qx "$target"; then
        local line; line=$(grep -nE "make +$target\b" "$file" | head -1 | cut -d: -f1)
        fail "$file:${line:-?} → 'make $target' is not a Makefile target"
        found=1
      fi
    done < <({ case "$file" in
                 # In markdown, a real invocation starts its line (optionally
                 # after a `$` prompt). This skips prose that merely contains
                 # the word: a Go comment "// make the request", UI copy
                 # "Orbit AI can make mistakes."
                 *.md) code_only "$file" | grep -E '^[[:space:]]*\$?[[:space:]]*make +[a-z]' ;;
                 # Shell: code only. A comment is prose, and this file's own
                 # comments would otherwise match themselves.
                 *)    grep -vE '^[[:space:]]*#' "$file" ;;
               esac; } | grep -ohE '\bmake +[a-z][a-zA-Z0-9_-]*' | awk '{print $2}' | sort -u)
  done < <(living_docs)
  [ "$found" -eq 0 ] && ok "every referenced make target exists"
}

# ── binaries ─────────────────────────────────────────────────────────────────
# docs/code-organization.md carries the authoritative binary table. Keep it
# honest in both directions rather than trusting a human to remember.
check_binaries() {
  head_ "Checking the binary table matches backend/cmd/..."
  local doc="docs/code-organization.md"
  local found=0
  if [ ! -f "$doc" ]; then fail "$doc missing"; return; fi

  for dir in backend/cmd/*/; do
    local bin; bin="$(basename "$dir")"
    if ! grep -qE "\`$bin\`|\`cmd/$bin/\`" "$doc"; then
      fail "$doc does not document binary: $bin"
      found=1
    fi
  done

  # Phantom entries: the doc names a cmd/<x>/ that no longer exists.
  while IFS= read -r bin; do
    [ -d "backend/cmd/$bin" ] || { fail "$doc documents a binary that does not exist: $bin"; found=1; }
  done < <(grep -oE '`cmd/[a-z0-9-]+/`' "$doc" | sed -E 's|`cmd/||; s|/`||' | sort -u)

  # The "Owns tables" column must agree with which binaries carry migrations/.
  for dir in backend/cmd/*/; do
    local bin row claims has
    bin="$(basename "$dir")"
    row="$(grep -E "^\| \`$bin\` \|" "$doc" | head -1)"
    [ -z "$row" ] && continue
    case "$row" in *"✅ |") claims=yes ;; *) claims=no ;; esac
    [ -d "$dir/migrations" ] && has=yes || has=no
    if [ "$claims" != "$has" ]; then
      fail "$doc: '$bin' owns-tables column says '$claims' but migrations/ present = '$has'"
      found=1
    fi
  done

  [ "$found" -eq 0 ] && ok "binary table matches backend/cmd/ (rows present both ways, owns-tables column)"
}

# ── counts ───────────────────────────────────────────────────────────────────
# "13 binaries" was true once. A count is a claim with a short shelf life and
# nothing to pin it to; name the components or point at the generated table.
check_counts() {
  head_ "Checking for hardcoded counts that will go stale..."
  local found=0
  while IFS= read -r file; do
    while IFS= read -r hit; do
      [ -z "$hit" ] && continue
      fail "$file → hardcoded count '$hit' (say what they are, or link the table)"
      found=1
    done < <(strip_fences < "$file" \
      | grep -oiE '\*{0,2}[0-9]+\*{0,2} (binaries|services|micro-?services)\b' | sort -u)
  done < <(md_files)
  [ "$found" -eq 0 ] && ok "no drift-prone counts in prose"
}

# ── status ───────────────────────────────────────────────────────────────────
# "Designed but not yet built" is true until the day it ships, and then it is a
# lie in however many files repeated it. Build status and roadmap claims stay out
# of the living docs entirely. ADRs and PRDs are exempt: they are point-in-time
# records by definition.
check_status() {
  head_ "Checking for build-status claims in living docs..."
  local found=0
  while IFS= read -r file; do
    while IFS= read -r hit; do
      [ -z "$hit" ] && continue
      fail "$file → '$hit' (build status belongs in ADRs/PRDs, not living docs)"
      found=1
    done < <({
      # Prose claims.
      strip_fences < "$file" \
        | grep -oiE "not yet (built|implemented)|designed but[ a-z]{0,12}(built|implemented)|coming soon|planned for a future[ a-z]*|in a future iteration"
      # Roadmap headings ("## Future", "### Future Enhancements"). Only as a
      # heading: "in the future" in a sentence is ordinary English.
      strip_fences < "$file" | grep -oiE "^#{1,6} .*\bfuture\b.*"
      # Status markers wearing an emoji instead of a sentence.
      strip_fences < "$file" | grep -oE "⛔|🚧"
    } | sort -u)
  done < <(md_files | grep -vE "^(docs/(adr|prd)/|\.claude/)")
  [ "$found" -eq 0 ] && ok "no build-status claims in living docs"
}

# ── prose ────────────────────────────────────────────────────────────────────
# House style: punctuation serves grammar, not drama. Use a comma, a full stop,
# or a colon. Code blocks are exempt; so are ADRs and PRDs.
check_prose() {
  head_ "Checking prose punctuation (living docs)..."
  local found=0
  while IFS= read -r file; do
    local n; n=$(strip_fences < "$file" | grep -c '—' || true)
    if [ "$n" -gt 0 ]; then
      local line; line=$(strip_fences < "$file" | grep -n '—' | head -1 | cut -d: -f1)
      fail "$file: $n em dash(es), first near line $line. Use a comma, full stop, or colon."
      found=1
    fi
  done < <(md_files | grep -vE '^(docs/(adr|prd)/|\.claude/)')  # .claude/ is agent prompts
  [ "$found" -eq 0 ] && ok "no em dashes in living docs"
}

case "${1:-all}" in
  links)    check_links ;;
  indexes)  check_indexes ;;
  targets)  check_targets ;;
  binaries) check_binaries ;;
  counts)   check_counts ;;
  status)   check_status ;;
  prose)    check_prose ;;
  all)      check_links; check_indexes; check_targets; check_binaries; check_counts; check_status; check_prose ;;
  *)        echo "usage: $0 [links|indexes|targets|binaries|counts|status|prose]" >&2; exit 2 ;;
esac

echo
if [ "$FAILURES" -gt 0 ]; then
  printf "%s\n" "${C_RED}✗ $FAILURES documentation issue(s)${C_RESET}"
  exit 1
fi
printf "%s\n" "${C_GREEN}✓ Documentation checks passed${C_RESET}"
