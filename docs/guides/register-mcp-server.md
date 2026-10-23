# Register an MCP server

An MCP server exposes tools an agent can call. Registering it as an `MCPServer` places it behind the tool-gateway, the single entry point kora governs for MCP calls. The gateway introspects the server's tools into policy scopes that agents can be granted. The tool-gateway then checks every call against policy before it reaches the server.

An `MCPServer` in `kora-system` is registered as platform-global, its tools become available to every tenant, like the kora MCP. When registered in a tenant namespace, it belongs to that tenant alone.

The following guide assumes kora is already installed. See [Deploying kora](deployment.md) for the install.

## Registering an MCP server

kora runs on a BYO MCP server model. You can register that MCP using the `MCPServer` CR. The operator adds it to the tool-gateway's registry, and the tool-gateway introspects the server's tools into `mcp:<server>:<tool>:use` scopes automatically.

```yaml
apiVersion: runkora.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: kora-tenant-example
spec:
  displayName: GitHub
  description: GitHub issues, pull requests, and repositories.
  endpoint: http://github-mcp.kora-tenant-example.svc:8000/mcp
  transport: streamable-http
  reintrospectIntervalSeconds: 900
  callerIdentity: none
```

- `endpoint`: the URL of the MCP server, either its in-cluster Service or an external HTTPS URL. The operator marks a CR `Invalid` when the URL contains userinfo or a query string. The tool-gateway does not follow redirects from the endpoint.
- `displayName` and `description`: shown in the tool catalog.
- `transport`: `streamable-http`, the default and only supported transport.
- `reintrospectIntervalSeconds`: how often the tool-gateway re-introspects the tool surface. `0` re-introspects on registry reload only.
- `callerIdentity`: `none` (the default) has the server act as itself with its own credential. `forward` passes the caller's bearer token, letting the server act as the caller against first-party kora APIs. For a delegated run it also passes the run's delegation token. `forward` is allowed only on a global (`kora-system`) server. A tenant-namespaced CR that sets it is rejected with status `Invalid`.
- `workload.selector`: the labels of the pods serving `endpoint`, in this CR's namespace. Naming them lets kora skip the injection those pods must not carry and generate the policies that let the tool-gateway reach them. Omit it for a global server, or one reached at an external URL.
- `auth.headers`: the credential headers the tool-gateway sends to the server. See [Servers that need a credential](#servers-that-need-a-credential).

An `MCPServer` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. Check its status:

```bash
kubectl get mcpservers -A
```

The `Phase` column reports `Pending`, `Aggregated`, or `Invalid`.

## Running the MCP server

The `MCPServer` CR is only the registration into kora's tool-gateway. You deploy the MCP server as a workload in the same namespace as its `MCPServer`. The tool-gateway should be the only service configured to reach it. A tenant server looks roughly like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-mcp
  namespace: kora-tenant-example
spec:
  template:
    metadata:
      labels:
        app: github-mcp                      # matched by spec.workload.selector
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
  namespace: kora-tenant-example
spec:
  selector:
    app: github-mcp
  ports:
    - name: http               # Istio reads the protocol from this
      appProtocol: http
      port: 8000
      targetPort: 8000
```

Name the Service port `http` and set `appProtocol: http`. Istio derives a port's protocol from that name, and treats an unnamed or unrecognised one as plain TCP.

Point `spec.workload.selector` at those pod labels and kora handles the rest. It generates a `NetworkPolicy` admitting only the tool-gateway on the endpoint's port, and an Istio `AuthorizationPolicy` admitting only the tool-gateway's workload identity.

A tool's scope only lets a caller call the tool. The tool-gateway ignores `_meta["orbit/requires"]` on tools from a tenant server, so a tenant server's tools confer no other scope.

The MCP endpoint has no authentication of its own and holds whatever upstream credential the server uses. The generated policies and scope vocabulary are what control access to the server via the tool-gateway.

## Servers that need a credential

Some MCP servers, such as Exa's hosted server, authenticate their client with an API key or a bearer token. The tool-gateway holds the credential and sends it on every request to the server. Agents never hold it. The value is stored in a Secret in the `MCPServer`'s namespace and referenced from `spec.auth.headers`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: exa-credentials
  namespace: kora-tenant-example
stringData:
  api-key: <your Exa API key>
---
apiVersion: runkora.io/v1alpha1
kind: MCPServer
metadata:
  name: exa
  namespace: kora-tenant-example
spec:
  displayName: Exa
  endpoint: https://mcp.exa.ai/mcp
  auth:
    headers:
      - name: Authorization
        prefix: "Bearer "
        secretRef: {name: exa-credentials, key: api-key}
```

- `name`: the HTTP header that carries the credential, such as `Authorization` or `x-api-key`.
- `prefix`: optional text placed before the Secret value, such as `"Bearer "`.
- `secretRef`: the Secret and key that hold the value.

With this configuration the tool-gateway sends `Authorization: Bearer <key>` on introspection and on every tool call. Tool scopes still decide which agents and users can call each tool.

A server can list up to eight headers. A header name uses letters, digits and hyphens and is at most 64 characters long. The operator rejects headers that HTTP, MCP or kora already set, such as `Host`, `Content-Type`, `Mcp-Session-Id`, `X-Forwarded-*` and `X-Envoy-*`. A server with `spec.auth` cannot also set `callerIdentity: forward`.

### Rotating a credential

Update the value in the Secret. The tool-gateway uses the new value within about six minutes without a restart.

### Troubleshooting

- **Missing Secret or key:** The server stays registered and its tool calls fail with `upstream credential unavailable`. The `Aggregated` condition reports the reason `CredentialUnavailable` with the Secret and key the operator could not read.
- **Rejected credential:** When the server returns 401 or 403, the tool-gateway logs `reason: auth_failed` and keeps the tools from the last successful introspection.

```bash
kubectl describe mcpserver exa -n kora-tenant-example
```

The tool-gateway does not log the response body of a server it sends a credential to.

### External endpoints

For an endpoint outside the cluster, the operator creates a ServiceEntry named `mcp-server-<host>` in `kora-system`. A mesh with `outboundTrafficPolicy: REGISTRY_ONLY` needs no further configuration. The platform network policy allows the tool-gateway to reach external hosts on port 443 only. Add an endpoint on any other port to `networkPolicy.externalServices`.

## Reference

See [mcp tool gateway](../architecture/mcp-tool-gateway.md) for how the tool-gateway introspects, scopes, and governs tools.
