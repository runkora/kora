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
OVERLAYS_DIR="${SCRIPT_DIR}/../../overlays/local"
SERVICES_DIR="${SCRIPT_DIR}/../../services/overlays/local"
SPIRE_CM_VERSION="0.6.3"

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
kubectl create namespace orbit --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace orbit istio-injection=enabled --overwrite
ok "Namespaces ready"

# ── 5. Install SPIRE Controller Manager CRDs ─────────────────────────────────
step "Installing SPIRE Controller Manager CRDs (v${SPIRE_CM_VERSION})"
if kubectl get crd clusterspiffeids.spire.spiffe.io &>/dev/null; then
  warn "SPIRE Controller Manager CRDs already installed — skipping"
else
  BASE_URL="https://raw.githubusercontent.com/spiffe/spire-controller-manager/v${SPIRE_CM_VERSION}/config/crd/bases"
  kubectl apply -f "${BASE_URL}/spire.spiffe.io_clusterspiffeids.yaml"
  kubectl apply -f "${BASE_URL}/spire.spiffe.io_clusterfederatedtrustdomains.yaml"
  kubectl apply -f "${BASE_URL}/spire.spiffe.io_clusterstaticentries.yaml"
  kubectl apply -f "${BASE_URL}/spire.spiffe.io_controllermanagerconfigs.yaml"
  ok "CRDs installed"
fi

# ── 6. Deploy SPIRE + NetworkPolicies ────────────────────────────────────────
step "Deploying SPIRE (Server, Agent, NetworkPolicies)"
# Applies the local overlay: base/spire + base/network-policy
kubectl apply -k "${OVERLAYS_DIR}"

echo "  Waiting for SPIRE Server to be ready..."
kubectl rollout status statefulset/spire-server -n spire --timeout=120s
ok "SPIRE Server ready"

echo "  Waiting for SPIRE Agent DaemonSet to be ready..."
kubectl rollout status daemonset/spire-agent -n spire --timeout=240s
ok "SPIRE Agent ready"

# ── 7. Apply service identities ──────────────────────────────────────────────
step "Applying service identities (ClusterSPIFFEIDs)"
# The SPIRE Controller Manager watches ClusterSPIFFEID resources and
# automatically creates SPIRE entries — no kubectl exec registration needed.
# To add a new service: add its ClusterSPIFFEID under infrastructure/services/
# and reference it in infrastructure/services/overlays/local/kustomization.yaml.
kubectl apply -k "${SERVICES_DIR}"
ok "Service identities applied"

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
