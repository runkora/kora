#!/usr/bin/env bash
# =============================================================================
# dev-cluster-up.sh — Bring up the Orbit local k3d development cluster
#
# Prerequisites (install once via make install-tools-cluster):
#   - k3d    >= 5.6
#   - kubectl >= 1.29
#   - helm    >= 3.14
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/overlays/local/k3d-config.yaml"
OVERLAYS_DIR="${REPO_ROOT}/infrastructure/overlays/local"
SERVICES_DIR="${REPO_ROOT}/infrastructure/services/overlays/local"
HELM_DIR="${REPO_ROOT}/infrastructure/helm"

# All pinned versions live in versions.env — do not hardcode versions here.
# shellcheck source=scripts/cluster/versions.env
source "${SCRIPT_DIR}/versions.env"

# Derive cluster name directly from k3d-config.yaml so this script never
# diverges from what k3d actually names the cluster.
CLUSTER_NAME="$(grep 'name:' "${K3D_CONFIG}" | head -1 | awk '{print $2}')"

# The pause image must be imported into the cluster IMMEDIATELY after creation
# so k3s can start sandbox containers (CoreDNS etc.) without hitting Docker Hub.
# DNS resolution inside k3s containers is unreliable until CoreDNS is running —
# which itself can't start without the pause image — a classic bootstrap chicken-egg.
PAUSE_IMAGE="rancher/mirrored-pause:3.6"

# All other large images — pre-pulled before cluster creation (slow, network-bound),
# then imported after pause image is seeded (fast, local tarball copy).
#
# NOTE: rancher/mirrored-library-busybox is used for k3s local-path-provisioner
# helper pods. busybox:1.36.1 and rancher/mirrored-library-busybox:1.36.1 are
# the same image — we import only the mirrored tag to avoid pulling twice.
PREFETCH_IMAGES=(
  # k3s system components — must be pre-loaded; Docker Hub is unreachable from inside k3d nodes
  "rancher/mirrored-coredns-coredns:1.10.1"
  "rancher/mirrored-metrics-server:v0.7.0"
  "rancher/local-path-provisioner:v0.0.26"
  "rancher/mirrored-library-busybox:${BUSYBOX_VERSION}"
  # SPIRE (installed via helm spiffe/spire)
  "ghcr.io/spiffe/spire-server:${SPIRE_SERVER_VERSION}"
  "ghcr.io/spiffe/spire-controller-manager:${SPIRE_CONTROLLER_MANAGER_VERSION}"
  "ghcr.io/spiffe/spire-agent:${SPIRE_AGENT_VERSION}"
  # spiffe/spire chart uses cgr.dev/chainguard/bash for the spire-agent
  # socketAlternate and fsGroupFix init containers. cgr.dev is unreachable from
  # inside k3d nodes, so we pre-load the :latest tag from the host instead.
  # values.local.yaml pins pullPolicy: Never so nodes never reach out to cgr.dev.
  "cgr.dev/chainguard/bash:latest"
  # Istio (installed via helm istio/istiod)
  "docker.io/istio/pilot:${ISTIO_VERSION}"
  "docker.io/istio/proxyv2:${ISTIO_VERSION}"
  # spiffe/spire chart pre-upgrade hook uses rancher/kubectl to patch the
  # ValidatingWebhookConfiguration. Docker Hub is unreachable from k3d nodes.
  "rancher/kubectl:${KUBECTL_VERSION}"
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
for cmd in k3d kubectl helm docker; do
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

# ── 3. Add Helm repos ─────────────────────────────────────────────────────────
step "Ensuring Helm repos are up to date"
helm repo add spiffe https://spiffe.github.io/helm-charts-hardened/ 2>/dev/null || true
helm repo add istio  https://istio-release.storage.googleapis.com/charts 2>/dev/null || true
helm repo update
ok "Helm repos ready"

# ── 4. Create k3d cluster ────────────────────────────────────────────────────
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

# ── 5. Seed pause image immediately ──────────────────────────────────────────
# k3s needs the pause (sandbox) image to start ANY pod — including CoreDNS.
# Import it before doing anything else so k3s never races Docker Hub for it.
step "Seeding pause image (${PAUSE_IMAGE})"
k3d image import "${PAUSE_IMAGE}" -c "${CLUSTER_NAME}" 2>&1 | grep -v "^$" | sed 's/^/  /' || true
ok "Pause image seeded"

# ── 6. Import remaining images into k3d nodes ─────────────────────────────────
# All images are already in the local Docker cache from step 2, so these imports
# are fast local tarball copies — no network required.
step "Importing images into k3d nodes"
for image in "${PREFETCH_IMAGES[@]}"; do
  echo -n "  Importing ${image} ... "
  k3d image import "${image}" -c "${CLUSTER_NAME}" 2>&1 | grep -v "^$" | sed 's/^/    /' || true
  echo "done"
done
ok "All images pre-loaded into k3d nodes"

# ── 7. Install Istio via Helm ─────────────────────────────────────────────────
# Two charts: istio/base (CRDs + cluster resources) then istio/istiod (control plane).
# Replaces istioctl entirely — no istioctl dependency needed.
step "Installing Istio ${ISTIO_CHART_VERSION} via Helm"
if kubectl get namespace istio-system &>/dev/null && \
   helm status istio-base -n istio-system &>/dev/null && \
   helm status istiod     -n istio-system &>/dev/null; then
  warn "Istio already installed — skipping (run 'make dev-cluster-down' to reset)"
else
  kubectl create namespace istio-system --dry-run=client -o yaml | kubectl apply -f -

  helm upgrade --install istio-base istio/base \
    --namespace istio-system \
    --version "${ISTIO_CHART_VERSION}" \
    -f "${HELM_DIR}/istio/values.yaml" \
    -f "${HELM_DIR}/istio/values.local.yaml" \
    --wait

  helm upgrade --install istiod istio/istiod \
    --namespace istio-system \
    --version "${ISTIO_CHART_VERSION}" \
    -f "${HELM_DIR}/istio/values.yaml" \
    -f "${HELM_DIR}/istio/values.local.yaml" \
    --wait --timeout 180s

  ok "Istio installed"
fi

# Wait for istiod to be fully Ready before proceeding.
# helm --wait should have handled this, but an explicit rollout check guards
# against edge cases where the pod scheduling lags behind helm's readiness check.
echo "  Verifying istiod is Ready..."
kubectl rollout status deployment/istiod -n istio-system --timeout=180s
ok "istiod Ready"

# ── 8. Verify postgres is ready ──────────────────────────────────────────────
# SPIRE server connects to orbit-postgres at startup (k3d nodes share
# orbit-network with docker-compose via k3d-config.yaml).
# If the postgres container is not healthy, the SPIRE server pod will
# crash-loop and the rollout wait below will time out.
# The Makefile wires 'db-up' as a prerequisite of 'dev-cluster', so this
# check is normally a fast no-op — but it guards against direct script invocation.
step "Verifying docker-compose postgres is healthy (required by SPIRE)"
DB_USER="${DB_USER:-orbit}"
DB_NAME="${DB_NAME:-orbit}"

if ! docker inspect orbit-postgres &>/dev/null; then
  echo "  ERROR: orbit-postgres container not found."
  echo "  Start it first:  make db-up   (or: docker compose up -d postgres)"
  exit 1
fi

echo -n "  Waiting for postgres to accept connections"
for i in $(seq 1 30); do
  if docker exec orbit-postgres pg_isready -U "${DB_USER}" -d "${DB_NAME}" -q 2>/dev/null; then
    echo ""
    break
  fi
  if [ "${i}" -eq 30 ]; then
    echo ""
    echo "  ERROR: Postgres did not become ready within 30s."
    echo "  Check logs:  docker compose logs postgres"
    exit 1
  fi
  echo -n "."
  sleep 1
done
ok "Postgres healthy (orbit-postgres)"

# ── 9. Register orbit-postgres in cluster DNS ─────────────────────────────────
# k3d pods use CoreDNS — not Docker's embedded DNS — so Docker container names
# like "orbit-postgres" are invisible inside pods even though the k3d nodes are
# on orbit-network. Fix: create a headless Service + manual Endpoints in each
# relevant namespace so CoreDNS resolves "orbit-postgres" to the container's
# actual Docker IP on orbit-network.
step "Registering orbit-postgres in cluster DNS (spire + orbit-system namespaces)"

POSTGRES_IP="$(docker inspect orbit-postgres --format '{{(index .NetworkSettings.Networks "orbit-network").IPAddress}}')"
if [ -z "${POSTGRES_IP}" ]; then
  echo "  ERROR: Could not determine orbit-postgres IP on orbit-network."
  echo "  Ensure docker-compose is running:  make db-up"
  exit 1
fi
echo "  orbit-postgres IP on orbit-network: ${POSTGRES_IP}"

# Create namespaces upfront — helm will see them already existing and skip re-creation.
kubectl create namespace spire        --dry-run=client -o yaml | kubectl apply --server-side -f -
kubectl create namespace orbit-system --dry-run=client -o yaml | kubectl apply --server-side -f -

for NS in spire orbit-system; do
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: orbit-postgres
  namespace: ${NS}
spec:
  clusterIP: None
  ports:
  - port: 5432
    targetPort: 5432
---
apiVersion: v1
kind: Endpoints
metadata:
  name: orbit-postgres
  namespace: ${NS}
subsets:
- addresses:
  - ip: ${POSTGRES_IP}
  ports:
  - port: 5432
EOF
  echo "  Registered orbit-postgres in namespace '${NS}'"
done
ok "orbit-postgres registered in CoreDNS at ${POSTGRES_IP}"

# ── 10. Deploy SPIRE via Helm ─────────────────────────────────────────────────
# CRDs must be installed before the main chart — spiffe/spire templates create
# ClusterSPIFFEID objects which will fail if the CRD doesn't exist yet.
step "Installing SPIRE CRDs ${SPIRE_CRDS_CHART_VERSION} via Helm"
helm upgrade --install spire-crds spiffe/spire-crds \
  --namespace spire \
  --create-namespace \
  --version "${SPIRE_CRDS_CHART_VERSION}" \
  --wait --timeout 60s
ok "SPIRE CRDs installed"

# Wait for CRDs to be fully established before installing the main chart
echo "  Waiting for SPIRE CRDs to be established..."
kubectl wait --for=condition=Established \
  crd/clusterspiffeids.spire.spiffe.io \
  crd/clusterfederatedtrustdomains.spire.spiffe.io \
  crd/clusterstaticentries.spire.spiffe.io \
  --timeout=60s
ok "SPIRE CRDs established"

# Now install the umbrella chart (server, agent, controller-manager, webhook)
# values.yaml = base/production defaults; values.local.yaml = local dev overrides.
step "Installing SPIRE ${SPIRE_CHART_VERSION} via Helm"
helm upgrade --install spire spiffe/spire \
  --namespace spire \
  --version "${SPIRE_CHART_VERSION}" \
  -f "${HELM_DIR}/spire/values.yaml" \
  -f "${HELM_DIR}/spire/values.local.yaml"
ok "SPIRE installed"

# Verify SPIRE server pod is fully Ready (both server + controller-manager containers)
echo "  Waiting for SPIRE Server pod to be Ready..."
kubectl wait --for=condition=Ready pod/spire-server-0 -n spire --timeout=180s
ok "SPIRE Server ready"

# Verify SPIRE Agent DaemonSet
echo "  Waiting for SPIRE Agent DaemonSet to be ready..."
kubectl rollout status daemonset/spire-agent -n spire --timeout=300s
ok "SPIRE Agent ready"

# ── 11. Deploy core infrastructure (NetworkPolicies + orbit-system namespace) ──
step "Deploying NetworkPolicies and orbit-system namespace"
kubectl apply -k "${OVERLAYS_DIR}"
ok "Core infrastructure applied"

# ── 12. Apply service identities ─────────────────────────────────────────────
# The SPIRE Controller Manager watches ClusterSPIFFEID resources and
# automatically creates SPIRE entries — no kubectl exec registration needed.
step "Applying service identities (ClusterSPIFFEIDs)"
kubectl apply -k "${SERVICES_DIR}"
ok "Service identities applied"

# ── 13. Summary ───────────────────────────────────────────────────────────────
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
echo "    kubectl get pods -n spire"
echo "    helm list -A"
echo "    make dev-cluster-down"
echo ""
