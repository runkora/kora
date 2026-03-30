#!/usr/bin/env bash
# =============================================================================
# dev-cluster-down.sh — Tear down the Orbit local k3d development cluster
#
# ┌─────────────────────────────────────────────────────────────────────────┐
# │  IMPORTANT: This script only tears down Tier 2 (the k3d cluster).      │
# │                                                                         │
# │  Tier 1 resources (postgres, redis, orbit-network) managed by          │
# │  Docker Compose are intentionally left running. This lets you           │
# │  recreate the cluster without losing your database state.               │
# │                                                                         │
# │  To also stop Tier 1 services, run afterwards:                         │
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
else
  warn "Cluster '${CLUSTER_NAME}' not found — nothing to do"
fi

echo ""
echo -e "${YELLOW}Note: Tier 1 (postgres, redis, orbit-network) is still running.${RESET}"
echo "  Run 'make dev-down' to stop Docker Compose services."
echo ""
