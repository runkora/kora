# MCP Server Registration & Introspection

<!-- Filename: 2026-06-29-mcp-server-registration-and-introspection.md -->

**Status:** Accepted
**Date:** 2026-06-29
**Deciders:** Justin Wilkin, kora Core Team
**Technical Story:** [MCP / Tool Gateway PRD](../prd/mcp-tool-gateway.md)

---

## Context

### Background

kora governs *which model* an agent may call (inference gateway) and *which external host* it may reach (egress/credential gateway). It does **not** govern *which tool* a principal may call on an MCP server. The PRD's central move is to lift per-tool enforcement out of the MCP server and into the platform: introspect each server's tool surface, register every tool as a platform scope (`mcp:<server>:<tool>:use`), and enforce at a gateway. That enforcement (the gateway) is the subject of a separate ADR; **this ADR is the foundation it stands on** — how an MCP server gets *into* the platform and how its tools become governable scopes.

Today there is no MCP registration of any kind. `internal/toolgateway` does not exist; the only MCP code is `orbit-runtime-mcp` (a single hardcoded `provision_sandbox` tool) and a hardcoded `runtime` server in the agent's `.mcp-config.json`. The [2026-03-05 registration ADR](2026-03-05-mcp-server-registration-deployment-and-governance.md) is a paper design that was never built and treated the tool manifest as non-security discovery metadata. This ADR supersedes and implements its registration concern, with one decisive change: **the introspected tool list is load-bearing for authorisation.**

### Problem Statement

We need a way to: (1) declare an MCP server as infrastructure, tenant-scoped or platform-global; (2) discover its tools by calling `tools/list`; (3) register each tool as a `mcp:<server>:<tool>:use` scope in the policy vocabulary so it can be granted; (4) keep that registration idempotent as servers drift; and (5) keep one tenant's servers and tool scopes invisible to every other tenant — all without a "create server" button, because an ungoverned UI mutation path for a security-critical resource is the wrong posture.

### Goals

- Declare MCP servers in infra (GitOps), reconciled by the operator — no CRUD API, no UI create.
- Tenant-scoped registration (a tenant's own servers) and platform-global registration (the kora MCP and platform servers kora registers itself).
- Introspect `tools/list` and register `mcp:<server>:<tool>:use` scopes into the existing policy vocabulary, reusing the grant/scope machinery wholesale.
- Idempotent re-sync on drift: appeared tools added, vanished tools retired; the platform never auto-creates a grant.
- Tenant isolation across the CR, introspection, the catalog, and grants (the scope vocabulary stays a global name registry, as for `inference:`/`connection:`).
- A read-only catalog (servers → tools → grant status) for admins.

### Non-Goals

- The gateway, the PEP, and the consumption paths — see the [MCP / Tool Gateway ADR](2026-06-29-mcp-tool-gateway.md).
- Human/agent authentication — see the [MCP OAuth Authentication ADR](2026-06-29-mcp-oauth-authentication.md).
- Remote third-party SaaS MCP servers (fast-follow); MVP registers kora-hosted servers.
- Credential injection on the platform→server hop: a kora-hosted MCP is a managed microservice that owns its own backend credentials (it is not an agent sandbox); the platform does not force-route or inject on this hop.
  *Amended 2026-10-22 by [MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md): a server that declares `spec.auth` receives a static service credential on this hop.*
- MCP `resources/*` and `prompts/*` surfaces — MVP introspects **tools** only.

---

## Decision Drivers

- **Reuse over reinvention.** kora already has a scope vocabulary with idempotent upsert + orphan pruning, an operator that aggregates tenant-namespaced CRs into mounted config, and a `ScopePublisher` that registers a service's scopes. Registration should be these patterns, composed — not new machinery.
- **GitOps posture.** Every other per-tenant concern (`OrbitTenant`, `Connection`, ClusterSPIFFEIDs, per-tenant CA) is a reconciled CR. An MCP server is the same shape of object and is declared the same way.
- **Security by construction.** No UI mutation path; default-deny; tenant isolation enforced structurally (namespace + tenant-scoped grants + the tenant-dimensioned catalog), not by convention.
- **Drift is the norm.** MCP servers add and remove tools over their lifetime. The design must converge idempotently without manual scope bookkeeping.

---

## Considered Options

### Decision 1 — How a server is declared

- **Option A: `MCPServer` CRD, operator-reconciled (chosen).** Tenant-namespaced for a tenant's own servers; `orbit-system` for platform-global. Mirrors `Connection` ([jit-credential ADR, Amendment 3](2026-06-06-jit-credential-brokering-egress-gateway.md)). GitOps-native, no mutation API, isolation via namespace.
- **Option B: CRUD API + Postgres registry.** A `POST /mcp/servers` admin API. Rejected for exactly the reasons the credential broker rejected it: no human-usable front, a security-critical resource behind a raw API, and config split from its lifecycle. The credential broker already walked this back to CRs.
- **Option C: Static config (`mcpservers.yaml`).** Too static — no per-tenant story, no reconcile loop, no status.

### Decision 2 — Who introspects and publishes scopes

Introspection means opening an MCP session to a (running) server and calling `tools/list`; the discovered tools then become published `mcp` scopes. This splits cleanly along the boundary kora already drew for connections (jit-credential ADR, Amendment 3): the **operator aggregates CRs into mounted config**, and the **gateway does the domain work and holds the policy-service identity**.

- **Option A: The tool-gateway introspects and publishes; the operator only aggregates (chosen).** The operator runs an `MCPServerAggregator` (mirror of `ConnectionAggregator`) that reconciles `MCPServer` CRs into one `orbit-system` ConfigMap — and nothing more; it stays purely Kubernetes-facing. The **tool-gateway** mounts that registry and, on reload, introspects each server (it is *already* an MCP client — proxying MCP is its job) and publishes the union `mcp` vocabulary via `ScopePublisher` — exactly as the egress-gateway republishes `connection` scopes and holds the service identity policy-service requires. This keeps each component to one concern and matches the connection precedent verbatim.
- **Option B: The operator reconciler introspects and registers scopes.** Puts MCP-protocol calls and policy-service registration into a pod whose only job is reconciling Kubernetes objects — breaking single-concern and diverging from the connection split. It would also give the operator its first outbound service calls and a new SPIFFE identity for policy-service. Rejected: the only thing it bought was writing introspection health onto CR status, and that is the wrong home for runtime state (see Decision below).
- **Option C: A dedicated `mcp-registry` service.** A third deployment that watches CRs and introspects. More moving parts than warranted; introspection is the gateway's own MCP concern.

**Where the publisher lives — a `replicas=1` deployment of the same binary, not every proxy replica.** `DeleteOrphans` is *destructive*: it strips pruned scopes out of every grant and role (`PurgeScopeFromAll`), so an empty or short keep-list does not merely under-advertise — it **revokes access**. That makes the publisher a single-writer problem, not a thundering-herd one. Running it in every horizontally-scaled proxy replica would have each replica prune against its *own* keep-list, and during any divergence window the prune is dangerous:

- **Cold start / partial mount:** a replica that publishes before its registry ConfigMap mount or its first introspection round completes publishes an empty/partial keep-list → wipes `mcp` scopes from grants.
- **Divergent keep-lists:** introspection is per-replica and fails per-replica; replica B finding server Y transiently unreachable on boot (nothing to retain) prunes Y's tools while replica A re-adds them. Policy-service's recompile debounce (~500 ms per tenant, ~1 s gateway) coalesces *recompiles*, not *opposing intents* — it does nothing to make the larger keep-list win. Idempotency holds only **at convergence**, which is exactly when a destructive prune is *not* live.

So the publisher runs as its own **`replicas=1` deployment of the `tool-gateway` binary** (publisher mode); the request-path proxy scales independently. Single-writer by deployment topology — no leader election, no `Lease`, no Kubernetes API in either pod. It is the same binary and component (a run-mode split), not a separate service — distinct from rejected Option C.

**Prune-guard (required, even for the single writer).** The publisher performs `DeleteOrphans` only once the registry is fully mounted *and* every server it lists has either been successfully introspected this lifetime or has prior tools to retain; until then it is **Upsert-only**. This stops a boot-time empty/partial keep-list from revoking live `mcp` grants.

**CR status is reconciliation state, not introspection health.** Because the operator no longer introspects, the `MCPServer` CR's status reflects only what the operator knows — accepted and aggregated into the registry. Introspection health (reachable? how many tools? last synced?) is **runtime state the gateway owns**, surfaced via OTel/metrics and the read-only catalog UI — the same way connection health is a UI/*Test-Connection* concern, not CR status.

### Decision 3 — How tools become scopes (mirror the connection `ScopePublisher`)

MCP tool scopes follow kora's established scope model: the scope string names a capability (`mcp:<server>:<tool>:use`), `policy_scope_vocabulary` is the global registry of those names, and tenancy is carried by the grant — the same model as `inference:<family>:<model>:use` and `connection:<name>:<action>`.

Registration mirrors the egress-gateway's `ScopePublisher`. The tool-gateway's publisher (`registeredBy = "tool-gateway"`) publishes the full `mcp` scope vocabulary globally, with the keep-list being the union of every registered server's tools across all tenants. The existing `DeleteOrphans` then retires a scope only when no server in any tenant still exposes it, so introspection drift in one tenant leaves other tenants' shared names intact. Two tenants that both run a `github` server share the `mcp:github` tool-scope names exactly as two tenants' `github` connections share their `connection:github` names today; the gateway resolves each caller's own server and authorises on the caller's tenant-scoped grants.

### Decision 4 — Tenant vs global

The scope *string* is tenant-free for both; the tenant/global distinction is **not** in the vocabulary. It is carried by (a) **tenant-scoped grants** — the authorisation boundary — and (b) the tenant-dimensioned `mcp_tools` catalog, which determines whose admin UI can see and grant which tools. A tenant server's tools appear only in that tenant's catalog (+ globals); a global (kora-registered) server's tools appear in every tenant's catalog. Global is not an admin toggle — it is simply *who applied the CR and where*.

### Decision 5 — Naming & stability in the scope path

`mcp:<server>:<tool>:use` — resource_type `mcp`, path `[<server>, <tool>]`, action `use`. Prefix coverage: `mcp:<server>:use` covers a server's whole surface, `mcp:use` everything. `<server>` is the CR's stable `metadata.name`; `<tool>` is the MCP tool name **normalised** to a scope-path-safe segment (lowercase, `[a-z0-9_-]`, documented mapping). A renamed tool becomes a new scope: the old name drops from the keep-list and is pruned, so a client using it gets a clean default-deny; the new name registers ungranted. Both transitions surface in the catalog as drift.

**Amendment (2026-06-30) — multi-segment tool paths.** The single-segment `<tool>` above is not sufficient; this amends the introspection derivation. Servers (e.g. the [kora MCP](2026-06-30-orbit-mcp.md)) want domain rollups like `mcp:<server>:agents:use` covering `mcp:<server>:agents:create:use`, which requires a *multi-segment* path. The policy scope model already supports arbitrary-length paths (`inference:<family>:<model>:use` is a 2-segment path); the change is in how introspection maps a tool **name** to scope **segments**:

- **Reserved `.` delimiter.** Introspection splits a tool name on `.` into path segments: `agents.create` → `[<server>, agents, create]`. The `.` is a *delimiter*, never part of a segment.
- **Charset unchanged per segment.** Each resulting segment is still normalised to `[a-z0-9_-]` (the charset rule above now applies *per segment*, not to the whole tool name).
- **Backward compatible.** A tool name with no `.` registers as a single segment exactly as before.
- **Universal, not convention-scoped.** The split is applied by the shared introspector to **every** server, first-party and third-party alike. A third-party tool named `foo.bar` therefore registers `mcp:<server>:foo:bar:use` and gains a `mcp:<server>:foo:use` rollup — intended and harmless (still default-deny; still per-tool grantable). Server authors who want a flat tool simply avoid `.`.
- **Leaf-vs-prefix collision (flagged).** A leaf tool whose name equals another tool's *domain prefix* collides on the rollup string: `search` registers `mcp:<server>:search:use`, which is *also* the rollup that prefix-covers `search.advanced` (`mcp:<server>:search:advanced:use`) — so granting the leaf `search` silently grants its siblings. First-party servers (orbit-mcp) avoid this by naming convention; for arbitrary third-party servers introspection **detects** the case (a tool's full scope path being a strict prefix of another tool's path) and flags it in the catalog (and the server `Degraded`) rather than registering a silently-over-granting scope.

This is a real dependency for any server wanting domain rollups, not a cosmetic clarification.

---

## Decision

| # | Decision |
|---|----------|
| 1 | MCP servers are declared as an `MCPServer` CRD, operator-reconciled — tenant-namespaced or `orbit-system` (global). No CRUD API, no UI create. |
| 2 | The **operator only aggregates** CRs into a mounted registry. The **tool-gateway** introspects (`tools/list`) and publishes scopes from a **`replicas=1` publisher deployment** of the same binary (single-writer by topology — no lease/k8s-API — because `DeleteOrphans` is destructive), with a prune-guard against cold/partial keep-lists. CR status is reconciliation state; introspection health is gateway runtime state. |
| 3 | MCP tool scopes are published to the vocabulary by a single owner with a union keep-list across all servers, mirroring the connection `ScopePublisher`; the existing `DeleteOrphans` retires names no server exposes. |
| 4 | Tenancy is carried by the grant and the tenant-dimensioned catalog. Tenant CRs populate that tenant's catalog; `orbit-system` CRs (kora-applied) populate every tenant's catalog. |
| 5 | Scope path is `mcp:<server>:<tool>:use`; renames are new scopes; vanished names are pruned and default-denied. |

### Rationale

The design composes existing kora patterns and keeps each component to one concern. The operator does what it already does for `Connection` — reconcile CRs into mounted config — and nothing more. The tool-gateway does what the egress-gateway already does — read the mounted registry and publish a capability vocabulary via `ScopePublisher`, holding the service identity policy-service requires — plus the one genuinely new thing, an MCP introspection client (which has a hand-rolled JSON-RPC precedent in `orbit-runtime-mcp`). There is no new operator outbound surface and no new operator identity; the gateway's policy-service identity is the same shape the egress-gateway already carries.

---

## Architecture

### The `MCPServer` CRD

Lives in `internal/operator` (the operator owns CRD Go types; feature packages never import them), mirroring `connection_types.go`.

```go
// MCPServer declares one MCP server Orbit governs. Tenant-namespaced for a
// tenant's own server; in orbit-system for a platform-global server.
type MCPServer struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   MCPServerSpec   `json:"spec,omitempty"`
    Status MCPServerStatus `json:"status,omitempty"`
}

type MCPServerSpec struct {
    // DisplayName for the catalog UI.
    DisplayName string `json:"displayName,omitempty"`
    // Endpoint is the in-mesh URL of the MCP server (Streamable HTTP).
    Endpoint string `json:"endpoint"`
    // Transport selects the MCP transport. MVP: "streamable-http".
    Transport string `json:"transport,omitempty"`
    // ReintrospectIntervalSeconds is read by the gateway's introspection worker
    // to re-sync the tool surface on a timer (besides on registry reload). 0 = on-reload only.
    ReintrospectIntervalSeconds int `json:"reintrospectIntervalSeconds,omitempty"`
    // CallerIdentity controls whether the gateway conveys the CALLER's identity to
    // this upstream on a tool call. Default "none": the caller's token is NEVER
    // forwarded (the caller's full-power aud=orbit-platform token must not leak to
    // an upstream) — the server acts as itself with its own backend credential.
    // "forward": the gateway forwards the caller's bearer so the server acts AS the
    // caller against first-party Orbit APIs.
    //
    // ENFORCED GLOBAL-ONLY: "forward" is valid ONLY on Orbit-applied global servers
    // (orbit-system namespace). A tenant-namespaced CR that sets "forward" is
    // REJECTED at reconcile (Status Invalid) — otherwise a tenant admin (who holds
    // CR-apply RBAC in their own namespace) could harvest every caller's full-power
    // token via a pod they control and replay it across all of Orbit. The gateway
    // additionally honors "forward" only for orbit-system servers (belt-and-braces).
    // (Future: "exchange" — RFC 8693 narrowed delegated token; see the gateway/OAuth ADRs.)
    CallerIdentity string `json:"callerIdentity,omitempty"` // none (default) | forward (orbit-system only)
}

// Status is reconciliation state owned by the operator — whether the CR was
// accepted and aggregated into the registry. Introspection health (reachability,
// tool count, last sync) is gateway runtime state, surfaced via metrics + the
// catalog UI, NOT here.
type MCPServerStatus struct {
    Phase            string             `json:"phase,omitempty"` // Pending|Aggregated|Invalid
    LastReconciledAt *metav1.Time       `json:"lastReconciledAt,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"` // Aggregated
}
```

```yaml
# A tenant registers its own server (GitOps, in the tenant namespace):
apiVersion: orbit.io/v1alpha1
kind: MCPServer
metadata:
  name: github           # → scope mcp:github:<tool>:use; stable slug, no tenant in path
  namespace: orbit-tenant-acme
spec:
  displayName: "GitHub"
  endpoint: http://github-mcp.orbit-tenant-acme.svc:8080/mcp
  transport: streamable-http
  reintrospectIntervalSeconds: 900
  # callerIdentity omitted → "none": the caller's token is NOT forwarded to this
  # third-party server; it acts as itself with its own backend credential.
```

kora registers the global kora MCP identically, in `orbit-system` — same kind, different namespace — but with `callerIdentity: forward`, because the kora MCP must act *as the caller* against first-party kora APIs (it holds no standing authority). That flag is the **only** thing that makes the gateway convey the caller's token to an upstream, and it is **enforced global-only**: because CR-apply authority is the tenant's own namespace RBAC, a guidance comment is not enough — a tenant CR setting `callerIdentity: forward` is **rejected at reconcile** (`Status: Invalid`), since otherwise a tenant admin could point it at a pod they control and harvest every caller's full-power `aud=orbit-platform` token to replay across all of kora (same-tenant escalation). `forward` is honoured only on `orbit-system` (kora-applied) servers.

### Two loops: operator aggregation, gateway introspection

Responsibility splits exactly as it does for connections.

**Operator — `MCPServerAggregator`** (mirror of `ConnectionAggregator`, registered in `scheme.go` alongside `OrbitTenant`/`Connection`):

```
Reconcile(MCPServer):
  1. validate spec; phase = Invalid(reason) on bad spec, else continue
  2. re-render the orbit-system registry ConfigMap "orbit-mcp-servers" =
        every active MCPServer across all tenant namespaces + orbit-system
        (name, endpoint, transport, tenant_id, reintrospectIntervalSeconds)
  3. phase = Aggregated, condition Aggregated=True, lastReconciledAt = now
```

Purely Kubernetes-facing: no MCP calls, no policy-service calls, no new identity.

**Tool-gateway publisher** (the `replicas=1` deployment; mounts the registry ConfigMap, `fsnotify` reload + timer):

```
on registry reload / timer:
  for each server in the mounted registry (subject to its reintrospectInterval):
    open MCP session to endpoint → initialize, tools/list
      └─ on error (unreachable / malformed): record server Degraded in
         metrics + catalog; RETAIN its prior tools in the keep-list; continue
    upsert tool metadata into the mcp_tools catalog (server_slug, tool_name,
       description, input_schema, tenant_id) for the read-only UI
  keep = union of every reachable/retained server's tools (all tenants)
  ScopePublisher → RegisterRoutes(service="tool-gateway",
     scopes={mcp:<server>:<tool>:use}…)            // Upsert always
  if registry fully mounted AND every listed server seen-or-retained:
     run global DeleteOrphans(keep)                 // prune-guard: else Upsert-only
```

Publication is global with a union keep-list, so `DeleteOrphans` (which runs for `tenantID == ""`) prunes a scope only when no server in any tenant still exposes it — exactly the connection `ScopePublisher` behaviour. A tenant's `github` and another tenant's `github` share the `mcp:github` names; that is fine, because the gateway resolves the *caller's* tenant's server and authorisation is on tenant-scoped grants. A server that is unreachable at introspection time keeps its prior tools in the keep-list (no silent prune) and is flagged `Degraded` in the gateway's metrics and the catalog — not on the CR.

### Scope registration & drift (the connection pattern, reused)

`policy_scope_vocabulary`, `Upsert`, and the global `DeleteOrphans` all already exist; MCP reuses them via a global union-keep-list publish. Drift handling:

| Server change | Re-introspect result | Vocabulary effect |
|---|---|---|
| New tool appears | enters the union keep-list | `Upsert` adds the scope; **ungranted** (default-deny) |
| Tool removed (no tenant has it) | drops from the union keep-list | `DeleteOrphans` retires the scope; purged from grants/roles |
| Tool still present in another tenant | stays in the union keep-list | scope name retained (shared); no cross-tenant prune |
| Tool renamed | old drops, new enters | old pruned (deny on old), new added ungranted |
| No change | same keep-list | all upserts idempotent; no-op |

The platform never creates a grant. A new tool is reachable only where the existing prefix-coverage rule already allows it — i.e. only under a pre-existing **server-level** grant `mcp:<server>:use` (the admin's standing opt-in to that server's whole surface, future tools included). The catalog flags newly-discovered tools regardless.

### Catalog storage (for the read-only UI)

Vocabulary holds the grantable scope; the **display** metadata (description, input schema) lives in a small catalog table.

> **Amendment (2026-07-01) — the catalog is owned by the tool-gateway, not orbit-agents.** This ADR originally placed `mcp_tools` with "the orbit-agents/registry feature," by analogy to the egress gateway's connection catalog (which was offloaded to orbit-agents *because the egress gateway is a CONNECT broker, not reachable over plain HTTP through the mesh*). That analogy does not hold here: the **tool-gateway is a plain-HTTP Streamable-HTTP service the BFF can reach directly**, and the catalog is an MCP-governance concern — not the agent control plane's. So the table, repository, and the tenant-scoped read API all live **with the tool-gateway** (`cmd/tool-gateway/migrations`, `internal/toolgateway`), and the gateway — already the sole introspector — is also the sole writer. The surrogate `id` is dropped in favour of the natural composite primary key.

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS mcp_tools (
    tenant_id     TEXT        NOT NULL,           -- "" for global servers
    server_slug   TEXT        NOT NULL,           -- MCPServer metadata.name
    tool_name     TEXT        NOT NULL,           -- raw MCP tool name
    scope         TEXT        NOT NULL,           -- mcp:<server>:<tool>:use
    description   TEXT        NOT NULL DEFAULT '',
    input_schema  JSONB       NOT NULL DEFAULT '{}',
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, server_slug, tool_name)  -- natural key; gateway is single writer
);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_tenant_server ON mcp_tools (tenant_id, server_slug);
-- +goose Down
DROP TABLE IF EXISTS mcp_tools;
```

The catalog is **derived state** — vocabulary remains the source of truth for authorisation; the table is the human-facing projection, rebuilt on each introspection.

### Tenant isolation

The scope vocabulary is a shared registry of capability names; tenant isolation is enforced by three mechanisms around it, matching the [tenant-isolation ADR](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md):

- **Tenant-scoped grants** are the authorisation boundary: a grant belongs to a tenant and can only be created within it, so a principal is authorised only on its own tenant's (and global) scope names — and the gateway resolves the *caller's* tenant's server, so a shared name like `mcp:github:create_issue:use` always means *that tenant's* github.
- **Tenant-dimensioned catalog:** `mcp_tools` carries `tenant_id`; every catalog read filters `WHERE tenant_id = $1 OR tenant_id = ''` (own + global), never another tenant's — so a tenant's admin UI can only see and grant its own (and global) servers' tools.
- **CR-apply authority** is the registration enforcement point: who can `kubectl apply` an `MCPServer` into `orbit-tenant-{id}` is governed by the tenant's RBAC, tied to the identity model — there is no create API at all, so nothing can register a server cross-tenant. Global (`orbit-system`) CRs are applicable only by kora, never a tenant.

### MCP client (introspection)

MVP speaks **Streamable HTTP** MCP and uses only `initialize` + `tools/list`. We adopt a maintained Go MCP client library if one fits the pinned spec version; otherwise we hand-roll the two calls (the `orbit-runtime-mcp` JSON-RPC server is the in-repo precedent). The client is small and lives in the tool-gateway's introspection worker — the same binary that already speaks MCP as a proxy.

---

## Consequences

### Positive

- Registration is pure GitOps; the security-critical resource has no mutation API and no UI create path.
- Each component keeps one concern: the operator only aggregates CRs (no MCP or policy-service calls, no new identity); the tool-gateway owns all MCP-protocol work and scope publishing, exactly as the egress-gateway owns connection scope publishing.
- Drift handling, tenant/global split, and scope validation are **reused**, not rebuilt — the only genuinely new component is the MCP introspection client.
- An unreachable or malformed server retains its prior tools and is flagged `Degraded` in gateway metrics + the catalog, without silently dropping scopes.
- The tool surface becomes authoritative and governable for *any* server, including unmodified open-source ones.

### Negative

- **The publisher is a separate `replicas=1` deployment.** Because `DeleteOrphans` revokes scopes from grants/roles, publishing must be single-writer over one complete keep-list (see Decision 2). This is one more deployment of the same binary (publisher mode) alongside the scaled proxy — the cost of avoiding both a destructive multi-writer race and a leader-election `Lease`.
- **A union republish on every change** (the same trade the connection `ScopePublisher` makes): the publisher recomputes and re-publishes the full `mcp` union per registry change, gaining prune-safety and reuse of the existing global prune.
- **Prune is gated on a complete keep-list.** Until the registry is mounted and every listed server has been seen-or-retained, the publisher is Upsert-only; a server that has *never* been reachable since boot defers its prune rather than wiping its scopes.
- **Introspection health is not in the Kubernetes API.** A `kubectl get mcpserver` shows reconciliation state, not reachability; an operator wanting "is this server healthy?" looks at the catalog UI / gateway metrics. This is the deliberate cost of keeping introspection out of the operator (and matches how connection health is surfaced).
- Introspection is point-in-time; a tool added between syncs is invisible until the next reload/timer (bounded by `reintrospectIntervalSeconds`).
- Derived catalog state must be kept consistent with the vocabulary (rebuilt each introspection; the vocabulary stays authoritative).

### Neutral

- A server-level grant remains a standing opt-in to future tools — a property of the prefix-coverage model, surfaced here, addressed (re-affirmation on drift) as possible future work.
- A renamed tool becomes a new scope; a client using the old name gets a clean default-deny rather than a silent re-expose.

---

## Implementation

- **Phase 1 — CRD + operator aggregator.** `MCPServer` types, scheme registration, and an `MCPServerAggregator` that renders the `orbit-mcp-servers` registry ConfigMap. RBAC: operator gains read/watch on `MCPServer` (it already manages ConfigMaps). No operator outbound calls, no new operator identity.
- **Phase 2 — Publisher deployment + introspection.** The `tool-gateway` binary in publisher mode as a `replicas=1` deployment: mount the registry, MCP client (`initialize` + `tools/list`), Degraded/retain-on-failure handling, `reintrospectIntervalSeconds` timer.
- **Phase 3 — Scope publishing + prune-guard.** `ScopePublisher` → global union-keep-list publish (`registeredBy = "tool-gateway"`, `tenantID = ""`), reusing the gateway's policy-service identity as the egress-gateway does; implement the prune-guard (Upsert-only until the keep-list is complete); verify drift end-to-end (add, remove, rename, shared-name retention) **and** the cold-start/partial-mount case does not revoke grants.
- **Phase 4 — Catalog table + read-only API.** `mcp_tools` migration, repository, and a tenant-scoped list endpoint for the UI (consumed by the gateway/dashboard ADRs).

Exit: applying an `MCPServer` CR registers its tools as scopes (operator aggregates → gateway introspects + publishes on reload); removing a tool prunes its scope; another tenant cannot see either; an unreachable server is flagged `Degraded` in the catalog/metrics with prior scopes retained.

---

## Validation

- **Success metrics:** apply-to-grantable within one introspection cycle (operator aggregate + gateway publish); zero manual scope bookkeeping across a drift cycle; no cross-tenant scope or catalog leakage in tests.
- **Monitoring (gateway-emitted):** `mcp_introspect_total{result}`, `mcp_scopes_registered`, `mcp_scopes_pruned`, introspection latency, count of `Degraded` servers, time since last successful publish.

---

## Related Decisions

- [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) — the PEP and consumption plane built on this registration.
- [MCP OAuth Authentication](2026-06-29-mcp-oauth-authentication.md) — human auth for the gateway.
- [MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md) — amends the credential-injection non-goal for servers that declare `spec.auth`.
- [MCP Server Registration, Deployment, and Governance](2026-03-05-mcp-server-registration-deployment-and-governance.md) — the prior paper design this supersedes/implements.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the `Connection` CRD + aggregator pattern mirrored here (Amendment 3).
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) — the scope vocabulary, prefix coverage, and grant model reused.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) — the `RegisterRoutes` / vocabulary upsert / orphan-prune path reused for scope registration.
- [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — the isolation model.

## References

- MCP specification — `tools/list`, `initialize`, Streamable HTTP transport (spec version pinned in implementation).
- `internal/operator/connection_types.go`, `connection_aggregator.go`, `tenant_controller.go`, `scheme.go` — the CRD + aggregator precedent the `MCPServer` CRD and `MCPServerAggregator` mirror.
- The egress-gateway `ScopePublisher` (publishes `connection` on registry reload, holding the gateway's policy-service identity) — the precedent the tool-gateway's MCP publisher mirrors.
- `internal/policyservice/vocabulary/repository.go`, `internal/policyservice/api/service.go` (`RegisterRoutes`) — scope registration.
