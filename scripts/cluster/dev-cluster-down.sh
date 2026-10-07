#!/usr/bin/env bash
# =============================================================================
# dev-cluster-down.sh — Tear down the kora local k3d development cluster
#
# ┌─────────────────────────────────────────────────────────────────────────┐
# │  IMPORTANT: This script only tears down the k3d cluster.               │
# │                                                                         │
# │  The backing services (postgres, redis, orbit-network) managed by      │
# │  Docker Compose are intentionally left running. This lets you           │
# │  recreate the cluster without losing your database state.               │
# │                                                                         │
# │  To also stop the backing services, run afterwards:                    │
# │    make dev-down        (stops postgres + redis)                        │
# │    make dev-down-clean  (stops postgres + redis and removes volumes)    │
# └─────────────────────────────────────────────────────────────────────────┘
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/bootstrap/k3d-config.yaml"

# Derive cluster name from k3d-config.yaml — same source as dev-cluster-up.sh.
CLUSTER_NAME="$(grep 'name:' "${K3D_CONFIG}" | head -1 | awk '{print $2}')"

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step() { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()   { echo -e "${GREEN}✓ $*${RESET}"; }
warn() { echo -e "${YELLOW}⚠ $*${RESET}"; }

step "Deleting k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list --no-headers | awk '{print $1}' | grep -qx "${CLUSTER_NAME}"; then
  k3d cluster delete "${CLUSTER_NAME}"
  ok "Cluster '${CLUSTER_NAME}' deleted"
  GENERATED="${REPO_ROOT}/deploy/reference/environments/local.generated.yaml"
  if [ -f "${GENERATED}" ] && command -v yq >/dev/null 2>&1; then
    yq -i 'del(.images)' "${GENERATED}"
    ok "Cleared recorded image digests from local.generated.yaml"
  fi
else
  warn "Cluster '${CLUSTER_NAME}' not found — nothing to do"
fi

echo ""
echo -e "${YELLOW}Note: postgres, redis and orbit-network are still running. Data is untouched.${RESET}"
echo "  Run 'make dev-down' to stop Docker Compose services."
echo ""
