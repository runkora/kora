#!/usr/bin/env bash
# =============================================================================
# create-secrets.sh — Create per-namespace orbit-secrets in the local cluster
#
# Each namespace block declares exactly the secrets it needs — no more.
# To add a new service namespace: add a create_secret block below.
#
# Secrets are applied via --dry-run=client | kubectl apply so the command is
# idempotent: safe to re-run on every cluster-deploy.
#
# Required environment variables (loaded from .env by the Makefile):
#   DB_PASSWORD              Postgres password
#   REDIS_PASSWORD           Redis password       (identity namespace only)
#   ORBIT_OIDC_CLIENT_SECRET Dex client secret    (optional — public client)
#
# Required files:
#   secrets/signing.key      RSA private key for JWT signing (identity only)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SECRETS_DIR="${REPO_ROOT}/secrets"

# Terminal colours
GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RED="\033[31m"
RESET="\033[0m"

step() { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()   { echo -e "${GREEN}✓ $*${RESET}"; }
warn() { echo -e "${YELLOW}⚠ $*${RESET}"; }
fail() { echo -e "${RED}✗ $*${RESET}"; exit 1; }

# =============================================================================
# create_secret <namespace> [kubectl secret flags...]
#
# Wraps kubectl create secret generic orbit-secrets with --dry-run=client so
# the apply is fully idempotent — creates on first run, patches on subsequent.
# =============================================================================
create_secret() {
  local namespace="$1"
  shift  # remaining args are --from-literal / --from-file flags

  kubectl create secret generic orbit-secrets \
    --namespace "${namespace}" \
    "$@" \
    --dry-run=client -o yaml | kubectl apply -f -

  ok "orbit-secrets in ${namespace}"
}

# ── Preflight ─────────────────────────────────────────────────────────────────
step "Checking prerequisites"

[[ -f "${SECRETS_DIR}/signing.key" ]] \
  || fail "secrets/signing.key not found — run 'make gen-signing-key' first"

[[ -n "${DB_PASSWORD:-}" ]] || fail "DB_PASSWORD is not set"

ok "Prerequisites satisfied"

# ── orbit-system (identity service) ──────────────────────────────────────────
# Needs: Postgres, Redis, RSA signing key.
# ORBIT_OIDC_CLIENT_SECRET is optional — Dex is configured as a public client
# for Device Grant so no secret is required. Include it only when set.
step "Creating secrets for orbit-system"

[[ -n "${REDIS_PASSWORD:-}" ]] || fail "REDIS_PASSWORD is not set (required for orbit-system)"

IDENTITY_SECRET_ARGS=(
  --from-literal=DB_PASSWORD="${DB_PASSWORD}"
  --from-literal=REDIS_PASSWORD="${REDIS_PASSWORD}"
  --from-file=ORBIT_TOKEN_SIGNING_KEY="${SECRETS_DIR}/signing.key"
)
if [[ -n "${ORBIT_OIDC_CLIENT_SECRET:-}" ]]; then
  IDENTITY_SECRET_ARGS+=(--from-literal=ORBIT_OIDC_CLIENT_SECRET="${ORBIT_OIDC_CLIENT_SECRET}")
  ok "ORBIT_OIDC_CLIENT_SECRET included (confidential client)"
else
  warn "ORBIT_OIDC_CLIENT_SECRET is empty — skipping (public client mode)"
fi

create_secret "orbit-system" "${IDENTITY_SECRET_ARGS[@]}"

# ── orbit-example-tenant (example agent) ─────────────────────────────────────
# Needs: Postgres only — no Redis, no OIDC client, no signing key
step "Creating secrets for orbit-example-tenant"
create_secret "orbit-example-tenant" \
  --from-literal=DB_PASSWORD="${DB_PASSWORD}"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  Secrets applied to all namespaces               ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  orbit-system          → DB + Redis + signing key (+ OIDC secret if set)"
echo "  orbit-example-tenant  → DB only"
echo ""
