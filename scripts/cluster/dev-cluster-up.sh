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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
K3D_CONFIG="${REPO_ROOT}/infrastructure/overlays/local/k3d-config.yaml"
OVERLAYS_DIR="${REPO_ROOT}/infrastructure/overlays/local"
SERVICES_DIR="${REPO_ROOT}/infrastructure/services/overlays/local"

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
# NOTE: rancher/mirrored-library-busybox is used for BOTH k3s local-path-provisioner
# helper pods AND the SPIRE agent init container. busybox:1.36.1 and
# rancher/mirrored-library-busybox:1.36.1 are the same image — we import only the
# mirrored tag to avoid pulling twice.
PREFETCH_IMAGES=(
  # k3s system components — must be pre-loaded; Docker Hub is unreachable from inside k3d nodes
  "rancher/mirrored-coredns-coredns:1.10.1"
  "rancher/mirrored-metrics-server:v0.7.0"
  "rancher/local-path-provisioner:v0.0.26"
  "rancher/mirrored-library-busybox:${BUSYBOX_VERSION}"
  # SPIRE
  "ghcr.io/spiffe/spire-server:${SPIRE_SERVER_VERSION}"
  "ghcr.io/spiffe/spire-controller-manager:${SPIRE_CONTROLLER_MANAGER_VERSION}"
  "ghcr.io/spiffe/spire-agent:${SPIRE_AGENT_VERSION}"
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

# ── 7. Verify postgres is ready ──────────────────────────────────────────────
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

# ── 7b. Register orbit-postgres in cluster DNS ───────────────────────────────
# k3d pods use CoreDNS — not Docker's embedded DNS — so Docker container names
# like "orbit-postgres" are invisible inside pods even though the k3d nodes are
# on orbit-network. Fix: create a headless Service + manual Endpoints in each
# relevant namespace so CoreDNS resolves "orbit-postgres" to the container's
# actual Docker IP on orbit-network.
#
# We create the entry in both 'spire' and 'orbit-system' so that application
# services in orbit-system can use the short hostname without a cross-namespace
# DNS reference (orbit-postgres.spire.svc.cluster.local).
step "Registering orbit-postgres in cluster DNS (spire + orbit-system namespaces)"

POSTGRES_IP="$(docker inspect orbit-postgres --format '{{(index .NetworkSettings.Networks "orbit-network").IPAddress}}')"
if [ -z "${POSTGRES_IP}" ]; then
  echo "  ERROR: Could not determine orbit-postgres IP on orbit-network."
  echo "  Ensure docker-compose is running:  make db-up"
  exit 1
fi
echo "  orbit-postgres IP on orbit-network: ${POSTGRES_IP}"

# Create namespaces upfront so we can register the Services before apply-k.
# kubectl apply -k will see them already existing and skip re-creation.
kubectl create namespace spire --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace orbit-system --dry-run=client -o yaml | kubectl apply -f -

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

# ── 8. Deploy core infrastructure ────────────────────────────────────────────
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

# Wait for the SPIRE Controller Manager sidecar to be fully Ready before
# applying ClusterSPIFFEIDs. The ValidatingWebhookConfiguration uses
# failurePolicy: Fail, meaning the API server calls the webhook for every
# ClusterSPIFFEID apply. The controller-manager is a sidecar in spire-server-0;
# waiting for pod Ready ensures all containers (server + controller-manager)
# have passed their readiness probes and the webhook port is bound.
echo "  Waiting for SPIRE Controller Manager to be ready..."
kubectl wait --for=condition=Ready pod/spire-server-0 -n spire --timeout=120s
ok "SPIRE Controller Manager ready"

# Wait for the SPIRE CRDs to be fully established before applying ClusterSPIFFEIDs.
# kubectl apply -k emits CRDs and workloads in the same pass; the API server may
# not have registered the new CRDs by the time the next step tries to POST
# ClusterSPIFFEID resources — resulting in "no matches for kind" errors.
echo "  Waiting for SPIRE CRDs to be established..."
kubectl wait --for=condition=Established \
  crd/clusterspiffeids.spire.spiffe.io \
  crd/clusterfederatedtrustdomains.spire.spiffe.io \
  crd/clusterstaticentries.spire.spiffe.io \
  --timeout=60s
ok "SPIRE CRDs established"

# ── 9. Apply service identities ──────────────────────────────────────────────
# The SPIRE Controller Manager watches ClusterSPIFFEID resources and
# automatically creates SPIRE entries — no kubectl exec registration needed.
# To add a new service: add its ClusterSPIFFEID under infrastructure/services/
# and reference it in infrastructure/services/overlays/local/kustomization.yaml.
step "Applying service identities (ClusterSPIFFEIDs)"
kubectl apply -k "${SERVICES_DIR}"
ok "Service identities applied"

# ── 10. Summary ──────────────────────────────────────────────────────────────
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
