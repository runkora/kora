# MCP Server Registration & Introspection

<!-- Filename: 2026-06-29-mcp-server-registration-and-introspection.md -->

**Status:** Proposed
**Date:** 2026-06-29
**Deciders:** Justin Wilkin, Orbit Core Team
**Technical Story:** [MCP / Tool Gateway PRD](../prd/mcp-tool-gateway.md)

---

## Context

### Background

Orbit governs *which model* an agent may call (inference gateway) and *which external host* it may reach (egress/credential gateway). It does **not** govern *which tool* a principal may call on an MCP server. The PRD's central move is to lift per-tool enforcement out of the MCP server and into the platform: introspect each server's tool surface, register every tool as a platform scope (`mcp:<server>:<tool>:use`), and enforce at a gateway. That enforcement (the gateway) is the subject of a separate ADR; **this ADR is the foundation it stands on** — how an MCP server gets *into* the platform and how its tools become governable scopes.

Today there is no MCP registration of any kind. `internal/toolgateway` does not exist; the only MCP code is `orbit-runtime-mcp` (a single hardcoded `provision_sandbox` tool) and a hardcoded `runtime` server in the agent's `.mcp-config.json`. The [2026-03-05 registration ADR](2026-03-05-mcp-server-registration-deployment-and-governance.md) is a paper design that was never built and treated the tool manifest as non-security discovery metadata. This ADR supersedes and implements its registration concern, with one decisive change: **the introspected tool list is load-bearing for authorisation.**

### Problem Statement

We need a way to: (1) declare an MCP server as infrastructure, tenant-scoped or platform-global; (2) discover its tools by calling `tools/list`; (3) register each tool as a `mcp:<server>:<tool>:use` scope in the policy vocabulary so it can be granted; (4) keep that registration idempotent as servers drift; and (5) keep one tenant's servers and tool scopes invisible to every other tenant — all without a "create server" button, because an ungoverned UI mutation path for a security-critical resource is the wrong posture.

### Goals

- Declare MCP servers in infra (GitOps), reconciled by the operator — no CRUD API, no UI create.
- Tenant-scoped registration (a tenant's own servers) and platform-global registration (the Orbit MCP and platform servers Orbit registers itself).
- Introspect `tools/list` and register `mcp:<server>:<tool>:use` scopes into the existing policy vocabulary, reusing the grant/scope machinery wholesale.
- Idempotent re-sync on drift: appeared tools added, vanished tools retired; the platform never auto-creates a grant.
- Tenant isolation across the CR, introspection, the catalog, and the scopes.
- A read-only catalog (servers → tools → grant status) for admins.

### Non-Goals

- The gateway, the PEP, and the consumption paths — see the [MCP / Tool Gateway ADR](2026-06-29-mcp-tool-gateway.md).
- Human/agent authentication — see the [MCP OAuth Authentication ADR](2026-06-29-mcp-oauth-authentication.md).
- Remote third-party SaaS MCP servers (fast-follow); MVP registers Orbit-hosted servers.
- Credential injection on the platform→server hop: an Orbit-hosted MCP is a managed microservice that owns its own backend credentials (it is not an agent sandbox); the platform does not force-route or inject on this hop.
- MCP `resources/*` and `prompts/*` surfaces — MVP introspects **tools** only.

---

## Decision Drivers

- **Reuse over reinvention.** Orbit already has a scope vocabulary with idempotent upsert + orphan pruning, an operator that aggregates tenant-namespaced CRs into mounted config, and a `ScopePublisher` that registers a service's scopes. Registration should be these patterns, composed — not new machinery.
- **GitOps posture.** Every other per-tenant concern (`OrbitTenant`, `Connection`, ClusterSPIFFEIDs, per-tenant CA) is a reconciled CR. An MCP server is the same shape of object and is declared the same way.
- **Security by construction.** No UI mutation path; default-deny; tenant isolation enforced structurally (namespace + `tenant_id`), not by convention.
- **Drift is the norm.** MCP servers add and remove tools over their lifetime. The design must converge idempotently without manual scope bookkeeping.

---

## Considered Options

### Decision 1 — How a server is declared

- **Option A: `MCPServer` CRD, operator-reconciled (chosen).** Tenant-namespaced for a tenant's own servers; `orbit-system` for platform-global. Mirrors `Connection` ([jit-credential ADR, Amendment 3](2026-06-06-jit-credential-brokering-egress-gateway.md)). GitOps-native, no mutation API, isolation via namespace.
- **Option B: CRUD API + Postgres registry.** A `POST /mcp/servers` admin API. Rejected for exactly the reasons the credential broker rejected it: no human-usable front, a security-critical resource behind a raw API, and config split from its lifecycle. The credential broker already walked this back to CRs.
- **Option C: Static config (`mcpservers.yaml`).** Too static — no per-tenant story, no reconcile loop, no status.

### Decision 2 — Where introspection runs

Introspection means opening an MCP session to a (running) server and calling `tools/list`. The result must flow to the CR's readiness status, and the discovered scopes must be registered into policy-service.

- **Option A: The operator reconciler introspects (chosen).** The operator already has the Kubernetes API (to read CRs and write status), already reconciles tenant resources, and already calls platform services. It opens an MCP client to the server's in-mesh endpoint, calls `tools/list`, registers scopes, and writes `status` (Ready / Degraded with reason). Introspection result and CR readiness stay coherent in one place.
- **Option B: The gateway introspects on registry reload.** Mirrors how the egress-gateway republishes connection scopes on mount reload. Clean, but the gateway is a mount-only data-plane pod with no Kubernetes API — it cannot write CR status, so the failure-mode requirement (un-ready CR on unreachable server) cannot be met there. Rejected for MVP; revisit if introspection cost forces it out of the operator.
- **Option C: A dedicated `mcp-registry` service.** A third deployment that watches CRs and introspects. More moving parts than warranted; the operator already is the CR reconciler.

### Decision 3 — How tools become scopes

Reuse, verbatim, the path dynamic route registration already uses: the introspector calls policy-service `RegisterRoutes(ctx, service, tenantID, routes, scopeDecls)`, which `Upsert`s each `ScopeDeclaration` into `policy_scope_vocabulary` and, for the owning service, `DeleteOrphans(registeredBy, keepScopes)` prunes vanished scopes and purges them from all grants/roles. **This gives idempotent drift handling for free** — re-introspection with the current tool set is the keep-list. No new registration surface.

### Decision 4 — Tenant vs global

A server CR in a tenant namespace registers **tenant-scoped** scopes (`tenant_id` = the tenant), visible and grantable only within that tenant. A server CR in `orbit-system`, applied by Orbit, registers **global** scopes (`tenant_id = ""`), grantable in any tenant. This is exactly the `tenant_id == ""` distinction the policy vocabulary already encodes for global vs tenant route registration. Global is not an admin toggle — it is simply *who applied the CR and where*.

### Decision 5 — Naming & stability in the scope path

`mcp:<server>:<tool>:use` — resource_type `mcp`, path `[<server>, <tool>]`, action `use`. `<server>` is the CR's stable `metadata.name` (the slug), never a renamable display field. `<tool>` is the MCP tool name **normalised** to a scope-path-safe segment (lowercase, `[a-z0-9_-]`, documented mapping). A renamed tool is, correctly, a new scope: the old scope is orphaned and pruned (silent deny on the old name), the new scope registers ungranted (default-deny). This is the safe failure direction and is surfaced in the catalog as drift.

---

## Decision

| # | Decision |
|---|----------|
| 1 | MCP servers are declared as an `MCPServer` CRD, operator-reconciled — tenant-namespaced or `orbit-system` (global). No CRUD API, no UI create. |
| 2 | The **operator reconciler** introspects (`tools/list`), registers scopes, and writes CR status. |
| 3 | Scopes register via the existing policy-service vocabulary path (`RegisterRoutes` → `Upsert` + `DeleteOrphans`); drift is handled by orphan pruning. |
| 4 | Tenant CRs → tenant-scoped scopes; `orbit-system` CRs (Orbit-applied) → global scopes. |
| 5 | Scope path is `mcp:<server-slug>:<normalised-tool>:use`; renames are new scopes; old ones pruned and default-denied. |

### Rationale

Every decision lands on an existing Orbit pattern: the CR + operator-aggregation model from `Connection`, the vocabulary upsert/orphan-prune from dynamic route registration, the `tenant_id == ""` global/tenant split from the policy service, and the namespace/`tenant_id` isolation from every multi-tenant table. The only genuinely new component is an **MCP client** for introspection — and even that has a hand-rolled JSON-RPC precedent in `orbit-runtime-mcp`. The result is a foundation that is small in new surface and maximally consistent with how Orbit already governs resources.

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
    // ReintrospectIntervalSeconds re-syncs the tool surface on a timer
    // (in addition to reconcile-on-change). 0 = reconcile-only.
    ReintrospectIntervalSeconds int `json:"reintrospectIntervalSeconds,omitempty"`
}

type MCPServerStatus struct {
    Phase            string             `json:"phase,omitempty"` // Pending|Introspecting|Ready|Degraded
    ToolCount        int                `json:"toolCount,omitempty"`
    LastIntrospected *metav1.Time       `json:"lastIntrospected,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"` // Introspected, ScopesRegistered
}
```

```yaml
# A tenant registers its own server (GitOps, in the tenant namespace):
apiVersion: orbit.io/v1alpha1
kind: MCPServer
metadata:
  name: github           # → scope segment <server>; stable, never renamed in place
  namespace: orbit-tenant-acme
spec:
  displayName: "GitHub"
  endpoint: http://github-mcp.orbit-tenant-acme.svc:8080/mcp
  transport: streamable-http
  reintrospectIntervalSeconds: 900
```

Orbit registers the global Orbit MCP identically, in `orbit-system` — same kind, different namespace.

### Reconcile lifecycle

A new `MCPServerReconciler` in the operator (registered in `scheme.go` alongside `OrbitTenant`/`Connection`):

```
Reconcile(MCPServer):
  1. phase = Introspecting
  2. open MCP session to spec.endpoint → call initialize, then tools/list
       └─ on error (unreachable / malformed): phase = Degraded,
          condition Introspected=False(reason), RETAIN prior scopes, requeue
  3. for each tool: build ScopeDeclaration{
        Scope: "mcp:<name>:<normalise(tool)>:use",
        ResourceType: "mcp", PathSegments: [<name>, <normalise(tool)>], Action: "use",
        Description: tool.description }
  4. policyClient.RegisterRoutes(ctx, service="mcp/<name>", tenantID=<ns→tenant or "">,
        routes=[], scopes=decls)
       └─ Upsert each scope; DeleteOrphans(service, keep=decls) prunes vanished tools
  5. upsert tool metadata into the mcp_tools catalog table (name, description, input
        schema) for the read-only UI
  6. phase = Ready, toolCount=N, conditions Introspected=True, ScopesRegistered=True
```

`service = "mcp/<server-slug>"` makes each server its own scope owner, so `DeleteOrphans` prunes only *that* server's tools — never another server's.

### Scope registration & drift (reused, not rebuilt)

`policy_scope_vocabulary` and `RegisterRoutes`/`DeleteOrphans` already exist. Drift handling is a direct consequence:

| Server change | Re-introspect result | Vocabulary effect |
|---|---|---|
| New tool appears | in `keep` list | `Upsert` adds the scope; **ungranted** (default-deny) |
| Tool removed | not in `keep` list | `DeleteOrphans` prunes scope; purged from all grants/roles |
| Tool renamed | old absent, new present | old pruned (deny on old), new added ungranted |
| No change | same `keep` list | all upserts idempotent; no-op |

The platform never creates a grant. A new tool is reachable only where the existing prefix-coverage rule already allows it — i.e. only under a pre-existing **server-level** grant `mcp:<server>:use` (the admin's standing opt-in to that server's whole surface, future tools included). The catalog flags newly-discovered tools regardless.

### Catalog storage (for the read-only UI)

Vocabulary holds the grantable scope; the **display** metadata (description, input schema) lives in a small catalog table owned by the orbit-agents/registry feature:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS mcp_tools (
    id            TEXT        PRIMARY KEY,
    tenant_id     TEXT        NOT NULL,           -- "" for global servers
    server_slug   TEXT        NOT NULL,           -- MCPServer metadata.name
    tool_name     TEXT        NOT NULL,           -- raw MCP tool name
    scope         TEXT        NOT NULL,           -- mcp:<server>:<tool>:use
    description   TEXT        NOT NULL DEFAULT '',
    input_schema  JSONB       NOT NULL DEFAULT '{}',
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, server_slug, tool_name)
);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_tenant_server ON mcp_tools (tenant_id, server_slug);
-- +goose Down
DROP TABLE IF EXISTS mcp_tools;
```

The catalog is **derived state** — vocabulary remains the source of truth for authorisation; the table is the human-facing projection, rebuilt on each introspection.

### Tenant isolation

Enforced structurally, matching every multi-tenant table and the [tenant-isolation ADR](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md):

- A tenant CR lives in `orbit-tenant-{id}`; the reconciler derives `tenant_id` from the namespace and stamps every scope and catalog row with it.
- CR-apply authority *is* the enforcement point: who can `kubectl apply` an `MCPServer` into a tenant namespace is governed by the tenant's RBAC, tied to the identity model — there is no Orbit API that can create a server cross-tenant because there is no create API at all.
- Global (`orbit-system`) CRs are applicable only by Orbit (cluster-admin / platform GitOps), never by a tenant.
- Every catalog/scope read filters `WHERE tenant_id = $1 OR tenant_id = ''` (own + global), never another tenant's.

### MCP client (introspection)

MVP speaks **Streamable HTTP** MCP and uses only `initialize` + `tools/list`. We adopt a maintained Go MCP client library if one fits the pinned spec version; otherwise we hand-roll the two calls (the `orbit-runtime-mcp` JSON-RPC server is the in-repo precedent). The client is small and lives in the operator's MCP-introspection package.

---

## Consequences

### Positive

- Registration is pure GitOps; the security-critical resource has no mutation API and no UI create path.
- Drift handling, tenant/global split, and scope validation are **reused**, not rebuilt — minimal new surface.
- Introspection status lives on the CR, so an unreachable or malformed server is visibly `Degraded` without silently dropping scopes.
- The tool surface becomes authoritative and governable for *any* server, including unmodified open-source ones.

### Negative

- The operator now makes outbound MCP protocol calls and owns an MCP client — a new responsibility for a component that was purely Kubernetes-facing.
- Introspection is point-in-time; a tool added between syncs is invisible until the next reconcile/timer (bounded by `reintrospectIntervalSeconds`).
- Derived catalog state must be kept consistent with the vocabulary (rebuilt each introspection; the vocabulary stays authoritative).

### Neutral

- A server-level grant remains a standing opt-in to future tools — a property of the prefix-coverage model, surfaced here, addressed (re-affirmation on drift) as possible future work.
- Renames are new scopes by design; clients relying on a renamed tool see a clean default-deny, not a silent re-expose.

---

## Implementation

- **Phase 1 — CRD + reconcile skeleton.** `MCPServer` types, scheme registration, a reconciler that reaches Ready without introspection (status only). RBAC: operator gains read/watch on `MCPServer`.
- **Phase 2 — Introspection.** MCP client (`initialize` + `tools/list`), Degraded/retain-on-failure handling, `reintrospectIntervalSeconds` timer.
- **Phase 3 — Scope registration.** Wire introspection to `RegisterRoutes`/`Upsert`/`DeleteOrphans`; verify idempotent drift end-to-end; tenant vs global.
- **Phase 4 — Catalog table + read-only API.** `mcp_tools` migration, repository, and a tenant-scoped list endpoint for the UI (consumed by the gateway/dashboard ADRs).

Exit: applying an `MCPServer` CR registers its tools as scopes; removing a tool prunes its scope; another tenant cannot see either; an unreachable server is `Degraded` with prior scopes retained.

---

## Validation

- **Success metrics:** apply-to-grantable in one reconcile; zero manual scope bookkeeping across a drift cycle; no cross-tenant scope or catalog leakage in tests.
- **Monitoring:** `mcp_introspect_total{result}`, `mcp_scopes_registered`, `mcp_scopes_pruned`, reconcile latency, count of `Degraded` servers.

---

## Related Decisions

- [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) — the PEP and consumption plane built on this registration.
- [MCP OAuth Authentication](2026-06-29-mcp-oauth-authentication.md) — human auth for the gateway.
- [MCP Server Registration, Deployment, and Governance](2026-03-05-mcp-server-registration-deployment-and-governance.md) — the prior paper design this supersedes/implements.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the `Connection` CRD + aggregator pattern mirrored here (Amendment 3).
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) — the scope vocabulary, prefix coverage, and grant model reused.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) — the `RegisterRoutes` / vocabulary upsert / orphan-prune path reused for scope registration.
- [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — the isolation model.

## References

- MCP specification — `tools/list`, `initialize`, Streamable HTTP transport (spec version pinned in implementation).
- `internal/operator/connection_types.go`, `connection_aggregator.go`, `tenant_controller.go`, `scheme.go` — CRD/reconciler/aggregator precedents.
- `internal/policyservice/vocabulary/repository.go`, `internal/policyservice/api/service.go` (`RegisterRoutes`) — scope registration.
