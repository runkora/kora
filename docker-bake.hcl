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
  default = "runkora"
}

variable "IMAGE_TAG" {
  default = "latest"
}

variable "REPOSITORY_URL" {
  default = "https://github.com/runkora/kora"
}

variable "NEXT_PUBLIC_APP_URL" {
  default = "https://local.runkora.io:8443"
}

// Set from versions.hcl, which every build passes as a second file. A build
// without them fails at the step that needs them. `bake --print` works without
// them, for the release scripts that only read tags.
variable "GO_VERSION" {}
variable "GOLANGCI_LINT_VERSION" {}
variable "BUF_VERSION" {}
variable "PNPM_VERSION" {}
variable "PROTOC_GEN_GO_VERSION" {}
variable "PROTOC_GEN_GO_GRPC_VERSION" {}
variable "DEADCODE_VERSION" {}
variable "GITLEAKS_VERSION" {}
variable "HELM_VERSION" {}

target "_backend-default" {
  context    = "./backend"
  dockerfile = "Dockerfile"
  pull       = true
}

target "backend-images" {
  inherits = ["_backend-default"]
  name     = "kora-${item.service}"
  matrix = {
    item = [
      { service = "identity" },
      { service = "identity-proxy" },
      { service = "inference-gateway" },
      { service = "policy-service" },
      { service = "policy-gateway" },
      { service = "egress-gateway" },
      { service = "tool-gateway" },
      { service = "migrate" },
      { service = "example" },
    ]
  }
  args = {
    SERVICE = item.service
  }
  labels = {
    "org.opencontainers.image.title" = "kora-${item.service}"
    "org.opencontainers.image.description" = "kora backend service image for ${item.service}"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-${item.service}:${IMAGE_TAG}"]
}

target "backend-tenant-images" {
  inherits = ["_backend-default"]
  name     = "kora-${item.service}"
  matrix = {
    item = [
      { service = "example" },
    ]
  }
  args = {
    SERVICE = item.service
  }
  labels = {
    "org.opencontainers.image.title" = "kora-${item.service}"
    "org.opencontainers.image.description" = "kora backend service image for ${item.service}"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-${item.service}:${IMAGE_TAG}"]
}

target "kora-agents" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "kora-agents"
  }
  labels = {
    "org.opencontainers.image.title" = "kora-agents"
    "org.opencontainers.image.description" = "kora agents service — agent type registry and run lifecycle management"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-agents:${IMAGE_TAG}"]
}

target "kora-triggers" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "kora-triggers"
  }
  labels = {
    "org.opencontainers.image.title" = "kora-triggers"
    "org.opencontainers.image.description" = "kora triggers service — webhook ingestion, trigger management API, manual fire, and the cron scheduler"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-triggers:${IMAGE_TAG}"]
}

target "kora-shell" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "kora-shell"
  }
  labels = {
    "org.opencontainers.image.title" = "kora-shell"
    "org.opencontainers.image.description" = "kora shell proxy for agent sandbox environments"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-shell:${IMAGE_TAG}"]
}

// Not a matrix entry: the matrix names images "kora-${service}", which would
// yield "kora-kora-mcp" for the kora-mcp service directory.
target "kora-mcp" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "kora-mcp"
  }
  labels = {
    "org.opencontainers.image.title" = "kora-mcp"
    "org.opencontainers.image.description" = "kora MCP server — kora's management surface as MCP tools over Streamable HTTP"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-mcp:${IMAGE_TAG}"]
}

target "kora-runtime-mcp" {
  inherits = ["_backend-default"]
  args = {
    SERVICE = "kora-runtime-mcp"
  }
  labels = {
    "org.opencontainers.image.title" = "kora-runtime-mcp"
    "org.opencontainers.image.description" = "kora MCP server for agent runtime tooling"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-runtime-mcp:${IMAGE_TAG}"]
}

target "kora-session-manager" {
  context    = "./backend"
  dockerfile = "Dockerfile.session-manager"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "kora-session-manager"
    "org.opencontainers.image.description" = "kora session-manager — Go binary + Node.js for Claude Code integration"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-session-manager:${IMAGE_TAG}"]
}

target "kora-sandbox-mount" {
  context    = "./backend"
  dockerfile = "Dockerfile.sandbox-mount"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-mount"
    "org.opencontainers.image.description" = "kora node agent that shares each sandbox's local folder with its session-manager pod over kernel NFS"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-mount:${IMAGE_TAG}"]
}

target "kora-sandbox-base" {
  context    = "./backend"
  dockerfile = "Dockerfile.sandbox-exec"
  pull       = true
  args = {
    SANDBOX_BASE_VERSION = IMAGE_TAG
    GO_VERSION           = GO_VERSION
  }
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-base"
    "org.opencontainers.image.description" = "kora sandbox base image: sandbox-exec, general CLIs, Go and Python"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-base:${IMAGE_TAG}"]
}

// Bundled sandbox images (docs/adr/2026-10-07-bundled-sandbox-images.md). Each
// builds FROM kora-sandbox-base, which the named context resolves to the base
// target in the same bake run. A matrix job never waits on a pushed base tag.
target "_sandbox-image" {
  context  = "./backend"
  contexts = {
    kora-sandbox-base = "target:kora-sandbox-base"
  }
  labels = {
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
}

target "kora-sandbox-go" {
  inherits   = ["_sandbox-image"]
  dockerfile = "Dockerfile.sandbox-go"
  args = {
    GOLANGCI_LINT_VERSION = GOLANGCI_LINT_VERSION
    BUF_VERSION           = BUF_VERSION
  }
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-go"
    "org.opencontainers.image.description" = "kora sandbox image: the base image plus golangci-lint, buf and protoc"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-go:${IMAGE_TAG}"]
}

target "kora-sandbox-python" {
  inherits   = ["_sandbox-image"]
  dockerfile = "Dockerfile.sandbox-python"
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-python"
    "org.opencontainers.image.description" = "kora sandbox image: the base image plus ruff and pytest"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-python:${IMAGE_TAG}"]
}

target "kora-sandbox-node" {
  inherits   = ["_sandbox-image"]
  dockerfile = "Dockerfile.sandbox-node"
  args = {
    PNPM_VERSION = PNPM_VERSION
  }
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-node"
    "org.opencontainers.image.description" = "kora sandbox image: the base image plus Node LTS and pnpm"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-node:${IMAGE_TAG}"]
}

// kora-dev: kora's own development tools at the versions in versions.hcl. The
// chart does not register it. kora's own deployment does.
target "kora-sandbox-dev" {
  inherits   = ["_sandbox-image"]
  dockerfile = "Dockerfile.sandbox-dev"
  contexts = {
    kora-sandbox-go   = "target:kora-sandbox-go"
    kora-sandbox-node = "target:kora-sandbox-node"
  }
  args = {
    PROTOC_GEN_GO_VERSION      = PROTOC_GEN_GO_VERSION
    PROTOC_GEN_GO_GRPC_VERSION = PROTOC_GEN_GO_GRPC_VERSION
    DEADCODE_VERSION           = DEADCODE_VERSION
    GITLEAKS_VERSION           = GITLEAKS_VERSION
    HELM_VERSION               = HELM_VERSION
  }
  labels = {
    "org.opencontainers.image.title" = "kora-sandbox-dev"
    "org.opencontainers.image.description" = "kora sandbox image for kora development: kora's pinned development tools"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-dev:${IMAGE_TAG}"]
}

target "kora-frontend" {
  context    = "./frontend"
  dockerfile = "Dockerfile"
  pull       = true
  args = {
    NEXT_PUBLIC_APP_URL = NEXT_PUBLIC_APP_URL
    KORA_VERSION        = IMAGE_TAG
  }
  labels = {
    "org.opencontainers.image.title" = "kora-frontend"
    "org.opencontainers.image.description" = "kora frontend service image"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-frontend:${IMAGE_TAG}"]
}

target "kora-operator" {
  context    = "."
  dockerfile = "Dockerfile.operator"
  pull       = true
  labels = {
    "org.opencontainers.image.title" = "kora-operator"
    "org.opencontainers.image.description" = "kora operator image"
    "org.opencontainers.image.source" = REPOSITORY_URL
    "org.opencontainers.image.url" = REPOSITORY_URL
    "org.opencontainers.image.licenses" = "Apache-2.0"
  }
  tags = ["${REGISTRY}/${IMAGE_NAMESPACE}/kora-operator:${IMAGE_TAG}"]
}

// Default group — all images built by CI and `docker buildx bake`.
// Adding a new image here automatically includes it in the CI matrix.
group "default" {
  targets = [
    "backend-images",
    "kora-agents",
    "kora-triggers",
    "kora-shell",
    "kora-mcp",
    "kora-runtime-mcp",
    "kora-session-manager",
    "kora-sandbox-mount",
    "kora-sandbox-base",
    "kora-sandbox-go",
    "kora-sandbox-python",
    "kora-sandbox-node",
    "kora-sandbox-dev",
    "kora-frontend",
    "kora-operator",
  ]
}
