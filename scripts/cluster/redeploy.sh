#!/usr/bin/env bash
# =============================================================================
# redeploy.sh: build, push, and restart a single Orbit service in the k3d
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
${C_BOLD}redeploy.sh${C_RESET}: build, push, and restart an Orbit service.

Usage:
  $0 <service> [--tenant <slug>] [--build-only] [--no-build]

Services:
$(list_services | sed 's/^/  /')

Flags:
  --tenant <slug>   Target tenant namespace (defaults to 'example' for tenant-scoped services)
  --build-only      Build and push to localhost:5001; skip the kubectl rollout
  --no-build        Skip build+push; only do the kubectl rollout
  --print-image     Print the image name for <service> and exit
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
#   workload    Workload kind/name pair (deployment/orbit-foo or statefulset/{slug}).
#               Use {slug} as a placeholder for the agent slug (== tenant).
#   extra_workloads  Optional space-separated list of additional workloads that
#               run the same image and must be rolled alongside `workload`.
#   post_build  Optional script step run after the base image is built.
#               Currently used only by identity to layer the mkcert CA in.
#   restart     'true' to do a rollout restart, 'false' for build-only services.
#   tenant_default  Optional default tenant slug for {tenant}/{slug} substitution.
#   values_key  The orbit chart values key for this image; the pushed digest is
#               recorded under images.<values_key>.digest in
#               deploy/reference/environments/local.generated.yaml.

service_spec() {
  case "$1" in
    identity)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=identity
image=orbit-identity
values_key=identity
namespace=orbit-system
workload=deployment/orbit-identity
post_build=layer_mkcert_ca
restart=true
EOF
      ;;
    identity-proxy)
      # Rendered into chart-templated system pods and webhook-injected tenant
      # pods; rolled by re-applying the orbit release.
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=identity-proxy
image=orbit-identity-proxy
values_key=identityProxy
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
values_key=inferenceGateway
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
values_key=policyService
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
values_key=policyGateway
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
values_key=egressGateway
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
values_key=agents
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
values_key=frontend
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
values_key=orbitMcp
namespace=orbit-system
workload=deployment/orbit-mcp
restart=true
EOF
      ;;
    orbit-triggers)
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=orbit-triggers
image=orbit-triggers
values_key=triggers
namespace=orbit-system
workload=deployment/orbit-triggers
restart=true
EOF
      ;;
    tool-gateway)
      # The publisher runs the same orbit-tool-gateway image in a different mode,
      # so it must be rolled with the gateway or it keeps serving the old build.
      cat <<'EOF'
dockerfile=backend/Dockerfile
context=backend
build_arg=SERVICE=tool-gateway
image=orbit-tool-gateway
values_key=toolGateway
namespace=orbit-system
workload=deployment/orbit-tool-gateway
extra_workloads=deployment/orbit-tool-gateway-publisher
restart=true
EOF
      ;;
    operator)
      cat <<'EOF'
dockerfile=Dockerfile.operator
context=.
build_arg=
image=orbit-operator
values_key=operator
namespace=orbit-system
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
      # No long-lived workload; sandbox-exec pods are created on demand by the
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
identity-proxy      (sidecar; restart by deleting injected pods)
inference-gateway   (deployment, orbit-system)
policy-service      (deployment, orbit-system)
policy-gateway      (deployment, orbit-system)
egress-gateway      (deployment, orbit-system)
agents              (deployment, orbit-system)
frontend            (deployment, orbit-system)
orbit-mcp           (deployment, orbit-system)
tool-gateway        (deployment, orbit-system; also rolls the publisher)
orbit-triggers      (deployment, orbit-system)
operator            (deployment, orbit-system)
example             (deployment, orbit-tenant-<TENANT>)
session-manager     (statefulset, orbit-tenant-<TENANT>; --slug to override workload name)
sandbox-exec        (build-only; picked up by next agent run)
EOF
}

# ── rollout ───────────────────────────────────────────────────────────────────

# Pin one workload to the exact digest we just pushed. Deterministic (kubelet
# always pulls exactly this image) and surfaces the running version in
# `kubectl describe`. Falls back to a plain rollout restart if we can't resolve
# the digest (e.g. registry hiccup). Reads $NAMESPACE/$IMAGE/$DIGEST/$GIT_SHA.
pin_workload() {
  local workload="$1"
  local kind="${workload%%/*}"
  local name="${workload##*/}"
  local containers container_name current

  # `|| true`: under `set -euo pipefail` a missing workload would otherwise kill
  # the script mid-run with no message; reachable when a chart hasn't created
  # the Deployment yet, or on a cluster predating an extra_workloads entry.
  containers=$(kubectl get "$kind" "$name" -n "$NAMESPACE" \
    -o jsonpath='{range .spec.template.spec.containers[*]}{.name} {.image}{"\n"}{end}' 2>/dev/null || true)
  if [ -z "$containers" ]; then
    info "$workload not found in $NAMESPACE; skipping rollout"
    return 0
  fi

  # Match the container running *this* image rather than taking the first one.
  # Deployment specs are single-container today (istio/identity-proxy sidecars
  # are injected at admission, so they never appear here), but the publisher's
  # container is named for its image, not its workload; never match on $name.
  container_name=$(printf '%s\n' "$containers" | grep -E "/${IMAGE}[@:]" | awk '{print $1; exit}' || true)
  if [ -z "$container_name" ]; then
    container_name=$(printf '%s\n' "$containers" | awk 'NF {print $1; exit}')
  fi

  if [ -n "$DIGEST" ] && [ -n "$container_name" ]; then
    # Skip the rollout entirely when the workload is already pinned to this
    # exact digest. Saves ~30s of pod churn on a no-op redeploy. Even with
    # source unchanged, Docker often produces a fresh manifest digest because
    # the runtime-stage export isn't deterministic (timestamps, layer order).
    current=$(printf '%s\n' "$containers" | awk -v c="$container_name" '$1 == c {print $2; exit}')
    if [ "$current" = "orbit-registry:5001/$IMAGE@$DIGEST" ]; then
      ok "$workload already at $IMAGE@${DIGEST:0:19}; no rollout needed"
      return 0
    fi
    log "Pinning $workload ($container_name) to $IMAGE@${DIGEST:0:19}..."
    # Push uses localhost:5001 (host perspective) but cluster nodes resolve
    # the registry as orbit-registry:5001. Digests are content-addressed so
    # they're identical from either side.
    kubectl set image "$workload" \
      "$container_name=orbit-registry:5001/$IMAGE@$DIGEST" \
      -n "$NAMESPACE"
  else
    log "Restarting $workload in $NAMESPACE (digest unavailable)..."
    kubectl rollout restart "$workload" -n "$NAMESPACE"
  fi
  kubectl rollout status "$workload" -n "$NAMESPACE" --timeout=180s
  ok "$workload updated to $IMAGE:$GIT_SHA"
}

# ── post-build hooks ──────────────────────────────────────────────────────────

layer_mkcert_ca() {
  if [ ! -f secrets/mkcert-ca.pem ]; then
    info "secrets/mkcert-ca.pem not found; skipping CA layer (Dex TLS trust may break)"
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
PRINT_IMAGE=0

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --tenant) TENANT="$2"; shift 2 ;;
    --slug) SLUG="$2"; shift 2 ;;
    --build-only) BUILD_ONLY=1; shift ;;
    --no-build) NO_BUILD=1; shift ;;
    --print-image) PRINT_IMAGE=1; shift ;;
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
WORKLOAD=""; EXTRA_WORKLOADS=""; POST_BUILD=""; RESTART=""; TENANT_DEFAULT=""; VALUES_KEY=""
while IFS='=' read -r k v; do
  [ -z "$k" ] && continue
  case "$k" in
    dockerfile) DOCKERFILE="$v" ;;
    context) CONTEXT="$v" ;;
    build_arg) BUILD_ARG="$v" ;;
    image) IMAGE="$v" ;;
    namespace) NAMESPACE="$v" ;;
    workload) WORKLOAD="$v" ;;
    extra_workloads) EXTRA_WORKLOADS="$v" ;;
    post_build) POST_BUILD="$v" ;;
    restart) RESTART="$v" ;;
    tenant_default) TENANT_DEFAULT="$v" ;;
    values_key) VALUES_KEY="$v" ;;
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
EXTRA_WORKLOADS="${EXTRA_WORKLOADS//\{tenant\}/$TENANT}"
EXTRA_WORKLOADS="${EXTRA_WORKLOADS//\{slug\}/$SLUG}"

if [[ "$NAMESPACE" == *"{tenant}"* ]] || [[ "$WORKLOAD" == *"{tenant}"* ]] || [[ "$WORKLOAD" == *"{slug}"* ]] \
  || [[ "$EXTRA_WORKLOADS" == *"{tenant}"* ]] || [[ "$EXTRA_WORKLOADS" == *"{slug}"* ]]; then
  err "Service '$SERVICE' requires --tenant <slug>"
  exit 1
fi

# Callers that only need to know which image a service maps to, e.g. to test
# whether it is already in the registry before paying for a rebuild.
if [ "$PRINT_IMAGE" -eq 1 ]; then
  printf '%s\n' "$IMAGE"
  exit 0
fi

# ── preflight ─────────────────────────────────────────────────────────────────
# Without this, a torn-down cluster surfaces as a raw
# `dial tcp [::1]:5001: connect: connection refused` from `docker push`.
if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "orbit-registry"; then
  err "The local image registry (orbit-registry) is not running."
  echo "  k3d owns it, so the cluster is down. Bring it up with: make up" >&2
  exit 1
fi
if ! kubectl cluster-info >/dev/null 2>&1; then
  err "No reachable Kubernetes cluster for the current kubectl context."
  echo "  Context: $(kubectl config current-context 2>/dev/null || echo '<none>') (expected k3d-orbit-local)" >&2
  echo "  Bring the cluster up with 'make up', or switch context." >&2
  exit 1
fi

# ── build + push ──────────────────────────────────────────────────────────────

# git-sha tag for this build; both :latest and :<sha> get pushed, and rollouts
# use the @sha256 digest so kubectl describe shows the actual image and there's
# no chance of pulling a stale :latest. SHA may include "-dirty" if the worktree
# has uncommitted changes; that's intentional, the image differs from HEAD.
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
if [ "$BUILD_ONLY" -eq 0 ] && [ "$RESTART" = "true" ] && [ "$WORKLOAD" != "on-demand" ]; then
  DIGEST="$(docker buildx imagetools inspect "localhost:5001/$IMAGE:latest" 2>/dev/null \
    | awk '/^Digest:/ {print $2; exit}')"
fi

# Record the pushed digest in local.generated.yaml for subsequent Helmfile renders.
GENERATED="$ROOT_DIR/deploy/reference/environments/local.generated.yaml"
if [ -n "$DIGEST" ] && [ -n "$VALUES_KEY" ]; then
  [ -f "$GENERATED" ] || echo "{}" > "$GENERATED"
  yq -i ".images.${VALUES_KEY}.digest = \"${DIGEST}\"" "$GENERATED"
fi

# ── restart ───────────────────────────────────────────────────────────────────

if [ "$BUILD_ONLY" -eq 1 ]; then
  ok "$SERVICE built and pushed (build-only); tag $GIT_SHA"
  exit 0
fi

if [ "$RESTART" != "true" ]; then
  ok "$SERVICE built and pushed; no long-lived workload to restart (picked up on next run)"
  exit 0
fi

case "$WORKLOAD" in
  sidecar)
    # Referenced by chart templates and the operator's injection config;
    # re-applying the release rolls system pods, tenant pods on next creation.
    log "$SERVICE is a sidecar, re-applying the orbit release with the new digest..."
    (cd "$ROOT_DIR/deploy/reference" && helmfile -e local apply --selector name=orbit)
    ok "$SERVICE rolled via the orbit release"
    ;;
  on-demand)
    info "$SERVICE has no long-lived workload to restart"
    ;;
  *)
    pin_workload "$WORKLOAD"
    for extra in $EXTRA_WORKLOADS; do
      pin_workload "$extra"
    done
    ;;
esac
