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
  "egress-gateway      orbit-egress-gateway      orbit-system            deployment/orbit-egress-gateway"
  "agents              orbit-agents              orbit-system            deployment/orbit-agents"
  "frontend            orbit-frontend            orbit-system            deployment/orbit-frontend"
  "orbit-mcp           orbit-mcp                 orbit-system            deployment/orbit-mcp"
  "tool-gateway        orbit-tool-gateway        orbit-system            deployment/orbit-tool-gateway"
  "tool-gw-publisher   orbit-tool-gateway        orbit-system            deployment/orbit-tool-gateway-publisher"
  "operator            orbit-operator            orbit-operator          deployment/orbit-operator"
  "example             orbit-example             orbit-tenant-example    deployment/orbit-example"
  "session-manager     orbit-session-manager     orbit-tenant-example    statefulset/example"
  "identity-proxy      orbit-identity-proxy      orbit-system            sidecar"
  "migrate             orbit-migrate             orbit-system            release"
  "sandbox-base        kora-sandbox-base         -                       on-demand"
)

# Local-registry digest for an image. Returns "" if the image isn't in the registry.
registry_digest() {
  local image="$1"
  docker buildx imagetools inspect "localhost:5001/$image:latest" 2>/dev/null \
    | awk '/^Digest:/ {print $2; exit}'
}

# Cluster digest for a workload. Returns the @sha256:... digest the workload is
# pinned to, "MISSING" if there's no such workload, or "" if it isn't pinned.
#
# We read the *spec* image, not the running pod's status.imageID. imageID is the
# repo digest containerd resolved the image to, and when a rebuild produces
# byte-identical content containerd reports a digest it already had on disk —
# so a freshly-rolled pod looks stale even though it is running the new image.
# The spec image is what redeploy.sh pinned, which is the question being asked.
#
# Containers are matched by image name (orbit-foo), not workload name: the
# tool-gateway publisher runs the orbit-tool-gateway image, and every meshed pod
# carries istio-proxy/identity-proxy sidecars we must not read the digest from.
cluster_digest() {
  local namespace="$1" workload="$2" image="$3"
  if [ "$namespace" = "-" ] || [ "$workload" = "on-demand" ] || [ "$workload" = "sidecar" ] || [ "$workload" = "release" ]; then
    echo ""
    return
  fi
  local kind="${workload%%/*}"
  local name="${workload##*/}"
  local images
  images=$(kubectl get "$kind" "$name" -n "$namespace" \
    -o jsonpath='{range .spec.template.spec.containers[*]}{.image}{"\n"}{end}' 2>/dev/null || true)
  if [ -z "$images" ]; then
    echo "MISSING"
    return
  fi
  local img
  img=$(printf '%s\n' "$images" | grep -E "/${image}[@:]" | head -1 || true)
  case "$img" in
    *@sha256:*) echo "${img##*@}" ;;
    # Pinned by tag (fresh install, or helm reclaimed the field) — nothing to compare.
    *) echo "" ;;
  esac
}

printf "%s\n" "${C_BLUE}Service              Cluster                                   Registry                                  Status${C_RESET}"
printf "%s\n" "${C_DIM}─────────────────────────────────────────────────────────────────────────────────────────────────────────────────${C_RESET}"

for row in "${SERVICES[@]}"; do
  read -r name image namespace workload <<< "$row"
  reg=$(registry_digest "$image")
  cluster=$(cluster_digest "$namespace" "$workload" "$image")

  reg_short="${reg#sha256:}"; reg_short="${reg_short:0:12}"
  cluster_short="${cluster#sha256:}"; cluster_short="${cluster_short:0:12}"

  if [ "$workload" = "on-demand" ] || [ "$workload" = "sidecar" ] || [ "$workload" = "release" ]; then
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
    status="${C_BLUE}info: pinned by tag (redeploy to pin a digest)${C_RESET}"
    cluster_short="${C_DIM}—${C_RESET}           "
  elif [ "$cluster" = "$reg" ]; then
    status="${C_GREEN}in sync${C_RESET}"
  else
    status="${C_YELLOW}stale (newer image in registry)${C_RESET}"
  fi

  printf "%-20s %-41s %-41s %s\n" "$name" "$cluster_short" "$reg_short" "$status"
done
