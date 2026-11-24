# Agent Pod Egress Lockdown

<!-- Filename: 2026-09-30-agent-pod-egress-lockdown.md  (decision date + slug; no sequential number) -->

**Status:** Accepted (Amended 2026-11-22: Amendment 1 Accepted)
**Date:** 2026-09-30
**Deciders:** Justin Wilkin
**Technical Story:** The agent pod (session-manager, the agent CLI it spawns, and the injected identity-proxy and OPA sidecars) can reach any host. Implements, for agent pods, the "external egress via gateway only" constraint of [2026-03-05: Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md), which was never built for them.

---

## Context

A run executes across two pods in the tenant namespace ([2026-09-18: Authenticated Sandbox Exec](2026-09-18-sandbox-exec-trust-boundary.md)). The sandbox pod's egress is locked by the operator's `sandbox-egress-to-gateway` NetworkPolicy to DNS, istiod, identity, the policy service and the egress gateway, and every external call it makes goes through the egress gateway and its policy ([2026-06-06: JIT Credential Brokering Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)). The agent pod has no egress policy.

Measured on the local cluster: `curl https://example.com` from the `session-manager` container returned 200, and a request to `api.github.com` was answered by GitHub rather than the egress gateway. The mesh runs with `outboundTrafficPolicy: ALLOW_ANY`, so Envoy forwards a connection to an unregistered host through its passthrough cluster. A TLS or plain TCP connection there never reaches the HTTP filter chain, so neither the identity-proxy nor the OPA sidecar sees it.

The consequences observed so far:

- OpenCode, with no model configured, sent every turn to its own hosted service at opencode.ai, outside the inference gateway (fixed separately in the [OpenCode runner](2026-06-04-opencode-agent-runner.md), Amendment 1).
- Any agent tool that does network I/O in its own process, rather than through the sandbox shell, reaches the internet without the egress gateway's policy or audit: Claude Code's WebFetch, OpenCode's webfetch, an MCP server started as a local process.
- The runtimes' own background calls (update checks, telemetry, plugin catalogues) leave the cluster.

This touches *What can it access?*, because it removes every destination the platform does not require, and *How does it connect?*, because the only paths out of the agent pod become the platform's gateways. *Who is this agent?* is unchanged: no identity or token flow moves.

### Goals

- The agent pod reaches only the destinations the platform requires, named in one place.
- A blocked connection fails at once, not after a connect timeout.
- The lockdown covers every container and port in the pod, including ports excluded from Envoy capture and UDP.
- Each runtime is told not to make the background calls the lockdown now blocks, so runs do not wait on them.

### Non-Goals

- Per-container egress rules inside the agent pod. The sidecars and the agent share the pod's network namespace.
- Changing the sandbox pod's egress, or the egress gateway.

---

## Decision Drivers

* The pod's traffic must be denied by default, with an explicit list of allowed destinations.
* Enforcement must not depend on the agent process honouring a setting.
* A tool's failed call should surface as an immediate error the agent can act on.
* Follow the existing operator-reconciled pattern for tenant namespaces.

---

## Considered Options

### Option 1: An operator-reconciled NetworkPolicy on agent pods

**Description:** The operator reconciles a NetworkPolicy in each tenant namespace selecting `runkora.io/managed-agent=true` pods with `policyTypes: [Egress]` and an allow-list, as it already does for sandbox pods.

**Pros:**
- Enforced by the CNI below the pod, for every container, port and protocol, including the ports excluded from Envoy capture (`50051`, `5432`) and UDP.
- Same mechanism and reconcile loop as the sandbox policy.

**Cons:**
- A dropped packet surfaces as a connect timeout. Envoy accepts the application's connection first and only fails after its upstream connect timeout (10s by default).
- Selects destinations by pod and namespace labels and ports, not by host name.

### Option 2: A per-workload Istio Sidecar with REGISTRY_ONLY

**Description:** The operator reconciles an Istio `Sidecar` resource in each tenant namespace selecting agent pods, with `outboundTrafficPolicy: REGISTRY_ONLY` and `egress.hosts` listing only the platform services.

**Pros:**
- A connection to any other host is refused by the pod's own Envoy at once (HTTP 502, or a reset for TLS and TCP).
- Host-level: only the named services are visible to the agent pod's Envoy, not every ServiceEntry in the mesh.

**Cons:**
- Only covers traffic Envoy captures: ports excluded from capture and UDP pass untouched.
- A process running as Envoy's UID bypasses capture. The pod's restricted security context prevents that today, but the control would rest on it.

### Option 3: Mesh-wide REGISTRY_ONLY

**Description:** Set the chart's existing `istio.outboundTrafficPolicy: REGISTRY_ONLY`.

**Pros:**
- One value, already supported by the chart.

**Cons:**
- Applies to every workload in the mesh, including the platform services, and needs every external host they use registered as a ServiceEntry.
- Every registered host, including the Bedrock ServiceEntries, stays reachable from agent pods.
- Same capture gaps as Option 2.

### Option 4: The agent pod's OPA sidecar

**Description:** Deny unknown hosts in the tenant `authz.rego`.

**Pros:**
- Policy as code, next to the existing route rules.

**Cons:**
- ext_authz runs on the HTTP filter chain only. TLS and TCP to an unregistered host go through the passthrough cluster and are never evaluated, which is exactly the traffic observed.

---

## Decision

**Chosen Option:** Options 1 and 2 together. The NetworkPolicy is the enforcement: it is the one control no process in the pod can route around. The Sidecar makes a blocked call fail immediately and narrows the agent pod's Envoy to the named services.

1. **NetworkPolicy `agent-egress`.** The operator reconciles it in every tenant namespace, next to the sandbox policies, selecting `runkora.io/managed-agent=true`. Egress is allowed only to:

   | Destination | Port | Used by |
   |---|---|---|
   | `kube-system` pods `k8s-app=kube-dns` | 53 UDP and TCP | Service name resolution |
   | Istio namespace | 15012 TCP | Envoy xDS from istiod |
   | `kora-system` `app=kora-identity` | 8080 TCP | identity-proxy SVID exchange and refresh; OPA JWKS |
   | `kora-system` `app=kora-agents` | 8080 TCP | session-manager run events, state, summaries and sandbox provisioning |
   | `kora-system` `app=kora-inference-gateway` | 8081 TCP | every runtime's model calls; the run summariser |
   | `kora-system` `app=kora-tool-gateway` | 8096 TCP | governed MCP servers |
   | `kora-system` `app=kora-policy-service` | 8083 TCP | OPA bundle polling |
   | this namespace, `runkora.io/role=sandbox` pods | 50051 TCP | orbit-shell, the session registry and the egress canary to `sandbox-exec` |
   | the collector: its namespace when in the cluster, otherwise the `networkPolicy.externalServices` ranges serving its port | the collector's port | OTLP export from the session-manager, identity-proxy and Claude Code ([2026-09-18: Telemetry Collector](2026-09-18-telemetry-collector-platform-destination.md)) |

   SPIRE needs no rule: SVIDs arrive over the CSI-mounted socket. The pod makes no Kubernetes API call; the session-manager provisions sandboxes through kora-agents. The collector is the tenant collector endpoint of [2026-09-18: Telemetry Collector](2026-09-18-telemetry-collector-platform-destination.md). When `global.observability.collectorNamespace` is set, the rule names that namespace. Otherwise the rule names the `networkPolicy.externalServices` CIDRs whose ports include the collector's port, the same entries that already open the collector for the platform services. No new value is added.

   The pod's only stdio MCP server is `orbit-runtime-mcp`, the same for all three runtimes. It calls the session-manager on loopback, which no policy covers. `orbit-shell` calls the session-manager on loopback and `sandbox-exec` on the sandbox pod. Governed MCP servers are remote entries through the tool gateway.

2. **Istio Sidecar `agent-egress`.** Reconciled beside it, selecting the same pods, with `outboundTrafficPolicy.mode: REGISTRY_ONLY` and `egress.hosts` listing the Service FQDNs of identity, kora-agents, the inference gateway, the tool gateway and the policy service in the system namespace, the `kube-dns` Service in `kube-system` for DNS over TCP, plus the collector: every service in its namespace when it runs in the cluster, or its host when it does not. An external host resolves only through a ServiceEntry, which [2026-09-18: Telemetry Collector](2026-09-18-telemetry-collector-platform-destination.md) already requires the install to provide for an out-of-cluster collector.

3. **No bypass ports.** `5432` is removed from the agent pod's `traffic.sidecar.istio.io/excludeOutboundPorts`: the session-manager holds no database credential and reaches Postgres through kora-agents. `50051` stays excluded, as decided in [2026-09-18: Authenticated Sandbox Exec](2026-09-18-sandbox-exec-trust-boundary.md), and the NetworkPolicy limits it to the namespace's sandbox pods.

4. **Runtime switches.** Each runtime is configured not to make background calls the lockdown blocks:
   - Claude Code: `DISABLE_AUTOUPDATER`, `DISABLE_TELEMETRY`, `DISABLE_ERROR_REPORTING`, `DISABLE_FEEDBACK_COMMAND`, `CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY` and `CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL`. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` is not used: it also turns off `CLAUDE_CODE_ENABLE_TELEMETRY`, which carries run traces to the platform collector.
   - Codex: `check_for_update_on_startup = false`, `[analytics] enabled = false`, `[feedback] enabled = false`, `[features] plugins = false` (the curated plugin sync clones `github.com/openai/plugins` at start), and `web_search = "disabled"`.
   - OpenCode: already configured by its runner (Amendment 1).

5. **Enforcement is unconditional.** Like the sandbox policies, the agent policies do not depend on `networkPolicy.enabled`, which governs the chart's own `kora-system` policies. A cluster whose CNI does not enforce NetworkPolicy keeps the Sidecar's host restriction.

A run without a sandbox therefore has no external egress except through governed MCP servers via the tool gateway. A run with a sandbox also reaches external hosts through the sandbox shell, and so through the egress gateway and its policy.

### Rationale

The NetworkPolicy alone meets the security goal but makes every blocked call hang for Envoy's connect timeout, which agents experience as a stalled tool. The Sidecar alone fails fast but leaves Envoy's capture gaps open. Together they give an immediate, readable failure for the common case and a kernel-level backstop for the rest, using two mechanisms the operator already reconciles for other workloads (NetworkPolicy for sandboxes, Istio resources for MCP servers). Options 3 and 4 do not constrain the traffic that was observed leaving the pod.

---

## Consequences

### Positive

- The agent pod's destinations are an explicit, reviewable list, and every model, tool and external call goes through a platform gateway.
- In-process network tools (Claude Code WebFetch, OpenCode webfetch, a local MCP server) fail at once instead of bypassing the egress gateway.
- A misconfigured runtime can no longer reach a hosted model outside the inference gateway.

### Negative

- A new platform dependency of the agent pod needs a rule in both resources, or it is blocked. Both live in one operator file, and the operator reconciles changes onto existing tenants.
- The collector address is the one external destination the agent process itself can reach. When it is a shared SaaS endpoint, a prompt-injected agent can export data under an attacker's own key to the attacker's account on the same service. Installs that care should run an in-cluster collector that forwards to the external one, so agent pods reach only the in-cluster address.
- The sidecars share the allow-list: the agent process can open a connection to the identity or policy service, as those sidecars must. Both services authenticate the caller, so reachability grants nothing on its own.

### Neutral

- DNS stays limited to the cluster resolver; the resolver's own upstream lookups remain possible, the same accepted residual as the sandbox policy.
- Ingress to the agent pod is unchanged.

---

## Implementation

- `backend/internal/operator/tenant_agent_egress.go`: `ensureAgentEgressPolicy` and `ensureAgentEgressSidecar`, reconciled with the sandbox policies in `tenant_netpol.go`; the Sidecar is written as unstructured, like the MCP server policies.
- `charts/kora/templates/operator/`: RBAC for `networking.istio.io` `sidecars`; `ORBIT_COLLECTOR_NAMESPACE` from `global.observability.collectorNamespace`, and `ORBIT_COLLECTOR_CIDRS` from the `networkPolicy.externalServices` entries serving the collector's port.
- `backend/internal/agents/k8s.go`: drop `5432` from the excluded outbound ports.
- `backend/internal/agents/sessionmanager/`: the Claude Code environment variable and the Codex `config.toml` keys.

---

## Validation

On the local cluster, for Claude Code, Codex and OpenCode runs:

- A run completes end to end: model calls recorded as gateway usage rows for the run, a governed MCP tool call, sandbox provisioning and a shell command in the sandbox.
- From the `session-manager` container, `curl https://example.com` and `curl http://example.com` fail at once.
- A WebFetch-style in-process call from a run fails with a connection error, and the run continues.
- The run's pod logs show no retries against blocked update, telemetry or plugin hosts.

---

## Amendment 1 (2026-11-22): Scope the sandbox and kora-system proxies

**Status:** Accepted

Touches Decision 2 and the Non-Goal that left the sandbox pod unchanged. It changes *How does it connect?* for the sandbox pod and the kora-system workloads: the set of hosts each proxy is programmed with. NetworkPolicies, AuthorizationPolicies, OPA and the egress gateway are unchanged. *Who is this agent?* and *What can it access?* are unchanged.

### Context

An Istio proxy with no `Sidecar` resource receives every Service in the mesh as an Envoy cluster. Each agent adds its headless Service and a DestinationRule. Constellation prod with 48 agents was measured on 2026-11-22:

*   An agent pod under `agent-egress` holds 27 clusters, 0.34MB of config and 36MiB of istio-proxy memory.
*   A sandbox pod has no Sidecar. It holds 266 clusters, 3.96MB of config and 69MiB of memory.
*   Memory grows about 0.14MiB per cluster, and each agent adds about 1.3 clusters.
*   Prod pins the sandbox istio-proxy at 128Mi request and limit through `operator.injection.sandboxSidecars`. The proxy reaches 128Mi at about 370 agents and about 245MiB at the design target of 960 agents.
*   The 25 kora-system proxies have no Sidecar. kora-agents holds 283 clusters and 3.83MB of config and peaked at 116MiB over 7 days.

On the local cluster with four agents, a kora-system proxy holds 59 clusters and an agent pod 25.

A sandbox pod reaches these in-cluster hosts:

*   `kora-identity` on port 80: the identity-proxy token exchange and JWKS (`KORA_IDENTITY_SERVICE_URL` in `backend/internal/operator/pod_webhook.go`).
*   `kora-policy-service` on port 80: OPA bundle polling (`kora-opa-config` in `backend/internal/operator/tenant_controller.go`). The `opa-policy-ext-authz` EnvoyFilter patches route config `80` at the virtual host `kora-policy-service.<system namespace>.svc.cluster.local:80`.
*   `kora-egress-gateway` on port 8085: the `egress-transparent-encap` internal listener routes to its `outbound|8085` cluster, and `egress-gateway-connect-outbound` patches the `allow_any` virtual host of route config `8085`.

The sandbox exports no telemetry: the webhook clears the identity-proxy OTLP endpoint on sandbox pods. `sandbox-exec` makes no in-cluster call. xDS does not use an Envoy cluster. Envoy captures DNS over TCP and routes it through the kube-dns listener.

The mesh runs `outboundTrafficPolicy: ALLOW_ANY` (`istio.outboundTrafficPolicy`). Istio renders the `allow_any` virtual host only in that mode. External TCP from a sandbox to a port without a dedicated listener takes `virtualOutbound`, where `egress-transparent-encap` patches 5 to 7 tunnel it to the egress gateway. TLS on `:443` takes the `0.0.0.0_443` listener (patches 3 and 4) when an in-scope Service creates that listener, and patch 7 otherwise. Both tunnel `CONNECT <sni>:443`. A `git clone` or package install reaches the egress gateway whichever in-cluster hosts the proxy knows.

Two kora-system workloads call into tenant namespaces. The chart's NetworkPolicies (`charts/kora/templates/platform/netpols.yaml`) list the same two:

*   kora-agents calls each session-manager on port 8090 through the agent's headless Service (`backend/internal/platform/clients/sessionmanager/client.go`).
*   The tool-gateway proxy and publisher call MCPServer endpoints in tenant namespaces. Both Deployments carry `app: kora-tool-gateway`.

The other kora-system workloads reach kora-system Services, the Kubernetes API, kube-dns, istiod, the collector, and external hosts. External hosts go through Envoy passthrough under `ALLOW_ANY`. Under `REGISTRY_ONLY` they need ServiceEntries. The chart (`serviceentries.yaml`) and the operator (inference providers and external MCP servers, `exportTo: ["."]`) create these in kora-system.

### Decision: Sandbox Sidecar

The operator reconciles an Istio Sidecar `sandbox-egress` in every tenant namespace beside `agent-egress`. It selects `runkora.io/role: sandbox` and lists four `egress.hosts`: the Service FQDNs of `kora-identity`, `kora-policy-service` and `kora-egress-gateway` in the system namespace, and of `kube-dns` in `kube-system`. It sets no `outboundTrafficPolicy` and inherits the mesh mode. `REGISTRY_ONLY` would remove the `allow_any` virtual host that `egress-gateway-connect-outbound` patches. `ALLOW_ANY` would open port 80 passthrough on a mesh that runs `REGISTRY_ONLY`. The operator creates and merge-patches it the same way as `agent-egress`.

### Decision: kora-system Sidecars

**Option O1 (chosen):** The chart renders Sidecars in kora-system. A namespace default Sidecar with no workload selector lists `./*`, `<istio namespace>/*`, the `kubernetes` Service in `default`, the `kube-dns` Service in `kube-system`, and the collector. The collector entry is `<collectorNamespace>/*` when `global.observability.collectorNamespace` is set. Otherwise it is `*/<endpoint host>`, with a `.svc` host expanded to `.svc.cluster.local`. A collector endpoint with no DNS host adds no entry. The default also lists `<namespace>/*` for a `config.db.host` or `config.redis.host` of the form `<name>.<namespace>.svc`. Two workload Sidecars select `app: kora-agents` and `app: kora-tool-gateway` and list `*/*`. A Sidecar with a workload selector takes precedence over the namespace default.

*   **Pros:** Every other kora-system proxy stops receiving tenant Services, and its config no longer grows with the agent count. The change is chart-only.
*   **Cons:** kora-agents and the tool-gateway keep the full config and still grow with the agent count. A call from another kora-system workload to a `.svc` host in another namespace leaves the mesh registry. This covers an in-cluster inference provider and an in-cluster brokered upstream. An `ALLOW_ANY` mesh sends the call through passthrough with no Istio mTLS or DestinationRule, and a `REGISTRY_ONLY` mesh refuses it. A destination with STRICT mTLS then resets the call.

**Option O2 (rejected):** kora-agents sets `networking.istio.io/exportTo: ".,<system namespace>"` on each agent headless Service and an `exportTo` on each agent DestinationRule, and the operator narrows the MCPServer DestinationRule from `["*"]`. `exportTo` is per namespace. Every kora-system proxy still receives every agent Service. It only hides agent Services from the unscoped pods of other tenant namespaces. MCPServer Services are tenant-authored, and kora does not set their `exportTo`.

**Option O3 (rejected):** O1 and O2 together. O2 adds hiding of agent Services from MCP server pods in other tenant namespaces. That gain does not reduce any measured proxy and adds code to kora-agents and the operator.

**Chosen Option:** O1. The trade-off is that kora-agents and the tool-gateway still scale with the agent count, and a new kora-system caller into another meshed namespace needs a Sidecar entry. A dependency outside the mesh keeps working through passthrough under `ALLOW_ANY`. On Constellation prod the only meshed namespaces are kora-system and the tenant namespaces, and RDS, monitoring and the collector run outside the mesh.

### Consequences

#### Positive

*   A sandbox proxy holds the same few clusters at any agent count. The 128Mi pin no longer depends on the agent count.
*   With O1, about 20 of the 25 prod kora-system proxies hold a config independent of the agent count, and istiod pushes less to each of them.

#### Negative

*   A new in-cluster dependency of the sandbox needs a host in `sandbox-egress` as well as a rule in `sandbox-egress-to-gateway`.
*   MCP server pods in tenant namespaces have no Sidecar and still receive every Service. The local `kora-example` MCP server holds 60 clusters.

#### Neutral

*   A sandbox connection to an unlisted in-cluster Service is still refused. On port 80 it takes `allow_any` passthrough and the NetworkPolicy drops it. On other ports it tunnels as `CONNECT <ip>:<port>` and the sandbox OPA applies IP posture.
*   TLS from a sandbox on `:443` takes the SNI chain on `virtualOutbound` from [JIT Credential Brokering Egress Gateway, Amendment 6](2026-06-06-jit-credential-brokering-egress-gateway.md#amendment-6-2026-10-27-tls-on-non-443-ports-tunnels-by-sni-and-dialled-port). TLS on `:443` with no SNI, or with an SNI that is not a plain DNS name, takes the catch-all and IP posture.

### Implementation

*   `backend/internal/operator/tenant_netpol.go`: `ensureSandboxEgressSidecar`, reconciled after `ensureAgentEgressSidecar` in `tenant_controller.go`. Both share one builder and one create-then-patch helper in `tenant_agent_egress.go`.
*   kora-system Sidecars: `charts/kora/templates/platform/sidecars.yaml`.

### Validation

On the local cluster:

*   `pilot-agent request GET clusters` on a sandbox pod and a kora-system pod before and after.
*   A run with a sandbox: a model call, `git clone` of a public GitHub repository through the egress gateway, a sandbox exec, a loaded OPA bundle and an identity-proxy token.
*   For the kora-system Sidecars: a run end to end through kora-agents and the session-manager, a governed MCP tool call through the tool-gateway, and an inference gateway model call.

---

## Related Decisions

- [2026-03-05: Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md): the "external egress via gateway only" constraint this implements for agent pods
- [2026-06-06: JIT Credential Brokering Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the only external path, through the sandbox
- [2026-09-18: Authenticated Sandbox Exec and Runner Isolation Parity](2026-09-18-sandbox-exec-trust-boundary.md): the sandbox exec port and its ingress policy
- [2026-09-18: Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md): the collector as the one non-gateway destination
- [2026-06-04: OpenCode Agent Runner](2026-06-04-opencode-agent-runner.md): Amendment 1 routes OpenCode through the inference gateway
