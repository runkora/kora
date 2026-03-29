#!/usr/bin/env bash
# =============================================================================
# create-secrets.sh — Create per-namespace orbit-secrets in the local cluster
#
# Usage:
#   bash create-secrets.sh                    # all namespaces
#   bash create-secrets.sh orbit-system       # platform secrets only
#   bash create-secrets.sh orbit-tenant-example  # tenant secrets only
#
# Each namespace block declares exactly the secrets it needs — no more.
# To add a new tenant namespace: add a create_secret block in the tenant
# section below.
#
# Secrets are applied via --dry-run=client | kubectl apply so the command is
# idempotent: safe to re-run on every cluster-deploy.
#
# Required environment variables (loaded from .env by the Makefile):
#   DB_PASSWORD              Postgres password
#   REDIS_PASSWORD           Redis password       (orbit-system only)
#   ORBIT_OIDC_CLIENT_SECRET Dex client secret    (optional — public client)
#
# Required files:
#   secrets/signing.key      RSA private key for JWT signing (orbit-system only)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SECRETS_DIR="${REPO_ROOT}/secrets"

# Optional: restrict to a single namespace. Empty = all namespaces.
TARGET_NAMESPACE="${1:-}"

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

# should_run <namespace> — returns 0 if this block should execute
should_run() {
  [[ -z "${TARGET_NAMESPACE}" || "${TARGET_NAMESPACE}" == "$1" ]]
}

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

[[ -n "${DB_PASSWORD:-}" ]] || fail "DB_PASSWORD is not set"

if should_run "orbit-system"; then
  [[ -f "${SECRETS_DIR}/signing.key" ]] \
    || fail "secrets/signing.key not found — run 'make gen-signing-key' first"
  [[ -n "${REDIS_PASSWORD:-}" ]] \
    || fail "REDIS_PASSWORD is not set (required for orbit-system)"
fi

ok "Prerequisites satisfied"

# ── orbit-system (identity service) ──────────────────────────────────────────
# Needs: Postgres, Redis, RSA signing key.
# ORBIT_OIDC_CLIENT_SECRET is optional — Dex is configured as a public client
# for Device Grant so no secret is required. Include it only when set.
if should_run "orbit-system"; then
  step "Creating secrets for orbit-system"

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
fi

# ── orbit-tenant-example (example agent) ─────────────────────────────────────
# Needs: Postgres only — no Redis, no OIDC client, no signing key.
if should_run "orbit-tenant-example"; then
  step "Creating secrets for orbit-tenant-example"
  create_secret "orbit-tenant-example" \
    --from-literal=DB_PASSWORD="${DB_PASSWORD}"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
if [[ -z "${TARGET_NAMESPACE}" ]]; then
  echo -e "${GREEN}  Secrets applied to all namespaces               ${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
  echo ""
  echo "  orbit-system         → DB + Redis + signing key (+ OIDC secret if set)"
  echo "  orbit-tenant-example → DB only"
else
  echo -e "${GREEN}  Secrets applied to ${TARGET_NAMESPACE}${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
fi
echo ""
