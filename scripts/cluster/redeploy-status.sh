#!/usr/bin/env bash
# =============================================================================
# redeploy-status.sh — Show drift between images deployed in the cluster and
# the latest images in the local registry. One row per known service.
#
# A green "in-sync" row means the running pods are using the same image digest
# as the most recent push to localhost:5001. A yellow "stale" row means you
# have a newer build that hasn't rolled out yet.
# =============================================================================

set -euo pipefail

C_RESET=$'\033[0m'
C_GREEN=$'\033[32m'
C_YELLOW=$'\033[33m'
C_RED=$'\033[31m'
C_BLUE=$'\033[36m'
C_DIM=$'\033[2m'

# (display name, image, namespace, workload, on-demand?)
SERVICES=(
  "identity            orbit-identity            orbit-system            deployment/orbit-identity"
  "inference-gateway   orbit-inference-gateway   orbit-system            deployment/orbit-inference-gateway"
  "policy-service      orbit-policy-service      orbit-system            deployment/orbit-policy-service"
  "policy-gateway      orbit-policy-gateway      orbit-system            deployment/orbit-policy-gateway"
  "agents              orbit-agents              orbit-system            deployment/orbit-agents"
  "frontend            orbit-frontend            orbit-system            deployment/orbit-frontend"
  "operator            orbit-operator            orbit-operator          deployment/orbit-operator"
  "example             orbit-example             orbit-tenant-example    deployment/orbit-example"
  "session-manager     orbit-session-manager     orbit-tenant-example    statefulset/example"
  "identity-proxy      orbit-identity-proxy      orbit-system            sidecar"
  "sandbox-exec        orbit-sandbox-exec        -                       on-demand"
)

# Local-registry digest for an image. Returns "" if the image isn't in the registry.
registry_digest() {
  local image="$1"
  docker buildx imagetools inspect "localhost:5001/$image:latest" 2>/dev/null \
    | awk '/^Digest:/ {print $2; exit}'
}

# Cluster digest for a workload. Returns the @sha256:... portion of the running container's image,
# or "" if there's no such workload / no digest yet.
cluster_digest() {
  local namespace="$1" workload="$2"
  if [ "$namespace" = "-" ] || [ "$workload" = "on-demand" ] || [ "$workload" = "sidecar" ]; then
    echo ""
    return
  fi
  # status.imageID is the digest the kubelet actually pulled, which is what we want.
  local kind="${workload%%/*}"
  local name="${workload##*/}"
  local selector
  selector=$(kubectl get "$kind" "$name" -n "$namespace" \
    -o jsonpath='{.spec.selector.matchLabels}' 2>/dev/null || true)
  if [ -z "$selector" ]; then
    echo "MISSING"
    return
  fi
  # Convert {"k":"v","a":"b"} → k=v,a=b
  local label_selector
  label_selector=$(echo "$selector" | sed 's/[{}"]//g; s/:/=/g; s/,/,/g')
  local imageid
  imageid=$(kubectl get pods -n "$namespace" -l "$label_selector" \
    -o jsonpath='{.items[0].status.containerStatuses[0].imageID}' 2>/dev/null || true)
  # Some pods have multiple containers (sidecars). Try to find the one whose
  # image matches the workload name; fall back to the first if no match.
  if [ -n "$imageid" ] && [[ "$imageid" != *"$name"* ]]; then
    local match
    match=$(kubectl get pods -n "$namespace" -l "$label_selector" \
      -o jsonpath="{.items[0].status.containerStatuses[?(@.name=='$name')].imageID}" 2>/dev/null || true)
    [ -n "$match" ] && imageid="$match"
  fi
  # imageID is typically "docker-pullable://localhost:5001/orbit-X@sha256:...". We want the digest.
  echo "${imageid##*@}"
}

printf "%s\n" "${C_BLUE}Service              Cluster                                   Registry                                  Status${C_RESET}"
printf "%s\n" "${C_DIM}─────────────────────────────────────────────────────────────────────────────────────────────────────────────────${C_RESET}"

for row in "${SERVICES[@]}"; do
  read -r name image namespace workload <<< "$row"
  reg=$(registry_digest "$image")
  cluster=$(cluster_digest "$namespace" "$workload")

  reg_short="${reg#sha256:}"; reg_short="${reg_short:0:12}"
  cluster_short="${cluster#sha256:}"; cluster_short="${cluster_short:0:12}"

  if [ "$workload" = "on-demand" ] || [ "$workload" = "sidecar" ]; then
    if [ -z "$reg" ]; then
      status="${C_DIM}— not built —${C_RESET}"
    else
      status="${C_BLUE}info: $workload (no rollout to compare)${C_RESET}"
    fi
    cluster_short="${C_DIM}n/a${C_RESET}        "
  elif [ -z "$reg" ]; then
    status="${C_DIM}not in registry${C_RESET}"
    reg_short="${C_DIM}—${C_RESET}           "
  elif [ "$cluster" = "MISSING" ]; then
    status="${C_RED}workload missing${C_RESET}"
    cluster_short="${C_DIM}—${C_RESET}           "
  elif [ -z "$cluster" ]; then
    status="${C_YELLOW}no pods${C_RESET}"
    cluster_short="${C_DIM}—${C_RESET}           "
  elif [ "$cluster" = "$reg" ]; then
    status="${C_GREEN}in sync${C_RESET}"
  else
    status="${C_YELLOW}stale (newer image in registry)${C_RESET}"
  fi

  printf "%-20s %-41s %-41s %s\n" "$name" "$cluster_short" "$reg_short" "$status"
done
