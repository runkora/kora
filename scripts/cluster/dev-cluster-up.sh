#!/usr/bin/env bash
# =============================================================================
# dev-cluster-up.sh — Bring up the Orbit local k3d development cluster
#
# This script creates the k3d cluster and patches CoreDNS.
# Orbit services are installed separately via `make cluster-deploy` (Helm).
#
# Prerequisites (install once via make install-tools-cluster):
#   - k3d    >= 5.6
#   - kubectl >= 1.29
#   - helm    >= 3.14
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/bootstrap/k3d-config.yaml"

# Derive cluster name directly from k3d-config.yaml so this script never
# diverges from what k3d actually names the cluster.
CLUSTER_NAME="$(grep 'name:' "${K3D_CONFIG}" | head -1 | awk '{print $2}')"

# Terminal colours
GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step()  { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }

# ── Preflight ─────────────────────────────────────────────────────────────
step "Checking prerequisites"
for cmd in k3d kubectl helm docker; do
  if ! command -v "${cmd}" &>/dev/null; then
    echo "  Missing: ${cmd}$([ "${cmd}" != "docker" ] && echo " — run 'make install-tools-cluster'" || echo "")"
    exit 1
  fi
done
ok "All prerequisites present"

# Check for postgres running
if ! docker inspect orbit-postgres &>/dev/null; then
  echo "  ERROR: orbit-postgres container not found."
  echo "  Start it first:  make dev-up   (or: docker compose up -d postgres)"
  exit 1
fi

# ── Create k3d cluster ────────────────────────────────────────────────────
step "Creating k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list | awk '{print $1}' | grep -qx "${CLUSTER_NAME}"; then
  warn "Cluster '${CLUSTER_NAME}' already exists — skipping creation"
else
  k3d cluster create --config "${K3D_CONFIG}"
  ok "Cluster created"
fi

# Use the k3d context — guard against it not yet being registered
if kubectl config get-contexts "k3d-${CLUSTER_NAME}" &>/dev/null; then
  kubectl config use-context "k3d-${CLUSTER_NAME}"
else
  echo "  ERROR: kubectl context 'k3d-${CLUSTER_NAME}' not found after cluster creation."
  exit 1
fi

# ── Patch CoreDNS for dex.orbit.local.clearroute.io → Docker bridge ────────────────────
# On the host Mac, dnsmasq resolves *.orbit.local.clearroute.io → 127.0.0.1.
# Inside k3d pods, 127.0.0.1 means the pod itself — not the host.
#
# The k3d Corefile already contains:
#   import /etc/coredns/custom/*.server
# which loads any *.server key from the coredns-custom ConfigMap as an
# additional server block. We write our orbit.local.clearroute.io zone there so k3d's
# own Corefile (and its NodeHosts block) is never touched or reconciled away.
step "Patching CoreDNS for orbit.local.clearroute.io hostnames"

DOCKER_BRIDGE_IP="$(docker network inspect orbit-network \
  --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || echo "")"

if [[ -z "${DOCKER_BRIDGE_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-network gateway."
  echo "  Is orbit-network created?  Run:  make dev-up"
  exit 1
fi

# Resolve Dex container IP dynamically — Docker assigns IPs from the subnet
# pool and they vary across machines and restarts.
DEX_IP="$(docker inspect orbit-dex \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || echo "")"
if [[ -z "${DEX_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-dex container IP."
  echo "  Is Dex running?  Run:  make dev-up"
  exit 1
fi

ok "Docker bridge gateway: ${DOCKER_BRIDGE_IP}, Dex: ${DEX_IP}"

# The key name must end in .server so CoreDNS imports it as a server block.
# orbit.local.clearroute.io and api.orbit.local.clearroute.io resolve to the gateway (Istio VirtualService
# health checks). dex.orbit.local.clearroute.io resolves to the Dex container IP directly
# because Dex isn't reachable via the gateway — only via its container IP on
# orbit-network.
ORBIT_SERVER_BLOCK="orbit.local.clearroute.io:53 {
    hosts {
      ${DEX_IP} dex.orbit.local.clearroute.io
      ${DOCKER_BRIDGE_IP} orbit.local.clearroute.io
      ${DOCKER_BRIDGE_IP} api.orbit.local.clearroute.io
      fallthrough
    }
    forward . /etc/resolv.conf
}
"

kubectl create configmap coredns-custom \
  --namespace kube-system \
  --from-literal=orbit.local.clearroute.io.server="${ORBIT_SERVER_BLOCK}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart CoreDNS to pick up the new custom block immediately.
kubectl rollout restart deployment/coredns -n kube-system
kubectl rollout status deployment/coredns -n kube-system --timeout=60s
ok "CoreDNS patched — dex.orbit.local.clearroute.io → ${DEX_IP}, orbit.local.clearroute.io/api.orbit.local.clearroute.io → ${DOCKER_BRIDGE_IP}"

# ── Summary ───────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  k3d cluster ready — install Orbit next          ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  Cluster:       k3d-${CLUSTER_NAME}"
echo "  Context:       k3d-${CLUSTER_NAME}"
echo ""
echo "  Next steps:"
echo ""
echo "    make cluster-operator   # install the operator + apply OrbitPlatform / OrbitTenant CRs"
echo "    make cluster-deploy     # build service images + apply workload fixtures"
echo ""
echo "  See deploy/local/README.md for the full annotated sequence."
echo ""
echo "  Other useful commands:"
echo "    kubectl get pods -A"
echo "    helm list -A"
echo "    make dev-cluster-down"
echo ""