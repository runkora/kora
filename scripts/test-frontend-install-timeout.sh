#!/usr/bin/env bash
# Verifies `make frontend-install` bounds Corepack registry calls and reports why it failed.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/nvm" "$tmp/bin"
echo 'nvm() { :; }' > "$tmp/nvm/nvm.sh"
printf '#!/bin/sh\nsleep 30\n' > "$tmp/bin/pnpm"
chmod +x "$tmp/bin/pnpm"

fail() { echo "FAIL: $1"; echo "$out"; exit 1; }

# run_install [make args...]; sets out, status.
run_install() {
	start=$SECONDS
	set +e
	out="$(cd "$root" && NVM_DIR="$tmp/nvm" PATH="$tmp/bin:$PATH" timeout 20 make frontend-install "${@}" 2>&1)"
	status=$?
	set -e
	elapsed=$((SECONDS - start))
}

# 1. Corepack stalls: exit 124, registry named, fails fast.
printf '#!/bin/sh\n[ "$1" = install ] && sleep 30\nexit 0\n' > "$tmp/bin/corepack"
chmod +x "$tmp/bin/corepack"
run_install COREPACK_TIMEOUT=1
{ [ "$status" -ne 0 ] && [ "$elapsed" -lt 15 ]; } || fail "stalled Corepack did not fail fast (status $status)"
grep -q "Timed out after 1s reaching https://registry.npmjs.org" <<<"$out" || fail "timeout error does not name the registry"

# 2. Corepack fails for another reason: not reported as a timeout.
printf '#!/bin/sh\n[ "$1" = install ] && exit 3\nexit 0\n' > "$tmp/bin/corepack"
run_install COREPACK_TIMEOUT=1
[ "$status" -ne 0 ] || fail "failing Corepack succeeded"
grep -q "Corepack failed with exit code 3" <<<"$out" || fail "non-timeout failure not reported"
! grep -q "Timed out" <<<"$out" || fail "non-timeout failure reported as timeout"

# 3. No timeout binary: brew hint, not exit 127.
run_install COREPACK_TIMEOUT=1 TIMEOUT_CMD=
[ "$status" -ne 0 ] || fail "missing timeout succeeded"
grep -q "brew install coreutils" <<<"$out" || fail "missing brew hint"
! grep -q "registry" <<<"$out" || fail "missing timeout reported as registry error"

echo "PASS"
