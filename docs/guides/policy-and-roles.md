# Policy, Roles, and Access Control

This guide explains how kora's authorisation model works and how to configure access for principals on your tenant. It covers the scope vocabulary, grants, roles, and the relationship between them.

---

## Overview

kora uses a **default-deny whitelist model**. No principal can access any resource unless an explicit grant or role assignment says they can. Every request (agent, human, service account) passes through policy evaluation before the resource is reached.

There are two complementary ways to give a principal access to something:

| Mechanism | Best for |
|---|---|
| **Roles** | Groups of users that share the same access surface, e.g. all `engineer` users get inference access |
| **Grants** | One-off or fine-grained per-principal assignments, e.g. a single agent gets access to one specific MCP tool |

Both compile into the same OPA bundle and are evaluated by the same Rego rules. They are not mutually exclusive.

---

## Concepts

### Scopes

Permissions use a colon-separated format: `{resource_type}:{path*}:{action}`.

```
inference:invoke                    all inference models, any action
inference:anthropic:invoke          Anthropic models only
mcp:gmail:invoke                    all Gmail MCP tools
mcp:gmail:send_email:invoke         one specific tool
platform:grants:write               manage grants
platform:roles:write                manage role→scope mappings
```

Parent scopes implicitly cover narrower child scopes. A grant of `inference:invoke` covers `inference:anthropic:invoke`. There is no need to enumerate every child scope.

### Scope Vocabulary

The vocabulary is the finite registered set of valid scopes for a tenant. Grants and role assignments are validated against it at write time: a grant referencing an unregistered scope is rejected. This prevents privilege escalation through arbitrary scope strings.

The vocabulary is populated as a side effect of route registration: a service declares its `required_scope`s when it registers its routes (see [Route Registration](#route-registration)), and those scope declarations are upserted into the vocabulary. There is no standalone write API: the vocabulary is read-only over HTTP (`GET /policy/vocabulary`). Platform-level scopes (`platform:grants:write`, `platform:roles:write`, …) are self-registered by the policy service at startup.

### Grants

A grant asserts that a named principal within a tenant holds a set of scopes.

```json
{
  "principal_type": "AGENT",
  "principal_id":   "agent-abc123",
  "scopes":         ["inference:anthropic:invoke", "mcp:gmail:read"]
}
```

`principal_type` is one of `AGENT`, `HUMAN`, or `SERVICE`.

The `tenant_id` is derived from your platform token. You cannot create grants in another tenant. SYSTEM principals may supply an explicit `tenant_id` in the request body for cross-tenant administration.

Grants are per-principal. If you have 50 engineers who all need the same access, you would need 50 grants: one per user sub. **This is what roles solve.**

### Roles

A role maps an OIDC role name to a set of scopes. When a user's JWT carries a role claim, they are granted all scopes associated with that role, without any per-user grant record.

```json
{
  "role":   "engineer",
  "scopes": ["inference:invoke", "mcp:gmail:read"]
}
```

Any user whose OIDC token contains `"roles": ["engineer"]` automatically receives `inference:invoke` and `mcp:gmail:read` within the tenant. Adding a new engineer requires no policy change: just assigning the OIDC role in your identity provider.

The `tenant_id` is derived from your platform token. SYSTEM principals may supply it explicitly.

### Reserved Roles

`platform-admin` is the one reserved role. It cannot be created, modified, or deleted via the API. At every bundle compile the policy service derives them from the registered vocabulary. 

Any user with the `platform-admin` OIDC role can manage grants and roles without a prior grant record existing.

---

## Authentication and Authorisation

All policy service endpoints require a valid platform JWT, enforced at the controller level. The token's principal type determines what operations are permitted:

| Principal type | Can operate on |
|---|---|
| `SYSTEM` | Any tenant: supply `tenant_id` explicitly in the request |
| `HUMAN` / `AGENT` / `SERVICE` | Their own tenant only: `tenant_id` is derived from the token |

Attempting to supply a `tenant_id` that does not match your token returns `403 Forbidden`. There is no way for a non-SYSTEM principal to read or write another tenant's data.

---

## Access Hierarchy

When a request arrives, OPA evaluates it in this order:

1. **Is the principal's JWT valid?** (signature, expiry, issuer, audience)
2. **Does the principal carry a role** that covers the required scope?
3. **Does the principal have a grant** that covers the required scope?
4. **If a delegation token is present**, enforce the intersection: `delegation_scopes ∩ delegator_grants ∩ agent_grants`. All three sets must cover the required scope.

If any of steps 2, 3, or 4 allows access, the request proceeds. If none do, it is denied.

---

## Reading the Scope Vocabulary

Before creating grants or roles, the scopes you reference must exist in the vocabulary. The vocabulary is populated by services registering their scope declarations (see [Route Registration](#route-registration)). There is no HTTP endpoint to add or remove a scope directly. Any authenticated principal can read it.

**Get a single scope:**

```
GET /api/v1/policy/vocabulary/mcp:jira:issues:write
```

**List registered scopes:**

```
GET /api/v1/policy/vocabulary
GET /api/v1/policy/vocabulary?resource_type=mcp
```

Passing `?tenant_id=` additionally returns `grant_count` and `role_count` per scope: how many grants and roles in that tenant reference it.

---

## Managing Roles

Roles are the preferred way to give groups of users platform access. Define the access surface once, then assign the OIDC role in your identity provider.

**Create or replace a role:**

```
PUT /api/v1/policy/roles
Authorization: Bearer <platform-token>

{
  "role":   "engineer",
  "scopes": ["inference:invoke", "mcp:gmail:read"]
}
```

The operation is a full replace: the `scopes` list provided becomes the complete set for that role. If the role does not exist it is created; if it exists its scopes are replaced.

All scopes must be registered in the vocabulary, or the request is rejected. SYSTEM principals must include `tenant_id` in the body.

**Get a specific role:**

```
GET /api/v1/policy/roles/engineer
```

SYSTEM principals must include `?tenant_id=acme-corp`.

**List roles for a tenant:**

```
GET /api/v1/policy/roles
```

Response:

```json
{
  "roles": [
    {
      "id":         "01jre...",
      "tenant_id":  "acme-corp",
      "role":       "engineer",
      "scopes":     ["inference:invoke", "mcp:gmail:read"],
      "created_at": "2026-04-13T12:00:00Z"
    }
  ]
}
```

**Delete a role:**

```
DELETE /api/v1/policy/roles/engineer
```

Deleting a role removes the scope mapping. Users who previously received access via this role lose it on the next bundle polling cycle (default: ≤ 30 seconds). The reserved `platform-admin` role cannot be deleted.

> Role management requires `platform:roles:write`.

---

## Managing Grants

Use grants for per-principal access that does not fit a role: a specific agent that needs access to a narrow resource, or a service account with a custom scope set.

**Create a grant:**

```
POST /api/v1/policy/grants
Authorization: Bearer <platform-token>

{
  "principal_type": "AGENT",
  "principal_id":   "agent-abc123",
  "scopes":         ["mcp:jira:issues:write"]
}
```

`principal_type` must be one of:
- `AGENT`: a registered agent identity
- `HUMAN`: a user identified by their OIDC `sub`
- `SERVICE`: a service account identified by its SPIFFE ID

SYSTEM principals must include `"tenant_id": "acme-corp"` in the body.

**Get a grant by ID:**

```
GET /api/v1/policy/grants/{id}
```

**List grants for a tenant or principal:**

```
GET /api/v1/policy/grants
GET /api/v1/policy/grants?principal_type=AGENT&principal_id=agent-abc123
```

SYSTEM principals must include `?tenant_id=acme-corp`.

**Delete a grant:**

```
DELETE /api/v1/policy/grants/{id}
```

Grant deletion takes effect within the bundle polling interval (default: ≤ 30 seconds). There is no update operation: to change a grant's scopes, delete and recreate it.

> Grant management requires `platform:grants:write`.

---

## Roles vs Grants: When to Use Which

| Situation | Use |
|---|---|
| All engineers in my org need inference access | Role (`engineer` → `inference:invoke`) |
| One agent needs access to one specific MCP tool | Grant (agent → `mcp:jira:issues:write`) |
| A service account needs a specific narrow scope | Grant (SERVICE → target scope) |
| A delegating human authorises an agent | Delegation token (intersection enforced automatically) |
| A new team member joins the engineering team | Add OIDC role in IdP: no policy change needed |
| An agent is decommissioned | Delete the agent's grants |

**Prefer roles for humans.** Per-user grants (`HUMAN` principal type with a `sub`) create maintenance sprawl: every new user needs a grant record, and every access change requires editing individual records. OIDC role assignments are managed in your identity provider and propagate automatically into policy bundles without any API calls.

**Prefer grants for agents.** Agents have specific, narrow purposes. Granting them exactly the scopes they need (principle of least privilege) is easier to audit and review than broad role assignments.

---

## Route Registration

Services declare which scopes protect their routes by registering at startup with the policy service. This is how Layer 1 (OPA Envoy sidecar) knows which scope a request needs before it reaches the service handler.

Platform services (inference gateway, egress gateway, tool gateway, orbit-agents, orbit-triggers) register globally: their routes are included in every tenant's bundle. Tenant-published services (MCP servers, tenant APIs) can register per-tenant routes, but every `required_scope` must exist in that tenant's vocabulary first.

### How It Works

A platform service registers with its own platform token. The identity client's `SelfTokenSource` exchanges the pod's SPIFFE SVID for that token in-process, and the `routescopes` client sends it as the bearer on the registration call only. The policy service derives the registration scope (global vs tenant) from the token's principal type:

- `SYSTEM` principal → global registration, compiled into every tenant bundle. The `service` field must equal the token's `display_name`, which is the pod's SPIFFE component (`/system/<name>`).
- `SERVICE` principal → tenant-scoped, compiled into that tenant's bundle only. The `service` field must equal the token's subject.

A registration for any other service is rejected with 403.

### Adding Route Registration to a Service

Use the `internal/platform/clients/routescopes` client with a token source, and hand the register function to `KeepRegistered`, which retries until the first success and then re-registers periodically:

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

### Inspecting Registered Routes

SYSTEM principals can see all currently registered route→scope mappings:

```
GET /api/v1/policy/routes
Authorization: Bearer <system-token>
```

Route registrations are in-memory in the policy service. On a policy service restart, services re-register as they come back up. During the brief window after restart, bundles have no routes and Layer 1 will deny all requests until services re-register.

> **Operational constraint:** Because the route store is in-memory, the policy service must currently run as a single replica. With multiple replicas, a service registering with replica-1 would not have its routes visible in bundles compiled by replica-2.

> **Security prerequisite:** The bundle endpoint (`GET /bundles/{tenant_id}`) is unauthenticated: access is restricted by Istio `PeerAuthentication` STRICT mTLS. If STRICT mode is disabled, per-tenant bundles (containing grants) are exposed. Ensure `PeerAuthentication` STRICT mode is enabled for `orbit-system` in all environments.

---

## Propagation Timing

Policy changes (grant writes, role updates, route registrations) trigger an immediate bundle recompile for the affected tenant. Enforcement points (OPA sidecars, embedded evaluators) poll for new bundles on a configurable interval (default: 30–60 seconds). Access changes are effective within one polling cycle.

Grant and role deletions follow the same timing: the access is revoked at the next polling cycle, not instantly at the API call.

---

## API Reference

All endpoints are under `/api/v1/policy/` and require a valid platform JWT.

### Vocabulary

| Method | Path | Principal | Description |
|---|---|---|---|
| `GET` | `/policy/vocabulary` | Any | List all registered scopes |
| `GET` | `/policy/vocabulary/{scope}` | Any | Get a single scope entry |

### Grants

| Method | Path | Required Scope | Description |
|---|---|---|---|
| `POST` | `/policy/grants` | `platform:grants:write` | Create a grant |
| `GET` | `/policy/grants/{id}` | `platform:grants:read` | Get a grant |
| `GET` | `/policy/grants` | `platform:grants:read` | List grants |
| `DELETE` | `/policy/grants/{id}` | `platform:grants:write` | Delete a grant |

### Roles

| Method | Path | Required Scope | Description |
|---|---|---|---|
| `PUT` | `/policy/roles` | `platform:roles:write` | Create or replace a role |
| `GET` | `/policy/roles/{role}` | `platform:roles:read` | Get a single role |
| `GET` | `/policy/roles` | `platform:roles:read` | List roles |
| `DELETE` | `/policy/roles/{role}` | `platform:roles:write` | Delete a role |

### Routes

| Method | Path | Principal | Description |
|---|---|---|---|
| `GET` | `/policy/routes` | SYSTEM only | List all registered route→scope mappings |
| `POST` | `/policy/routes/register` | SYSTEM or SERVICE | Register a service's route→scope mappings |

### Bundles

| Method | Path | Description |
|---|---|---|
| `GET` | `/policy/bundles/{tenant_id}` | OPA bundle polling endpoint (internal) |

---

## Related

- [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md)
- [dynamic-route-scope-registration](../adr/2026-04-14-dynamic-route-scope-registration.md)
