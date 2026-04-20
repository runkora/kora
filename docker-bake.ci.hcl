// docker-bake.ci.hcl
//
// CI-only Bake overrides. This file adds GitHub Actions cache backends with a
// distinct scope per generated image so concurrent backend targets do not
// overwrite each other's cache entries.

target "backend-images" {
  name = "orbit-${item.service}"
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
  cache-from = ["type=gha,scope=orbit-${item.service}"]
  cache-to = ["type=gha,scope=orbit-${item.service},mode=max"]
}

target "orbit-frontend" {
  cache-from = ["type=gha,scope=orbit-frontend"]
  cache-to = ["type=gha,scope=orbit-frontend,mode=max"]
}
