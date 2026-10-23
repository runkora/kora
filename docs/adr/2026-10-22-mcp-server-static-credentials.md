# MCP Server Static Credentials

**Status:** Accepted  
**Date:** 2026-10-22  
**Deciders:** Justin Wilkin  
**Technical Story:** The tool-gateway presents a static service credential (an API key or a bearer token) to an MCP server that requires one, such as Exa's hosted server at `https://mcp.exa.ai/mcp`. Resolves runkora/kora#190.

---

## Context

The tool-gateway reaches a registered MCP server on two paths. The publisher deployment introspects it with `initialize` and `tools/list` (`internal/toolgateway/introspector.go`). The proxy deployment forwards each `tools/call` (`internal/toolgateway/proxy.go`). Neither path sends a credential. The one exception is `callerIdentity: forward` on a global server, where the proxy sends the caller's platform bearer and delegation token.

A hosted MCP server typically authenticates its client with an API key in a request header. Exa accepts `x-api-key` or `Authorization: Bearer`. Today a tenant can register such a server only by running a relay pod that holds the key. The [registration ADR](2026-06-29-mcp-server-registration-and-introspection.md) lists credential injection on the platform-to-server hop as a non-goal.

This decision touches *How does it connect?*. The gateway presents a service credential to the upstream, and the agent never holds it. *What can it access?* is unchanged: `mcp:<server>:<tool>:use` stays the only authorisation gate on a tool call. *Who is this agent?* is unchanged: the gateway authenticates the caller and logs the call under the caller's principal, and the upstream sees the service credential only.

### Background

The current code behaves as follows:

*   `MCPServerSpec` (`internal/operator/mcpserver_types.go`) has no credential field. The operator renders every valid `MCPServer` into the `orbit-mcp-servers` ConfigMap (`servers.json`) in the system namespace. Both tool-gateway deployments mount it. The gateway has no Kubernetes API access.
*   `ConnectionAggregator` (`internal/operator/connection_aggregator.go`) copies the Secrets that tenant `Connection` CRs reference into the `orbit-tenant-connection-secrets` Secret in the system namespace, and the egress gateway mounts it. The operator reads Secrets uncached and holds no Secret informer (`cmd/operator/main.go`). The tenant reconciler re-runs every aggregator on a five-minute requeue.
*   The `MCPServer` reconciler sets the CR's `Aggregated` condition on spec changes only.
*   The proxy dials with `http.DefaultClient`, and the introspector dials with a tracing client. Both follow redirects. `validateMCPServerSpec` checks only that `spec.endpoint` is non-empty.
*   On a non-2xx response, the MCP Go SDK places the upstream's JSON-RPC error body in the returned error. The introspector logs that error with the endpoint and keeps it in the server snapshot.
*   An introspection failure marks the server `Degraded` and retains its prior tools. A server that leaves the registry leaves the publisher's keep-list, and `DeleteOrphans` then purges its tool scopes from every grant and role.
*   The operator renders one ServiceEntry per InferenceProvider for meshes with `outboundTrafficPolicy: REGISTRY_ONLY` ([CRD inference providers, Amendment 1](2026-10-01-crd-inference-providers.md#amendment-1-2026-10-02-the-operator-registers-provider-hosts-with-the-mesh)). No ServiceEntry covers an external MCP host.
*   Issue runkora/kora#190 reports that a tenant-controlled `spec.endpoint` is a server-side request forgery surface, and that both gateway clients follow redirects.

### Goals

*   An admin registers an MCP server that requires an API key or a bearer token with one `MCPServer` and one Secret, in a tenant namespace or the system namespace.
*   The credential value reaches only the tool-gateway and the declared upstream.
*   A rotated Secret takes effect without a gateway restart.
*   An `MCPServer` on an external host works on a `REGISTRY_ONLY` mesh without an admin-written ServiceEntry.

### Non-Goals

*   OAuth to the upstream, per-user upstream credentials, and token exchange.
*   Connections and the `internal/credentials` adapters.
*   Network-level limits on the addresses `spec.endpoint` may target.

---

## Decision Drivers

*   The credential value must not appear in `servers.json`, logs, errors, spans, CR status, or the catalog.
*   The gateway gains no Kubernetes API access, and the operator gains no Secret informer.
*   A missing or wrong credential must not revoke granted tool scopes.
*   One place decides which credential an upstream request carries.

---

## Considered Options

### D1. Credential shape

*   **Option A (chosen):** `spec.auth.headers` is a list. Each item has a header `name`, an optional free-text `prefix`, and a `secretRef {name, key}` to a Secret in the CR's namespace. The gateway sends the prefix followed by the secret value under that header.
*   **Option B (rejected):** Typed schemes such as `bearer` and `apiKey`. Each new vendor convention would need a new scheme, and Exa alone accepts two.
*   **Option C (rejected):** A single header. A server that needs a key and an account id in separate headers could not be registered.

### D2. Delivery to the gateway

*   **Option A (chosen):** The operator copies each referenced value into one Secret in the system namespace, `orbit-mcp-server-secrets`, and both tool-gateway deployments mount it. `servers.json` carries the header name, the prefix and the data key in that Secret.
*   **Option B (rejected):** The gateway watches `MCPServer` CRs and Secrets itself, as the inference gateway does for InferenceProviders. Tenant Secrets live in tenant namespaces. The gateway would need cluster-wide Secret read.
*   **Option C (rejected):** A `Connection` brokered by the egress gateway. The tool-gateway's upstream calls do not pass through the egress gateway. A `Connection` would also add `connection:` scopes as a second gate beside the tool scope.

### D3. Rotation

*   **Option A (chosen):** The operator re-reads every referenced Secret on each aggregation pass. A pass runs on every `MCPServer` reconcile and every tenant reconcile. The `MCPServer` reconciler requeues a server with `spec.auth` every five minutes to keep its condition current.
*   **Option B (rejected):** A Secret informer in the operator, scoped by a label on referenced Secrets. Rotation would apply within seconds. The operator would cache Secrets, and every tenant would have to label each referenced Secret.

### D4. Gateway read of the mounted Secret

*   **Option A (chosen):** The gateway reads `<mount>/<key>` each time it builds an upstream request. The kubelet swaps the mounted files atomically. A read sees the old value or the new one.
*   **Option B (rejected):** Hold the values in memory and reload them on an fsnotify event, as the registry does. It adds a watcher and a cache for files of a few bytes read once per call.

### D5. Credential lookup

*   **Option A (chosen):** One lookup returns the headers for an upstream request. The proxy calls it on every `tools/call` with the caller's bearer and delegation token. The introspector calls it with no caller. `callerIdentity: forward` moves behind it together with its global-only check. A server with `spec.auth` receives its service credential whatever the caller.
*   **Option B (rejected):** Add the service credential beside the forward branch in `MCPController` and add a second branch in the introspector. Two call sites would each decide which credential an upstream receives. A per-user credential would add a third.

### D6. Missing credential

*   **Option A (chosen):** The server stays in the registry. The gateway fails a call closed and sends nothing upstream. Introspection handles it as any other introspection failure. The operator reports the missing Secret or key in the `Aggregated` condition message.
*   **Option B (rejected):** Leave the server out of the registry, as the connection aggregator does for a `Connection` without its Secret. `DeleteOrphans` would purge the server's tool scopes from every grant and role.

### D7. Endpoint safety

*   **Option A (chosen):** Protect the credential. Both gateway clients stop following redirects, and the operator rejects a `spec.endpoint` with userinfo or a query string.
*   **Option B (rejected):** Refuse private, link-local and system-namespace addresses at dial time for tenant servers. A tenant MCP server in its own namespace has a private address by design.
*   **Option C (rejected):** An endpoint allowlist or admin approval for tenant servers. `MCPServer` registration has no create API and is applied from git, where it is reviewed.

### D8. Mesh registration of external hosts

*   **Option A (chosen):** The operator renders one ServiceEntry per distinct external endpoint host in the system namespace. The `MCPServer` aggregator computes the desired set on each pass and deletes labelled ServiceEntries outside it.
*   **Option B (rejected):** A ServiceEntry in the CR's namespace, owner-referenced by the CR and exported to the system namespace. The operator's ServiceEntry Role and cache cover the system namespace only. It would need cluster-wide ServiceEntry write.
*   **Option C (rejected):** A system-namespace ServiceEntry owner-referenced by a tenant CR. Kubernetes does not allow cross-namespace owner references, and the garbage collector deletes dependents that carry one.
*   **Option D (rejected):** One ServiceEntry per `MCPServer` follows the InferenceProvider shape. Two servers on one host would register the host twice.

---

## Decision

**Chosen Option:** Option A in each of D1 to D8.

### 1. The `spec.auth` field

```yaml
apiVersion: orbit.io/v1alpha1
kind: MCPServer
metadata:
  name: exa
  namespace: orbit-tenant-example
spec:
  endpoint: https://mcp.exa.ai/mcp
  auth:
    headers:
      - name: Authorization
        prefix: "Bearer "
        secretRef: {name: exa-credentials, key: api-key}
```

The operator marks an `MCPServer` `Invalid` when any of these rules fails:

*   `spec.auth.headers` lists at most 8 headers.
*   Each header `name` uses only letters, digits and hyphens, is at most 64 characters, and is unique case-insensitively. These names are also valid in a Secret data key, and the longest key is 192 characters.
*   A `prefix` is at most 256 characters.
*   A header `name` is not reserved. The reserved names are `Host`, `Content-Length`, `Content-Type`, `Accept`, `Transfer-Encoding`, `Connection`, `Keep-Alive`, `Proxy-Connection`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailer`, `Upgrade`, `Mcp-Session-Id`, `Mcp-Protocol-Version`, `Last-Event-ID`, `traceparent`, `tracestate`, `X-Orbit-Delegation-Token` (`mw.DelegationTokenHeader`), and any name starting with `X-Forwarded-` or `X-Envoy-`.
*   A `prefix` contains no control character other than tab.
*   `secretRef.name` is a valid Secret name, and `secretRef.key` is a valid Secret data key.
*   `spec.auth` and `callerIdentity: forward` are not both set. Both would set `Authorization`.
*   `spec.endpoint` parses as a URL with no userinfo and no query string.

Tenant and global `MCPServer` CRs follow the same rules and the same path.

### 2. Operator aggregation

*   `MCPServerAggregator` reads each referenced Secret from the CR's namespace and writes the value into the `orbit-mcp-server-secrets` Secret in the system namespace. The data key is derived from the namespace, the server name and the lowercased header name. A reordered header list cannot send a value under another header. The operator creates the Secret on its first pass, empty or not.
*   The operator trims one trailing `\n`, `\r\n` or `\r` from the value. An empty value, a value over 4 KiB, or one that still contains a control character other than tab, is treated as missing. A value in a Secret the operator manages, such as a copied registry pull secret, is treated as missing.
*   The server's `servers.json` entry lists each header's name, prefix and data key. It never holds the value.
*   When a Secret or key is missing, the entry keeps the header and the aggregated Secret omits the key. The `Aggregated` condition stays `True` with reason `CredentialUnavailable` and a message that cites the Secret and the key.
*   A Secret read that fails for another reason keeps the value already aggregated. The pass returns the error after writing the registry, and the reconcile retries.
*   Rotation takes up to one operator pass plus the kubelet's Secret projection delay. No gateway pod restarts.

### 3. Gateway

*   The Secret volume is optional. A gateway pod can start before the operator's first pass.
*   The lookup has this shape:

    ```go
    type Caller struct{ Bearer, Delegation string }

    type UpstreamCredentials interface {
        Headers(ctx context.Context, srv Server, caller *Caller) (http.Header, error)
    }
    ```

*   For a server with `spec.auth`, `Headers` reads each data key from the mount and returns the prefixed values. A missing or empty file returns `ErrCredentialUnavailable`. For a global `forward` server with a caller, it returns the caller's bearer and delegation token. For any other server, it returns no headers.
*   The proxy returns a JSON-RPC error for a `tools/call` whose credential is unavailable and opens no upstream session.
*   Neither client follows a redirect, and a 3xx response fails the call.
*   An introspection that ends in 401 or 403 records the reason `auth_failed` with the status code and discards the upstream's response body.
*   An introspection with an unavailable credential records the reason `credential_unavailable`. The failure log carries the reason as a field.
*   A `tools/call` that ends in 401 or 403 logs `auth_failed` and the status code and discards the upstream's response body.
*   Any other error status from an upstream that was sent credentials is recorded as `upstream_status` with the status code. Any other error from such an upstream is recorded as `upstream_error` with its network, DNS, TLS or deadline cause. None carries upstream response text. Each keeps the stage and server it occurred in, such as `connect upstream "exa": auth_failed: HTTP 401`.
*   The gateway keeps the first auth failure and the first error status of each upstream session. A 405 on the MCP client's optional GET stream is not an error status.

### 4. ServiceEntries for external hosts

*   The aggregator renders one ServiceEntry for each distinct host among valid `MCPServer` endpoints, named `mcp-server-<host>`, in the system namespace. Hosts are lowercased and stripped of a trailing dot first. In-cluster hosts and IP addresses are skipped by the rule the InferenceProvider ServiceEntries use. The rule covers short names, IP addresses, and names ending in `.svc`, `.pod` or `.cluster.local`.
*   A host whose last label is the name of a namespace is skipped. It resolves to a Service or pod through the cluster search domains.
*   A host that another operator-managed ServiceEntry registers is skipped. Two entries for one host could declare clashing port protocols.
*   Each ServiceEntry has `location: MESH_EXTERNAL`, `resolution: DNS`, `exportTo: ["."]`, the ports of every endpoint on that host, and the label `orbit.io/component: mcp-server-host`.
*   On each pass the aggregator deletes labelled ServiceEntries whose host no valid `MCPServer` uses.

### Rationale

Delivery reuses the connection aggregator path, and the gateway gains no Kubernetes API access. One lookup decides every upstream credential. A per-user credential replaces one implementation of it. Failing closed while the server stays registered keeps every grant on its tools.

---

## Consequences

### Positive

*   A hosted MCP server needs one `MCPServer`, one Secret and no relay pod.
*   Introspection and `tools/call` share one lookup. A per-user credential can replace the service credential behind it without a change at either call site.
*   The gateway's credentials cannot follow a redirect to another host.

### Negative

*   A rotated Secret takes up to five minutes plus the kubelet projection delay to apply.
*   Every caller granted a tool acts upstream with the same service credential. The upstream cannot tell callers apart.
*   Redirects stop for every registered server, including servers without `spec.auth`. A server that redirects its endpoint, such as to add a trailing slash, fails until its `spec.endpoint` names the final URL.
*   The aggregated Secret in the system namespace holds every tenant's MCP credentials. Anything that can read Secrets in the system namespace can read them.
*   A tenant `spec.endpoint` can still target any address the tool-gateway reaches. Review of the `MCPServer` in git is the control.
*   A host the mesh registers is visible to every workload in the system namespace.
*   The `Aggregated` condition for a server with `spec.auth` can lag a Secret change by up to five minutes.
*   A new server whose credential is missing or wrong is never reached. The prune guard then defers the scope prune for every server until that server is reached.

### Neutral

*   `callerIdentity: forward` keeps its global-only rule and behavior.
*   Under `outboundTrafficPolicy: ALLOW_ANY` the ServiceEntries change nothing.
*   The proxy uses the tracing transport for every upstream call. Non-forward upstreams, external hosts included, receive `traceparent`.

---

## Implementation

1.  This ADR and its index entry.
2.  The credential lookup and endpoint validation:
    *   `internal/toolgateway`: the `UpstreamCredentials` lookup with `callerIdentity: forward` behind it, and clients that do not follow redirects.
    *   `internal/operator`: endpoint validation.
3.  Static credentials:
    *   `internal/operator`: `spec.auth` types, DeepCopy, validation, the aggregated Secret and the `CredentialUnavailable` condition.
    *   `charts/kora-crds`: the CRD schema.
    *   `charts/kora`: the mount and values.
    *   `internal/toolgateway`: the file-backed lookup, `auth_failed` and `credential_unavailable`.
    *   `docs/guides/register-mcp-server.md`: external servers with `spec.auth`.
4.  `internal/operator`: ServiceEntries for external MCP hosts.

---

## Validation

On the local cluster:

*   A server that requires `Authorization` and `x-api-key` is introspected, its scopes publish, and `tools/call` succeeds through the gateway.
*   `servers.json`, both gateway deployments' logs and the CR status hold no secret value.
*   A wrong key marks the server `Degraded` with `auth_failed`, retains its tools, and fails `tools/call` closed. Restoring the key recovers without a gateway restart.
*   A missing key fails `tools/call` closed, and the `Aggregated` condition message cites it.
*   An endpoint with userinfo or a query string is `Invalid`. A redirect is not followed.
*   Exa's hosted server is introspected, and the operator renders its ServiceEntry.

---

## Related Decisions

*   [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md): amended. Credential injection on the platform-to-server hop is now in scope for servers with `spec.auth`.
*   [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md): the proxy and `callerIdentity: forward`.
*   [Tenant MCP Servers as a Recognised Workload Class](2026-09-17-mcp-workload-class.md): tenant servers reached in-mesh, unaffected by the credential path.
*   [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the aggregated-Secret pattern reused here.
*   [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): the ServiceEntry shape and the in-mesh host rule reused here.

---

## References

*   [RFC 7230, section 3.2.6](https://www.rfc-editor.org/rfc/rfc7230#section-3.2.6): header field name tokens.
*   [Owners and dependents](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/): cross-namespace owner references.
*   [Istio ServiceEntry](https://istio.io/latest/docs/reference/config/networking/service-entry/)

---

**Last Updated:** 2026-10-22
