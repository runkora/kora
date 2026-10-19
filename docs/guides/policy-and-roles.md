# Policy, Roles, and Access Control

kora denies every request unless a grant or a role held by the caller covers the scope the request needs. This guide covers the scope format, the built-in roles, per-agent access, delegated runs, triggers, and the APIs that manage grants and roles.

## Scopes

A scope has the form `{resource_type}:{path...}:{action}`:

```
inference:invoke                       reach the LLM endpoints
inference:anthropic:use                every Anthropic model
agents:runs:coder:write                start and steer runs of the coder agent
connection:github:issues:write         write GitHub issues through the github connection
mcp:invoke                             reach the MCP tool gateway
mcp:gmail:send_email:use               call one Gmail MCP tool
platform:grants:write                  manage grants
```

A scope covers another scope when both have the same resource type and action and its path is a prefix of the other's path. `agents:runs:write` covers `agents:runs:coder:write`. A two-part scope such as `inference:invoke` covers every `inference:...:invoke` scope. There are no wildcards.

The vocabulary is the global set of valid scopes, and every tenant validates against it. Services declare their scopes when they register their routes (see [Route Registration](#route-registration)), and the policy service self-registers the `platform:*` scopes. Grant and role writes are validated against the vocabulary. Read it with `GET /api/v1/policy/vocabulary`.

A vocabulary entry can declare `requires`: the scopes the holder also needs for the scope to work. The bundle compiler adds every required scope to each grant and tenant role that covers the entry. The built-in roles take no `requires` expansion. An MCP tool scope on the orbit server requires the platform scope its upstream call needs.

A grant or role write returns `warnings` for scopes that do nothing without a companion scope the write does not include. Examples are a model scope without `inference:invoke`, an MCP tool scope without `mcp:invoke`, a GitHub category scope without the connection's `metadata:read`, a per-agent run scope without read on that agent, an agent-type write without `agents:types:read`, and `triggers:write` without `triggers:read`. A warning never blocks the write. The principal may hold the companion through another grant or role.

## Grants and Roles

*   **Grant:** A grant gives one principal (`AGENT`, `HUMAN`, or `SERVICE`) a set of scopes. Agents, PATs, and service accounts get access only through grants.
*   **Role:** A role maps a role name to a set of scopes. Roles reach `HUMAN` principals only.

A role name matches an IdP role or group of the same name literally. Your identity provider decides who holds a role. Adding a person to the `kora-user` group in the IdP gives them the `kora-user` scopes with no policy change.

### How IdP Groups Reach the Token

1.  At sign-in the identity service reads the `roles` and `groups` claims of the IdP's ID token (`internal/identity/service.go`). Entra and Auth0 typically send `roles`, and Dex and Okta typically send `groups`.
2.  The identity service merges both lists into the comma-separated `roles` claim of the platform token.
3.  Each policy decision resolves the caller's role names to scopes from the compiled bundle. The bundle also carries a snapshot of each user's role names to resolve the person behind a delegated run.

Locally, Dex reads groups from glauth over LDAP. Group membership is set in `config/glauth/config.cfg`.

## Built-in Roles

kora ships five reserved roles. Built-in roles cannot be created, edited, or deleted by a tenant. `PUT` and `DELETE` on a reserved role return `400` with `role "<name>" is reserved and cannot be modified`. Tenants add their own roles alongside them, and a person holds the union of every role and grant they have.

`platform-admin` holds the covering set of the whole vocabulary. The four `kora-*` roles hold a fixed scope list defined in `internal/policyservice/bundle/builtin_roles.go`. Each tier holds every scope of the tier above it in the table.

| Role | Adds | What a person with the role can see and do |
|---|---|---|
| `kora-viewer` | `agents:types:read`, `agents:environments:read`, `triggers:read`, `inference:list`, `platform:connections:read` | See every agent definition, environment, trigger, and connection, and the models they can use. See their own runs and the runs of agents they hold per-agent read on. Start nothing. |
| `kora-user` | `inference:invoke`, `agents:runs:delegated:write`, `mcp:invoke` | Start runs of any delegated agent, and message and stop their own. Each run acts with the person's own access and can reach the MCP tool gateway. |
| `kora-builder` | `agents:types:delegated:write` | Create and change delegated agents. |
| `kora-operator` | `agents:types:autonomous:write`, `egress:connect`, `triggers:write`, `trigger:fire`, `agents:usage:read`, `platform:analytics:read`, `platform:budgets:read`, `identity:users:read` | Create autonomous agents within their own access, run them, and put triggers on them. Give those agents general internet access. Fire triggers. Read every agent's usage, tenant analytics, budgets, and the user directory. |
| `platform-admin` | Every scope in the vocabulary | Administer the tenant: grants, roles, budgets, the model catalog, every agent and run. |

The built-in roles carry no model, connection, or MCP tool scopes. Add a tenant role for each model family a group may use (e.g. `inference:anthropic:use`) and for the connections and tools they may act through. A `kora-user` with no model scope cannot run a model.

The bundle compiles only the declared scopes that the vocabulary holds. `GET /api/v1/policy/roles` marks each reserved role with `"reserved": true` and lists a built-in role's declared scopes that no service has registered under `unregistered_scopes`.

## Per-Agent Access

Each agent publishes its own scopes under its slug:

*   `agents:types:<slug>:read` shows the agent's definition.
*   `agents:runs:<slug>:read` shows the agent's runs, with their messages, events, and outputs.
*   `agents:runs:<slug>:write` starts, messages, interrupts, and terminates the agent's runs.
*   `agents:usage:<slug>:read` shows the agent's spend and usage.

The tenant-wide parents `agents:types:read`, `agents:runs:read`, `agents:runs:write`, and `agents:usage:read` cover these for every agent. Lists return only the agents and runs the caller may read.

These rules apply without a per-agent scope:

*   A person in an interactive session sees the runs they started. A PAT, agent, or service needs a read scope.
*   The creator of an agent sees it and can view, start, and act on its runs while they hold `agents:types:<mode>:write` for the agent's identity mode. An agent created under delegation belongs to the person behind the delegation.
*   A delegated run is visible only to its starter unless the caller holds `agents:delegated:read`. Only the starter can message or interrupt it.

## Delegated and Autonomous Agents

An agent runs in one of two identity modes:

*   **Delegated:** The agent acts on behalf of the person who starts the run. Its own grant is a ceiling on that person's access.
*   **Autonomous:** The agent acts under its own standing grant.

Creating or changing an agent needs `agents:types:delegated:write` or `agents:types:autonomous:write` for its mode. `agents:types:write` covers both.

The caller must also hold the agent's grant:

*   An autonomous agent's grant cannot hold a scope the caller does not hold.
*   A delegated agent's grant is exempt apart from its own egress. The caller must hold that egress.
*   Egress that a connector derives is exempt in both modes.
*   Editing an environment is checked the same way for every attached agent and needs each agent's mode write scope.

`agents:runs:delegated:write` starts runs of any delegated agent and does not reach autonomous agents. Messaging and interrupting a delegated run stay with its starter, and terminating another person's delegated run also needs `agents:delegated:read`. `agents:runs:write` reaches every agent in both modes. The slugs `delegated` and `autonomous` are reserved to keep per-agent scopes from colliding with the mode scopes.

### How Delegated Runs Use the Person's Access

Each call a delegated run makes must be covered by three sets: the delegation token's scopes, the agent's grant, and the person's own grants and roles. The person's access is resolved live from the bundle. A role change reaches in-flight runs on the next bundle propagation.

A delegated run needs the person to hold these scopes:

*   A model scope for each model the run calls.
*   A connection scope for each connection action the run takes.
*   `mcp:invoke` to reach the tool gateway, and each MCP tool's scope or every scope in that tool's `requires`.

Egress is the agent's own connectivity posture. At run time a delegated run's egress is checked against the agent's grant and environment alone. The person's control over it comes from the creation check above.

## Triggers

*   `triggers:read` shows trigger sources, triggers, prompt templates, and delivery payloads.
*   `triggers:write` creates, edits, and deletes triggers and sources, and replays deliveries.

Binding a trigger to an agent needs authority to start that agent's runs: `agents:runs:<slug>:write`, or creator access to the agent. The trigger's principal receives that agent's run scopes. Editing what a trigger runs (its prompt, inputs, schedule, filters, sender restriction, or enablement) and replaying a delivery need the same authority. A trigger cannot bind a delegated agent.

Firing a manual trigger (`POST /api/v1/trigger-fires/{id}`) needs `trigger:<trigger-id>:fire` for that trigger or `trigger:fire` for every trigger. `triggers:write` does not fire a trigger. The per-trigger scope can be held in a grant and cannot be held in a role.

## User Directory

`identity:users:read` lists the tenant's users and reads any user's full profile. The analytics and policy pages use it. Any signed-in caller can read their own profile and resolve user names by id with no scope.

## Scopes to Keep Out of Non-Admin Roles

These scopes administer the tenant or confer authority beyond the holder. Keep them in `platform-admin` and in narrow, reviewed grants:

*   `platform:grants:write`, `platform:roles:write`, `platform:budgets:write`, and `platform:catalog:write`.
*   `agents:types:write`, `agents:runs:write`, and `agents:environments:write`.
*   Any `connection:<name>:write` parent, GitHub `admin:write` and `repo:write`, and Kubernetes `secrets` and `rbac:write`.
*   `mcp:<server>:use` on a server with write tools.
*   `mcp:orbit:use` and the orbit rollups for policy, agents, runs, triggers, and budgets. They confer platform scopes through `requires`.

## Managing Roles

Role management needs `platform:roles:write`. A caller can only put scopes it holds into a role.

**Create or Replace a Role:**

```
PUT /api/v1/policy/roles

{
  "role":   "anthropic-users",
  "scopes": ["inference:anthropic:use"]
}
```

The `scopes` list replaces the role's scopes in full. Every scope must be in the vocabulary. A repository-qualified connection scope such as `connection:github:issues:orbit:write` is accepted when its unqualified base is registered. A role accepts `egress:connect` and rejects per-host (`egress:<host>:connect`) and connector egress scopes. Those scopes belong in agent grants.

**Change a Role's Budget:**

A `PUT` that keeps the stored budget needs only `platform:roles:write`. A `PUT` that sets, changes, or clears it also needs `platform:budgets:write`. To change only the budget, use the budget endpoint:

```
PATCH /api/v1/policy/roles/engineer/budget

{ "budget": { "limit_usd": 50, "reset_cadence": "monthly" } }
```

A `null` budget clears it. Reserved roles carry no budget. A delegated run cannot make any policy write: grants, roles and budgets all refuse it.

**Read and Delete Roles:**

```
GET    /api/v1/policy/roles
GET    /api/v1/policy/roles/{role}
DELETE /api/v1/policy/roles/{role}
```

The list starts with the five reserved roles on the first page.

The `tenant_id` comes from your platform token. SYSTEM principals supply `tenant_id` in the body or the query string.

## Managing Grants

Creating and deleting grants needs `platform:grants:write`, and a caller can only grant scopes it holds.

**Create a Grant:**

```
POST /api/v1/policy/grants

{
  "principal_type": "AGENT",
  "principal_id":   "agent-abc123",
  "scopes":         ["mcp:jira:create_issue:use"]
}
```

Grants accept vocabulary scopes, egress host scopes, repository-qualified connection scopes, and `trigger:<trigger-id>:fire`.

**Read and Delete Grants:**

```
GET    /api/v1/policy/grants?principal_type=AGENT&principal_id=agent-abc123
GET    /api/v1/policy/grants/{id}
DELETE /api/v1/policy/grants/{id}
```

Grants have no update operation for scopes. Delete the grant and create a new one.

## Route Registration

Services declare which scope protects each route by registering with the policy service at startup. The edge gateway and each pod's OPA sidecar read these routes from the bundle. Platform services (inference gateway, egress gateway, tool gateway, orbit-agents, orbit-triggers) register globally. Identity registers its scope vocabulary and no routes, and its callers reach it east-west. Tenant-published services such as MCP servers register per-tenant routes.

A platform service registers with its own platform token. `SelfTokenSource` exchanges the pod's SPIFFE SVID for that token, and the `routescopes` client sends it on the registration call:

*   A `SYSTEM` principal registers globally. The `service` field must equal the pod's SPIFFE component.
*   A `SERVICE` principal registers for its tenant only. The `service` field must equal the token's subject, and every `required_scope` must already be in the vocabulary.

A registration for any other service is rejected with 403.

Scope declarations write the global vocabulary. Only a `SYSTEM` registration may declare scopes, and a `SERVICE` registration that declares any is rejected with 403. The first service to declare a scope owns it (`registered_by`), and only the owner can change its description or `requires`. A declaration of a scope another service owns is skipped and logged as a warning, and the rest of the registration proceeds.

A declaration's `requires` has three states. Absent or `null` keeps the stored set, `[]` clears it, and a non-empty list replaces it. A service with a partial view of a scope, such as the tool gateway while an MCP server is degraded, sends `null`.

```go
selfToken := identityclient.NewClient(cfg.Identity.InternalURL, logger).SelfTokenSource()
client := routescopes.NewClient(cfg.Policy.PolicyServiceInternalURL, selfToken, logger)
register := func(ctx context.Context) error {
    return client.Register(ctx, "my-service", []routescopes.RouteScope{
        {PathPrefix: "/v1/my-resource/", Method: "POST", RequiredScope: "my-scope:write"},
        {PathPrefix: "/v1/my-resource/", Method: "GET", RequiredScope: "my-scope:read"},
    }, nil)
}
go routescopes.KeepRegistered(ctx, logger, "my-service", register)
```

The service name must match the pod's `spiffe.io/component` label.

### Authenticated-Only Routes

A route with an empty `required_scope` needs a valid token and no scope. The service behind it holds the real check. Use one when the governing scope depends on the request body or a stored row. Register these routes with the rest of the table. The policy service marks them `authenticated_only`, and the edge and sidecar OPA admit any valid token on them.

Only global registrations can carry authenticated-only routes. The policy service refuses a tenant route with an empty scope or an empty path prefix, and an empty scope declaration from any service. A path that matches no registered route is denied. A route that must stay closed to agents and north-south callers is left out of the registration: identity publishes no routes, and the policy service publishes only its scoped routes.

### Route Matching

The edge OPA, the sidecar OPA, and the in-process `ScopeEnforcementMiddleware` resolve a request to a route the same way. An exact-method route beats a `"*"` route, and then the longest matching prefix wins. A prefix matches on a path segment boundary: `/api/v1/runs` covers `/api/v1/runs/r1` and does not cover `/api/v1/runs-export`. Global routes take precedence over a tenant's own routes, and a tenant service cannot shadow a platform route with a longer prefix. In the OPA layers, two equally specific routes that disagree match nothing, and the request is denied.

### Route Storage

Route registrations live in the policy service's memory. After a restart, bundles hold no routes until services re-register. The policy service must run as a single replica.

The bundle endpoint `GET /api/v1/policy/bundles/{tenant_id}` is unauthenticated and restricted by Istio `PeerAuthentication` STRICT mTLS. Keep STRICT mode enabled for `orbit-system` in every environment.

## Propagation Timing

Grant writes, role writes, and route registrations trigger a bundle recompile. Enforcement points poll for new bundles every 30 to 60 seconds. A grant or role change, including a revocation, takes effect within one polling cycle.

## API Reference

All endpoints are under `/api/v1/policy/` and need a valid platform token:

*   `GET /policy/vocabulary` and `GET /policy/vocabulary/{scope}` need `platform:grants:read`.
*   `GET /policy/grants` needs `platform:grants:read`, and `POST` and `DELETE` on it need `platform:grants:write`.
*   `GET /policy/roles` needs `platform:roles:read`, and `PUT` and `DELETE` on it need `platform:roles:write`.
*   `PATCH /policy/roles/{role}/budget` needs `platform:budgets:write`.
*   `GET /policy/routes` is open to any authenticated principal.
*   `POST /policy/routes/register` is open to SYSTEM and SERVICE principals.

## Related

*   [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md)
*   [dynamic-route-scope-registration](../adr/2026-04-14-dynamic-route-scope-registration.md)
*   [delegated-agent-identity](../adr/2026-07-23-delegated-agent-identity.md)
