#!/usr/bin/env bash
# =============================================================================
# dev-cluster-down.sh — Tear down the Orbit local k3d development cluster
# =============================================================================
set -euo pipefail

CLUSTER_NAME="orbit-local"

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step() { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()   { echo -e "${GREEN}✓ $*${RESET}"; }
warn() { echo -e "${YELLOW}⚠ $*${RESET}"; }

step "Deleting k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
  k3d cluster delete "${CLUSTER_NAME}"
  ok "Cluster deleted"
else
  warn "Cluster '${CLUSTER_NAME}' not found — nothing to do"
fi
