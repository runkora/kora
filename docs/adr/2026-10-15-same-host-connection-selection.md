# Same-Host Connection Selection in the Egress Gateway

<!-- Filename: 2026-10-15-same-host-connection-selection.md -->

**Status:** Proposed  
**Date:** 2026-10-15  
**Deciders:** Justin Wilkin  
**Technical Story:** Two GitHub App connections for different GitHub organizations in one tenant.

---

## Context

### Background

The egress gateway resolves the connection for a request by tenant and destination host. `Broker.resolveConnection` (`backend/internal/egressgw/service.go`) is the only selection point, and it calls `ConfigMapRegistry.GetByHost` (`backend/internal/credentials/configmap.go`). `GetByHost` returns the first connection in load order whose base host or adapter host family matches.

The operator's `ConnectionAggregator.collect` (`backend/internal/operator/connection_aggregator.go`) renders every active tenant's connections into one ConfigMap, `orbit-tenant-connections`, sorted by tenant and then by connection name. `collect` already skips a single connection with a warning when its base host is invalid or its Secret is missing.

Every github.com `github_app` connection claims the same host family: `github.com`, `api.github.com`, `codeload.github.com` and `*.githubusercontent.com` (`githubEgressHosts` in `backend/internal/platform/connections/adapters.go`). A GitHub Enterprise connection claims only its own base host.

A `github_app` connection declares the account its installation is bound to in the `owner` config field (`backend/internal/credentials/adapters/githubapp/githubapp.go`). Classification denies a git or REST path whose owner segment differs from the configured owner. Content-host paths with a foreign owner classify as unqualified `contents:read`.

An agent's connectors compile to scopes of the form `connection:<name>:<category>[:<repo>]:<verb>` (`AgentType.ConnectorScopes` in `backend/internal/agents/models.go`). `ScopeResolver.EffectiveScopes` (`backend/internal/agents/effective_scopes.go`) adds the connectors of every attached environment. The resulting grant reaches the egress gateway in its compiled policy bundle.

### Problem Statement

A test against the real registry loaded two connections into one tenant: `clear-route-github` (owner `clear-route`) and `runkora-github` (owner `runkora`). Requests to `github.com` and `api.github.com` both resolved to `clear-route-github`, and `runkora-github` was never selected. Nothing at aggregation reported the overlap between the two connections. Kubernetes connections declare distinct hosts and are unaffected.

### Goals

*   The gateway selects among connections in one tenant that serve the same host.
*   A request that addresses an owner uses the connection bound to that owner.
*   A request that addresses no owner uses the one candidate connection the caller holds, and is denied with a clear reason when the caller holds several.
*   A tenant with one connection per host behaves exactly as it does today.
*   Aggregation rejects same-host connections that cannot be told apart.

### Non-Goals

*   The GitHub MCP server deployment.
*   Changes to `constellation-iac` and to production agents and triggers.
*   GitHub Enterprise behavior beyond keeping its single-connection path unchanged.

---

## Which of the three questions this touches

This decision touches **How does it connect?** It changes which connection and upstream credential serve a request.

**Who is this agent?** and **What can it access?** are preserved. Policy is evaluated against the selected connection's scopes exactly as it is today. The caller's scope set is read only to choose a candidate and never authorizes a call. No credential is minted for a request that resolves to no connection, and no credential reaches the agent.

---

## Decision Drivers

*   The gateway must not pick a credential silently when more than one candidate could serve the caller.
*   The signal for "the caller holds connection X" must already be present in the gateway and must cover environment connectors.
*   A misconfigured connection added to one tenant must not stop connection updates for other tenants.

---

## Considered Options

### D1. Selecting by owner

**Option 1a (chosen):** The adapter reports the owner a request path addresses. The broker selects the candidate whose configured `owner` equals it, compared case-insensitively.

**Option 1b (rejected):** Classify the request against every candidate and keep the candidate whose classification does not deny it. A successful classification does not identify the owner of a content-host path.

### D2. The signal for "the caller holds connection X"

**Option 2a (chosen):** Read the caller's scope set from the gateway's in-process policy evaluator. `Evaluator.Scopes` (`backend/internal/platform/policy/evaluator.go:26`, implemented at `engine.go:395`) evaluates `principal_all_scopes` (`backend/internal/policyservice/bundle/authz.rego:823`). That rule returns every scope on the caller's grants and roles. The caller holds connection X when one of those scopes starts with `connection:X:`.

**Option 2b (rejected):** Run a policy check of the classified action against each candidate and keep the candidates that allow it. Selection would depend on the action. A caller with read on one connection and write on another would be routed between them without a denial. A single check also cannot ask whether the caller holds anything under `connection:X`. `ScopeCovers` (`backend/internal/platform/policy/hierarchy.go:74`) requires the grant and the required scope to share an action.

**Option 2c (rejected):** Read the held connections from the platform token. `PlatformClaims` (`backend/internal/platform/token/claims.go:65`) carries no scopes.

**Option 2d (rejected):** Fetch the agent's connectors from `orbit-agents`. The gateway would need a new typed client and a service call per request. The grant compiled from those connectors is already in the gateway's bundle.

**Option 2e (rejected):** Select GraphQL requests by the owner named in the document. `classifyGraphQL` already resolves every `repository(owner:, name:)` call (`backend/internal/credentials/adapters/githubapp/graphql.go:483`). Documents that use `node`, `viewer` or `search` name no owner. GraphQL would still need the held-connection rule, and one endpoint would carry two selection rules.

### D3. Requests that resolve to no candidate

**Option 3a (rejected):** Deny every request that does not resolve to exactly one candidate. Today a read of another account's public repository is served by the anonymous read fallback. In a tenant with two GitHub connections, a dependency clone from a third account would be denied.

**Option 3b (chosen):** Treat a request that no candidate's credential can serve like today's foreign-owner request, and deny only an ambiguous request outright.

### D4. How the operator rejects an overlap

**Option 4a (chosen):** Drop only the offending connections from the rendered registry and log a warning for each.

**Option 4b (rejected):** Fail the render of `orbit-tenant-connections`. The ConfigMap holds every tenant's connections. One tenant's misconfiguration would freeze connection changes for all tenants.

---

## Decision

The egress gateway resolves every connection in the caller's tenant that serves the destination host and selects one by the request's owner or by the caller's held connections.

### Details

*   **Single candidate:** When one connection serves the host, the broker uses it without reading the owner or the caller's scopes.
*   **Owner-addressed requests:** For `github_app`, the addressed owner is the first path segment of a path with at least two segments on `github.com`, `codeload.github.com` and `raw.githubusercontent.com`. On the API it is the segment after `/repos/`, `/orgs/` or `/users/`, after any `/api/v3` prefix is removed. The candidate whose `owner` matches is selected.
*   **Owner-less requests:** Every other path addresses no owner, including `/graphql`, `/search`, `/user` and opaque content-host paths. The broker reads the caller's scope set once and selects the one candidate the caller holds.
*   **Unresolved requests:** An owner bound to no candidate, or an owner-less request from a caller that holds no candidate, mints no credential. The anonymous read fallback serves it when it is a read and the caller's own egress reaches the host. Otherwise it is denied.
*   **Ambiguous requests:** An owner-less request from a caller that holds two or more candidates is denied.
*   **Denial:** Both denials use a new reason, `connection_unresolved`. The denial body lists the candidate connection names and states that owner-addressed requests use the connection bound to that owner.
*   **Aggregation guard:** The operator groups a tenant's connections whose host families intersect. In a group of two or more, a connection is dropped when it declares no `owner` or when another member declares the same `owner`, compared case-insensitively. A group of one is rendered unchanged.

---

## Consequences

### Positive

*   A tenant can hold one GitHub App connection per GitHub organization, and each request uses the installation bound to its owner.
*   An overlap that cannot be resolved appears as an operator warning at aggregation.
*   A caller denied for ambiguity receives the candidate connection names.

### Negative

*   An agent that holds two GitHub connections cannot call owner-less endpoints such as GraphQL, `/search` and `/user`.
*   Two connections that declare the same owner are both dropped. Adding a duplicate disables the existing connection.
*   Two Kubernetes connections with the same base host in one tenant are both dropped. Kubernetes connections declare no owner.
*   The anonymous read fallback for an unresolved request carries no connection CA. A GitHub Enterprise host with a private CA and several connections cannot serve it.

### Neutral

*   An owner-less request in a multi-candidate tenant adds one in-process scope-set evaluation.

---

## Implementation

1.  **Registry and broker selection:** `ConfigMapRegistry` returns every connection that serves a host. The `github_app` connector reports its configured owner and the owner a request addresses. `Broker.resolveConnection` selects among the candidates and returns the `connection_unresolved` denial.
2.  **Operator guard:** `ConnectionAggregator.collect` drops overlapping connections that lack a distinct owner.

---

## Related Decisions

*   [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the broker, the registry and the anonymous read fallback (Amendment 5)
*   [Connection Scope Qualifiers](2026-07-22-connection-scope-qualifiers.md): the scope shape connectors compile to
*   [GitHub Connection: Category-Scoped Actions](2026-07-22-github-connection-category-scoped-actions.md): the `owner` field and the recorded gap
*   [GitHub GraphQL Authorised by the Granular Actions of Selected Fields](2026-09-29-github-graphql-field-classification.md): GraphQL repository references

**Last Updated:** 2026-10-15
