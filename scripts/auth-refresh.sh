#!/usr/bin/env bash
# =============================================================================
# kora Token Refresh — rotate the platform session in the credentials file
# =============================================================================
#
# Usage: ./scripts/auth-refresh.sh [OPTIONS]
#
# Exchanges the refresh_token stored in the credentials file for a fresh
# access/refresh token pair via POST {api-base}/identity/session, and writes
# the new pair back atomically.
#
# The credentials file is NEVER overwritten unless the response was validated
# to contain both a new access_token and refresh_token. (Piping curl straight
# into the credentials file destroys the refresh token whenever the request
# fails — that is exactly the failure mode this script exists to prevent.)
#
# Refresh tokens rotate on every successful exchange: the old refresh token is
# consumed and the response carries its replacement. Losing the response means
# re-authenticating from scratch with auth-device.sh.
#
# Note: the in-cluster Istio gateway does not admit unauthenticated calls to
# the session endpoint, so refreshing against https://api.orbit.local.… fails
# with an empty 403. Use `make auth-refresh-k3d`, which port-forwards to the
# identity service directly.
#
# Options:
#   --api-base <url>     API base URL (default: $KORA_API_BASE or http://localhost:8080/api/v1)
#   --tenant-id <id>     Tenant ID for the session (default: $KORA_TENANT_ID or example)
#   --creds <path>       Credentials file (default: /tmp/kora_credentials.json)
#   --help, -h           Show this help message and exit
#
# Prerequisites: curl, jq
# =============================================================================

set -euo pipefail

# ─── Colours ──────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;36m'
RESET='\033[0m'

info()    { echo -e "${BLUE}ℹ${RESET}  $*"; }
success() { echo -e "${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "${YELLOW}⚠${RESET}  $*"; }
error()   { echo -e "${RED}✗${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }

# ─── Dependency check ─────────────────────────────────────────────────────────

for dep in curl jq; do
  command -v "$dep" >/dev/null 2>&1 || die "Required dependency '$dep' not found in PATH"
done

# ─── Arguments ────────────────────────────────────────────────────────────────

API_BASE="${KORA_API_BASE:-http://localhost:8080/api/v1}"
TENANT_ID="${KORA_TENANT_ID:-example}"
CREDS_FILE="/tmp/kora_credentials.json"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --api-base)  API_BASE="$2";   shift 2 ;;
    --tenant-id) TENANT_ID="$2";  shift 2 ;;
    --creds)     CREDS_FILE="$2"; shift 2 ;;
    --help|-h)   sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)           die "Unknown option: $1 (see --help)" ;;
  esac
done

# ─── Load the current refresh token ───────────────────────────────────────────

[[ -f "$CREDS_FILE" ]] || die "No credentials file at $CREDS_FILE — run: make auth"

REFRESH_TOKEN=$(jq -r '.refresh_token // ""' "$CREDS_FILE" 2>/dev/null) ||
  die "Credentials file $CREDS_FILE is not valid JSON — run: make auth"
[[ -n "$REFRESH_TOKEN" ]] || die "No refresh_token in $CREDS_FILE — run: make auth"

# ─── Exchange it ──────────────────────────────────────────────────────────────

info "Refreshing session at ${API_BASE}/identity/session (tenant: ${TENANT_ID})"

BODY_FILE=$(mktemp /tmp/kora_refresh_resp.XXXXXX)
trap 'rm -f "$BODY_FILE"' EXIT
chmod 600 "$BODY_FILE"

HTTP_CODE=$(curl -s --retry 5 --retry-connrefused --retry-delay 1 \
  -o "$BODY_FILE" -w '%{http_code}' \
  -X POST "${API_BASE}/identity/session" \
  -H "Content-Type: application/json" \
  -d "{\"grant_type\": \"refresh_token\", \"refresh_token\": \"${REFRESH_TOKEN}\", \"tenant_id\": \"${TENANT_ID}\"}") ||
  die "Could not reach ${API_BASE} — is the cluster up? (make up)"

if [[ "$HTTP_CODE" != "200" ]]; then
  error "Refresh rejected (HTTP ${HTTP_CODE}): $(head -c 300 "$BODY_FILE")"
  error "Existing credentials left untouched at $CREDS_FILE"
  die   "If the refresh token has expired, re-authenticate with: make auth"
fi

# ─── Validate before touching the credentials file ────────────────────────────

NEW_ACCESS=$(jq -r '.access_token // ""' "$BODY_FILE" 2>/dev/null) || NEW_ACCESS=""
NEW_REFRESH=$(jq -r '.refresh_token // ""' "$BODY_FILE" 2>/dev/null) || NEW_REFRESH=""

[[ -n "$NEW_ACCESS" && -n "$NEW_REFRESH" ]] ||
  die "Response did not contain a new token pair — credentials left untouched at $CREDS_FILE"

# ─── Atomic write ─────────────────────────────────────────────────────────────

TMP_CREDS=$(mktemp "${CREDS_FILE}.XXXXXX")
cp "$BODY_FILE" "$TMP_CREDS"
chmod 600 "$TMP_CREDS"
mv "$TMP_CREDS" "$CREDS_FILE"

EXPIRES_IN=$(jq -r '.expires_in // ""' "$CREDS_FILE")
if [[ -n "$EXPIRES_IN" ]]; then
  success "Token refreshed — saved to $CREDS_FILE (access token expires in ${EXPIRES_IN}s)"
else
  success "Token refreshed — saved to $CREDS_FILE"
fi
