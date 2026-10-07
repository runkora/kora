#!/usr/bin/env bash
# =============================================================================
# refresh-bedrock-creds.sh: register local Bedrock providers from your AWS SSO
# session.
#
# Usage:
#   make cluster-bedrock-creds        # loads AWS_PROFILE and BEDROCK_* from .env
#
# .env sets AWS_PROFILE, BEDROCK_ACCOUNTS (comma-separated aliases) and
# BEDROCK_<ALIAS>_REGION per alias.
#
# What it does:
#   1. Logs in to AWS SSO if the session has expired.
#   2. Writes the session into Secret orbit-bedrock-credentials.
#   3. Applies one InferenceProvider per alias, named after it.
# The inference gateway watches both and reloads without a restart. Re-run it
# whenever Bedrock returns auth errors.
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

step() { echo -e "\n${YELLOW}▶ $*${RESET}"; }
ok()   { echo -e "${GREEN}✓ $*${RESET}"; }
fail() { echo -e "${RED}✗ $*${RESET}"; exit 1; }

NAMESPACE="${ORBIT_SYSTEM_NAMESPACE:-orbit-system}"
SECRET=orbit-bedrock-credentials

[[ -n "${AWS_PROFILE:-}" ]] || fail "AWS_PROFILE is not set; add AWS_PROFILE=<your-profile> to .env"
[[ -n "${BEDROCK_ACCOUNTS:-}" ]] || fail "BEDROCK_ACCOUNTS is not set; add BEDROCK_ACCOUNTS=<alias,...> to .env"

step "Checking AWS SSO session for profile '${AWS_PROFILE}'"
if ! aws sts get-caller-identity --profile "${AWS_PROFILE}" &>/dev/null; then
  echo "  Session expired or not found; launching SSO login..."
  aws sso login --profile "${AWS_PROFILE}"
fi
CREDS=$(aws configure export-credentials --profile "${AWS_PROFILE}" --format env-no-export) \
  || fail "Failed to export credentials from profile '${AWS_PROFILE}'"
ACCESS_KEY=$(echo "${CREDS}"    | grep '^AWS_ACCESS_KEY_ID'     | cut -d= -f2-)
SECRET_KEY=$(echo "${CREDS}"    | grep '^AWS_SECRET_ACCESS_KEY' | cut -d= -f2-)
SESSION_TOKEN=$(echo "${CREDS}" | grep '^AWS_SESSION_TOKEN'     | cut -d= -f2- || echo "")

step "Writing Secret ${SECRET} in ${NAMESPACE}"
SECRET_ARGS=(--from-literal=accessKeyID="${ACCESS_KEY}" --from-literal=secretAccessKey="${SECRET_KEY}")
[[ -n "${SESSION_TOKEN}" ]] && SECRET_ARGS+=(--from-literal=sessionToken="${SESSION_TOKEN}")
kubectl create secret generic "${SECRET}" -n "${NAMESPACE}" "${SECRET_ARGS[@]}" --dry-run=client -o yaml \
  | kubectl label --local -f - orbit.io/inference-provider=true -o yaml \
  | kubectl apply -f -

step "Applying Bedrock InferenceProviders"
IFS=',' read -ra ALIASES <<< "${BEDROCK_ACCOUNTS}"
for RAW in "${ALIASES[@]}"; do
  ALIAS="${RAW// /}"
  [[ -z "${ALIAS}" ]] && continue
  REGION_VAR="BEDROCK_${ALIAS^^}_REGION"
  REGION="${!REGION_VAR:-}"
  [[ -n "${REGION}" ]] || fail "${REGION_VAR} is not set in .env"
  kubectl apply -f - <<YAML
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: ${ALIAS}
  namespace: ${NAMESPACE}
spec:
  type: bedrock
  bedrock:
    region: ${REGION}
    credentials:
      secretRef:
        name: ${SECRET}
YAML
done

ok "Bedrock providers registered: ${BEDROCK_ACCOUNTS}"
