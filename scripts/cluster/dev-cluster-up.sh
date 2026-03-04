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
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/overlays/local/k3d-config.yaml"
OVERLAYS_DIR="${REPO_ROOT}/infrastructure/overlays/local"
SERVICES_DIR="${REPO_ROOT}/infrastructure/services/overlays/local"
SPIRE_CM_VERSION="0.6.3"

# Images that are large / slow to pull — pre-pulled into k3d nodes so that
# rollout waits complete in seconds rather than minutes.
PREFETCH_IMAGES=(
  "ghcr.io/spiffe/spire-server:1.9.4"
  "ghcr.io/spiffe/spire-controller-manager:0.6.3"
  "ghcr.io/spiffe/spire-agent:1.9.4"
  "busybox:1.36.1"
)

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
for cmd in k3d kubectl istioctl helm docker; do
  if ! command -v "${cmd}" &>/dev/null; then
    echo "  Missing: ${cmd}$([ "${cmd}" != "docker" ] && echo " — run 'make install-tools-cluster'" || echo "")"
    exit 1
  fi
done
ok "All prerequisites present"

# ── 2. Create k3d cluster ────────────────────────────────────────────────────
step "Creating k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
  warn "Cluster '${CLUSTER_NAME}' already exists — skipping creation"
else
  k3d cluster create --config "${K3D_CONFIG}"
  ok "Cluster created"
fi

kubectl config use-context "k3d-${CLUSTER_NAME}"

# ── 3. Pre-pull images into k3d nodes ────────────────────────────────────────
# Importing images directly into the cluster's containerd cache means that when
# SPIRE manifests are applied the images are already present on every node.
# This turns a 3–5 minute image-pull wait into a near-instant rollout.
step "Pre-pulling images into k3d nodes (this is the slow step on a cold start)"
for image in "${PREFETCH_IMAGES[@]}"; do
  echo -n "  Pulling ${image} ... "
  if docker image inspect "${image}" &>/dev/null; then
    echo "already in local Docker cache"
  else
    docker pull "${image}"
  fi
  echo -n "  Importing ${image} into k3d cluster ... "
  k3d image import "${image}" -c "${CLUSTER_NAME}" --verbose=false 2>&1 \
    | grep -v "^$" \
    | sed 's/^/    /' \
    || true
  echo "  done"
done
ok "All images pre-loaded into k3d nodes"

# ── 4. Install Istio ─────────────────────────────────────────────────────────
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

  # Sidecar injection is enabled on orbit-system via the declarative namespace
  # manifest at infrastructure/base/orbit-system-namespace/namespace.yaml.
  ok "Istio installed"
fi

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

# ── 6. Deploy core infrastructure ────────────────────────────────────────────
step "Deploying SPIRE, NetworkPolicies, and orbit-system namespace"
# Applies the local overlay: base/orbit-system-namespace + base/spire + base/network-policy
# The orbit-system namespace (with istio-injection=enabled) is created declaratively here.
kubectl apply -k "${OVERLAYS_DIR}"

echo "  Waiting for SPIRE Server to be ready..."
kubectl rollout status statefulset/spire-server -n spire --timeout=300s
ok "SPIRE Server ready"

echo "  Waiting for SPIRE Agent DaemonSet to be ready..."
kubectl rollout status daemonset/spire-agent -n spire --timeout=300s
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
echo "  Cluster:       k3d-${CLUSTER_NAME}"
echo "  Trust domain:  spiffe://orbit.local"
echo "  Istio ingress: http://localhost:8080 / https://localhost:8443"
echo ""
echo "  Useful commands:"
echo "    kubectl get pods -A"
echo "    istioctl proxy-status"
echo "    make dev-cluster-down"
echo ""
