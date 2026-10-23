#!/usr/bin/env bash
# =============================================================================
# kora Device Authorization Grant — CLI Authentication Script
# =============================================================================
#
# Usage: ./scripts/auth-device.sh [OPTIONS]
#
# Authenticates against the kora identity service using the OAuth 2.0 Device
# Authorization Grant (RFC 8628). No browser on this machine is required — you
# just need to visit a URL and enter a short code on any device.
#
# Options:
#   --tenant-id <id>     Tenant ID for the session (required)
#   --api-base <url>     API base URL (default: http://localhost:8080/api/v1)
#   --help, -h           Show this help message and exit
#
# Environment Variables:
#   KORA_TENANT_ID      Default tenant ID if --tenant-id not provided
#
# ─── Local dev (default) ──────────────────────────────────────────────────────
#
#   Dex is started automatically by `make up`.
#   No extra environment variables needed — just run:
#
#     make auth
#
#   or directly:
#
#     ./scripts/auth-device.sh --tenant-id example
#
#   Test user:  dev@orbit.local.clearroute.io / password
#   Client ID:  orbit-local  (public client, no secret)
#
# ─── Real provider (Entra, Okta, …) ──────────────────────────────────────────
#
#   OIDC config is per-tenant — configure it in config/tenants.yaml (local dev)
#   or via the OrbitTenant CR spec.oidc.* fields (cluster). Each tenant declares
#   its own issuerURL, clientID, and clientSecretEnv. There are no global
#   KORA_OIDC_* environment variables.
#
#   Example OrbitTenant OIDC config:
#     spec:
#       oidc:
#         issuerURL: https://login.microsoftonline.com/<tenant-id>/v2.0
#         clientID: <app-registration-client-id>
#         clientSecretEnv: KORA_TENANT_EXAMPLE_OIDC_CLIENT_SECRET
#         scopes: [openid, profile, email]
#
#   Then: make auth-device-k3d tenant=<tenant-name>
#
# ─── Environment variables ────────────────────────────────────────────────────
#
#   KORA_API_BASE    — kora API base URL  (default: http://localhost:8080/api/v1)
#   KORA_TENANT_ID   — kora tenant ID     (overridden by --tenant-id flag)
#
# Prerequisites: curl, jq
# =============================================================================

set -euo pipefail

# ─── Colours ──────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

# ─── Helpers ──────────────────────────────────────────────────────────────────

info()    { echo -e "${BLUE}ℹ${RESET}  $*"; }
success() { echo -e "${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "${YELLOW}⚠${RESET}  $*"; }
error()   { echo -e "${RED}✗${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }

# ─── Dependency check ─────────────────────────────────────────────────────────

for dep in curl jq; do
  command -v "$dep" >/dev/null 2>&1 || die "Required dependency not found: $dep"
done

# ─── Argument parsing ─────────────────────────────────────────────────────────

TENANT_ID="${KORA_TENANT_ID:-}"
API_BASE="${KORA_API_BASE:-http://localhost:8080/api/v1}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tenant-id)
      TENANT_ID="$2"
      shift 2
      ;;
    --api-base)
      API_BASE="$2"
      shift 2
      ;;
    --help|-h)
      sed -n '/^# Usage:/,/^# =====$/p' "$0" | grep -v '^# =====$' | sed 's/^# \?//'
      exit 0
      ;;
    *)
      die "Unknown argument: $1. Run with --help for usage."
      ;;
  esac
done

[[ -n "$TENANT_ID" ]] || die "Tenant ID is required. Pass --tenant-id <id> or set KORA_TENANT_ID."

# ─── Step 1: Initiate device flow ─────────────────────────────────────────────

echo ""
echo -e "${BOLD}kora Device Authentication${RESET}"
echo -e "API: ${BLUE}${API_BASE}${RESET}  Tenant: ${BLUE}${TENANT_ID}${RESET}"
echo ""
info "Initiating device authorization flow..."
echo ""

DEVICE_RESP=$(curl -s \
  -X POST "${API_BASE}/identity/device" \
  -H "Content-Type: application/json" \
  -d "{\"tenant_id\": \"${TENANT_ID}\"}" \
  2>&1)

# Check if curl itself failed (network/DNS error)
if [ $? -ne 0 ]; then
  error "Failed to reach kora API at ${API_BASE}"
  echo ""
  echo "  Is the stack running?  make up"
  echo ""
  echo "  Dex (local OIDC) is started by make up. Check it is healthy:"
  echo "    docker compose ps dex"
  echo ""
  exit 1
fi

# Check for API-level error response
if echo "$DEVICE_RESP" | jq -e '.code' >/dev/null 2>&1; then
  ERR_CODE=$(echo "$DEVICE_RESP" | jq -r '.code')
  ERR_MSG=$(echo "$DEVICE_RESP" | jq -r '.message // "unknown error"')
  echo ""
  error "kora returned an error: ${ERR_CODE}"
  echo "  ${ERR_MSG}"
  echo ""
  if [[ "$ERR_CODE" == "oidc_not_configured" ]]; then
    echo "  The tenant '${TENANT_ID}' is not registered or has no OIDC config."
    echo ""
    echo "  ── Local dev ──────────────────────────────────────────────────"
    echo "  1. Ensure Dex is running:   make dev-up"
    echo "  2. Ensure the tenant CR is applied and the operator has reconciled it:"
    echo "       kubectl apply -f deploy/local/tenants/example.yaml"
    echo "       kubectl get orbittenant -A"
    echo "  3. Check the tenant registry ConfigMap:"
    echo "       kubectl get configmap orbit-tenants -n orbit-system -o yaml"
    echo ""
    echo "  ── Cluster (k3d) ──────────────────────────────────────────────"
    echo "  Run 'make cluster-deploy' to register tenants, or apply the"
    echo "  OrbitTenant CR directly:"
    echo "       kubectl apply -f deploy/local/tenants/<tenant>.yaml"
    echo ""
    echo "  ── Real provider (Entra, Okta, …) ────────────────────────────"
    echo "  OIDC config is per-tenant in config/tenants.yaml (local dev)"
    echo "  or via OrbitTenant CR spec.oidc.* (cluster)."
    echo "  There are no global KORA_OIDC_* env vars — configure per tenant."
    echo ""
  fi
  exit 1
fi

DEVICE_CODE=$(echo "$DEVICE_RESP"    | jq -r '.device_code')
USER_CODE=$(echo "$DEVICE_RESP"      | jq -r '.user_code')
VERIFY_URI=$(echo "$DEVICE_RESP"     | jq -r '.verification_uri')
EXPIRES_IN=$(echo "$DEVICE_RESP"     | jq -r '.expires_in // 300')
POLL_INTERVAL=$(echo "$DEVICE_RESP"  | jq -r '.interval // 5')

[[ "$DEVICE_CODE" != "null" && -n "$DEVICE_CODE" ]] || die "Unexpected response — missing device_code: $DEVICE_RESP"

# ─── Step 2: Display action prompt ────────────────────────────────────────────

echo -e "  ${BOLD}Action required${RESET}"
echo ""
echo -e "  1. Open:  ${BLUE}${VERIFY_URI}${RESET}"
echo ""
echo -e "  2. Enter: ${BOLD}${GREEN}${USER_CODE}${RESET}"
echo ""
echo -e "  Expires in ${EXPIRES_IN}s — polling every ${POLL_INTERVAL}s"
echo ""

# Auto-open the verification URL in the default browser (best-effort).
if command -v open >/dev/null 2>&1; then
  open "$VERIFY_URI" 2>/dev/null || true
elif command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$VERIFY_URI" 2>/dev/null || true
fi

# ─── Step 3: Poll until authorised ────────────────────────────────────────────

ELAPSED=0

while true; do
  sleep "$POLL_INTERVAL"
  ELAPSED=$((ELAPSED + POLL_INTERVAL))

  if [[ $ELAPSED -ge $EXPIRES_IN ]]; then
    echo ""
    die "Device code expired after ${EXPIRES_IN}s. Run the script again to start a new flow."
  fi

  POLL_HTTP_CODE=$(curl -s -o /tmp/kora_poll_resp.json -w "%{http_code}" \
    -X POST "${API_BASE}/identity/session" \
    -H "Content-Type: application/json" \
    -d "{\"grant_type\": \"urn:ietf:params:oauth:grant-type:device_code\", \"device_code\": \"${DEVICE_CODE}\", \"tenant_id\": \"${TENANT_ID}\"}")

  POLL_RESP=$(cat /tmp/kora_poll_resp.json)
  chmod 600 /tmp/kora_poll_resp.json

  if [[ "$POLL_HTTP_CODE" == "202" ]]; then
    CODE=$(echo "$POLL_RESP" | jq -r '.code // ""')
    if [[ "$CODE" == "slow_down" ]]; then
      warn "Polling too fast — increasing interval by 5s"
      POLL_INTERVAL=$((POLL_INTERVAL + 5))
    fi
    # authorization_pending or slow_down — keep waiting
    echo -ne "\r  Waiting for authorisation... (${ELAPSED}s elapsed)  "
    continue
  fi

  if [[ "$POLL_HTTP_CODE" == "200" ]]; then
    # Success — extract and display the platform token
    ACCESS_TOKEN=$(echo "$POLL_RESP" | jq -r '.access_token // ""')
    REFRESH_TOKEN=$(echo "$POLL_RESP" | jq -r '.refresh_token // ""')
    EXPIRES=$(echo "$POLL_RESP" | jq -r '.expires_in // ""')

    echo ""
    echo ""
    success "Authenticated successfully!"
    echo ""

    if [[ -n "$ACCESS_TOKEN" ]]; then
      echo -e "  ${BOLD}Access token${RESET} (expires in ${EXPIRES}s):"
      echo ""
      echo "  $ACCESS_TOKEN"
      echo ""
    fi

    if [[ -n "$REFRESH_TOKEN" ]]; then
      echo -e "  ${BOLD}Refresh token${RESET} (store securely):"
      echo ""
      echo "  $REFRESH_TOKEN"
      echo ""
    fi

    # Optionally export for use in subsequent curl calls
    echo -e "  ${YELLOW}To use in curl:${RESET}"
    echo ""
    echo "    export KORA_TOKEN=${ACCESS_TOKEN}"
    echo "    curl -H \"Authorization: Bearer \$KORA_TOKEN\" ${API_BASE}/..."
    echo ""

    # Write to a temp credentials file for convenience
    CREDS_FILE="/tmp/kora_credentials.json"
    echo "$POLL_RESP" > "$CREDS_FILE"
    chmod 600 "$CREDS_FILE"
    info "Full response saved to ${CREDS_FILE} (mode 600)"
    echo ""

    exit 0
  fi

  # Any other status is a terminal error
  echo ""
  ERR_CODE=$(echo "$POLL_RESP" | jq -r '.error.code // "unknown"')
  ERR_MSG=$(echo "$POLL_RESP" | jq -r '.error.message // "unknown error"')
  echo ""
  die "Authentication failed (HTTP ${POLL_HTTP_CODE}): ${ERR_CODE} — ${ERR_MSG}"
done
