# Tenant MCP Servers as a Recognised Workload Class

<!-- Filename: 2026-09-17-mcp-workload-class.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-09-17
**Deciders:** Justin Wilkin, Tom Morelly
**Technical Story:** Make a tenant-namespaced MCP server run, and reach the tool-gateway, without the operator hand-writing policy or opting out of enforcement. Resolves clear-route/orbit#301 and #300.

---

## Context

A tenant MCP server is a workload an operator deploys into a tenant namespace, registered with an `MCPServer` CR, whose tools the tool-gateway then introspects and governs through `mcp:<server>:<tool>:use` scopes.

### Background

The mutating webhook stamps `runkora.io/identity-proxy: "enabled"` on every pod it admits, the `identity-proxy-ext-proc` EnvoyFilter attaches to every pod carrying that label, and it injects `Authorization: Bearer <platform token>` into every outbound HTTP request. A server authenticating to its own upstream has that header overwritten.

The validating webhook from [2026-09-04](2026-09-04-enforcement-sidecar-injection-and-preflight.md) denies any tenant pod carrying `sidecar.istio.io/inject: "false"`, or lacking an `istio-proxy`, `identity-proxy` or `opa` container. That ADR also deleted the `runkora.io/skip-injection` opt-out, because "it is a pod label, so anyone who can create a tenant pod could remove enforcement". `docs/guides/register-mcp-server.md` still instructs operators to set both labels, so the documented path is denied at admission.

The chart's `kora-system` policies admit tool-gateway ingress *from* tenant namespaces (`allow-tenant-ingress-tool-gateway`, port 8096) and egress within `kora-system` and to external `:443`. Egress into a tenant namespace is absent, so the tool-gateway cannot dial a tenant MCP.

### Problem Statement

kora recognises two kinds of tenant pod, the agent and the sandbox, and treats every other pod in a tenant namespace as an agent. An MCP server is neither. It holds its own upstream credential, serves only the tool-gateway, and needs no platform identity: OPA adds nothing, and identity-proxy overwrites the credential.

So a tenant MCP server cannot run, because the only documented way to keep its credential is an opt-out the platform denies. It also cannot be reached: nothing admits the tool-gateway into a tenant namespace, and nothing names the gateway as the only caller.

An injection exemption addresses the first of those and leaves the second.

### Goals

- A tenant MCP server runs from an ordinary Deployment, a Service and an `MCPServer` CR, with no kora-specific pod annotations.
- The tool-gateway reaches it without an operator writing policy.
- Only the tool-gateway may call it, verified by workload identity.
- The exemption cannot be granted by anything `kora-agents` can create.
- Agent and sandbox pods keep identity-proxy and OPA, unconditionally.

### Non-Goals

- kora provisioning the MCP workload. Operators keep their own Deployment, or an upstream chart.
- Governing MCP tools. That is the tool-gateway's job and already exists.
- Global MCP servers in the system namespace, and MCP servers reached at an external URL. Neither is a tenant pod, so neither is affected.
- Federating platform identity into an MCP server.

---

## Decision Drivers

* **The exemption must not be forgeable by `kora-agents`.** It creates pods in tenant namespaces, so a pod label cannot be the authority.
* **Least configuration for the operator.** Three objects, no annotations, nothing to keep in sync by hand.
* **Authenticate the caller, not just the route.** Only the tool-gateway should invoke an MCP server's tools, and a port rule cannot identify the caller.
* **Report configuration that does nothing.** A selector matching no pods, or policies not yet reconciled, must be visible.
* **Do not weaken agent enforcement to serve a second workload type.**

---

## Considered Options

### Option 1: Honour a pod label again

**Description:** Reinstate an opt-out label; the validating webhook skips its checks for pods carrying it.

**Pros:**
- Smallest change. The guide already documents a label.

**Cons:**
- Reverses [2026-09-04](2026-09-04-enforcement-sidecar-injection-and-preflight.md). A compromised `kora-agents` sets one label and runs unenforced code in a tenant namespace.
- Says nothing about reachability, so #300 remains.

### Option 2: kora provisions the MCP workload from the CR

**Description:** `MCPServer` gains image, args, env, secrets, probes and resources. The operator renders the Deployment and Service.

**Pros:**
- The operator created the pods, so the class cannot be forged.
- Less for the operator to write for a simple server.

**Cons:**
- Rebuilds a Deployment API inside the CRD, and every upstream MCP chart has to be re-expressed in it.
- Any field kora has not modelled is unreachable, and MCP images vary widely.

### Option 3: The CR names the workload, and the operator derives the rest (chosen)

**Description:** `MCPServer.spec.workload.selector` names the pods serving the endpoint. The pod webhook matches pods against registered CRs in their namespace: a match skips identity-proxy and OPA and carries an operator-set marker the validating webhook honours. The `MCPServer` controller generates the `NetworkPolicy` and Istio `AuthorizationPolicy` for each server.

**Pros:**
- Operators keep their own Deployment, or any upstream chart, and add one label.
- The authority is a CR, which `kora-agents` has no RBAC to create, so the exemption is not forgeable by it.
- Reachability and the caller check come from the same field, with the port parsed from `endpoint` so the two cannot drift.
- Agent and sandbox pods are untouched.

**Cons:**
- One more field, and a webhook that now reads CRs at admission.
- A selector matching no pods does nothing until it is noticed. The status condition reports it.

### Option 4: Run tenant MCP servers in the system namespace

**Description:** Deploy MCP workloads into `kora-system`, where neither webhook applies, and keep the CR tenant-scoped.

**Pros:**
- Needs no code.

**Cons:**
- A tenant's server runs in platform infrastructure, shared across tenants.
- Drops tenant-namespaced MCP servers, which the guide documents.

---

## Decision

**Chosen Option:** Option 3 - The CR names the workload, and the operator derives the rest

### Rationale

kora has no model of an MCP workload, so it treats one as an agent pod. Naming the pods in the CR supplies that model, and one field then determines which pods skip identity-proxy and OPA, which port to admit, which principal may call it, and what to remove when the server is deregistered.

The authority stays where [2026-09-04](2026-09-04-enforcement-sidecar-injection-and-preflight.md) put it. That ADR rejected a pod label because any actor able to create a tenant pod could set it. A label grants nothing here: the webhook matches the pod against `MCPServer` CRs at admission, and `kora-agents` has no RBAC for those.

### The class

An MCP pod keeps the Istio sidecar and drops identity-proxy and OPA:

| | Agent and sandbox pods | MCP pods |
|---|---|---|
| Istio sidecar | yes | yes |
| SPIRE identity | `agent` or `sandbox` path | `tenant/<ns>/service/<server>` |
| identity-proxy | yes | no |
| OPA | yes | no |
| `runkora.io/identity-proxy` label | set | not set |
| Generated policies | sandbox egress | MCP ingress, L4 and L7 |

kora's sidecars take their certificates from SPIRE (`PILOT_CERT_PROVIDER: spire`), so an MCP pod needs a registration of its own or its sidecar has no certificate and the tool-gateway cannot complete mTLS. The webhook labels the pod `spiffe.io/service`, which the existing `kora-tenant-service` ClusterSPIFFEID already matches, so no new registration is introduced.

The sidecar supplies the workload identity the generated `AuthorizationPolicy` matches on. A `NetworkPolicy` admits a namespace and a port and cannot identify the caller, so it serves as the L4 layer beneath that check. With identity-proxy skipped, the sidecar does not alter the server's outbound requests.

`sidecar.istio.io/inject: "false"` remains denied for MCP pods, as for agents. The guide's instruction to set it is deleted.

### Configuration

```yaml
apiVersion: runkora.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: kora-tenant-clearroute
spec:
  displayName: GitHub
  endpoint: http://github-mcp.kora-tenant-clearroute.svc.cluster.local:8000/mcp
  transport: streamable-http
  workload:
    selector:
      app: github-mcp
```

`workload` is optional. Omitted, the server is external or global, there are no pods in the CR's namespace to classify, and behaviour is unchanged.

The workload itself carries no kora configuration:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-mcp
  namespace: kora-tenant-clearroute
spec:
  selector:
    matchLabels: { app: github-mcp }
  template:
    metadata:
      labels: { app: github-mcp }
    spec:
      containers:
        - name: server
          image: ghcr.io/github/github-mcp-server:v0.18.0
          ports:
            - containerPort: 8000
```

### Generated resources

Three per `MCPServer` with a `workload.selector`, all in the CR's namespace and all owner-referenced by it, so deregistering the server removes them:

- **`NetworkPolicy`** selecting the workload, `policyTypes: [Ingress]`, admitting only the system namespace on the endpoint's port.
- **Istio `AuthorizationPolicy`** selecting the same pods, admitting only the tool-gateway's principal.
- **Istio `DestinationRule`** naming the SPIFFE SAN a caller must expect, with `exportTo: ["*"]` so the tool-gateway in the system namespace picks it up. Istio otherwise derives the SAN from the destination's ServiceAccount, which never matches a SPIRE-issued identity, and the caller's handshake fails with a 503 against a server that is running normally.

The port comes from parsing `spec.endpoint`, defaulting to the scheme's port when absent.

The three are applied in sequence and the reconcile returns on the first failure, so a server can sit with some of them present until the next attempt succeeds. The trust domain is checked before any of the identity-bearing resources are written: a principal built from an empty one matches nothing, so an unset `global.trustDomain` fails the reconcile rather than leaving the server behind the address check alone. The server's name is bounded to a label value's 63 characters for the same reason: the generated resources carry it in a label, and a longer name would be rejected when the resource is written.

The tool-gateway's own egress is a single rule in the chart, admitting it to namespaces labelled `runkora.io/role: tenant`. Per-server egress would have to live in the system namespace, where a CR in a tenant namespace cannot own it: cross-namespace owner references are invalid and the garbage collector deletes dependents carrying one. That leaves a finalizer, a delete permission and a deletion-event predicate to maintain, to narrow an L4 rule that the `AuthorizationPolicy` already narrows by identity. The chart rule costs ten lines and no reconciliation.

### Status

The existing `Aggregated` condition's message reports how many pods the selector matched, so a selector matching none is visible in `kubectl describe` rather than surfacing later as an unreachable server. A second condition would carry no information this one cannot.

---

## Consequences

### Positive

- A tenant MCP server is three ordinary objects, with injection and policy derived from the CR.
- Callers are authenticated by mesh identity rather than by network range.
- Deregistering an `MCPServer` removes its policies by owner reference, with no cleanup path of the operator's own to get wrong.
- The guide stops documenting a mechanism the platform rejects.

### Negative

- The validating webhook's answer now depends on cluster state, not the pod alone, so an `MCPServer` CR that arrives after its pods leaves those pods enforced as agents until they restart.
- Operators following the current guide have `inject=false` in their manifests and are rejected on upgrade. The remedy is deleting two lines and adding `workload.selector`.
- The pod webhook reads `MCPServer` CRs on every tenant pod admission. The read is cached, but it is work on the admission path.
- A third workload class to keep in mind whenever injection or tenant policy changes.
- The tool-gateway's egress is open to every tenant namespace at L4. The `AuthorizationPolicy` on each server is what keeps the reach narrow, and it is only in force where the mesh is.

### Neutral

- Global and external MCP servers are unaffected, since neither has pods in a tenant namespace.
- Tool governance is untouched: scopes, introspection and the registry work exactly as before.

---

## Implementation

1. `MCPServerSpec.Workload.Selector`, with CRD and DeepCopy updates, and reconcile-time validation that the selector is non-empty when `workload` is present and that the name fits a label value.
2. `PodMutator`: look up `MCPServer` CRs in the pod's namespace, match labels, and on a match skip the identity-proxy and OPA containers, their volumes and the two labels, setting the marker label instead.
3. `PodValidator`: for marker pods require the Istio sidecar and the namespace injection label, and drop the identity-proxy and OPA requirements. `inject=false` and `interceptionMode=NONE` stay denied for every pod.
4. `MCPServer` controller: reconcile the three resources with owner references, and report the matched pod count in the `Aggregated` condition's message.
5. Operator RBAC: `create` and `patch` on `networkpolicies`, `security.istio.io/authorizationpolicies` and `networking.istio.io/destinationrules`, and `list` on `pods` for the matched count. The ClusterRole has none of them. The pod count reads through the API server rather than the manager's cache, which would otherwise start a cluster-wide Pod informer and need a watch on every pod in the cluster.
6. The tool-gateway egress rule in the platform NetworkPolicies.
7. Rewrite the MCP guide: no `inject=false`, no `runkora.io/skip-injection`, no hand-written `NetworkPolicy`, and document `workload.selector`.
8. Tests: webhook match and no-match, a forged marker label on an unmatched pod, endpoint port parsing, and policy generation.

### Migration Path

A tenant MCP following the current guide is rejected at admission, so nothing is live to migrate. Adopting this means deleting `sidecar.istio.io/inject` and `runkora.io/skip-injection` from the pod template, adding `workload.selector` to the CR, and deleting any hand-written `NetworkPolicy` for the server.

### Timeline

Before v0.1.0. The guide documents tenant MCP servers as supported, and they cannot run until this lands.

---

## Validation

### Success Metrics

- A tenant MCP server runs, is introspected, and serves tools through the gateway, with no policy written by hand.
- A workload other than the tool-gateway is refused by the mesh when it calls the MCP's port.
- A pod carrying the marker label with no matching `MCPServer` is injected and validated as an agent pod.

### Monitoring

The `Aggregated` condition on each `MCPServer` reports how many pods the selector matched. Webhook rejections surface as events on the pod's owning ReplicaSet.

---

## Related Decisions

- [enforcement-sidecar-injection-and-preflight](2026-09-04-enforcement-sidecar-injection-and-preflight.md) - removed the pod-label opt-out this decision replaces with a CR-backed class.
- [mcp-server-registration-and-introspection](2026-06-29-mcp-server-registration-and-introspection.md) - the `MCPServer` CR and registry this extends.
- [mcp-tool-gateway](2026-06-29-mcp-tool-gateway.md) - the caller the generated policies admit, and where tool governance lives.
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) - why agent and sandbox pods carry identity-proxy and OPA, and why an MCP server does not.

---

## References

- [Owners and dependents](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/) - cross-namespace owner references are invalid and their dependents are garbage collected, which is why the gateway's egress rule is in the chart.
- [Istio AuthorizationPolicy](https://istio.io/latest/docs/reference/config/security/authorization-policy/) - the principal match the generated policy uses.
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)

---

## Notes

This is the first tenant workload class besides the agent and the sandbox. A future class should reuse the shape, a CR naming its pods and the operator deriving their policy, rather than introduce a second exemption mechanism.

**Last Updated:** 2026-09-17
