#!/usr/bin/env bash
# Write local Docker networking values for Helmfile and CoreDNS.
#
# NodeHosts makes Docker service names resolvable from pods.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RED="\033[31m"
RESET="\033[0m"

step()  { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }
die()   { echo -e "${RED}✗ $*${RESET}" >&2; exit 1; }

# Helper: resolve a container's IP on kora-network.
# Docker Compose v2 may name the network key "kora-network" or "kora_kora-network"
# depending on project naming. Try the explicit name first, then fall back to
# iterating all networks.
container_ip() {
  local name="$1"
  local ip

  # Try explicit network name (set via `name: kora-network` in docker-compose.yml)
  ip="$(docker inspect "${name}" \
    --format '{{with index .NetworkSettings.Networks "kora-network"}}{{.IPAddress}}{{end}}' 2>/dev/null || true)"

  # Fall back to first network IP (works regardless of key naming).
  # The space after IPAddress prevents concatenation when a container is on
  # multiple networks (e.g. "172.18.0.3 172.19.0.2" instead of "172.18.0.3172.19.0.2").
  if [[ -z "${ip}" ]]; then
    ip="$(docker inspect "${name}" \
      --format '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' 2>/dev/null \
      | awk '{print $1}' || true)"
  fi

  if [[ -z "${ip}" ]]; then
    die "Could not resolve IP for container '${name}': is it running? (make dev-up)"
  fi
  echo "${ip}"
}

# ── Resolve IPs ──────────────────────────────────────────────────────────────
step "Resolving Docker container IPs on kora-network"

DEX_IP="$(container_ip kora-dex)"
ok "kora-dex:      ${DEX_IP}"

POSTGRES_IP="$(container_ip kora-postgres)"
ok "kora-postgres: ${POSTGRES_IP}"

REDIS_IP="$(container_ip kora-redis)"
ok "kora-redis:    ${REDIS_IP}"

REGISTRY_IP="$(container_ip kora-registry)"
ok "kora-registry: ${REGISTRY_IP}"

# ── Resolve subnet CIDR ─────────────────────────────────────────────────────
SUBNET="$(docker network inspect kora-network \
  --format '{{(index .IPAM.Config 0).Subnet}}' 2>/dev/null || true)"

if [[ -z "${SUBNET}" ]]; then
  die "Could not detect kora-network subnet: is the network created? (make dev-up)"
fi
ok "kora-network:  ${SUBNET}"

# ── Patch CoreDNS NodeHosts ─────────────────────────────────────────────────
# CoreDNS cannot reach Docker's embedded DNS, so the kora-network containers go into NodeHosts.
# k3s drops k3d's kora-registry entry on restart, and the operator reads sandbox images from it.
step "Patching CoreDNS NodeHosts with Docker container IPs"

kubectl get configmap coredns -n kube-system -o json \
  | jq --arg pg "${POSTGRES_IP} kora-postgres" \
       --arg rd "${REDIS_IP} kora-redis" \
       --arg rg "${REGISTRY_IP} kora-registry" \
       '.data.NodeHosts = (
         (.data.NodeHosts // "") |
         split("\n") |
         map(select(length > 0 and (split(" ")[1] | . != "kora-postgres" and . != "kora-redis" and . != "kora-registry"))) |
         . + [$pg, $rd, $rg] |
         join("\n") + "\n"
       )' \
  | kubectl apply -f -

kubectl rollout restart deployment/coredns -n kube-system
kubectl rollout status deployment/coredns -n kube-system --timeout=60s
ok "CoreDNS NodeHosts patched: kora-postgres → ${POSTGRES_IP}, kora-redis → ${REDIS_IP}, kora-registry → ${REGISTRY_IP}"

# ── Write the generated environment file ────────────────────────────────────
# Preserves the image digest pins redeploy.sh writes.
step "Writing deploy/reference/environments/local.generated.yaml"

GENERATED="${REPO_ROOT}/deploy/reference/environments/local.generated.yaml"
[[ -f "${GENERATED}" ]] || echo "{}" > "${GENERATED}"
yq -i "
  .identity.hostAliases = [{\"ip\": \"${DEX_IP}\", \"hostnames\": [\"dex.local.runkora.io\"]}] |
  .networkPolicy.externalServices = [{\"cidr\": \"${SUBNET}\", \"ports\": [5432, 6379, 5556]}]
" "${GENERATED}"

# Ollama on the host, for a local compatible InferenceProvider at
# http://host.k3d.internal:11434/v1.
HOST_IP=$(kubectl get configmap coredns -n kube-system -o jsonpath='{.data.NodeHosts}' 2>/dev/null | awk '/host.k3d.internal/ {print $1; exit}' || true)
if [[ -n "${HOST_IP}" ]]; then
  yq -i ".networkPolicy.externalServices += [{\"cidr\": \"${HOST_IP}/32\", \"ports\": [11434]}]" "${GENERATED}"
fi

ok "local.generated.yaml written"
echo ""
echo "  Dex hostAlias:     ${DEX_IP}"
echo "  Postgres:          ${POSTGRES_IP}"
echo "  Redis:             ${REDIS_IP}"
echo "  NetworkPolicy CIDR: ${SUBNET}"
echo "  Host (Ollama):     ${HOST_IP:-not found}"
echo ""
