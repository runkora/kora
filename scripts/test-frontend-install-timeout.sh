#!/usr/bin/env bash
# Verifies `make frontend-install` fails fast, naming the registry, when Corepack stalls.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/nvm" "$tmp/bin"
echo 'nvm() { :; }' > "$tmp/nvm/nvm.sh"
printf '#!/bin/sh\n[ "$1" = install ] && sleep 30\nexit 0\n' > "$tmp/bin/corepack"
printf '#!/bin/sh\nsleep 30\n' > "$tmp/bin/pnpm"
chmod +x "$tmp/bin/corepack" "$tmp/bin/pnpm"

start=$SECONDS
set +e
out="$(cd "$root" && NVM_DIR="$tmp/nvm" PATH="$tmp/bin:$PATH" timeout 20 make frontend-install COREPACK_TIMEOUT=1 2>&1)"
status=$?
set -e

if [ "$status" -eq 0 ] || [ $((SECONDS - start)) -ge 15 ]; then
	echo "FAIL: frontend-install did not fail fast (status $status)"; echo "$out"; exit 1
fi
if ! grep -q "registry.npmjs.org" <<<"$out"; then
	echo "FAIL: error does not name the registry"; echo "$out"; exit 1
fi
echo "PASS"
