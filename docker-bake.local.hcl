// docker-bake.local.hcl
//
// Overlay for local k3d development — re-tags every image at localhost:5001
// (the k3d local registry) and adds a :git-sha tag so kubectl describe shows
// which build is running. Used only by `make redeploy-platform-bake`; CI
// continues to use docker-bake.hcl alone.
//
// Usage (typically driven by Makefile):
//   docker buildx bake -f docker-bake.hcl -f docker-bake.local.hcl \
//     --no-parallel --load <target>

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
target "orbit-example" {
  tags = [
    "localhost:5001/orbit-example:latest",
    "localhost:5001/orbit-example:${GIT_SHA}",
  ]
}

// Re-tag the standalone targets (different Dockerfiles).
target "orbit-agents" {
  tags = [
    "localhost:5001/orbit-agents:latest",
    "localhost:5001/orbit-agents:${GIT_SHA}",
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
// during `cluster-operator`. Excludes orbit-shell / orbit-runtime-mcp because
// they're produced as side-effect binaries inside the session-manager image.
group "local-platform" {
  targets = [
    "orbit-identity",
    "orbit-identity-proxy",
    "orbit-inference-gateway",
    "orbit-policy-service",
    "orbit-policy-gateway",
    "orbit-agents",
    "orbit-frontend",
    "orbit-operator",
    "orbit-session-manager",
    "orbit-sandbox-exec",
  ]
}

group "local-tenant" {
  targets = ["orbit-example"]
}
