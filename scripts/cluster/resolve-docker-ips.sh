#!/usr/bin/env bash
# =============================================================================
# resolve-docker-ips.sh — Resolve Docker container IPs and inject into cluster
#
# Docker assigns container IPs dynamically — they vary across machines and
# restarts. This script detects the actual IPs at cluster setup time and
# patches the OrbitPlatform CR so the operator can propagate them into the
# orbit and orbit-platform Helm releases.
#
# Called by: make cluster-operator (after applying the static CR)
#
# What it patches:
#   spec.orbitValues.identity.hostAliases   → Dex container IP
#   spec.platformValues.networkPolicy       → orbit-network subnet CIDR
#   CoreDNS NodeHosts configmap             → orbit-postgres, orbit-redis IPs
#
# Why CoreDNS NodeHosts?
#   Pods use CoreDNS (10.43.0.10) for DNS. The `forward . /etc/resolv.conf`
#   rule in CoreDNS's default zone forwards to Docker's embedded DNS
#   (127.0.0.11 on the node), but from inside CoreDNS's own container
#   127.0.0.11 is the container's own loopback — not the node's Docker DNS.
#   Docker container hostnames like orbit-postgres and orbit-redis are
#   therefore unreachable via forwarding. The NodeHosts file is loaded
#   directly by the CoreDNS hosts plugin running in-process, bypassing
#   the forwarding problem entirely.
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RED="\033[31m"
RESET="\033[0m"

step()  { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }
die()   { echo -e "${RED}✗ $*${RESET}" >&2; exit 1; }

# Helper: resolve a container's IP on orbit-network.
# Docker Compose v2 may name the network key "orbit-network" or "orbit_orbit-network"
# depending on project naming. Try the explicit name first, then fall back to
# iterating all networks.
container_ip() {
  local name="$1"
  local ip

  # Try explicit network name (set via `name: orbit-network` in docker-compose.yml).
  # Read .IPAddress directly — dumping the whole endpoint struct and grepping for
  # an IPv4 picks up .Gateway first, which sorts before .IPAddress in the dump.
  ip="$(docker inspect "${name}" \
    --format '{{with index .NetworkSettings.Networks "orbit-network"}}{{.IPAddress}}{{end}}' 2>/dev/null \
    | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"

  # Fall back to first network IP (works regardless of key naming).
  # The space after IPAddress prevents concatenation when a container is on
  # multiple networks (e.g. "172.18.0.3 172.19.0.2" instead of "172.18.0.3172.19.0.2").
  if [[ -z "${ip}" ]]; then
    ip="$(docker inspect "${name}" \
      --format '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' 2>/dev/null \
      | awk '{print $1}' || true)"
  fi

  if [[ -z "${ip}" ]]; then
    die "Could not resolve IP for container '${name}' — is it running? (make dev-up)"
  fi
  echo "${ip}"
}

# ── Resolve IPs ──────────────────────────────────────────────────────────────
step "Resolving Docker container IPs on orbit-network"

DEX_IP="$(container_ip orbit-dex)"
ok "orbit-dex:      ${DEX_IP}"

POSTGRES_IP="$(container_ip orbit-postgres)"
ok "orbit-postgres: ${POSTGRES_IP}"

REDIS_IP="$(container_ip orbit-redis)"
ok "orbit-redis:    ${REDIS_IP}"

# ── Resolve subnet CIDR ─────────────────────────────────────────────────────
SUBNET="$(docker network inspect orbit-network \
  --format '{{(index .IPAM.Config 0).Subnet}}' 2>/dev/null || true)"

if [[ -z "${SUBNET}" ]]; then
  die "Could not detect orbit-network subnet — is the network created? (make dev-up)"
fi
ok "orbit-network:  ${SUBNET}"

# ── Patch CoreDNS NodeHosts ─────────────────────────────────────────────────
# orbit-postgres and orbit-redis are Docker containers on orbit-network.
# CoreDNS cannot resolve them via `forward . /etc/resolv.conf` because
# 127.0.0.11 (Docker's embedded DNS) is unreachable from inside CoreDNS's
# container. Injecting them into NodeHosts makes the hosts plugin resolve
# them in-process without forwarding.
step "Patching CoreDNS NodeHosts with Docker container IPs"

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

kubectl rollout restart deployment/coredns -n kube-system
kubectl rollout status deployment/coredns -n kube-system --timeout=60s
ok "CoreDNS NodeHosts patched — orbit-postgres → ${POSTGRES_IP}, orbit-redis → ${REDIS_IP}"

# ── Patch OrbitPlatform CR ───────────────────────────────────────────────────
step "Patching OrbitPlatform CR with resolved IPs"

kubectl patch orbitplatform orbit --type=merge -p "{
  \"spec\": {
    \"orbitValues\": {
      \"identity\": {
        \"hostAliases\": [{
          \"ip\": \"${DEX_IP}\",
          \"hostnames\": [\"dex.orbit.local.clearroute.io\"]
        }]
      }
    },
    \"platformValues\": {
      \"networkPolicy\": {
        \"externalServices\": [
          {\"cidr\": \"${SUBNET}\", \"ports\": [5432, 6379, 5556]}
        ]
      }
    }
  }
}"

ok "OrbitPlatform CR patched — operator will reconcile with resolved IPs"
echo ""
echo "  Dex hostAlias:     ${DEX_IP}"
echo "  Postgres:          ${POSTGRES_IP}"
echo "  Redis:             ${REDIS_IP}"
echo "  NetworkPolicy CIDR: ${SUBNET}"
echo ""
