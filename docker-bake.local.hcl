// docker-bake.local.hcl
//
// Overlay for local k3d development — re-tags every image at localhost:5001
// (the k3d local registry) and adds a :git-sha tag so kubectl describe shows
// which build is running. Used only by `make redeploy-platform-bake` (the
// ORBIT_PARALLEL_BUILDS=1 path); CI continues to use docker-bake.hcl alone.
//
// Keep the local-platform group in sync with BAKE_PLATFORM_IMAGES in the
// Makefile and REDEPLOY_PLATFORM_SERVICES — a service missing here silently
// never rebuilds on the parallel path.
//
// Usage (typically driven by Makefile):
//   docker buildx bake -f docker-bake.hcl -f docker-bake.local.hcl \
//     --load local-platform

variable "GIT_SHA" {
  default = "dev"
}

// Re-tag every backend matrix target.
target "orbit-identity" {
  tags = [
    "localhost:5001/orbit-identity:latest",
    "localhost:5001/orbit-identity:${GIT_SHA}",
  ]
}
target "orbit-identity-proxy" {
  tags = [
    "localhost:5001/orbit-identity-proxy:latest",
    "localhost:5001/orbit-identity-proxy:${GIT_SHA}",
  ]
}
target "orbit-inference-gateway" {
  tags = [
    "localhost:5001/orbit-inference-gateway:latest",
    "localhost:5001/orbit-inference-gateway:${GIT_SHA}",
  ]
}
target "orbit-policy-service" {
  tags = [
    "localhost:5001/orbit-policy-service:latest",
    "localhost:5001/orbit-policy-service:${GIT_SHA}",
  ]
}
target "orbit-policy-gateway" {
  tags = [
    "localhost:5001/orbit-policy-gateway:latest",
    "localhost:5001/orbit-policy-gateway:${GIT_SHA}",
  ]
}
target "orbit-egress-gateway" {
  tags = [
    "localhost:5001/orbit-egress-gateway:latest",
    "localhost:5001/orbit-egress-gateway:${GIT_SHA}",
  ]
}
target "orbit-tool-gateway" {
  tags = [
    "localhost:5001/orbit-tool-gateway:latest",
    "localhost:5001/orbit-tool-gateway:${GIT_SHA}",
  ]
}
target "orbit-migrate" {
  tags = [
    "localhost:5001/orbit-migrate:latest",
    "localhost:5001/orbit-migrate:${GIT_SHA}",
  ]
}
target "orbit-example" {
  tags = [
    "localhost:5001/orbit-example:latest",
    "localhost:5001/orbit-example:${GIT_SHA}",
  ]
}

// Re-tag the standalone targets (different Dockerfiles / non-matrix names).
target "orbit-mcp" {
  tags = [
    "localhost:5001/orbit-mcp:latest",
    "localhost:5001/orbit-mcp:${GIT_SHA}",
  ]
}
target "orbit-agents" {
  tags = [
    "localhost:5001/orbit-agents:latest",
    "localhost:5001/orbit-agents:${GIT_SHA}",
  ]
}
target "orbit-triggers" {
  tags = [
    "localhost:5001/orbit-triggers:latest",
    "localhost:5001/orbit-triggers:${GIT_SHA}",
  ]
}
target "orbit-shell" {
  tags = [
    "localhost:5001/orbit-shell:latest",
    "localhost:5001/orbit-shell:${GIT_SHA}",
  ]
}
target "orbit-runtime-mcp" {
  tags = [
    "localhost:5001/orbit-runtime-mcp:latest",
    "localhost:5001/orbit-runtime-mcp:${GIT_SHA}",
  ]
}
target "orbit-session-manager" {
  tags = [
    "localhost:5001/orbit-session-manager:latest",
    "localhost:5001/orbit-session-manager:${GIT_SHA}",
  ]
}
target "orbit-sandbox-exec" {
  tags = [
    "localhost:5001/orbit-sandbox-exec:latest",
    "localhost:5001/orbit-sandbox-exec:${GIT_SHA}",
  ]
}
target "orbit-frontend" {
  args = {
    ORBIT_VERSION = GIT_SHA
  }
  tags = [
    "localhost:5001/orbit-frontend:latest",
    "localhost:5001/orbit-frontend:${GIT_SHA}",
  ]
}
target "orbit-operator" {
  tags = [
    "localhost:5001/orbit-operator:latest",
    "localhost:5001/orbit-operator:${GIT_SHA}",
  ]
}

// Local platform bring-up group: every long-lived service the operator pulls
// during `cluster-platform`. Excludes orbit-shell / orbit-runtime-mcp because
// they're produced as side-effect binaries inside the session-manager image.
group "local-platform" {
  targets = [
    "orbit-identity",
    "orbit-identity-proxy",
    "orbit-inference-gateway",
    "orbit-policy-service",
    "orbit-policy-gateway",
    "orbit-egress-gateway",
    "orbit-agents",
    "orbit-triggers",
    "orbit-frontend",
    "orbit-mcp",
    "orbit-tool-gateway",
    "orbit-migrate",
    "orbit-operator",
    "orbit-session-manager",
    "orbit-sandbox-exec",
  ]
}

group "local-tenant" {
  targets = ["orbit-example"]
}
