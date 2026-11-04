# Kubernetes Connection - Product Requirements Document

**Status:** Draft
**Created:** 2026-07-06
**Last Updated:** 2026-07-06
**Owner:** Platform Engineering
**Stakeholders:** Tenant Admins, Agent Authors, Security/Compliance, Platform Engineering / SRE

---

## Executive Summary

Agents that do real operational work — deploy a change, debug a failing pod, scale a service — need to run `kubectl` and Kubernetes SDK calls against a real cluster. Today there is no governed way to give an agent that access: the only path is handing it a kubeconfig, which puts a standing cluster credential inside the sandbox. This PRD adds **Kubernetes** as a new connection type in the Connections tab, following exactly the pattern already proven by the **GitHub connection**: a GitOps-registered `Connection` custom resource, a pluggable credential adapter the egress gateway brokers, and a read-only catalog view. The `kubernetes` adapter type is already reserved in code (`backend/internal/credentials/models.go:29`) and the flow is already designed in the credential-broker ADR — this PRD scopes the work to build it.

---

## Problem Statement

### Current State

kora's credential broker has one connection type fully built end-to-end: **GitHub**, using a `github_app` adapter that mints short-lived installation tokens and injects them on the gateway's outbound leg. The Connections tab (`frontend/src/app/(dashboard)/connections/`) is a read-only table sourced from `GET /api/v1/connections`, showing each connection's adapter, grantable actions, and which agents use it — with copy that explicitly states connections are GitOps `Connection` CRs reconciled by the operator, not something created in the UI.

Kubernetes is already anticipated but not built:
- `AdapterKubernetesBearer = "kubernetes"` is declared as a constant with no adapter implementation behind it.
- The credential-broker ADR (`docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md:163-168`) already designs the flow: a credential-less kubeconfig in the sandbox, OPA authorizing `connection:k8s:<cluster>:<verb-class>`, the gateway resolving a ServiceAccount bearer token and injecting it on the re-originated leg to the real API server.
- The credential-broker PRD (`docs/prd/credential-broker-and-outbound-connections.md:101`) lists Kubernetes as a named "fast-follow" connection.
- No adapter package, no gateway registration, no example `Connection` CR, and no frontend label exist yet.

### User Pain Points

- **Agent Authors** cannot build an "ops" agent (deploy, restart, scale, debug) that touches a real cluster without smuggling a kubeconfig into the agent's environment.
- **Tenant Admins** have no way to register a cluster the way they can register GitHub — there is no equivalent connection type to grant.
- **Security/Compliance** cannot answer "what did this agent do against our cluster" because there is no governed connection or audit trail for Kubernetes calls.
- **Developers** running an ops agent today must either give up on the use case or pass a personal/service kubeconfig by hand, defeating the whole point of the credential broker.

### Business Impact

- DevOps/SRE-style agents — a headline use case for "agent that can act, not just advise" — are blocked entirely without a governed Kubernetes connection.
- The credential-broker story is incomplete without its second connection type; GitHub alone doesn't prove the adapter pattern generalizes.

---

## Goals and Objectives

### Primary Goals

1. Let an agent run `kubectl` and Kubernetes SDK (`client-go`, etc.) calls against an admin-authorized cluster from the sandbox, using ordinary tools unmodified.
2. Never let the agent hold the cluster's real bearer token or kubeconfig credential — the same structural guarantee the GitHub connection already provides.
3. Reuse the existing GitOps `Connection` CR + adapter-registry pattern with no new core architecture: implement `kubernetes` as the second `CredentialAdapter`, mirroring `github_app`'s package shape.
4. Enforce a coarse read/write policy gate at the gateway and rely on the target cluster's own RBAC, applied to the injected identity, as the fine-grained backstop (defense in depth — the same posture the ADR already states for Kubernetes).
5. Surface the new connection type in the existing read-only Connections tab with the minimum possible frontend change.

### Success Metrics

- An agent can `kubectl get pods` / `kubectl apply` against a granted cluster connection with **zero cluster credentials present in the sandbox**, verified by inspection. Verified so far via the gateway's direct-HTTP CONNECT-port mechanism, which exercises the same broker/adapter/CA-trust path a live sandbox would use; a literal live-sandbox `kubectl` run is still outstanding (see [ADR Amendment 3](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-3--2026-07-06-local-re-verification-of-the-full-adapterbroker-path)).
- A tenant admin can register a cluster connection (apply a `Connection` CR + a `Secret` holding a bearer token) in **under 5 minutes**, with no engineering ticket.
- **100%** of API-server calls made through a Kubernetes connection are authorized before execution and produce an audit record naming the agent, run, connection, and verb-class.
- Revoking a grant stops new calls **immediately**.
- The Connections tab correctly renders a Kubernetes connection (adapter label, action badges, "used by" agents) with **no new UI component** — only a label-map entry, matching the GitHub connection's zero-bespoke-UI precedent.

### Non-Goals

- **Client-certificate authentication.** V1 is bearer-token only (`kubernetes`); client-cert auth is deferred.
- **JIT-minted, per-call tokens.** V1 resolves a static, admin-supplied bearer token (Pattern B-style resolution), not a per-call `TokenRequest` mint against the target cluster. See Open Questions and Alternatives Considered — true per-call minting (parity with GitHub's installation-token exchange) is a deferred fast-follow, not a launch requirement.
- **Fine-grained (namespace/resource/verb) policy modeling inside kora.** kora authorizes only a coarse verb-class; the target cluster's own RBAC on the injected identity is the fine-grained enforcement point.
- **Auto-provisioning ServiceAccounts or RBAC bindings on the target cluster.** The admin brings their own token, exactly as they bring their own GitHub App install.
- **A connection creation/edit UI.** Registration stays GitOps-only (`kubectl apply`), per the existing ADR decision that the management UI is read-only.
- **Delegated ("act as the human") Kubernetes identity.** Out of scope, consistent with the broader credential-broker PRD's non-goals.
- **Multi-cluster network tunneling/peering.** The target cluster's API server must already be reachable from the gateway's network; solving reachability for isolated/private clusters is not in scope for this PRD.

---

## User Stories

### Primary User Stories

**As a** Tenant Admin,
**I want to** register a Kubernetes cluster as a connection by providing its API endpoint, CA certificate, and a bearer token,
**So that** agents can act against that cluster without me ever handing an agent a kubeconfig.

**Acceptance Criteria:**
- [ ] I can apply a `Connection` CR with `adapter: kubernetes` referencing a `Secret` that holds the bearer token.
- [ ] `baseHost` must be a DNS hostname, not a bare IP — a live-cluster validation pass found that an IP literal carries no TLS SNI, which the sandbox's transparent-egress interception fails closed on; this is now rejected at `kubectl apply` time (see [ADR Amendment 2](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-2--2026-07-06-basehost-must-be-a-dns-hostname-not-an-ip-literal)).
- [ ] The bearer token is never displayed by any API or UI after the Secret is created.
- [ ] The connection appears in the Connections tab labeled "Kubernetes" with its grantable actions.

---

**As an** Agent Author,
**I want to** grant my agent read or write access to a specific cluster connection,
**So that** my agent can run `kubectl`/SDK calls against that cluster within a bounded action level.

**Acceptance Criteria:**
- [ ] I can grant an agent `cluster:read` and/or `cluster:write` on a named Kubernetes connection.
- [ ] An agent with no grant on the connection cannot reach the cluster's API server; the attempt is denied and recorded.

---

**As a** Developer running an ops agent,
**I want to** have the agent run `kubectl get`, `kubectl apply`, etc. against a granted cluster with zero credential setup,
**So that** the agent behaves like it would on my laptop, without me ever configuring or seeing a token.

**Acceptance Criteria:**
- [ ] The sandbox's kubeconfig points at the gateway-routed cluster endpoint with no real credential embedded.
- [ ] `kubectl`/SDK calls succeed transparently when the agent has a grant, and fail cleanly when it doesn't.

---

**As a** Security reviewer,
**I want to** see every Kubernetes API call an agent made, and confirm no cluster credential ever entered the agent,
**So that** I can sign off on an agent acting against a production cluster.

**Acceptance Criteria:**
- [ ] Every API-server call is attributed to an agent, run, connection, and verb-class in an audit record.
- [ ] Inspection of the agent's sandbox shows no bearer token or kubeconfig credential present.
- [ ] An unauthorized call is denied before it reaches the cluster and is recorded as denied.

### Secondary User Stories

- As a Tenant Admin, I want to register multiple Kubernetes connections (e.g. `k8s-staging`, `k8s-prod`) so different agents can be scoped to different clusters.
- As a Tenant Admin, I want to revoke a compromised cluster connection's grants and have access stop immediately, without rotating anything on the agent side.
- As a Platform Engineer, I want the Connections tab's "used by" column to show me which agents can currently reach which clusters, for a quick blast-radius check.

---

## Proposed Solution

### High-Level Approach

Implement Kubernetes as the second connection type on the already-built credential-broker architecture — no new core mechanism, only a new adapter and its registrations:

1. A new `backend/internal/credentials/adapters/k8sbearer/` package implementing `CredentialAdapter` (`Resolve`, `Actions`, `ClassifyAction`), mirroring the shape of `adapters/githubapp/`.
2. A `platform/connections` registry entry for `AdapterKubernetesBearer`, declaring the grantable action vocabulary (`cluster:read`, `cluster:write`) and `egressHosts` (the connection's declared cluster API host).
3. Registration of the new adapter in the gateway's `AdapterRegistry` (`backend/cmd/egress-gateway/main.go`), alongside `githubapp` and `passthrough`.
4. One new gateway capability: **per-connection custom CA trust** for the re-originated TLS leg. GitHub's upstream is publicly CA-trusted; Kubernetes API servers commonly present a private or self-signed CA, so the gateway must validate the real upstream certificate against the connection's own declared CA rather than only the system trust store.
5. An example `Connection` CR (`deploy/local/connections/kubernetes.yaml`) for local/dev testing, alongside the existing `github.yaml`.
6. One frontend change: an `ADAPTER_LABELS` entry (`frontend/src/app/(dashboard)/connections/_types/index.ts:60-63`) mapping `kubernetes` → `"Kubernetes"`. The table, badges, and "used by" logic are already adapter-agnostic and need no other change.

No `Connection` CRD schema change is required — `spec.config` is already `x-kubernetes-preserve-unknown-fields: true`, so a Kubernetes connection's `config: {cluster_name, ca_cert}` fits the existing freeform shape.

### User Experience

The experience mirrors the GitHub connection's three-moment separation (register → grant → use), with no new UI surface.

#### User Flow

1. **Register (Admin, once per cluster).** The admin provisions a bearer token for kora against the target cluster (a `ServiceAccount` token, following their own cluster's normal RBAC provisioning — kora does not create it), creates a `Secret` in the tenant namespace holding that token and the cluster's CA certificate, and applies a `Connection` CR: `adapter: kubernetes`, `baseHost: <cluster-api-host:port>`, `secretRef`/`secretKey` pointing at the `Secret`, `config: {cluster_name, ca_cert}`.
2. **Reconcile.** The operator's existing `ConnectionAggregator` picks up the CR and its referenced `Secret` and renders them into the same `kora-tenant-connections` / `kora-tenant-connection-secrets` objects the gateway already mounts — no new operator code path.
3. **Appear.** The connection shows up in the Connections tab: adapter "Kubernetes", actions `cluster:read` / `cluster:write`, no agents yet listed under "used by".
4. **Grant.** The admin/agent author grants an agent a connector on this connection at `cluster:read` and/or `cluster:write`, exactly as for GitHub.
5. **Use.** The agent's sandbox kubeconfig names the cluster's API server with no real credential. `kubectl`/SDK calls route through the gateway; OPA authorizes `connection:<name>:cluster:<verb-class>`; the gateway resolves the bearer token from the mounted `Secret`, terminates the API-server TLS (validating against the connection's declared CA), injects `Authorization: Bearer <token>`, re-originates to the real API server, streams the response, and emits an audit record.
6. **Audit & revoke.** Every call is attributed to agent, run, connection, and verb-class. Removing the grant or disabling the connection stops new calls immediately.

### Key Components

- **`k8sbearer` adapter** — resolves the bearer token from the mounted secret and classifies each request into `cluster:read` (`get`/`list`/`watch`-shaped requests) or `cluster:write` (mutating verbs), matching the read/write split already used for GitHub.
- **Registry metadata** — action vocabulary and `egressHosts` for `kubernetes` in `platform/connections/adapters.go`, the single source the adapter, the connector picker, and the auto-egress deriver already share.
- **Gateway per-connection CA trust** — the one genuinely new gateway capability this feature needs; without it, self-signed/private cluster CAs cannot be validated on the re-originated leg.
- **Example CR** — `deploy/local/connections/kubernetes.yaml`, giving local dev (which already runs a k3d cluster) an end-to-end path to test against.
- **Frontend label entry** — the only product-facing code change.

---

## Scope

### In Scope

- `kubernetes` `CredentialAdapter` implementation (bearer-token resolution only).
- Coarse `cluster:read` / `cluster:write` action vocabulary and policy wiring through the existing `connection:<name>:<action>` scope hierarchy.
- GitOps registration via `Connection` CR (no CRD change expected).
- Per-connection custom CA trust on the gateway's re-originated TLS leg.
- Connections-tab surfacing (adapter label + action badges) using the existing generic table — no new UI.
- Example local `Connection` CR and setup documentation.
- Audit records for every brokered API-server call.

### Out of Scope

- Client-certificate authentication (may be added as a second Kubernetes adapter variant later).
- kora-side `TokenRequest`-based minting of short-lived tokens against the target cluster (true Pattern-A parity with GitHub).
- Fine-grained (namespace/resource/verb) authorization inside kora's own policy — delegated to the target cluster's RBAC.
- Auto-provisioning of ServiceAccounts, RBAC bindings, or tokens on the target cluster.
- `kubectl exec` / `port-forward` (SPDY/WebSocket-upgraded, not plain REST) — see Open Questions.
- Solving network reachability for clusters not already reachable from the gateway (VPN/PrivateLink/tunneling).

### Future Considerations

- `TokenRequest`-based JIT minting, once a safe design exists for kora to hold the privilege needed to mint bounded-TTL tokens against arbitrary customer clusters.
- Client-certificate auth as an alternate `kubernetes`-sibling adapter.
- A "Test connection" dry-run action against the cluster, as already floated (but not built) in the credential-broker ADR's read-only-management-UI section.
- `kubectl exec`/`port-forward` support if ops-agent usage demands it.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `kubernetes` `CredentialAdapter` implementing `Resolve`, `Actions`, `ClassifyAction`.
- [ ] Registry entry in `platform/connections/adapters.go` (action vocabulary + `egressHosts`).
- [ ] Registration in the gateway's `AdapterRegistry` (`cmd/egress-gateway/main.go`).
- [ ] Gateway support for per-connection custom CA trust on the re-originated TLS leg.
- [ ] `connection:<name>:cluster:read` / `connection:<name>:cluster:write` scopes wired through the existing `scope_covers` hierarchy.
- [ ] Example `Connection` CR for local/dev testing.
- [ ] `baseHost` restricted to DNS hostnames (no bare IP literal), enforced at the CRD-validation, operator-aggregation, and adapter-registry layers.
- [ ] `ADAPTER_LABELS` entry for `kubernetes` in the frontend.
- [ ] Audit record per API-server call: agent, run, connection, verb-class, result.

**Should Have (P1):**
- [ ] A connection-status signal (e.g. surfaced in the catalog) indicating whether the registered token currently authenticates successfully against the cluster.
- [ ] Admin-facing documentation for provisioning a bearer token for kora on a target cluster.

**Nice to Have (P2):**
- [ ] A "Test connection" dry-run action, consistent with the future consideration noted in the credential-broker ADR.

### Non-Functional Requirements

**Performance:**
- Credential resolution adds negligible latency versus the direct call path — no per-call minting round-trip (the token is read from the mounted secret, not exchanged).

**Security:**
- The cluster bearer token never enters the sandbox at any point.
- The token is stored only as a Kubernetes `Secret` referenced by `secretRef`, never in the `Connection` CR itself.
- The gateway validates the real upstream API-server certificate against the connection's declared CA before injecting the credential.

**Reliability:**
- Token rotation (updating the referenced `Secret`) requires no change to the `Connection` CR, any grant, or the agent — the gateway picks up the rotated secret on its next resolve via the existing `fsnotify` reload.

**Scalability:**
- A tenant can register multiple Kubernetes connections (e.g. one per cluster/environment) with no core changes; each is an independent `Connection` CR.

**Observability:**
- Every brokered call is structured-logged and audited with cluster/connection/verb-class attribution.
- Adapter-level failures (invalid or expired token, CA trust failure) are distinguishable from policy denials in logs and audit records.

---

## Dependencies

### Internal Dependencies

- `platform/connections` registry — extend with the new adapter's metadata. (Built, extending.)
- `egressgw.AdapterRegistry` / broker / proxy — register the new adapter; broker/proxy hot path is unchanged. (Built, extending.)
- Operator `ConnectionAggregator` and `Connection` CRD — reused unchanged; no schema change expected. (Built.)
- `k8sfile.SecretResolver` — reused unchanged to read the mounted bearer-token secret. (Built.)
- Policy-service `connection:*` scope vocabulary and `scope_covers` hierarchy — reused unchanged. (Built.)
- Frontend Connections page/types — one label-map entry. (Built, extending.)

### External Dependencies

- The target Kubernetes cluster's API server must be network-reachable from the egress gateway (a private-VPC or on-prem cluster may require the admin to expose it, or reachability work that is out of scope here).
- The admin's own cluster RBAC/ServiceAccount provisioning process — kora consumes a token but does not create ServiceAccounts or role bindings.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Target cluster API servers are often not network-reachable from the gateway (private VPC/on-prem) | High | Medium | Document the reachability requirement in admin setup; defer solving isolated-network reachability to a future PRD |
| Cluster API servers commonly present private/self-signed CAs, which the gateway's TLS re-origination doesn't yet validate per-connection | High (launch-blocking) | High | Build per-connection CA trust as a P0 requirement, not a fast-follow |
| A long-lived, broadly-scoped bearer token pasted into a connection secret is a standing high-privilege credential if leaked | High | Medium | Document bounded-TTL token provisioning and least-privilege RBAC as setup best practice; track true per-call minting as the v1.x fast-follow that closes this gap |
| Coarse read/write verb-class policy doesn't match some customers' expectations of namespace/resource-scoped grants | Medium | Medium | State explicitly that fine-grained enforcement is delegated to the cluster's own RBAC on the injected identity (defense in depth); revisit granularity based on early feedback |
| Any `kubernetes` connection whose API server isn't on the default HTTPS port 443 (i.e. almost every real cluster, which use `:6443` or an LB port) breaks the agent-type Connector's auto-derived `egress:<host>:connect` grant — confirmed live, unfixed | High (blocks Connector-based grants for realistic clusters) | High | Confirmed bug tracked in [ADR Amendment 2](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#separate-confirmed-bug-independent-of-the-above-unfixed); create the policy grant directly via the policy-service API as a workaround meanwhile, and fix `isStructurallyValidEgressScope` (or strip the port before compiling the scope) before relying on Connector auto-derivation for this connection type |

---

## Alternatives Considered

### Alternative 1: kora mints short-lived tokens via the Kubernetes `TokenRequest` API (true Pattern A, like GitHub)

**Description:** The gateway calls `TokenRequest` against the target cluster to mint a bounded-TTL token per call, mirroring GitHub's App-JWT-to-installation-token exchange.

**Why not chosen:**
- Requires kora to hold a credential against every customer's cluster capable of issuing tokens for arbitrary ServiceAccounts — a materially larger trust ask than "the admin hands us one bounded token."
- No existing mechanism for provisioning that elevated access safely; deferred until a design exists, tracked as a v1.x fast-follow.

### Alternative 2: Client-certificate authentication instead of bearer tokens

**Description:** Authenticate to the API server with a client certificate rather than a bearer token.

**Why not chosen:**
- Bearer-token header-swap fits the gateway's existing Pattern-A/B injection mechanism with no new plumbing; presenting a client cert on the re-originated leg is a materially different code path.
- Deferred as a possible sibling adapter, not built for v1.

### Alternative 3: Inject a scoped kubeconfig directly into the sandbox (no gateway involvement)

**Description:** Skip brokering; mount a real, scoped kubeconfig into the agent's sandbox at pod start.

**Why not chosen:**
- Puts a real cluster credential inside the sandbox, violating the platform's core "agent never holds the secret" guarantee. Rejected outright.

---

## Open Questions

1. **Should kora ever mint bounded-TTL tokens itself (`TokenRequest`) instead of always relying on an admin-supplied static token?**
   - **Status:** Open
   - **Owner:** Platform Engineering (ADR)

2. ~~What exact verb-class taxonomy should the adapter expose~~ — just `cluster:read`/`cluster:write`, or a third tier for higher-risk operations (e.g. `delete`)?
   - **Status:** Resolved — a resource-category taxonomy (`workloads`, `config`, `secrets`, `networking`, `storage`, `rbac`, each with `:read`/`:write`, plus `cluster:read`/`cluster:write` as the catch-all for other well-known cluster-scoped/admin resources and API discovery), not a third verb tier. See [Amendment 1](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-1--2026-07-06-resource-category-verb-taxonomy). Resources not in the category map (CRDs, anything not yet enumerated) are denied outright rather than folded into a catch-all.
   - **Owner:** Platform Engineering

3. ~~How does the gateway obtain and trust each cluster's CA — a PEM embedded in the `Connection`'s `config`, or a reference to a mounted CA-bundle secret alongside the token?~~
   - **Status:** Resolved — embedded as a PEM string in `config.ca_cert` on the `Connection` CR itself (see Proposed Solution → User Flow step 1, Key Components). CA certificates are public material, not secrets, so no `Secret`-mounted indirection is needed; only the bearer token goes through `secretRef`/`secretKey`.
   - **Owner:** Platform Engineering

4. **Is "must already be reachable from the gateway's network" an acceptable v1 constraint**, or do we need reachability tooling (VPN/PrivateLink/tunnel) sooner than a future PRD?
   - **Status:** Open
   - **Owner:** Platform Engineering

5. **Does `kubectl exec`/`port-forward` (SPDY/WebSocket-upgraded, not plain REST) need explicit v1 support**, given ops agents commonly use `exec` for debugging?
   - **Status:** Open
   - **Owner:** Platform Engineering

6. **Should `isStructurallyValidEgressScope` allow a trailing `:<port>` segment**, so an agent-type Connector's auto-derived `egress:<host>:connect` scope works for the non-443 API servers virtually every real Kubernetes connection uses? Discovered live (see [ADR Amendment 2](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#separate-confirmed-bug-independent-of-the-above-unfixed)); still unfixed.
   - **Status:** Open
   - **Owner:** Platform Engineering

---

## Timeline and Milestones

### Phases

**Phase 1: Adapter + gateway CA trust** (Target: TBD)
- `k8sbearer` adapter implementation and registration
- Per-connection custom CA trust on the gateway's TLS re-origination

**Phase 2: Registration, catalog, and local verification** (Target: TBD)
- Example `Connection` CR, frontend label entry, admin setup docs
- End-to-end verification against the local k3d cluster

**Phase 3: Fast-follow exploration** (Target: TBD)
- `TokenRequest`-based minting design (Open Question 1)
- Client-certificate adapter variant

### Key Milestones

- [ ] PRD Approved: TBD
- [x] ADR Created/Amended: 2026-07-06 (amended same day — resource-category verb taxonomy, `baseHost` DNS-hostname requirement, local re-verification)
- [x] Implementation Started: 2026-07-06
- [x] Local e2e verified against k3d: 2026-07-06 — full 8-item test matrix passed via the gateway's direct-HTTP mechanism against a hostname-based local connection; live-sandbox `kubectl` verification specifically still outstanding (see [ADR Amendment 3](../adr/2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-3--2026-07-06-local-re-verification-of-the-full-adapterbroker-path))
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [Credential Broker & Outbound Connections PRD](credential-broker-and-outbound-connections.md) — the umbrella PRD; Kubernetes is its named fast-follow connection.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway ADR](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md) — the technical design this PRD builds on, including the Kubernetes flow (§"Per-Tool Flows" → Kubernetes) and the GitOps `Connection` CR model (Amendment 3).
- [kora Kubernetes Operator ADR](../adr/2026-03-31-orbit-kubernetes-operator.md) — background on CR reconciliation used by the `Connection` CRD.

### Glossary

- **Connection:** A tenant-scoped registration of one external service, sourced from a GitOps `Connection` custom resource, not created via a CRUD API.
- **Adapter:** The credential mechanism a connection uses (`github_app`, `kubernetes`, `passthrough`, …); one adapter implementation serves every connection of that type.
- **Verb-class:** The coarse action category (`cluster:read` / `cluster:write`) the gateway classifies a Kubernetes API request into for policy purposes, distinct from the cluster's own fine-grained RBAC verbs.
- **ServiceAccount token:** A Kubernetes-issued bearer credential identifying a ServiceAccount to the API server; the credential kora resolves and injects for a Kubernetes connection.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
