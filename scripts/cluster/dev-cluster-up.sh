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

# Override CLUSTER_NAME by exporting the variable before calling this script,
# e.g.:  CLUSTER_NAME=my-orbit make dev-cluster
# Must match the 'name' field in k3d-config.yaml.
CLUSTER_NAME="${CLUSTER_NAME:-orbit-local}"
ISTIO_VERSION="1.21.2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/overlays/local/k3d-config.yaml"
OVERLAYS_DIR="${REPO_ROOT}/infrastructure/overlays/local"
SERVICES_DIR="${REPO_ROOT}/infrastructure/services/overlays/local"

# The pause image must be imported into the cluster IMMEDIATELY after creation
# so k3s can start sandbox containers (CoreDNS etc.) without hitting Docker Hub.
# DNS resolution inside k3s containers is unreliable until CoreDNS is running —
# which itself can't start without the pause image — a classic bootstrap chicken-egg.
PAUSE_IMAGE="rancher/mirrored-pause:3.6"

# All other large images — pre-pulled before cluster creation (slow, network-bound),
# then imported after pause image is seeded (fast, local tarball copy).
PREFETCH_IMAGES=(
  # k3s system components — must be pre-loaded; Docker Hub is unreachable from inside k3d nodes
  "rancher/mirrored-coredns-coredns:1.10.1"
  "rancher/mirrored-metrics-server:v0.7.0"
  "rancher/local-path-provisioner:v0.0.26"
  # local-path-provisioner spawns a busybox helper pod to mkdir the PVC path on the node
  "rancher/mirrored-library-busybox:1.36.1"
  # SPIRE
  "ghcr.io/spiffe/spire-server:1.9.4"
  "ghcr.io/spiffe/spire-controller-manager:0.6.3"
  "ghcr.io/spiffe/spire-agent:1.9.4"
  # busybox used by SPIRE agent init container (nc probe)
  "busybox:1.36.1"
  # Istio
  "docker.io/istio/pilot:${ISTIO_VERSION}"
  "docker.io/istio/proxyv2:${ISTIO_VERSION}"
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

# ── 2. Pre-pull all images to local Docker cache ──────────────────────────────
# Do this BEFORE cluster creation so the slow network step is completely out of
# the way. Once the cluster is up, k3d image import is a fast local tarball copy.
step "Pre-pulling images to local Docker cache (slow on first run)"
ALL_IMAGES=("${PAUSE_IMAGE}" "${PREFETCH_IMAGES[@]}")
for image in "${ALL_IMAGES[@]}"; do
  if docker image inspect "${image}" &>/dev/null; then
    echo "  ${image} — already cached"
  else
    echo "  Pulling ${image} ..."
    docker pull "${image}"
  fi
done
ok "All images in local Docker cache"

# ── 3. Create k3d cluster ────────────────────────────────────────────────────
step "Creating k3d cluster '${CLUSTER_NAME}'"
if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
  warn "Cluster '${CLUSTER_NAME}' already exists — skipping creation"
else
  k3d cluster create --config "${K3D_CONFIG}"
  ok "Cluster created"
fi

kubectl config use-context "k3d-${CLUSTER_NAME}"

# ── 4. Seed pause image immediately ──────────────────────────────────────────
# k3s needs the pause (sandbox) image to start ANY pod — including CoreDNS.
# Import it before doing anything else so k3s never races Docker Hub for it.
step "Seeding pause image (${PAUSE_IMAGE})"
k3d image import "${PAUSE_IMAGE}" -c "${CLUSTER_NAME}" 2>&1 | grep -v "^$" | sed 's/^/  /' || true
ok "Pause image seeded"

# ── 5. Import remaining images into k3d nodes ─────────────────────────────────
# All images are already in the local Docker cache from step 2, so these imports
# are fast local tarball copies — no network required.
step "Importing images into k3d nodes"
for image in "${PREFETCH_IMAGES[@]}"; do
  echo -n "  Importing ${image} ... "
  k3d image import "${image}" -c "${CLUSTER_NAME}" 2>&1 | grep -v "^$" | sed 's/^/    /' || true
  echo "done"
done
ok "All images pre-loaded into k3d nodes"

# ── 6. Install Istio ─────────────────────────────────────────────────────────
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

# Wait for istiod to be fully Ready before proceeding.
#
# istioctl install returns as soon as resources are applied — the pod itself
# takes another 10–30s to become Ready. If we run kubectl apply -k before istiod
# is Ready, its ValidatingWebhookConfiguration self-poisons:
#   "internal.istio.io/webhook-always-reject" annotation found, rejecting
# This bricks the API server — every subsequent apply is rejected until istiod
# recovers. The rollout wait below ensures the webhook is functional first.
echo "  Waiting for istiod to be Ready..."
kubectl rollout status deployment/istiod -n istio-system --timeout=180s
ok "istiod Ready"

# ── 7. Deploy core infrastructure ────────────────────────────────────────────
# Applies the local overlay: orbit-system-namespace + SPIRE CRDs (vendored) +
# SPIRE server/agent + NetworkPolicies.
# The orbit-system namespace (with istio-injection=enabled) is created here.
step "Deploying SPIRE, NetworkPolicies, and orbit-system namespace"
kubectl apply -k "${OVERLAYS_DIR}"

echo "  Waiting for SPIRE Server to be ready..."
kubectl rollout status statefulset/spire-server -n spire --timeout=300s
ok "SPIRE Server ready"

echo "  Waiting for SPIRE Agent DaemonSet to be ready..."
kubectl rollout status daemonset/spire-agent -n spire --timeout=300s
ok "SPIRE Agent ready"

# ── 8. Apply service identities ──────────────────────────────────────────────
# The SPIRE Controller Manager watches ClusterSPIFFEID resources and
# automatically creates SPIRE entries — no kubectl exec registration needed.
# To add a new service: add its ClusterSPIFFEID under infrastructure/services/
# and reference it in infrastructure/services/overlays/local/kustomization.yaml.
step "Applying service identities (ClusterSPIFFEIDs)"
kubectl apply -k "${SERVICES_DIR}"
ok "Service identities applied"

# ── 9. Summary ───────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  Orbit local cluster is ready                    ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  Cluster:       k3d-${CLUSTER_NAME}"
echo "  Trust domain:  spiffe://orbit.local"
echo "  Istio ingress: http://localhost:8880 / https://localhost:8443"
echo ""
echo "  Useful commands:"
echo "    kubectl get pods -A"
echo "    istioctl proxy-status"
echo "    make dev-cluster-down"
echo ""
