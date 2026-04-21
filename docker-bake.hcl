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
