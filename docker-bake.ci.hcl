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

target "orbit-agents" {
  cache-from = ["type=gha,scope=orbit-agents"]
  cache-to = ["type=gha,scope=orbit-agents,mode=max"]
}

target "orbit-shell" {
  cache-from = ["type=gha,scope=orbit-shell"]
  cache-to = ["type=gha,scope=orbit-shell,mode=max"]
}

target "orbit-runtime-mcp" {
  cache-from = ["type=gha,scope=orbit-runtime-mcp"]
  cache-to = ["type=gha,scope=orbit-runtime-mcp,mode=max"]
}

target "orbit-session-manager" {
  cache-from = ["type=gha,scope=orbit-session-manager"]
  cache-to = ["type=gha,scope=orbit-session-manager,mode=max"]
}

target "orbit-sandbox-exec" {
  cache-from = ["type=gha,scope=orbit-sandbox-exec"]
  cache-to = ["type=gha,scope=orbit-sandbox-exec,mode=max"]
}

target "orbit-frontend" {
  cache-from = ["type=gha,scope=orbit-frontend"]
  cache-to = ["type=gha,scope=orbit-frontend,mode=max"]
}

target "orbit-operator" {
  cache-from = ["type=gha,scope=orbit-operator"]
  cache-to = ["type=gha,scope=orbit-operator,mode=max"]
}
