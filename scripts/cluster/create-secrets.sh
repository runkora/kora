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
#   REDIS_PASSWORD           Redis password           (orbit-system only)
#   BFF_SESSION_SECRET       Frontend session secret  (orbit-system only, min 32 chars)
#   ORBIT_TRIGGERS_ENCRYPTION_KEY  At-rest key for webhook signing secrets (base64, 32 bytes)
#   ORBIT_OIDC_CLIENT_SECRET Dex client secret        (optional — public client)
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
  [[ -n "${BFF_SESSION_SECRET:-}" ]] \
    || fail "BFF_SESSION_SECRET is not set. 'make setup' generates it into .env"
  [[ ${#BFF_SESSION_SECRET} -ge 32 ]] \
    || fail "BFF_SESSION_SECRET must be at least 32 characters (got ${#BFF_SESSION_SECRET})"
  # orbit-triggers refuses to start without this: it encrypts every webhook
  # source's signing secret at rest, and there is deliberately no in-code
  # fallback to boot without one.
  [[ -n "${ORBIT_TRIGGERS_ENCRYPTION_KEY:-}" ]] \
    || fail "ORBIT_TRIGGERS_ENCRYPTION_KEY is not set. 'make setup' generates it into .env"
  [[ $(printf %s "${ORBIT_TRIGGERS_ENCRYPTION_KEY}" | base64 -d 2>/dev/null | wc -c) -eq 32 ]] \
    || fail "ORBIT_TRIGGERS_ENCRYPTION_KEY must be base64 of exactly 32 bytes (openssl rand -base64 32)"
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
    --from-literal=ORBIT_TRIGGERS_ENCRYPTION_KEY="${ORBIT_TRIGGERS_ENCRYPTION_KEY}"
  )
  if [[ -n "${ORBIT_OIDC_CLIENT_SECRET:-}" ]]; then
    IDENTITY_SECRET_ARGS+=(--from-literal=ORBIT_OIDC_CLIENT_SECRET="${ORBIT_OIDC_CLIENT_SECRET}")
    ok "ORBIT_OIDC_CLIENT_SECRET included (confidential client)"
  else
    warn "ORBIT_OIDC_CLIENT_SECRET is empty — skipping (public client mode)"
  fi

  # AWS Bedrock credentials — pulled from a named AWS profile when BEDROCK_ENABLED=true
  # and AWS_PROFILE is set. Falls back to raw key vars, then skips (IRSA or disabled).
  if [[ "${BEDROCK_ENABLED:-}" == "true" ]]; then
    if [[ -n "${AWS_PROFILE:-}" ]]; then
      step "Exporting Bedrock credentials from AWS profile '${AWS_PROFILE}'"
      if ! aws sts get-caller-identity --profile "${AWS_PROFILE}" &>/dev/null; then
        echo "  Session expired — launching SSO login..."
        aws sso login --profile "${AWS_PROFILE}"
      fi
      CREDS=$(aws configure export-credentials --profile "${AWS_PROFILE}" --format env-no-export) \
        || fail "Failed to export credentials from profile '${AWS_PROFILE}'"
      AWS_ACCESS_KEY_ID=$(echo "${CREDS}"    | grep '^AWS_ACCESS_KEY_ID'     | cut -d= -f2-)
      AWS_SECRET_ACCESS_KEY=$(echo "${CREDS}" | grep '^AWS_SECRET_ACCESS_KEY' | cut -d= -f2-)
      AWS_SESSION_TOKEN=$(echo "${CREDS}"    | grep '^AWS_SESSION_TOKEN'     | cut -d= -f2- || echo "")
    fi

    if [[ -n "${AWS_ACCESS_KEY_ID:-}" && -n "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
      IDENTITY_SECRET_ARGS+=(--from-literal=AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID}")
      IDENTITY_SECRET_ARGS+=(--from-literal=AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY}")
      [[ -n "${AWS_SESSION_TOKEN:-}" ]] && \
        IDENTITY_SECRET_ARGS+=(--from-literal=AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN}")
      ok "AWS Bedrock credentials included in orbit-secrets"
    else
      warn "BEDROCK_ENABLED=true but no credentials found — set AWS_PROFILE or AWS_ACCESS_KEY_ID in .env"
    fi
  else
    warn "BEDROCK_ENABLED not set — skipping Bedrock credentials (set BEDROCK_ENABLED=true in .env to enable)"
  fi

  create_secret "orbit-system" "${IDENTITY_SECRET_ARGS[@]}"

  # orbit-frontend-secrets — BFF iron-session cookie signing key.
  # Kept separate from orbit-secrets so the identity service pod does not receive
  # unrelated frontend credentials via its envFrom secretRef.
  kubectl create secret generic orbit-frontend-secrets \
    --namespace orbit-system \
    --from-literal=BFF_SESSION_SECRET="${BFF_SESSION_SECRET}" \
    --dry-run=client -o yaml | kubectl apply -f -

  ok "orbit-frontend-secrets in orbit-system"
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
  echo "  orbit-system         → orbit-secrets (DB + Redis + signing key) + orbit-frontend-secrets (BFF session)"
  echo "  orbit-tenant-example → DB only"
else
  echo -e "${GREEN}  Secrets applied to ${TARGET_NAMESPACE}${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
fi
echo ""
