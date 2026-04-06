#!/usr/bin/env bash
# =============================================================================
# refresh-bedrock-creds.sh — Inject AWS credentials into the cluster for
#                             local Bedrock development.
#
# Usage:
#   make cluster-bedrock-creds        # recommended — loads AWS_PROFILE from .env
#   bash scripts/cluster/refresh-bedrock-creds.sh  # direct
#
# Requires AWS_PROFILE to be set (in .env or environment).
#
# What it does:
#   1. Checks if the current SSO session is still valid.
#   2. If not, runs `aws sso login` interactively to refresh it.
#   3. Exports the live credentials and patches them into orbit-secrets.
#   4. Restarts the inference gateway to pick up the new credentials.
#
# SSO sessions typically last 8–12 hours. Re-run this command any time
# you get Bedrock auth errors — it handles the login automatically.
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

step() { echo -e "\n${YELLOW}▶ $*${RESET}"; }
ok()   { echo -e "${GREEN}✓ $*${RESET}"; }
fail() { echo -e "${RED}✗ $*${RESET}"; exit 1; }

# ── Preflight ─────────────────────────────────────────────────────────────────

[[ -n "${AWS_PROFILE:-}" ]] \
  || fail "AWS_PROFILE is not set — add AWS_PROFILE=<your-profile> to .env"

# ── Ensure SSO session is valid ───────────────────────────────────────────────

step "Checking AWS SSO session for profile '${AWS_PROFILE}'"

if ! aws sts get-caller-identity --profile "${AWS_PROFILE}" &>/dev/null; then
  echo "  Session expired or not found — launching SSO login..."
  aws sso login --profile "${AWS_PROFILE}"
fi

ok "SSO session valid"

# ── Export credentials ────────────────────────────────────────────────────────

step "Reading credentials from profile '${AWS_PROFILE}'"

CREDS=$(aws configure export-credentials --profile "${AWS_PROFILE}" --format env-no-export) \
  || fail "Failed to export credentials from profile '${AWS_PROFILE}'"

ACCESS_KEY=$(echo "${CREDS}"    | grep '^AWS_ACCESS_KEY_ID'     | cut -d= -f2-)
SECRET_KEY=$(echo "${CREDS}"    | grep '^AWS_SECRET_ACCESS_KEY' | cut -d= -f2-)
SESSION_TOKEN=$(echo "${CREDS}" | grep '^AWS_SESSION_TOKEN'     | cut -d= -f2- || echo "")

ok "Credentials exported"

# ── Patch orbit-secrets ───────────────────────────────────────────────────────

step "Patching orbit-secrets in orbit-system"

kubectl patch secret orbit-secrets -n orbit-system \
  --type=json \
  -p="[
    {\"op\":\"add\",\"path\":\"/data/AWS_ACCESS_KEY_ID\",    \"value\":\"$(echo -n "${ACCESS_KEY}"    | base64)\"},
    {\"op\":\"add\",\"path\":\"/data/AWS_SECRET_ACCESS_KEY\",\"value\":\"$(echo -n "${SECRET_KEY}"    | base64)\"},
    {\"op\":\"add\",\"path\":\"/data/AWS_SESSION_TOKEN\",    \"value\":\"$(echo -n "${SESSION_TOKEN}" | base64)\"}
  ]"

ok "orbit-secrets patched"

# ── Restart inference gateway ─────────────────────────────────────────────────

step "Restarting inference gateway"

kubectl rollout restart deployment/orbit-inference-gateway -n orbit-system
kubectl rollout status deployment/orbit-inference-gateway -n orbit-system --timeout=60s

ok "Bedrock credentials refreshed — inference gateway is live"
