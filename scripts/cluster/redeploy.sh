#!/usr/bin/env bash
# =============================================================================
# redeploy.sh — Build, push, and restart a single Orbit service in the k3d
# cluster. Driven by the per-service registry below so every service uses the
# same entry point, and edge cases (identity's mkcert CA layer, session-manager
# living in a tenant namespace as a StatefulSet, sandbox-exec being build-only)
# are encoded in one place instead of scattered across the Makefile.
#
# Usage:
#   scripts/cluster/redeploy.sh <service> [--tenant <slug>] [--build-only] [--no-build]
#
# Examples:
#   scripts/cluster/redeploy.sh identity
#   scripts/cluster/redeploy.sh frontend
#   scripts/cluster/redeploy.sh session-manager --tenant example
#   scripts/cluster/redeploy.sh sandbox-exec --build-only   # build & push only
#
# To add a new service, register it in the `service_spec` function below.
# =============================================================================

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

# Colors (matched to Makefile palette).
C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_RED=$'\033[31m'
C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_BLUE=$'\033[36m'

log()  { printf "%s\n" "${C_YELLOW}$*${C_RESET}"; }
ok()   { printf "%s\n" "${C_GREEN}✓ $*${C_RESET}"; }
err()  { printf "%s\n" "${C_RED}✗ $*${C_RESET}" >&2; }
info() { printf "%s\n" "${C_BLUE}ℹ $*${C_RESET}"; }

usage() {
  cat <<EOF
${C_BOLD}redeploy.sh${C_RESET} — build, push, and restart an Orbit service.

Usage:
  $0 <service> [--tenant <slug>] [--build-only] [--no-build]

Services:
$(list_services | sed 's/^/  /')

Flags:
  --tenant <slug>   Target tenant namespace (defaults to 'example' for tenant-scoped services)
  --build-only      Build and push to localhost:5001; skip the kubectl rollout
  --no-build        Skip build+push; only do the kubectl rollout
EOF
}

# ── service registry ──────────────────────────────────────────────────────────
# Each spec is whitespace-separated key=value, lowercase keys.
#
#   dockerfile  Path to the Dockerfile, relative to repo root.
#   context     Build context, relative to repo root.
#   build_arg   Optional --build-arg, KEY=VALUE; empty if not needed.
#   image       Image name (without tag); IMAGE=orbit-foo → orbit-foo:latest.
#   namespace   Cluster namespace. Use {tenant} as a placeholder.
#   workload    Comma-separated list of kind/name pairs (deployment/orbit-foo or
#               statefulset/{slug}). Multiple entries let one image update several
#               workloads (e.g. tool-gateway's proxy + publisher deployments).
#               Use {slug} as a placeholder for the agent slug (== tenant).
#   post_build  Optional script step run after the base image is built.
#               Currently used only by identity to layer the mkcert CA in.
#   restart     'true' to do a rollout restart, 'false' for build-only services.
#   tenant_default  Optional default tenant slug for {tenant}/{slug} substitution.

service_spec() {
  case "$1" in
    identity)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=identity
image=orbit-identity
namespace=orbit-system
workload=deployment/orbit-identity
post_build=layer_mkcert_ca
restart=true
EOF
      ;;
    identity-proxy)
      # Sidecar injected via operator template; restart by deleting injected pods.
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=identity-proxy
image=orbit-identity-proxy
namespace=orbit-system
workload=sidecar
restart=true
EOF
      ;;
    inference-gateway)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=inference-gateway
image=orbit-inference-gateway
namespace=orbit-system
workload=deployment/orbit-inference-gateway
restart=true
EOF
      ;;
    policy-service)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=policy-service
image=orbit-policy-service
namespace=orbit-system
workload=deployment/orbit-policy-service
restart=true
EOF
      ;;
    policy-gateway)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=policy-gateway
image=orbit-policy-gateway
namespace=orbit-system
workload=deployment/orbit-policy-gateway
restart=true
EOF
      ;;
    egress-gateway)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=egress-gateway
image=orbit-egress-gateway
namespace=orbit-system
workload=deployment/orbit-egress-gateway
restart=true
EOF
      ;;
    agents|orbit-agents)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=orbit-agents
image=orbit-agents
namespace=orbit-system
workload=deployment/orbit-agents
restart=true
EOF
      ;;
    frontend)
      cat <<'EOF'
dockerfile=frontend/Dockerfile
context=frontend
build_arg=NEXT_PUBLIC_APP_URL=https://orbit.local.clearroute.io:8443
image=orbit-frontend
namespace=orbit-system
workload=deployment/orbit-frontend
restart=true
EOF
      ;;
    orbit-mcp)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=orbit-mcp
image=orbit-mcp
namespace=orbit-system
workload=deployment/orbit-mcp
restart=true
EOF
      ;;
    tool-gateway)
      # Publisher deployment shares this image (ORBIT_TOOL_GATEWAY_MODE=publisher
      # env var is the only difference) — pin both on redeploy.
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=tool-gateway
image=orbit-tool-gateway
namespace=orbit-system
workload=deployment/orbit-tool-gateway,deployment/orbit-tool-gateway-publisher
restart=true
EOF
      ;;
    operator)
      cat <<'EOF'
dockerfile=Dockerfile.operator
context=.
build_arg=
image=orbit-operator
namespace=orbit-operator
workload=deployment/orbit-operator
restart=true
EOF
      ;;
    example)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=example
image=orbit-example
namespace=orbit-tenant-{tenant}
workload=deployment/orbit-example
restart=true
tenant_default=example
EOF
      ;;
    session-manager)
      # Per-tenant StatefulSet whose name == agent slug. Defaults to {tenant} for
      # the local 'example' agent, which uses the same name. Override by passing
      # --slug for non-default agents.
      cat <<'EOF'
dockerfile=backend/Dockerfile.session-manager
context=backend
build_arg=
image=orbit-session-manager
namespace=orbit-tenant-{tenant}
workload=statefulset/{slug}
restart=true
tenant_default=example
EOF
      ;;
    sandbox-exec)
      # No long-lived workload — sandbox-exec pods are created on demand by the
      # session-manager. Push the new image and let the next run pick it up.
      cat <<'EOF'
dockerfile=backend/Dockerfile.sandbox-exec
context=backend
build_arg=
image=orbit-sandbox-exec
namespace=
workload=on-demand
restart=false
EOF
      ;;
    *) return 1 ;;
  esac
}

list_services() {
  cat <<'EOF'
identity            (deployment, orbit-system, +mkcert CA layer)
identity-proxy      (sidecar — restart by deleting injected pods)
inference-gateway   (deployment, orbit-system)
policy-service      (deployment, orbit-system)
policy-gateway      (deployment, orbit-system)
agents              (deployment, orbit-system)
frontend            (deployment, orbit-system)
orbit-mcp           (deployment, orbit-system)
operator            (deployment, orbit-operator)
example             (deployment, orbit-tenant-<TENANT>)
session-manager     (statefulset, orbit-tenant-<TENANT>; --slug to override workload name)
sandbox-exec        (build-only — picked up by next agent run)
EOF
}

# ── post-build hooks ──────────────────────────────────────────────────────────

layer_mkcert_ca() {
  if [ ! -f secrets/mkcert-ca.pem ]; then
    info "secrets/mkcert-ca.pem not found — skipping CA layer (Dex TLS trust may break)"
    return 0
  fi
  log "Layering mkcert CA into orbit-identity for local Dex TLS trust..."
  printf 'FROM orbit-identity:latest\nCOPY mkcert-ca.pem /usr/local/share/ca-certificates/mkcert-ca.crt\nRUN update-ca-certificates\n' | \
    docker build -t orbit-identity:latest -f - secrets/
  ok "orbit-identity:latest (with mkcert CA)"
}

# ── argument parsing ──────────────────────────────────────────────────────────

SERVICE=""
TENANT=""
SLUG=""
BUILD_ONLY=0
NO_BUILD=0

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --tenant) TENANT="$2"; shift 2 ;;
    --slug) SLUG="$2"; shift 2 ;;
    --build-only) BUILD_ONLY=1; shift ;;
    --no-build) NO_BUILD=1; shift ;;
    --) shift; break ;;
    -*) err "Unknown flag: $1"; usage; exit 2 ;;
    *)
      if [ -z "$SERVICE" ]; then SERVICE="$1"; else err "Unexpected arg: $1"; exit 2; fi
      shift
      ;;
  esac
done

if [ -z "$SERVICE" ]; then
  usage
  exit 1
fi

if ! SPEC="$(service_spec "$SERVICE")"; then
  err "Unknown service: $SERVICE"
  echo
  echo "Known services:"
  list_services | sed 's/^/  /'
  exit 1
fi

# Parse spec.
DOCKERFILE=""; CONTEXT=""; BUILD_ARG=""; IMAGE=""; NAMESPACE=""
WORKLOAD=""; POST_BUILD=""; RESTART=""; TENANT_DEFAULT=""
while IFS='=' read -r k v; do
  [ -z "$k" ] && continue
  case "$k" in
    dockerfile) DOCKERFILE="$v" ;;
    context) CONTEXT="$v" ;;
    build_arg) BUILD_ARG="$v" ;;
    image) IMAGE="$v" ;;
    namespace) NAMESPACE="$v" ;;
    workload) WORKLOAD="$v" ;;
    post_build) POST_BUILD="$v" ;;
    restart) RESTART="$v" ;;
    tenant_default) TENANT_DEFAULT="$v" ;;
  esac
done <<< "$SPEC"

# Default tenant if not provided but service needs one.
if [ -z "$TENANT" ] && [ -n "$TENANT_DEFAULT" ]; then
  TENANT="$TENANT_DEFAULT"
fi
# slug defaults to tenant (matches the local example agent layout).
if [ -z "$SLUG" ]; then SLUG="$TENANT"; fi

# Substitute placeholders.
NAMESPACE="${NAMESPACE//\{tenant\}/$TENANT}"
WORKLOAD="${WORKLOAD//\{tenant\}/$TENANT}"
WORKLOAD="${WORKLOAD//\{slug\}/$SLUG}"

if [[ "$NAMESPACE" == *"{tenant}"* ]] || [[ "$WORKLOAD" == *"{tenant}"* ]] || [[ "$WORKLOAD" == *"{slug}"* ]]; then
  err "Service '$SERVICE' requires --tenant <slug>"
  exit 1
fi

# ── build + push ──────────────────────────────────────────────────────────────

# git-sha tag for this build — both :latest and :<sha> get pushed, and rollouts
# use the @sha256 digest so kubectl describe shows the actual image and there's
# no chance of pulling a stale :latest. SHA may include "-dirty" if the worktree
# has uncommitted changes — that's intentional, the image differs from HEAD.
GIT_SHA="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
if [ -n "$(git -C "$ROOT_DIR" status --porcelain 2>/dev/null)" ]; then
  GIT_SHA="${GIT_SHA}-dirty"
fi

DIGEST=""

if [ "$NO_BUILD" -eq 0 ]; then
  log "Building $IMAGE:$GIT_SHA from $DOCKERFILE (context=$CONTEXT)..."
  build_args=()
  if [ -n "$BUILD_ARG" ]; then build_args+=(--build-arg "$BUILD_ARG"); fi
  docker build "${build_args[@]+"${build_args[@]}"}" \
    -t "$IMAGE:latest" \
    -t "$IMAGE:$GIT_SHA" \
    -f "$DOCKERFILE" "$CONTEXT"
  ok "$IMAGE:latest, $IMAGE:$GIT_SHA"

  if [ -n "$POST_BUILD" ]; then
    "$POST_BUILD"
    # Re-tag git-sha after post-build hook (which rebuilds :latest).
    docker tag "$IMAGE:latest" "$IMAGE:$GIT_SHA"
  fi

  log "Pushing $IMAGE to localhost:5001 (latest + $GIT_SHA)..."
  docker tag "$IMAGE:latest" "localhost:5001/$IMAGE:latest"
  docker tag "$IMAGE:$GIT_SHA" "localhost:5001/$IMAGE:$GIT_SHA"
  docker push --quiet "localhost:5001/$IMAGE:latest"
  docker push --quiet "localhost:5001/$IMAGE:$GIT_SHA"
  ok "localhost:5001/$IMAGE:{latest,$GIT_SHA}"
fi

# Resolve the digest we'll pin the deployment to. Even with NO_BUILD=1 we still
# want a deterministic image reference, so query the registry. `docker buildx
# imagetools inspect --format` is unreliable across versions, so just grep the
# default output for the Digest line.
if [ "$BUILD_ONLY" -eq 0 ] && [ "$RESTART" = "true" ] && [ "$WORKLOAD" != "sidecar" ] && [ "$WORKLOAD" != "on-demand" ]; then
  DIGEST="$(docker buildx imagetools inspect "localhost:5001/$IMAGE:latest" 2>/dev/null \
    | awk '/^Digest:/ {print $2; exit}')"
fi

# ── restart ───────────────────────────────────────────────────────────────────

if [ "$BUILD_ONLY" -eq 1 ]; then
  ok "$SERVICE built and pushed (build-only) — tag $GIT_SHA"
  exit 0
fi

if [ "$RESTART" != "true" ]; then
  ok "$SERVICE built and pushed — no long-lived workload to restart (picked up on next run)"
  exit 0
fi

case "$WORKLOAD" in
  sidecar)
    log "$SERVICE is a sidecar — deleting injected pods so they're recreated with the new image..."
    # Identity-proxy is injected into orbit-system data-plane pods (anything that
    # talks outbound through the platform token). Restart all deployments in
    # orbit-system that have istio injection enabled — operator's webhook will
    # re-inject the new image on the way back up.
    pods=$(kubectl get pods -n "$NAMESPACE" -l 'orbit.dev/identity-proxy=injected' -o name 2>/dev/null || true)
    if [ -z "$pods" ]; then
      info "No pods found with label orbit.dev/identity-proxy=injected — falling back to restart-all in $NAMESPACE"
      kubectl rollout restart deployment -n "$NAMESPACE"
    else
      echo "$pods" | xargs kubectl delete -n "$NAMESPACE" --grace-period=10
    fi
    ok "$SERVICE pods restarted"
    ;;
  on-demand)
    info "$SERVICE has no long-lived workload to restart"
    ;;
  *)
    # Pin to the exact digest we just pushed. This is deterministic (kubelet
    # always pulls exactly this image) and surfaces the running version in
    # `kubectl describe`. Falls back to a plain rollout restart if we can't
    # resolve the digest (e.g. registry hiccup).
    IFS=',' read -r -a workloads <<< "$WORKLOAD"
    for wl in "${workloads[@]}"; do
      kind="${wl%%/*}"
      name="${wl##*/}"
      container_name=$(kubectl get "$kind" "$name" -n "$NAMESPACE" \
        -o jsonpath='{.spec.template.spec.containers[?(@.image)].name}' 2>/dev/null \
        | awk '{print $1}')
      if [ -n "$DIGEST" ] && [ -n "$container_name" ]; then
        # Skip the rollout entirely when the deployment is already pinned to this
        # exact digest. Saves ~30s of pod churn on a no-op redeploy. Even with
        # source unchanged, Docker often produces a fresh manifest digest because
        # the runtime-stage export isn't deterministic (timestamps, layer order).
        current=$(kubectl get "$wl" -n "$NAMESPACE" \
          -o jsonpath='{.spec.template.spec.containers[?(@.name=="'"$container_name"'")].image}' 2>/dev/null)
        if [ "$current" = "orbit-registry:5001/$IMAGE@$DIGEST" ]; then
          ok "$wl already at $IMAGE@${DIGEST:0:19} — no rollout needed"
          continue
        fi
        log "Pinning $wl ($container_name) to $IMAGE@${DIGEST:0:19}..."
        # Push uses localhost:5001 (host perspective) but cluster nodes resolve
        # the registry as orbit-registry:5001. Digests are content-addressed so
        # they're identical from either side.
        kubectl set image "$wl" \
          "$container_name=orbit-registry:5001/$IMAGE@$DIGEST" \
          -n "$NAMESPACE"
      else
        log "Restarting $wl in $NAMESPACE (digest unavailable)..."
        kubectl rollout restart "$wl" -n "$NAMESPACE"
      fi
      kubectl rollout status "$wl" -n "$NAMESPACE" --timeout=180s
      ok "$wl updated to $IMAGE:$GIT_SHA"
    done
    ;;
esac
