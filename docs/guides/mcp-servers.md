# MCP servers

An MCP server exposes tools an agent can call. Registering it as an `MCPServer` places it behind the tool-gateway, the single entry point Orbit governs for MCP calls. The gateway introspects the server's tools into policy scopes that agents can be granted. The tool-gateway then checks every call against policy before it reaches the server.

An `MCPServer` in `orbit-system` is registered as platform-global, its tools become available to every tenant, like the Orbit MCP. When registered in a tenant namespace, it belongs to that tenant alone.

The following guide assumes Orbit is already installed. See [Deploying Orbit](deployment.md) for the install.

## Registering an MCP server

Orbit runs on a BYO MCP server model. You can register that MCP using the `MCPServer` CR. The operator adds it to the tool-gateway's registry, and the tool-gateway introspects the server's tools into `mcp:<server>:<tool>:use` scopes automatically.

```yaml
apiVersion: orbit.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: orbit-tenant-example
spec:
  displayName: GitHub
  description: GitHub issues, pull requests, and repositories.
  endpoint: http://github-mcp.orbit-tenant-example.svc:8000/mcp
  transport: streamable-http
  reintrospectIntervalSeconds: 900
  callerIdentity: none
```

- `endpoint`: the in-cluster URL of your MCP server's Service.
- `displayName` and `description`: shown in the tool catalog.
- `transport`: `streamable-http`, the default and only supported transport.
- `reintrospectIntervalSeconds`: how often the tool-gateway re-introspects the tool surface. `0` re-introspects on registry reload only.
- `callerIdentity`: `none` (the default) has the server act as itself with its own credential. `forward` passes the caller's bearer token, letting the server act as the caller against first-party Orbit APIs. `forward` is allowed only on a global (`orbit-system`) server. A tenant-namespaced CR that sets it is rejected with status `Invalid`.

An `MCPServer` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. Check its status:

```bash
kubectl get mcpservers -A
```

The `Phase` column reports `Pending`, `Aggregated`, or `Invalid`.

## Running the MCP server

The `MCPServer` CR is only the registration into Orbit's tool-gateway. You deploy the MCP server as a workload in the same namespace as its `MCPServer`. The tool-gateway should be the only service configured to reach it. A tenant server looks roughly like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-mcp
  namespace: orbit-tenant-example
spec:
  template:
    metadata:
      labels:
        app: github-mcp
        orbit.io/skip-injection: "true"      # skip the identity-proxy and OPA injection
      annotations:
        sidecar.istio.io/inject: "false"     # tenant MCP servers run outside the mesh
    spec:
      containers:
        - name: github-mcp
          image: <your MCP server image, serving Streamable HTTP>
          ports:
            - containerPort: 8000
          env:                               # the server authenticates upstream itself
            - name: GITHUB_APP_PRIVATE_KEY
              valueFrom:
                secretKeyRef:
                  name: github-app-key
                  key: key.pem
---
apiVersion: v1
kind: Service
metadata:
  name: github-mcp
  namespace: orbit-tenant-example
spec:
  selector:
    app: github-mcp
  ports:
    - port: 8000
      targetPort: 8000
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy                          # only the tool-gateway may reach the endpoint
metadata:
  name: github-mcp-ingress
  namespace: orbit-tenant-example
spec:
  podSelector:
    matchLabels:
      app: github-mcp
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: orbit-system
      ports:
        - protocol: TCP
          port: 8000
```

A tenant-namespaced MCP workload runs outside the Orbit mesh. Set `sidecar.istio.io/inject: "false"` and `orbit.io/skip-injection: "true"` on its pods. Orbit's platform identity does not federate into a tenant MCP, and any identity-proxy would otherwise overwrite the server's own outbound `Authorization` header.

The MCP endpoint has no authentication of its own and holds whatever upstream credential the server uses. Restrict its ingress to the tool-gateway with a `NetworkPolicy` in the server's namespace that admits only `orbit-system`. Nothing else in the cluster should reach it. Tool access is then governed by the `mcp:<server>:<tool>:use` scopes.

## Reference

See [mcp tool gateway](../architecture/mcp-tool-gateway.md) for how the tool-gateway introspects, scopes, and governs tools.
