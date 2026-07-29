# orbit-agents Route & Scope Registration

## Status

Accepted

## Context

orbit-agents exposes an HTTP management API for agent types and runs. Traffic reaching it from external clients flows through the Orbit policy gateway (Envoy + OPA), which enforces route-level scope checks compiled from the policy-service's OPA bundles.

Until orbit-agents registers its routes with the policy-service, the gateway has no bundle entries for those paths. Depending on the gateway's default posture this means requests are either silently passed through without scope enforcement or blanket-denied — neither is acceptable for a production control-plane API.

The platform already has a solution for exactly this: the `routescopes.Client` ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)), used by inference-gateway and policy-service itself. Services POST their route→scope mappings and scope declarations at startup; the policy-service upserts them into the vocabulary and compiles the OPA bundles.

orbit-agents is a platform service deployed in `orbit-system`. Its SPIFFE identity is attested as a **SYSTEM** principal by the identity-proxy sidecar, which means its registration is **global** — the declared scopes and routes are compiled into every tenant's OPA bundle, making them available for tenant administrators to grant to their team members via the policy portal.

Internal callback routes (`/internal/*`) are intentionally excluded from registration: they are protected by Istio `AuthorizationPolicy` at the mTLS layer, restricting access to the session-manager workload's SPIFFE identity only. Scope-based OPA enforcement would add no value there.

## Decision

orbit-agents will register its public routes and scope vocabulary with the policy-service at startup using `routescopes.Client`, following the same pattern as `inference-gateway`.

### Route → Scope Mapping

| Method | Path | Required Scope |
|--------|------|----------------|
| `POST` | `/api/v1/agent-types` | `agents:types:write` |
| `GET` | `/api/v1/agent-types` | `agents:types:read` |
| `GET` | `/api/v1/agent-types/` | `agents:types:read` |
| `DELETE` | `/api/v1/agent-types/` | `agents:types:write` |
| `POST` | `/api/v1/runs` | *(authenticated only)* |
| `GET` | `/api/v1/runs` | `agents:runs:read` |
| `GET` | `/api/v1/runs/` | `agents:runs:read` |
| `DELETE` | `/api/v1/runs/` | `agents:runs:write` |
| `POST` | `/api/v1/runs/` | `agents:runs:write` |

The `/api/v1/runs/` POST prefix covers both `POST /api/v1/runs/{id}/messages` and any future per-run write actions. The `/api/v1/runs/` GET prefix covers `GET /api/v1/runs/{id}`, `GET /api/v1/runs/{id}/events`, and future per-run read actions.

**Correction: starting a run is authenticated-only at the route layer.** This ADR originally bound `POST /api/v1/runs` to `agents:runs:write`. That binding is wrong, and [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md) corrects it.

The scope that governs starting a run is per-agent (`agents:runs:{slug}:write`), and the slug comes from the agent type named in the request body, which a static path-prefix route map cannot see. A route-level `agents:runs:write` therefore does not narrow the endpoint, it **widens** it: by the `scope_covers` prefix rule stated above, the parent scope covers every per-agent child, so every principal permitted to start any run would have to hold the scope that permits starting *all* of them. That is what allowed a webhook source's minted token to start any agent type in its tenant.

The route is registered with an empty required scope and excluded from policy-service registration, exactly as the drafts and agent-type-mode routes are. The authoritative check is `Authz.checkRun`, evaluated in `RunService.authorizeStart` before the first write, so a refused start leaves nothing behind and returns `403`, not `500`. The parent `agents:runs:write` still satisfies that per-agent check for the admin roles that legitimately hold it, so nothing an administrator could do before is lost.

### Scope Vocabulary

| Scope | Resource Type | Action | Description |
|-------|--------------|--------|-------------|
| `agents:types:read` | `agents` | `read` | View and list agent type definitions |
| `agents:types:write` | `agents` | `write` | Create and delete agent type definitions |
| `agents:runs:read` | `agents` | `read` | View runs and stream agent events |
| `agents:runs:write` | `agents` | `write` | Start, message, and terminate runs |

These four scopes form the complete permission surface for the agent management API. Platform administrators can compose them into roles (e.g. an `agent-operator` role with all four, or a read-only `agent-viewer` role with only the two read scopes) and grant them to human principals or PATs via the policy portal.

### Per-Agent-Type Scope Vocabulary

In addition to the four static scopes above, orbit-agents registers per-agent-type scopes for every agent type slug defined in the platform. This mirrors the inference-gateway's per-model scope vocabulary and enables tenant administrators to grant access to individual agent types rather than all agents at once.

| Scope | Resource Type | Path Segments | Action | Description |
|-------|--------------|---------------|--------|-------------|
| `agents:runs:{slug}:read` | `agents` | `["runs", "{slug}"]` | `read` | View runs and events for a specific agent type |
| `agents:runs:{slug}:write` | `agents` | `["runs", "{slug}"]` | `write` | Start, message, and terminate runs for a specific agent type |

For example, a `coder` agent type produces:
- `agents:runs:coder:read` — read access to coder runs
- `agents:runs:coder:write` — write access to coder runs

**OPA scope hierarchy.** The existing `scope_covers` rule in `authz.rego` uses colon-delimited path-prefix matching: a principal holding `agents:runs:write` automatically satisfies `agents:runs:coder:write` because the prefix matches. No OPA policy changes are required. Per-agent-type scopes serve as fine-grained vocabulary entries for the portal and grants — they are most useful for creating roles scoped to a single agent type (e.g. an `agent-coder-operator` role), while `agents:runs:write` remains the catch-all for admins.

**Dynamic re-registration.** The scope list is rebuilt and re-registered whenever an agent type is created or deleted. `Service` exposes a `SetAgentTypeHook(fn func())` method; `initScopeRegistration` installs a hook that triggers a background re-registration after each agent-type mutation. The hook queries `ListAllAgentTypeSlugs` across all tenants to collect the current distinct slug set.

### Exclusions

`/internal/*` routes are not registered. They are protected at the Istio mTLS layer by an `AuthorizationPolicy` that permits only the session-manager's SPIFFE identity (`spiffe://*/tenant/*/agent/*` within the same namespace). Registering them in OPA would be redundant and could create confusion about their access model.

`GET /health` is not registered — health endpoints are exempt from scope enforcement platform-wide.

### Implementation

`initScopeRegistration` is added to `cmd/orbit-agents/main.go`, mirroring inference-gateway's pattern:

```go
func initScopeRegistration(ctx context.Context, logger logging.Logger, repo agents.Repository, svc *agents.Service) {
    if agentsConfig.Current.Platform.PolicyServiceURL == "" {
        return // local dev without policy-service
    }
    client := routescopes.NewClient(agentsConfig.Current.Platform.PolicyServiceURL, logger)

    register := func() {
        slugs, err := repo.ListAllAgentTypeSlugs(ctx)
        if err != nil {
            logger.Error(ctx, "list agent type slugs for scope registration failed",
                logging.Field{Key: "error", Value: err.Error()},
            )
            return
        }
        if err := client.Register(ctx, "orbit-agents", agentRoutes(), agentScopes(slugs)); err != nil {
            logger.Error(ctx, "route scope registration failed",
                logging.Field{Key: "error", Value: err.Error()},
            )
        }
    }

    svc.SetAgentTypeHook(func() { go register() })
    go register()
}
```

`agentRoutes()` returns the static route→scope table. `agentScopes(slugs)` returns the four static `ScopeDeclaration` entries plus two per-slug entries (`agents:runs:{slug}:read` and `agents:runs:{slug}:write`) for each slug in the current agent type roster.

Registration runs in a background goroutine so it does not block server startup. The `routescopes.Client` retries with exponential backoff (up to 15 attempts, capped at 30 s) to tolerate the policy-service being temporarily unavailable. The 15-second initial delay allows the identity-proxy sidecar to exchange its SPIFFE certificate for a platform token before the first attempt.

`PolicyServiceURL` is already present in the orbit-agents config under `platform.policyServiceURL` (used by the policy grant client), so no new config fields are required.

## Consequences

**Policy portal visibility.** Once registered, the four static `agents:*` scopes plus two per-agent-type scopes appear in the policy vocabulary. Tenant administrators can immediately grant them to team members or PATs without any platform team involvement — the same self-service flow used for inference scopes today.

**Gateway enforcement.** Requests to `POST /api/v1/agent-types` without `agents:types:write` in the caller's grant set will be rejected at the Envoy layer before reaching orbit-agents. orbit-agents itself does not need to duplicate scope checks in its handlers.

**Granular agent-type access.** A tenant admin can create a role with only `agents:runs:coder:read/write` and assign it to a user, giving that user access only to the `coder` agent type — not all agents. The `scope_covers` hierarchy means a principal holding `agents:runs:write` automatically satisfies both read and write on all agent types.

**Restart idempotency.** Route registration is stateless and idempotent. Each orbit-agents pod re-registers on startup, which is harmless — the policy-service's route store overwrites the previous entry for the same `(service, tenantID)` pair.

**No schema migration.** Scope declarations are stored in the policy-service's in-memory vocabulary store; they are not persisted to a database. They are re-populated each time orbit-agents (re)starts.

**Scope evolution.** Adding a new orbit-agents endpoint in future requires adding one `RouteScope` and optionally one `ScopeDeclaration` entry. The policy-service reconciles orphaned scopes from grants and roles automatically when a scope is removed from the registration. Deleting an agent type triggers an immediate re-registration that removes its per-type scopes from the vocabulary.
