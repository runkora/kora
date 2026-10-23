#!/usr/bin/env bash
# =============================================================================
# create-secrets.sh — Create per-namespace kora-secrets in the local cluster
#
# Usage:
#   bash create-secrets.sh                    # all namespaces
#   bash create-secrets.sh kora-system       # platform secrets only
#   bash create-secrets.sh kora-tenant-example  # tenant secrets only
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
#   REDIS_PASSWORD           Redis password           (kora-system only)
#   BFF_SESSION_SECRET       Frontend session secret  (kora-system only, min 32 chars)
#   KORA_TRIGGERS_ENCRYPTION_KEY  At-rest key for webhook signing secrets (base64, 32 bytes)
#   KORA_OIDC_CLIENT_SECRET Dex client secret        (optional — public client)
#
# Required files:
#   secrets/signing.key      RSA private key for JWT signing (kora-system only)
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
# Wraps kubectl create secret generic kora-secrets with --dry-run=client so
# the apply is fully idempotent — creates on first run, patches on subsequent.
# =============================================================================
create_secret() {
  local namespace="$1"
  shift  # remaining args are --from-literal / --from-file flags

  kubectl create secret generic kora-secrets \
    --namespace "${namespace}" \
    "$@" \
    --dry-run=client -o yaml | kubectl apply -f -

  ok "kora-secrets in ${namespace}"
}

# ── Preflight ─────────────────────────────────────────────────────────────────
step "Checking prerequisites"

[[ -n "${DB_PASSWORD:-}" ]] || fail "DB_PASSWORD is not set"

if should_run "kora-system"; then
  [[ -f "${SECRETS_DIR}/signing.key" ]] \
    || fail "secrets/signing.key not found — run 'make gen-signing-key' first"
  [[ -n "${REDIS_PASSWORD:-}" ]] \
    || fail "REDIS_PASSWORD is not set (required for kora-system)"
  [[ -n "${BFF_SESSION_SECRET:-}" ]] \
    || fail "BFF_SESSION_SECRET is not set. 'make setup' generates it into .env"
  [[ ${#BFF_SESSION_SECRET} -ge 32 ]] \
    || fail "BFF_SESSION_SECRET must be at least 32 characters (got ${#BFF_SESSION_SECRET})"
  # kora-triggers refuses to start without this: it encrypts every webhook
  # source's signing secret at rest, and there is deliberately no in-code
  # fallback to boot without one.
  #
  # Generated and persisted here rather than failing, so a machine whose .env
  # predates this key does not stop mid-`make up`. It cannot come from `make
  # env`: the Makefile parses .env once at startup, so a key appended during the
  # same run is invisible to this script's environment.
  if [[ -z "${KORA_TRIGGERS_ENCRYPTION_KEY:-}" ]]; then
    ENV_FILE="${REPO_ROOT}/.env"
    if [[ -f "$ENV_FILE" ]] && grep -q '^KORA_TRIGGERS_ENCRYPTION_KEY=' "$ENV_FILE"; then
      KORA_TRIGGERS_ENCRYPTION_KEY=$(grep '^KORA_TRIGGERS_ENCRYPTION_KEY=' "$ENV_FILE" | cut -d= -f2-)
    else
      KORA_TRIGGERS_ENCRYPTION_KEY=$(openssl rand -base64 32)
      {
        printf '\n%s\n' "# At-rest key for webhook signing secrets, generated on first use."
        printf '%s\n' "KORA_TRIGGERS_ENCRYPTION_KEY=${KORA_TRIGGERS_ENCRYPTION_KEY}"
      } >> "$ENV_FILE"
      warn "generated KORA_TRIGGERS_ENCRYPTION_KEY into .env"
      warn "webhook sources created before this used a built-in dev key and can no longer be decrypted; rotate their secrets"
    fi
    export KORA_TRIGGERS_ENCRYPTION_KEY
  fi
  [[ $(printf %s "${KORA_TRIGGERS_ENCRYPTION_KEY}" | base64 -d 2>/dev/null | wc -c) -eq 32 ]] \
    || fail "KORA_TRIGGERS_ENCRYPTION_KEY must be base64 of exactly 32 bytes (openssl rand -base64 32)"
fi

ok "Prerequisites satisfied"

# ── kora-system (identity service) ──────────────────────────────────────────
# Needs: Postgres, Redis, RSA signing key.
# KORA_OIDC_CLIENT_SECRET is optional — Dex is configured as a public client
# for Device Grant so no secret is required. Include it only when set.
if should_run "kora-system"; then
  step "Creating secrets for kora-system"

  IDENTITY_SECRET_ARGS=(
    --from-literal=DB_PASSWORD="${DB_PASSWORD}"
    --from-literal=REDIS_PASSWORD="${REDIS_PASSWORD}"
    --from-file=KORA_TOKEN_SIGNING_KEY="${SECRETS_DIR}/signing.key"
    --from-literal=KORA_TRIGGERS_ENCRYPTION_KEY="${KORA_TRIGGERS_ENCRYPTION_KEY}"
  )
  if [[ -n "${KORA_OIDC_CLIENT_SECRET:-}" ]]; then
    IDENTITY_SECRET_ARGS+=(--from-literal=KORA_OIDC_CLIENT_SECRET="${KORA_OIDC_CLIENT_SECRET}")
    ok "KORA_OIDC_CLIENT_SECRET included (confidential client)"
  else
    warn "KORA_OIDC_CLIENT_SECRET is empty — skipping (public client mode)"
  fi

  create_secret "kora-system" "${IDENTITY_SECRET_ARGS[@]}"

  # kora-frontend-secrets — BFF iron-session cookie signing key.
  # The dashboard loads this Secret whole, named by the chart's frontend.existingSecret.
  kubectl create secret generic kora-frontend-secrets \
    --namespace kora-system \
    --from-literal=BFF_SESSION_SECRET="${BFF_SESSION_SECRET}" \
    --dry-run=client -o yaml | kubectl apply -f -

  ok "kora-frontend-secrets in kora-system"
fi

# ── kora-tenant-example (example agent) ─────────────────────────────────────
# Needs: Postgres only — no Redis, no OIDC client, no signing key.
if should_run "kora-tenant-example"; then
  step "Creating secrets for kora-tenant-example"
  create_secret "kora-tenant-example" \
    --from-literal=DB_PASSWORD="${DB_PASSWORD}"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
if [[ -z "${TARGET_NAMESPACE}" ]]; then
  echo -e "${GREEN}  Secrets applied to all namespaces               ${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
  echo ""
  echo "  kora-system         → kora-secrets (DB + Redis + signing key) + kora-frontend-secrets (BFF session)"
  echo "  kora-tenant-example → DB only"
else
  echo -e "${GREEN}  Secrets applied to ${TARGET_NAMESPACE}${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
fi
echo ""
