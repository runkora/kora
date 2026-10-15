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

The operator serves admission webhooks on port 9443 behind the `orbit-operator-webhook` Service (`charts/orbit/templates/operator/webhook.yaml`). The `orbit-agent-pod-validator` ValidatingWebhookConfiguration routes pod creation in tenant namespaces to `/validate-pod`. `webhookcert.Manager.wire` (`backend/internal/operator/webhookcert/manager.go:266`) sets the CA bundle and `failurePolicy: Fail` on every entry of that configuration, and the `orbit-webhooks` preflight check (`backend/internal/operator/preflight/preflight.go`) requires `Fail`. `TenantReconciler` watches every Connection (`backend/internal/operator/tenant_controller.go:327`), and the manager's cache holds them all.

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
*   Registration rejects a connection that cannot be told apart from an existing connection in the same tenant.

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
*   The author of a conflicting connection must receive the rejection when the connection is applied.
*   A conflicting connection must never disable an existing connection.
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

### D4. Where the operator rejects a conflict

**Option 4a (chosen):** A validating admission webhook on Connection CREATE and UPDATE rejects the conflicting connection. The connection aggregator keeps a backstop for connections that reach the cluster without passing the webhook.

**Option 4b (rejected):** Drop conflicting connections at aggregation only, with a warning. The Connection is accepted by the API server, and the conflict appears only in operator logs.

**Option 4c (rejected):** Fail the render of `orbit-tenant-connections`. The ConfigMap holds every tenant's connections. One tenant's misconfiguration would freeze connection changes for all tenants.

### D5. Which connection the aggregation backstop keeps

**Option 5a (chosen):** Keep the earlier connection by creation timestamp, then by name, and drop the later one with a warning.

**Option 5b (rejected):** Drop every connection in the conflict. Adding a conflicting connection would disable the existing one.

### D6. Webhook failure policy

**Option 6a (chosen):** `failurePolicy: Fail`. While the webhook is unreachable, the API server rejects Connection CREATE and UPDATE. DELETE is not intercepted. Pod creation in tenant namespaces is already blocked during the same outage.

**Option 6b (rejected):** `failurePolicy: Ignore`. Any Connection would be admitted during an outage, and only the aggregation backstop would catch a conflict. `webhookcert.Manager.wire` and the preflight check would each need an exception for one entry.

### D7. Connections for the same owner

**Option 7a (chosen):** Two connections with the same owner in one tenant conflict. Connector `resources` serve per-agent least privilege within one organization. They compile to repository-qualified scopes (`AgentType.ConnectorScopes` in `backend/internal/agents/models.go:240`). The minted token is narrowed to the request's repository by `reposFor` (`backend/internal/credentials/adapters/githubapp/githubapp.go:693`).

**Option 7b (rejected):** Allow same-owner connections when each declares a repository list and the lists are disjoint, and select by owner and then by repository. Each connection would need its own repository list, and selection would need a repository lookup. Requests that name no repository, such as GraphQL and search, would carry the same ambiguity across the same-owner connections.

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
*   **Conflict rule:** Two connections in one tenant namespace conflict when they cannot be told apart. For an adapter that declares an owner (`github_app`), that is when their host families intersect and either side has no `owner` or both declare the same `owner`, compared case-insensitively. For every other adapter, that is when their normalized base hosts are equal.
*   **Registration guard:** The operator registers a Connection handler on its webhook server and adds a Connection entry to `orbit-agent-pod-validator`. The entry covers CREATE and UPDATE in namespaces labeled `orbit.io/role: tenant` and inherits the CA bundle and `failurePolicy: Fail` from `webhookcert.Manager`. The handler lists the namespace's Connections from the manager's cached client and compares the incoming Connection with every connection created before it, by creation timestamp and then by name. A conflict rejects the request with a message that cites the conflicting connection. The existing connection is never changed.
*   **Aggregation backstop:** `ConnectionAggregator.collect` orders each tenant's connections by creation timestamp and then by name. It drops a connection that conflicts with an earlier kept connection and logs a warning that cites both. This covers connections created before the webhook existed and concurrent creates admitted against a stale cache.

---

## Consequences

### Positive

*   A tenant can hold one GitHub App connection per GitHub organization, and each request uses the installation bound to its owner.
*   `kubectl apply` or a GitOps sync of a conflicting Connection fails with the name of the connection it conflicts with.
*   A caller denied for ambiguity receives the candidate connection names.

### Negative

*   An agent that holds two GitHub connections cannot call owner-less endpoints such as GraphQL, `/search` and `/user`.
*   A github.com connection without an `owner` blocks every second github.com connection in its tenant until it declares one.
*   Connection CREATE and UPDATE fail while the operator webhook is unreachable.
*   A later conflicting connection that bypasses the webhook is absent from the gateway registry. Only an operator log warning reports it.
*   One GitHub organization maps to one connection per tenant.
*   The anonymous read fallback for an unresolved request carries no connection CA. A GitHub Enterprise host with a private CA and several connections cannot serve it.

### Neutral

*   An owner-less request in a multi-candidate tenant adds one in-process scope-set evaluation.
*   The conflict rule can be relaxed to allow same-owner connections with disjoint repository lists without changing owner selection. A distinct bot identity per agent, different App permission sets and installations administered by different teams would need that relaxation.

---

## Implementation

1.  **Registry and broker selection:** `ConfigMapRegistry` returns every connection that serves a host. The `github_app` connector reports its configured owner and the owner a request addresses. `Broker.resolveConnection` selects among the candidates and returns the `connection_unresolved` denial.
2.  **Registration guard and aggregation backstop:** One conflict function in `backend/internal/operator` serves the Connection webhook handler and `ConnectionAggregator.collect`. The chart change sits in `charts/orbit/templates/operator/webhook.yaml`, and the handler is wired in `backend/cmd/operator/main.go`.

---

## Related Decisions

*   [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the broker, the registry and the anonymous read fallback (Amendment 5)
*   [Connection Scope Qualifiers](2026-07-22-connection-scope-qualifiers.md): the scope shape connectors compile to
*   [GitHub Connection: Category-Scoped Actions](2026-07-22-github-connection-category-scoped-actions.md): the `owner` field and the recorded gap
*   [GitHub GraphQL Authorised by the Granular Actions of Selected Fields](2026-09-29-github-graphql-field-classification.md): GraphQL repository references

**Last Updated:** 2026-10-15
