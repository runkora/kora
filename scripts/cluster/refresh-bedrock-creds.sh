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
#   3. Writes the live credentials into Secret orbit-bedrock-credentials.
#   4. Annotates each Bedrock InferenceProvider so the operator re-renders it.
#      The inference gateway reloads without a restart.
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

# ── Patch orbit-bedrock-credentials ───────────────────────────────────────────

step "Patching orbit-bedrock-credentials in orbit-system"

SECRET_ARGS=(--from-literal=accessKeyID="${ACCESS_KEY}" --from-literal=secretAccessKey="${SECRET_KEY}")
[[ -n "${SESSION_TOKEN}" ]] && SECRET_ARGS+=(--from-literal=sessionToken="${SESSION_TOKEN}")
kubectl create secret generic orbit-bedrock-credentials -n orbit-system \
  "${SECRET_ARGS[@]}" --dry-run=client -o yaml | kubectl apply -f -

ok "orbit-bedrock-credentials updated"

# ── Re-render the providers ───────────────────────────────────────────────────
# The operator does not watch Secrets. An annotation change on each Bedrock
# InferenceProvider makes it re-render the aggregate now. The gateway reloads
# the mounted aggregate without a restart.

step "Re-rendering Bedrock InferenceProviders"

NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
PROVIDERS=$(kubectl get inferenceproviders -n orbit-system \
  -o jsonpath='{range .items[?(@.spec.type=="bedrock")]}{.metadata.name}{"\n"}{end}')
[[ -n "${PROVIDERS}" ]] || fail "no Bedrock InferenceProvider in orbit-system; run make up with BEDROCK_ACCOUNTS set in .env"
for NAME in ${PROVIDERS}; do
  kubectl annotate inferenceprovider "${NAME}" -n orbit-system --overwrite \
    "orbit.io/credentials-refreshed-at=${NOW}" >/dev/null
done

ok "Bedrock credentials refreshed for: $(echo ${PROVIDERS} | tr ' ' ',')"
