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

# ── inotify headroom ──────────────────────────────────────────────────────
# Every meshed pod, the kubelet, and each Orbit service that hot-reloads a
# ConfigMap opens inotify instances. Docker's Linux VM ships with
# fs.inotify.max_user_instances=128, which a full Orbit cluster exhausts.
# orbit-identity then cannot watch the tenant registry ("too many open files"),
# never picks up the tenant, and `make auth` fails with oidc_not_configured.
#
# The k3d nodes are containers sharing the VM's kernel, so raising it once from a
# privileged container fixes every node. It does not survive a VM restart, which
# is why this runs on every cluster creation.
INOTIFY_MIN_INSTANCES=1024

step "Ensuring inotify headroom in the Docker VM"
current="$(docker run --rm --privileged alpine:3.20 sysctl -n fs.inotify.max_user_instances 2>/dev/null || echo 0)"
if [ "${current}" -ge "${INOTIFY_MIN_INSTANCES}" ] 2>/dev/null; then
  ok "fs.inotify.max_user_instances=${current}"
elif docker run --rm --privileged alpine:3.20 \
    sysctl -w "fs.inotify.max_user_instances=${INOTIFY_MIN_INSTANCES}" >/dev/null 2>&1; then
  ok "fs.inotify.max_user_instances raised ${current} -> ${INOTIFY_MIN_INSTANCES}"
else
  warn "Could not raise fs.inotify.max_user_instances (currently ${current})."
  warn "ConfigMap hot-reload may fail with 'too many open files'."
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

# ── Patch CoreDNS ──────────────────────────────────────────────────────────
# Two problems to solve:
#
# 1. *.orbit.local.clearroute.io hostnames: on the host Mac, dnsmasq resolves
#    these to 127.0.0.1, but inside k3d pods 127.0.0.1 is the pod itself.
#    We add a custom server block via the coredns-custom ConfigMap so k3d's
#    own Corefile is never modified.
#
# 2. orbit-postgres / orbit-redis bare hostnames: the `forward . /etc/resolv.conf`
#    rule points CoreDNS to 127.0.0.11 (Docker's embedded DNS), but from inside
#    CoreDNS's own container that address is unreachable. We inject these into
#    CoreDNS's NodeHosts file, which the hosts plugin resolves in-process.
step "Patching CoreDNS for orbit.local.clearroute.io hostnames and Docker service IPs"

DOCKER_BRIDGE_IP="$(docker network inspect orbit-network \
  --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || echo "")"

if [[ -z "${DOCKER_BRIDGE_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-network gateway."
  echo "  Is orbit-network created?  Run:  make dev-up"
  exit 1
fi

DEX_IP="$(docker inspect orbit-dex \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || echo "")"
if [[ -z "${DEX_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-dex container IP."
  echo "  Is Dex running?  Run:  make dev-up"
  exit 1
fi

POSTGRES_IP="$(docker inspect orbit-postgres \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || echo "")"
if [[ -z "${POSTGRES_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-postgres container IP."
  echo "  Is Postgres running?  Run:  make dev-up"
  exit 1
fi

REDIS_IP="$(docker inspect orbit-redis \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || echo "")"
if [[ -z "${REDIS_IP}" ]]; then
  echo "  ERROR: Could not detect orbit-redis container IP."
  echo "  Is Redis running?  Run:  make dev-up"
  exit 1
fi

ok "Docker bridge gateway: ${DOCKER_BRIDGE_IP}, Dex: ${DEX_IP}, Postgres: ${POSTGRES_IP}, Redis: ${REDIS_IP}"

# Custom server block for *.orbit.local.clearroute.io zone resolution.
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

# Inject orbit-postgres and orbit-redis into NodeHosts so the hosts plugin
# resolves them in-process without forwarding.
kubectl get configmap coredns -n kube-system -o json \
  | jq --arg pg "${POSTGRES_IP} orbit-postgres" \
       --arg rd "${REDIS_IP} orbit-redis" \
       '.data.NodeHosts = (
         (.data.NodeHosts // "") |
         split("\n") |
         map(select(length > 0 and (split(" ")[1] | . != "orbit-postgres" and . != "orbit-redis"))) |
         . + [$pg, $rd] |
         join("\n") + "\n"
       )' \
  | kubectl apply -f -

# Restart CoreDNS to pick up all changes immediately.
kubectl rollout restart deployment/coredns -n kube-system
kubectl rollout status deployment/coredns -n kube-system --timeout=60s
ok "CoreDNS patched — dex → ${DEX_IP}, orbit.local.clearroute.io → ${DOCKER_BRIDGE_IP}, orbit-postgres → ${POSTGRES_IP}, orbit-redis → ${REDIS_IP}"

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
echo "    make cluster-platform   # SPIRE, Istio and Orbit from deploy/reference/helmfile.yaml.gotmpl"
echo "    make cluster-deploy     # build service images + apply workload fixtures"
echo ""
echo "  See deploy/local/README.md for the full annotated sequence."
echo ""
echo "  Other useful commands:"
echo "    kubectl get pods -A"
echo "    helm list -A"
echo "    make dev-cluster-down"
echo ""
