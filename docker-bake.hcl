// docker-bake.hcl
//
// Uses matrix targets to enumerate every backend image built by the generic
// backend/Dockerfile. Each expanded target maps 1:1 to a directory under
// backend/cmd/ and supplies the SERVICE build arg that selects the correct
// main package.

variable "REGISTRY" {
  default = "ghcr.io"
}

variable "IMAGE_NAMESPACE" {
  default = "orbit-sh"
}

variable "IMAGE_TAG" {
  default = "latest"
}

variable "REPOSITORY_URL" {
  default = "https://github.com/clear-route/orbit"
}

variable "NEXT_PUBLIC_APP_URL" {
  default = "https://orbit.local.clearroute.io:8443"
}

target "_backend-default" {
  context    = "./backend"
  dockerfile = "Dockerfile"
  pull       = true
}

target "backend-images" {
  inherits = ["_backend-default"]
  name     = "orbit-${item.service}"
  matrix = {
    item = [
      { service = "identity" },
      { service = "identity-proxy" },
      { service = "inference-gateway" },
      { service = "policy-service" },
      { service = "policy-gateway" },
      { service = "example" },
    ]
  }
  args = {
    SERVICE = item.service
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-${item.service}"
    "org.opencontainers.image.description" = "Orbit backend service image for ${item.service}"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-${item.service}:${IMAGE_TAG}"]
}

target "backend-tenant-images" {
  inherits = ["_backend-default"]
  name     = "orbit-${item.service}"
  matrix = {
    item = [
      { service = "example" },
    ]
  }
  args = {
    SERVICE = item.service
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-${item.service}"
    "org.opencontainers.image.description" = "Orbit backend service image for ${item.service}"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-${item.service}:${IMAGE_TAG}"]
}

target "orbit-agents" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "orbit-agents"
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-agents"
    "org.opencontainers.image.description" = "Orbit agents service — agent type registry and run lifecycle management"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-agents:${IMAGE_TAG}"]
}

target "orbit-shell" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "orbit-shell"
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-shell"
    "org.opencontainers.image.description" = "Orbit shell proxy for agent sandbox environments"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-shell:${IMAGE_TAG}"]
}

target "orbit-runtime-mcp" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "orbit-runtime-mcp"
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-runtime-mcp"
    "org.opencontainers.image.description" = "Orbit MCP server for agent runtime tooling"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-runtime-mcp:${IMAGE_TAG}"]
}

target "orbit-session-manager" {
  context    = "./backend"
  dockerfile = "Dockerfile.session-manager"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "orbit-session-manager"
    "org.opencontainers.image.description" = "Orbit session-manager — Go binary + Node.js for Claude Code integration"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-session-manager:${IMAGE_TAG}"]
}

target "orbit-sandbox-exec" {
  context    = "./backend"
  dockerfile = "Dockerfile.sandbox-exec"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "orbit-sandbox-exec"
    "org.opencontainers.image.description" = "Orbit sandbox-exec — gRPC exec server for isolated agent run environments"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-sandbox-exec:${IMAGE_TAG}"]
}

target "orbit-frontend" {
  context    = "./frontend"
  dockerfile = "Dockerfile"
  pull       = true
  args = {
    NEXT_PUBLIC_APP_URL = NEXT_PUBLIC_APP_URL
  }
  labels = {
    "org.opencontainers.image.title" = "orbit-frontend"
    "org.opencontainers.image.description" = "Orbit frontend service image"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-frontend:${IMAGE_TAG}"]
}

target "orbit-operator" {
  context    = "."
  dockerfile = "Dockerfile.operator"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "orbit-operator"
    "org.opencontainers.image.description" = "Orbit operator image"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/orbit-operator:${IMAGE_TAG}"]
}
