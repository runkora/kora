# Same-Host Connection Selection in the Egress Gateway

<!-- Filename: 2026-10-15-same-host-connection-selection.md -->

**Status:** Accepted  
**Date:** 2026-10-15  
**Deciders:** Justin Wilkin  
**Technical Story:** Two GitHub App connections for different GitHub organizations in one tenant.

---

## Context

### Background

The egress gateway resolves the connection for a request by tenant and destination host. `Broker.resolveConnection` (`backend/internal/egressgw/service.go`) is the only selection point, and it calls `ConfigMapRegistry.GetByHost` (`backend/internal/credentials/configmap.go`). `GetByHost` returns the first connection in load order whose base host or adapter host family matches.

The operator's `ConnectionAggregator.collect` (`backend/internal/operator/connection_aggregator.go`) renders every active tenant's connections into one ConfigMap, `orbit-tenant-connections`, sorted by tenant and then by connection name. `collect` already skips a single connection with a warning when its base host is invalid or its Secret is missing.

The operator serves admission webhooks on port 9443 behind the `orbit-operator-webhook` Service (`charts/kora/templates/operator/webhook.yaml`). The `orbit-agent-pod-validator` ValidatingWebhookConfiguration routes pod creation in tenant namespaces to `/validate-pod`. `webhookcert.Manager.wire` (`backend/internal/operator/webhookcert/manager.go:266`) sets the CA bundle and `failurePolicy: Fail` on every entry of that configuration, and the `orbit-webhooks` preflight check (`backend/internal/operator/preflight/preflight.go`) requires `Fail`. `TenantReconciler` watches every Connection (`backend/internal/operator/tenant_controller.go:327`), and the manager's cache holds them all.

Every github.com `github_app` connection claims the same host family: `github.com`, `api.github.com`, `codeload.github.com` and `*.githubusercontent.com` (`githubEgressHosts` in `backend/internal/platform/connections/adapters.go`). A GitHub Enterprise connection claims only its own base host.

A `github_app` connection declares the account its installation is bound to in the `owner` config field (`backend/internal/credentials/adapters/githubapp/githubapp.go`). Classification denies a git or REST path whose owner segment differs from the configured owner. Content-host paths with a foreign owner classify as unqualified `contents:read`.

An agent's connectors compile to scopes of the form `connection:<name>:<category>[:<repo>]:<verb>` (`AgentType.ConnectorScopes` in `backend/internal/agents/models.go`). `ScopeResolver.EffectiveScopes` (`backend/internal/agents/effective_scopes.go`) adds the connectors of every attached environment. The resulting grant reaches the egress gateway in its compiled policy bundle.

### Problem Statement

A test against the real registry loaded two GitHub App connections into one tenant: `org-a-github` (owner `org-a`) and `org-b-github` (owner `org-b`). Requests to `github.com` and `api.github.com` both resolved to `org-a-github`, and `org-b-github` was never selected. Nothing at aggregation reported the overlap between the two connections. Kubernetes connections declare distinct hosts and are unaffected.

### Goals

*   The gateway selects among connections in one tenant that serve the same host.
*   A request that addresses an owner uses the connection bound to that owner.
*   A GraphQL or search request is tied to an owner by the owners and node ids its document or query names.
*   A request that can be tied to no owner uses the one candidate connection the caller holds, and is denied with a clear reason when the caller holds several.
*   A tenant with one connection per host behaves exactly as it does today.
*   Registration rejects a connection that cannot be told apart from an existing connection in the same tenant.

### Non-Goals

*   The GitHub MCP server deployment.
*   GitHub Enterprise behavior beyond keeping its single-connection path unchanged.

---

## Which of the three questions this touches

This decision touches **How does it connect?** It changes which connection and upstream credential serve a request.

**Who is this agent?** and **What can it access?** are preserved. Policy is evaluated against the selected connection's scopes exactly as it is today. The caller's scope set is read only to choose a candidate and never authorizes a call. No credential is minted for a request that resolves to no connection, and no credential reaches the agent. The node id lookup runs with a read-only token, and neither the token nor the lookup result reaches the agent.

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

**Option 2a (chosen):** Read the caller's scope set from the gateway's in-process policy evaluator. `Evaluator.Scopes` (`backend/internal/platform/policy/evaluator.go:26`, implemented at `engine.go:395`) evaluates `principal_all_scopes` (`backend/internal/policyservice/bundle/authz.rego:823`). That rule returns every scope on the caller's grants and roles. The caller holds connection X when one of those scopes starts with `connection:X:`. A two-part `connection:<verb>` scope covers every connection and holds every candidate.

**Option 2b (rejected):** Run a policy check of the classified action against each candidate and keep the candidates that allow it. Selection would depend on the action. A caller with read on one connection and write on another would be routed between them without a denial. A single check also cannot ask whether the caller holds anything under `connection:X`. `ScopeCovers` (`backend/internal/platform/policy/hierarchy.go:74`) requires the grant and the required scope to share an action.

**Option 2c (rejected):** Read the held connections from the platform token. `PlatformClaims` (`backend/internal/platform/token/claims.go:65`) carries no scopes.

**Option 2d (rejected):** Fetch the agent's connectors from `orbit-agents`. The gateway would need a new typed client and a service call per request. The grant compiled from those connectors is already in the gateway's bundle.

**Option 2e (chosen):** Select GraphQL and search requests by the owners the document or query names, and fall back to the held-connection rule when they name none. `classifyGraphQL` already walks every `repository(owner:, name:)` call, including variables and fragments (`backend/internal/credentials/adapters/githubapp/graphql.go:483`). An agent that holds several GitHub connections otherwise has no way to call GraphQL or search. The anonymous fallback cannot serve these requests. Anonymous GraphQL returns 403, anonymous `/user` returns 401, and anonymous search omits private repositories.

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

### D8. Documents addressed only by node id

**Option 8a (chosen):** Resolve each node id to its owner login with a lookup query the gateway sends with the installation tokens of the held candidates. The request is then selected by the resolved owners.

**Option 8b (rejected):** Deny every document that is addressed only by node id. `gh pr view`, review comments and most mutations address objects by node id. An agent holding several GitHub connections could not use them.

**Option 8c (rejected):** Decode the owner from the node id. A node id encodes the object type and database ids. It carries no owner login, and mapping it to an owner needs a lookup anyway.

**Option 8d (rejected):** Configure a default connection per tenant for requests that name no owner. The gateway would mint the default installation's token for requests addressed to another organization's objects without a denial.

---

## Decision

The egress gateway resolves every connection in the caller's tenant that serves the destination host and selects one by the request's owner or by the caller's held connections.

### Details

*   **Single candidate:** When one connection serves the host, the broker uses it without reading the owner or the caller's scopes.
*   **Owner-addressed requests:** For `github_app`, the addressed owner is the first path segment of a path with at least two segments on `github.com`, `codeload.github.com` and `raw.githubusercontent.com`. On the API it is the segment after `/repos/`, `/orgs/` or `/users/`, after any `/api/v3` prefix is removed. The candidate whose `owner` matches is selected.
*   **Owner-less paths:** Every other path addresses no owner, including `/graphql`, `/search`, `/user` and opaque content-host paths.
*   **GraphQL owners:** The classifier walk returns the owners a document addresses through `repository(owner:)`, `organization(login:)`, `repositoryOwner(login:)` and `user(login:)`. It reads literals, variables and variable defaults at any depth and inside fragments.
*   **Search owners:** For `/search/*`, the owners are the owner of each `repo:owner/name` term and the value of each `org:` and `user:` qualifier in `q`. Negated qualifiers address no owner.
*   **Owner rule:** When every named owner maps to one candidate, that candidate is selected. Owners that span candidates deny the request. One request carries one token. An owner bound to no candidate falls back to the held-connection rule. The held token reaches only public data of an owner it does not serve. The request is unresolved when the caller does not hold exactly one candidate.
*   **Node ids:** A GraphQL document that names no owner is resolved by its node ids when the caller holds several candidates. The ids are the string values of every argument or input-object field named `id` or `ids`, or ending in `Id` or `Ids`. This covers `node(id:)`, `nodes(ids:)` and mutation inputs such as `subjectId` and `labelIds`. Six names match the rule and carry no node id in the GitHub schema, and they are excluded: `clientMutationId`, `externalId`, `ghsaId`, `iterationId`, `multiSelectOptionIds` and `singleSelectOptionId`. The ids of a document that names an owner are not resolved. The token minted for that owner's installation cannot read another installation's private objects.
*   **Node id lookup:** The gateway mints a read-only token for a held candidate and sends one aliased `node(id:)` field per id, with inline fragments that return the owner login of a repository or a repository-scoped object. A user or organization id resolves to no owner. The token carries `metadata` and read access to the categories the caller holds on that connection without a repository qualifier, and a connection where the caller holds only qualified grants is not used for the lookup. The lookup can then see only what the caller's own grants on that connection reach. A document that names more than 100 node ids is unresolved. The lookup runs with each held candidate's token as needed, and collects the owners of the ids that resolve. Ids that no held candidate can see are ignored for selection. The selected installation's token cannot read an object that it could not see in the lookup. Selection uses the resolved owners among the caller's held candidates and ignores which candidate's token saw each id. A public object is visible to every installation. The request is unresolved when no id resolves or the lookup fails.
*   **Lookup cache:** Resolved owners are cached for 10 minutes, with at most 10,000 entries. An entry is keyed by tenant, host, node id, the connection whose token resolved it and that token's categories, and it counts only for a caller that holds the same. An id the connection cannot see is cached as unseen.
*   **Lookup audit:** Each lookup is logged with the run attribution, the candidate used and the number of ids, and is recorded as a span. Its result is never returned to the agent.
*   **Held connections:** A request tied to no owner selects the one candidate the caller holds. The broker reads the caller's scope set once.
*   **Unresolved requests:** An owner bound to no candidate, or an owner-less request from a caller that holds no candidate, mints no credential. The anonymous read fallback serves it when it is a read and the caller's own egress reaches the host. Otherwise it is denied.
*   **Ambiguous requests:** An owner-less request from a caller that holds two or more candidates is denied.
*   **Denial:** Both denials use a new reason, `connection_unresolved`. The denial body lists the candidate connection names, states which part of the request could not be tied to an owner, and states that owner-addressed requests use the connection bound to that owner. A denial names only owners the caller wrote in the path, document or query. An owner resolved from a node id does not appear in a denial.
*   **Conflict rule:** Two connections in one tenant namespace conflict when they cannot be told apart. For an adapter that declares an owner (`github_app`), that is when their host families intersect and either side has no `owner` or both declare the same `owner`, compared case-insensitively. For every other pair, that is when their host families intersect.
*   **Registration guard:** The operator registers a Connection handler on its webhook server and adds a Connection entry to `orbit-agent-pod-validator`. The entry covers CREATE and UPDATE in namespaces labeled `runkora.io/role: tenant` and inherits the CA bundle and `failurePolicy: Fail` from `webhookcert.Manager`. The handler lists the namespace's Connections from the manager's cached client. A CREATE is rejected when it conflicts with any of them. An UPDATE is rejected only for a conflict its previous version did not have. The rejection message cites the conflicting connection. The existing connection is never changed.
*   **Aggregation backstop:** `ConnectionAggregator.collect` orders each tenant's connections by creation timestamp and then by name. It drops a connection that conflicts with an earlier kept connection and logs a warning that cites both. The conflict is decided by the spec alone, and an earlier connection whose Secret is missing still wins. This covers connections created before the webhook existed and concurrent creates admitted against a stale cache.

---

## Consequences

### Positive

*   A tenant can hold one GitHub App connection per GitHub organization, and each request uses the installation bound to its owner.
*   `kubectl apply` or a GitOps sync of a conflicting Connection fails with the name of the connection it conflicts with.
*   A caller denied for ambiguity receives the candidate connection names.

### Negative

*   An agent that holds two GitHub connections cannot call `/user`, `viewer` queries, or GraphQL and search requests that name no owner and no resolvable node id.
*   A node id lookup that misses the cache adds one GitHub API call to the request and counts against the candidate installation's rate limit.
*   A caller with unqualified grants on two connections can drive one lookup per connection per request before its action is checked. It can also evict other callers' entries from the shared cache.
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
2.  **Registration guard and aggregation backstop:** One conflict function in `backend/internal/operator` serves the Connection webhook handler and `ConnectionAggregator.collect`. The chart change sits in `charts/kora/templates/operator/webhook.yaml`, and the handler is wired in `backend/cmd/operator/main.go`.
3.  **GraphQL and search owners:** The GraphQL walk returns the owners and node ids a document names, `/search` parses its owner qualifiers, and the broker resolves node ids through the lookup and its cache.

---

## Related Decisions

*   [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the broker, the registry and the anonymous read fallback (Amendment 5)
*   [Connection Scope Qualifiers](2026-07-22-connection-scope-qualifiers.md): the scope shape connectors compile to
*   [GitHub Connection: Category-Scoped Actions](2026-07-22-github-connection-category-scoped-actions.md): the `owner` field and the recorded gap
*   [GitHub GraphQL Authorised by the Granular Actions of Selected Fields](2026-09-29-github-graphql-field-classification.md): GraphQL repository references

**Last Updated:** 2026-10-15
