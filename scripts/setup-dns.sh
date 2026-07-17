#!/usr/bin/env bash
# =============================================================================
# setup-dns.sh — Verify DNS for *.orbit.local.clearroute.io
#
# With a real wildcard DNS record (*.orbit.local.clearroute.io → 127.0.0.1),
# no local DNS tooling is needed — the OS resolves it like any other domain.
#
# This script verifies that the DNS record is in place and reachable.
# If it isn't, it prints instructions for adding the record.
#
# Prerequisites (one-time, done in your DNS provider):
#   Type: A
#   Name: *.orbit.local
#   Value: 127.0.0.1
#   TTL: 60
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RED="\033[31m"
RESET="\033[0m"

info()  { echo -e "${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }
die()   { echo -e "${RED}✗ $*${RESET}" >&2; exit 1; }

APEX="orbit.local.clearroute.io"
WILDCARD_TEST="dex.orbit.local.clearroute.io"

info "Checking DNS for *.${APEX}..."

# Use a public resolver to bypass stale router/ISP caches during verification.
# The system resolver picks up changes once router TTLs expire naturally.
WILDCARD_IP=$(dig +short "${WILDCARD_TEST}" @8.8.8.8 2>/dev/null | head -1)
APEX_IP=$(dig +short "${APEX}" @8.8.8.8 2>/dev/null | head -1)

MISSING=()
[[ "${WILDCARD_IP}" != "127.0.0.1" ]] && MISSING+=("wildcard")
[[ "${APEX_IP}"     != "127.0.0.1" ]] && MISSING+=("apex")

if [[ ${#MISSING[@]} -eq 0 ]]; then
  ok "${APEX} → 127.0.0.1  ✓"
  ok "*.${APEX} → 127.0.0.1  ✓"
else
  echo ""
  echo -e "${RED}Missing DNS records in your clearroute.io provider:${RESET}"
  echo ""
  if [[ " ${MISSING[*]} " == *" wildcard "* ]]; then
    echo "  Type: A  |  Name: *.orbit.local  |  Value: 127.0.0.1  |  TTL: 60"
    echo "    covers: dex.orbit.local.clearroute.io, api.orbit.local.clearroute.io, ..."
  fi
  if [[ " ${MISSING[*]} " == *" apex "* ]]; then
    echo "  Type: A  |  Name: orbit.local     |  Value: 127.0.0.1  |  TTL: 60"
    echo "    covers: orbit.local.clearroute.io (frontend)"
  fi
  echo ""
  die "Add the missing record(s) above, then re-run: make setup-dns"
fi

echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  DNS for *.orbit.local.clearroute.io is ready   ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  No local DNS tooling required — resolves via public DNS."
echo "  The k3d cluster is patched automatically during: make up"
echo ""
echo "  Test:  dig +short dex.orbit.local.clearroute.io"
echo "  Dex:   https://dex.orbit.local.clearroute.io:5556/dex"
echo ""
