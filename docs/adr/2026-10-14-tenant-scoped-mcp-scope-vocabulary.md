# Tenant-Scoped MCP Scope Vocabulary

<!-- Filename: YYYY-MM-DD-kebab-title.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-10-14
**Deciders:** Abigail Adjei (author); reviewer to be confirmed
**Technical Story:** Tool-gateway publishes every tenant's MCP scopes as one global vocabulary, so tenants that register a server with the same name collide (GitHub issue link to be added)

---

## Context

kora is multi-tenant: a tenant is a logically separate install that shares infrastructure with others (a region, or a business unit). Each tenant can register its own MCP servers with an `MCPServer` resource in its namespace. The tool-gateway introspects each server's tools and publishes one scope per tool, for example `mcp:slack:messages:post:use`, into the policy-service scope vocabulary. Admins pick from that vocabulary when they create grants and roles.

### Background

[MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) made the vocabulary a deliberate global name registry:

- **Decision 3:** `policy_scope_vocabulary` is "the global registry of those names, and tenancy is carried by the grant". Two tenants that both run `slack` are meant to share the `mcp:slack:...` names, and the prune keep-list is the union across all tenants.
- **Decision 4:** the tenant/global distinction is "not in the vocabulary". It is carried by tenant-scoped grants and by the tenant-dimensioned `mcp_tools` catalog.

The same ADR states the goal of keeping "one tenant's servers and tool scopes invisible to every other tenant".

What the code does today (verified by reading):

- `BuildScopes` (`backend/internal/toolgateway/scopes.go`) builds every scope string from the server **name** alone and de-duplicates on that string. `Server.TenantID` is read only for collision logging. A unit test (`TestBuildScopes_SameServerNameAcrossTenantsCollapses`) shows Acme's and Globex's `slack` collapsing into one set of declarations, with the first snapshot's description winning.
- The publisher registers once, as a SYSTEM principal. `handleRegisterRoutes` (`policyservice/api/handlers_routes.go`) treats SYSTEM as global (`tenantID = ""`), and the request has no field for naming a tenant.
- `upsertDeclaredScopes` (`policyservice/api/service.go`) writes vocabulary rows without any tenant, even for a tenant-scoped registration. The tenant is recorded only on the route registration.
- `policy_scope_vocabulary` has `PRIMARY KEY (scope)`. `mcp_tools` already keys on `(tenant_id, server_slug, tool_name)`.
- Vocabulary readers take no tenant: `Exists` and `GetByScope` (grant and role validation), `List` (also used by bundle compilation in `bundle/server.go`), and `ListWithUsage`, which uses `tenantID` only to count grants and roles and does not filter the rows it returns.

### Problem Statement

The vocabulary cannot say whose a scope is. Consequences:

- **Collisions:** two tenants registering `slack` produce one row. The last writer sets its description and path.
- **Leakage:** every tenant sees every other tenant's tool names in the dashboard picker, and the compiled bundle loads the whole vocabulary.
- **Wrong validation:** `Exists` asks "does this scope exist anywhere?", so Globex can create a grant for a tool only Acme has.
- **Dangling grants:** because the keep-list is a union, Acme's scope survives Acme deleting its server whenever Globex still exposes the same tool.
- **No tenant-level derivation:** "all of Acme's MCP scopes" cannot be computed, which blocks deriving a tenant-admin role (#167).

### Goals

- Two tenants that register a server with the same name produce two independent vocabulary entries.
- A tenant's grants, roles, dashboard views and bundles see that tenant's entries plus global ones, and nothing from other tenants.
- Servers registered in `kora-system` stay global.
- Existing data keeps working.

### Non-Goals

- Designing or changing orphan pruning. It is expected to keep working and will be verified after implementation (see Consequences and Notes).
- Changing the scope string format. The tenant is not part of the scope path.
- Changing `connection:` scope publishing by the egress-gateway.

---

## Decision Drivers

- Scope strings are what grants and the Rego hierarchy match on, so their format must not change.
- The registration endpoint already supports tenant-scoped registrations, and the tool-gateway already knows each server's tenant. They are only not connected.
- Consistency with `mcp_tools`, which already carries `tenant_id` with `''` meaning global.
- Existing grants must continue to resolve after migration.

---

## Considered Options

### Option 1: Add a tenant dimension to the vocabulary

**Description:** Key `policy_scope_vocabulary` by `(tenant_id, scope)` with `''` for global. The tool-gateway publishes each tenant's servers under that tenant, and readers use the tenant's rows plus global rows.

**Pros:**
- Same-named servers in different tenants no longer collide.
- Matches how `mcp_tools` is already modelled.
- Scope strings, grants and Rego are untouched.

**Cons:**
- Reverses part of Decisions 3 and 4 of the MCP registration ADR.
- Touches the repository, the registration endpoint, validation, listing and bundle compilation.

### Option 2: Put the tenant in the scope string

**Description:** Publish `mcp:acme:slack:messages:post:use`.

**Pros:**
- No schema change.

**Cons:**
- Every grant, role and Rego rule that matches scope paths would have to understand a tenant segment, and the same logical tool would have a different scope in every tenant.
- Contradicts the existing "no tenant in path" convention.

### Option 3: Keep the global vocabulary

**Description:** Keep Decisions 3 and 4 and rely on grants and the `mcp_tools` catalog for tenant separation.

**Pros:**
- No change.

**Cons:**
- Leaves every problem listed above in place. The vocabulary is where validation and the picker read, and it is the part without a tenant.

---

## Decision

**Chosen Option:** Option 1 - Add a tenant dimension to the vocabulary.

### Rationale

The tenant is a property of the vocabulary entry, not of the scope name. Adding a column fixes the collapse where it happens and leaves everything that matches on scope strings unchanged. The `mcp_tools` table already models tenancy this way.

### Design

1. **Schema.** Add `tenant_id TEXT NOT NULL DEFAULT ''` to `policy_scope_vocabulary` and change the primary key to `(tenant_id, scope)`. Existing rows become global (`''`). The migration is generated with `make migrate-create` and passes `make migrate-lint`.
2. **Registration endpoint.** A SYSTEM caller may name the tenant explicitly in the request, following the rule `resolveTenantID` already applies to other endpoints. Any other caller is limited to the tenant in its token, and a mismatch is rejected with 403. An omitted tenant from SYSTEM means global, so existing callers such as the egress-gateway are unchanged.
3. **Vocabulary writes.** `upsertDeclaredScopes` writes each row with the registration's tenant, and the upsert conflicts on `(tenant_id, scope)`.
4. **Publisher.** The tool-gateway groups snapshots by `Server.TenantID` and registers once per tenant. Global servers (`kora-system`) and the `mcp:invoke` coarse scope stay in the global registration.
5. **Readers return the tenant's rows plus global rows.** This applies to grant and role validation (`Exists`, `GetByScope`), the dashboard list (`List`, `ListWithUsage`), and bundle compilation for a tenant. A scope present for both the tenant and global resolves to the **global** entry, so a tenant cannot redefine a platform server by reusing its name. This matches the tool-gateway, which routes a request to the global server when a tenant's server has the same name (#692).
6. **Orphan deletion is limited to the registration's tenant.** `DeleteOrphans` currently deletes every row owned by a service that is missing from the keep-list. With a tenant column, a global registration would delete tenant rows, so the query must only consider rows with the same `tenant_id` as the registration. This is a safety requirement of the schema change, not a redesign of pruning.
7. **Name collisions with a global server are rejected at admission.** The MCPServer aggregator in the operator does not register a tenant `MCPServer` whose name matches a global server, and the server's status carries a condition naming the global server it collides with. Same names across two tenants stay allowed. An existing colliding server is not deleted; it gets the condition and is already unreachable under global-wins. With this rule the routing and the vocabulary agree for every same-name case: global plus tenant (global), two tenants (each its own), and tenant only (the tenant's).

---

## Consequences

### Positive

- Tenants cannot see or grant each other's MCP tools through the vocabulary.
- Same-named servers in different tenants are independent.
- A tenant's own MCP scopes can be listed, which unblocks deriving a tenant-admin role (#167).

### Negative

- Reverses part of a deliberate earlier decision, so the old ADR's Decisions 3 and 4 no longer describe the system.
- Every vocabulary reader needs a tenant argument, so several call sites change.
- Existing global rows that were really one tenant's stay global until that tenant's server is next published.

### Neutral

- Scope strings, grants and Rego policy are unchanged.
- The egress-gateway's `connection:` publishing is unchanged.

---

## Implementation

### Migration Path

Add the column with default `''`, then swap the primary key in one migration. Existing rows keep working as global. After deploy, the tool-gateway republishes each tenant's scopes under that tenant on its next sync.

---

## Validation

### Success Metrics

- A unit test shows two tenants registering `slack` produce separate declarations per tenant.
- A Postgres-backed repository test shows two rows for the same scope with different tenants, and that `Exists` and `List` return the tenant's rows plus global rows only.
- A Postgres-backed test shows a global prune does not delete tenant rows.
- On the local cluster, two tenants with a same-named server each see only their own tools in the dashboard.

### Monitoring

No new runtime metrics. The existing registration and bundle-recompile logs show the per-tenant registrations.

---

## Related Decisions

- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) - Decisions 3 and 4 are reversed in part by this ADR
- [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) - the publisher and proxy modes
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) - the registration endpoint extended here

---

## Notes

**Open items to confirm:**

- **Pruning of tenant scopes.** Pruning runs only for global registrations (`RegisterRoutesWithPrune` in `service.go`: `if tenantID == ""`), and its comment says tenant registrations never prune. After this change, scopes registered under a tenant may therefore not be pruned when the `MCPServer` is deleted. This is out of scope here and is to be verified after implementation and reported as a follow-up if it is a regression. `PurgeScopeFromAll` strips a scope from every tenant's grants and roles, which also needs tenant-awareness when pruning is revisited.
- **Frontend.** Whether the dashboard needs changes beyond the API returning tenant-filtered rows has not been checked.
- **Reviewer questions:** whether bundle compilation should load only the tenant's rows plus global rows (assumed above), and whether existing global MCP rows should be re-attributed or left global.

**Last Updated:** 2026-10-14
