#!/usr/bin/env bash
# =============================================================================
# dev-cluster-up.sh — Bring up the Orbit local k3d development cluster
#
# Prerequisites (install once via make install-tools-cluster):
#   - k3d    >= 5.6
#   - kubectl >= 1.29
#   - istioctl >= 1.21
#   - helm    >= 3.14
# =============================================================================
set -euo pipefail

CLUSTER_NAME="orbit-local"
ISTIO_VERSION="1.21.2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INFRA_DIR="${SCRIPT_DIR}/.."

# Terminal colours
GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step()  { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }

# ── 1. Preflight ─────────────────────────────────────────────────────────────
step "Checking prerequisites"
for cmd in k3d kubectl istioctl helm; do
  if ! command -v "${cmd}" &>/dev/null; then
    echo "  Missing: ${cmd} — run 'make install-tools-cluster'"
    exit 1
  fi
done
ok "All prerequisites present"

# ── 2. Create k3d cluster ────────────────────────────────────────────────────
step "Creating k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
  warn "Cluster '${CLUSTER_NAME}' already exists — skipping creation"
else
  k3d cluster create --config "${INFRA_DIR}/k3d-config.yaml"
  ok "Cluster created"
fi

kubectl config use-context "k3d-${CLUSTER_NAME}"

# ── 3. Install Istio ─────────────────────────────────────────────────────────
step "Installing Istio ${ISTIO_VERSION}"
if kubectl get namespace istio-system &>/dev/null; then
  warn "Istio already installed — skipping"
else
  # minimal profile: istiod + ingress gateway, no egress gateway
  istioctl install \
    --set profile=minimal \
    --set values.pilot.resources.requests.cpu=50m \
    --set values.pilot.resources.requests.memory=128Mi \
    -y

  # Enable sidecar injection in the orbit namespace (created below)
  ok "Istio installed"
fi

# ── 4. Create namespaces ─────────────────────────────────────────────────────
step "Creating namespaces"
kubectl apply -f "${INFRA_DIR}/spire/namespace.yaml"

kubectl create namespace orbit --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace orbit istio-injection=enabled --overwrite

ok "Namespaces ready"

# ── 5. Deploy SPIRE ──────────────────────────────────────────────────────────
step "Deploying SPIRE Server"
kubectl apply -f "${INFRA_DIR}/spire/server.yaml"

echo "  Waiting for SPIRE Server to be ready..."
kubectl rollout status statefulset/spire-server -n spire --timeout=120s
ok "SPIRE Server ready"

step "Deploying SPIRE Agent"
kubectl apply -f "${INFRA_DIR}/spire/agent.yaml"

echo "  Waiting for SPIRE Agent DaemonSet to be ready..."
kubectl rollout status daemonset/spire-agent -n spire --timeout=240s
ok "SPIRE Agent ready"

# ── 6. Register SPIFFE IDs ───────────────────────────────────────────────────
step "Registering SPIFFE IDs"
# The spire-server image is distroless (no shell), so we exec the spire-server
# binary directly via kubectl exec.
#
# Entry model:
#   - One node entry (flag: -node) anchors all k8s_psat-attested agents under
#     spiffe://orbit.local/node. This acts as the parent for all workloads.
#   - Workload entries use -parentID spiffe://orbit.local/node and declare two
#     selectors: namespace + service account.
#
# Idempotency: `entry create` exits non-zero if the entry already exists, so we
# suppress that specific error and continue.

SPIRE_SOCKET="/tmp/spire-server/private/api.sock"
NODE_SPIFFE_ID="spiffe://orbit.local/node"

spire_exec() {
  kubectl exec -n spire spire-server-0 -- /opt/spire/bin/spire-server "$@" \
    -socketPath "${SPIRE_SOCKET}" 2>&1
}

# 6a. Node entry — one entry that matches all k8s_psat-attested agents in this cluster.
echo "  Registering node entry: ${NODE_SPIFFE_ID}"
spire_exec entry create \
  -node \
  -spiffeID "${NODE_SPIFFE_ID}" \
  -selector "k8s_psat:cluster:orbit-local" \
  | grep -v "^$" || true

# 6b. Workload entries — each bound to namespace + service account selectors.
register_workload() {
  local spiffe_id="$1"
  local ns="$2"
  local sa="$3"
  echo "  Registering workload: ${spiffe_id}"
  spire_exec entry create \
    -spiffeID "${spiffe_id}" \
    -parentID "${NODE_SPIFFE_ID}" \
    -selector "k8s:ns:${ns}" \
    -selector "k8s:sa:${sa}" \
    | grep -v "^$" || true
}

register_workload "spiffe://orbit.local/system/identity"         "orbit" "identity"
register_workload "spiffe://orbit.local/system/ext-authz"        "orbit" "ext-authz"
register_workload "spiffe://orbit.local/system/outbound-gateway" "orbit" "outbound-gateway"
register_workload "spiffe://orbit.local/tenant/dev-tenant/agent/smoke-agent" "orbit" "smoke-agent"

ok "SPIFFE IDs registered"

# ── 7. Apply NetworkPolicies ─────────────────────────────────────────────────
step "Applying NetworkPolicies"
kubectl apply -f "${INFRA_DIR}/network-policy/default-deny.yaml"
ok "NetworkPolicies applied"

# ── 8. Summary ───────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  Orbit local cluster is ready                    ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  Cluster:    k3d-${CLUSTER_NAME}"
echo "  Trust domain: spiffe://orbit.local"
echo "  Istio ingress: http://localhost:8080 / https://localhost:8443"
echo ""
echo "  Useful commands:"
echo "    kubectl get pods -A"
echo "    istioctl proxy-status"
echo "    make dev-cluster-down"
echo ""
