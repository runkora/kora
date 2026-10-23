// docker-bake.local.hcl
//
// Overlay for local k3d development — re-tags every image at localhost:5001
// (the k3d local registry) and adds a :git-sha tag so kubectl describe shows
// which build is running. Used by `make redeploy-platform-bake` (the
// ORBIT_PARALLEL_BUILDS=1 path), `make redeploy-tenant-bake` and
// `make redeploy-sandbox-images`; CI continues to use docker-bake.hcl alone.
//
// Keep the local-platform group in sync with BAKE_PLATFORM_IMAGES in the
// Makefile and REDEPLOY_PLATFORM_SERVICES — a service missing here silently
// never rebuilds on the parallel path. Keep local-sandbox-images in sync with
// BAKE_SANDBOX_IMAGES.
//
// Usage (typically driven by Makefile):
//   docker buildx bake -f docker-bake.hcl -f docker-bake.local.hcl \
//     -f versions.hcl --load local-platform

variable "GIT_SHA" {
  default = "dev"
}

// Re-tag every backend matrix target.
target "kora-identity" {
  tags = [
    "localhost:5001/kora-identity:latest",
    "localhost:5001/kora-identity:${GIT_SHA}",
  ]
}
target "kora-identity-proxy" {
  tags = [
    "localhost:5001/kora-identity-proxy:latest",
    "localhost:5001/kora-identity-proxy:${GIT_SHA}",
  ]
}
target "kora-inference-gateway" {
  tags = [
    "localhost:5001/kora-inference-gateway:latest",
    "localhost:5001/kora-inference-gateway:${GIT_SHA}",
  ]
}
target "kora-policy-service" {
  tags = [
    "localhost:5001/kora-policy-service:latest",
    "localhost:5001/kora-policy-service:${GIT_SHA}",
  ]
}
target "kora-policy-gateway" {
  tags = [
    "localhost:5001/kora-policy-gateway:latest",
    "localhost:5001/kora-policy-gateway:${GIT_SHA}",
  ]
}
target "kora-egress-gateway" {
  tags = [
    "localhost:5001/kora-egress-gateway:latest",
    "localhost:5001/kora-egress-gateway:${GIT_SHA}",
  ]
}
target "kora-tool-gateway" {
  tags = [
    "localhost:5001/kora-tool-gateway:latest",
    "localhost:5001/kora-tool-gateway:${GIT_SHA}",
  ]
}
target "kora-migrate" {
  tags = [
    "localhost:5001/kora-migrate:latest",
    "localhost:5001/kora-migrate:${GIT_SHA}",
  ]
}
target "kora-example" {
  tags = [
    "localhost:5001/kora-example:latest",
    "localhost:5001/kora-example:${GIT_SHA}",
  ]
}

// Re-tag the standalone targets (different Dockerfiles / non-matrix names).
target "kora-mcp" {
  tags = [
    "localhost:5001/kora-mcp:latest",
    "localhost:5001/kora-mcp:${GIT_SHA}",
  ]
}
target "kora-agents" {
  tags = [
    "localhost:5001/kora-agents:latest",
    "localhost:5001/kora-agents:${GIT_SHA}",
  ]
}
target "kora-triggers" {
  tags = [
    "localhost:5001/kora-triggers:latest",
    "localhost:5001/kora-triggers:${GIT_SHA}",
  ]
}
target "kora-shell" {
  tags = [
    "localhost:5001/kora-shell:latest",
    "localhost:5001/kora-shell:${GIT_SHA}",
  ]
}
target "kora-runtime-mcp" {
  tags = [
    "localhost:5001/kora-runtime-mcp:latest",
    "localhost:5001/kora-runtime-mcp:${GIT_SHA}",
  ]
}
target "kora-session-manager" {
  tags = [
    "localhost:5001/kora-session-manager:latest",
    "localhost:5001/kora-session-manager:${GIT_SHA}",
  ]
}
target "kora-sandbox-mount" {
  tags = [
    "localhost:5001/kora-sandbox-mount:latest",
    "localhost:5001/kora-sandbox-mount:${GIT_SHA}",
  ]
}
target "kora-sandbox-base" {
  args = {
    SANDBOX_BASE_VERSION = GIT_SHA
  }
  tags = [
    "localhost:5001/kora-sandbox-base:latest",
    "localhost:5001/kora-sandbox-base:${GIT_SHA}",
  ]
}
// The bundled sandbox images and kora-sandbox-dev. They stay out of local-platform, and make up does
// not build them. Build them with the local-sandbox-images group.
target "kora-sandbox-go" {
  tags = [
    "localhost:5001/kora-sandbox-go:latest",
    "localhost:5001/kora-sandbox-go:${GIT_SHA}",
  ]
}
target "kora-sandbox-python" {
  tags = [
    "localhost:5001/kora-sandbox-python:latest",
    "localhost:5001/kora-sandbox-python:${GIT_SHA}",
  ]
}
target "kora-sandbox-node" {
  tags = [
    "localhost:5001/kora-sandbox-node:latest",
    "localhost:5001/kora-sandbox-node:${GIT_SHA}",
  ]
}
target "kora-sandbox-dev" {
  tags = [
    "localhost:5001/kora-sandbox-dev:latest",
    "localhost:5001/kora-sandbox-dev:${GIT_SHA}",
  ]
}
target "kora-frontend" {
  args = {
    ORBIT_VERSION = GIT_SHA
  }
  tags = [
    "localhost:5001/kora-frontend:latest",
    "localhost:5001/kora-frontend:${GIT_SHA}",
  ]
}
target "kora-operator" {
  tags = [
    "localhost:5001/kora-operator:latest",
    "localhost:5001/kora-operator:${GIT_SHA}",
  ]
}

// Local platform bring-up group: every long-lived service the operator pulls
// during `cluster-platform`. Excludes kora-shell / kora-runtime-mcp because
// they're produced as side-effect binaries inside the session-manager image.
group "local-platform" {
  targets = [
    "kora-identity",
    "kora-identity-proxy",
    "kora-inference-gateway",
    "kora-policy-service",
    "kora-policy-gateway",
    "kora-egress-gateway",
    "kora-agents",
    "kora-triggers",
    "kora-frontend",
    "kora-mcp",
    "kora-tool-gateway",
    "kora-migrate",
    "kora-operator",
    "kora-session-manager",
    "kora-sandbox-mount",
    "kora-sandbox-base",
  ]
}

group "local-tenant" {
  targets = ["kora-example"]
}

group "local-sandbox-images" {
  targets = ["kora-sandbox-go", "kora-sandbox-python", "kora-sandbox-node", "kora-sandbox-dev"]
}
