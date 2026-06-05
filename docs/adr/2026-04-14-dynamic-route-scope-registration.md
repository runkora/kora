# Dynamic Route-Scope Registration

**Status:** Accepted  
**Date:** 2026-04-14
**Deciders:** Engineering Team  
**Technical Story:** Eliminate hardcoded route→scope mappings from the policy service so that any service — platform or tenant-published — can declare the scopes required to access its own routes without a policy-service code change or redeploy.

---

## Context

[policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) established the two-layer OPA enforcement model. Layer 1 (OPA Envoy sidecar) intercepts requests before they reach a service and derives the required scope from `data.routes.route_scopes` in the compiled bundle. Layer 2 (in-process `platform/policy.Evaluator`) makes fine-grained resource-level decisions inside the service handler.

The Layer 1 route→scope table is currently hardcoded in the policy-service Go source (`bundle/compiler.go`). Adding a route for a new service, or for a tenant-published MCP server or API, requires a policy-service code change and redeploy. The JWKS URL used by the Rego for JWT validation is also hardcoded in `authz.rego`, which is embedded in every compiled bundle — changing it requires another redeploy.

### Problem Statement

1. Route→scope mappings are owned by the service that exposes the routes, not by the policy service. Hardcoding them in the policy service creates an incorrect coupling.
2. Tenants will publish their own services (MCP servers, APIs) that need Layer 1 enforcement. There is no mechanism for them to register routes without platform team involvement.
3. The JWKS URL and JWT verification parameters (issuer, audience) are baked into Rego source, making cluster-specific configuration require a source code change.

### Goals

- Services declare their own route→scope mappings at startup; no policy-service change required
- Tenant-published services can register routes scoped to their tenant without platform involvement
- Vocabulary validation prevents tenants from escalating privilege via route registration
- JWKS URL and JWT parameters are injected at runtime via environment variables
- Registration is cluster-internal only; no secret management required

### Non-Goals

- Persistent route storage across policy-service restarts (acceptable tradeoff; services re-register on startup)
- Per-route allow/deny overrides (grant model from [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) handles this)
- Dynamic update of Rego rules themselves

---

## Decision Drivers

- No secret management for service-to-service registration (cluster network is the trust boundary)
- Tenant safety: a misbehaving tenant service must not be able to grant access to platform-level scopes
- Reusable: any service imports a platform client and calls one method; no bespoke HTTP plumbing per service
- Minimal operational overhead: no new database table, no new infrastructure component

---

## Considered Options

### Option 1: Config file per service

Each service ships a YAML file listing its route→scope mappings. The policy service reads these at startup from a shared config volume.

**Pros:** Simple, no network calls  
**Cons:** Requires shared volume or config map per service; adding a service still requires policy-service infra changes; tenant-published services cannot use it

### Option 2: Hardcoded with code-level extension points

Keep the Go slice but expose a registration function that services call at link time via `init()`.

**Pros:** No network call, zero latency  
**Cons:** Requires all services to be compiled into the same binary; impossible for tenant services; doesn't solve the dynamic case

### Option 3: In-memory registration via internal HTTP endpoint

Services call a cluster-internal HTTP endpoint on the policy service at startup. The policy service stores registrations in memory, keyed by `(service, tenant_id)`. Bundle recompilation is triggered on each registration change.

**Pros:** No secrets, no DB table, services own their own route declarations, works for platform and tenant services, reusable client in `internal/platform/routereg`  
**Cons:** Registrations are lost on policy-service restart (mitigated by services re-registering on their own startup); brief window after policy-service restart where bundles have no routes

---

## Decision

**Chosen Option:** Option 3 — In-memory registration via internal HTTP endpoint

### Rationale

The identity-proxy sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) already injects platform tokens on all outbound requests. Reusing this mechanism for route registration requires no additional secret distribution or management — the service's SPIFFE SVID is the credential; the identity-proxy exchanges it for a platform token automatically. In-memory storage is sufficient because services register at startup anyway; the policy service restart window is brief and bounded by service readiness probes.

---

## Design

### Registration tiers

| Tier | Declared by | Compiled into | Scope constraint |
|---|---|---|---|
| **Global** | Platform services (inference-gw, policy-gw) | Every tenant bundle | None — platform scopes are not in any tenant vocabulary |
| **Per-tenant** | Tenant-published services | That tenant's bundle only | Every `required_scope` must exist in the tenant's scope vocabulary |

The vocabulary is the trust anchor for tenant registration. Since vocabulary writes require an authenticated API call with `platform:grants:write`, a tenant cannot register a route that maps to a scope they have not explicitly defined.

### Registration request

Sent by each service at startup to the policy service API:

```
POST /api/v1/policy/routes/register
Authorization: Bearer <platform-token>   (injected automatically by identity-proxy sidecar)
Content-Type: application/json

{
  "service":   "inference-gateway",
  "routes": [
    {"path_prefix": "/v1/chat/completions", "method": "POST", "required_scope": "inference:invoke"},
    {"path_prefix": "/v1/messages",          "method": "POST", "required_scope": "inference:invoke"},
    {"path_prefix": "/v1/models",            "method": "GET",  "required_scope": "inference:list"}
  ],
  "scopes": [
    {"scope": "inference:invoke", "resource_type": "inference", "path": [], "action": "invoke", "description": "Call the inference endpoints"},
    {"scope": "inference:use",    "resource_type": "inference", "path": [], "action": "use",    "description": "Use any model across all providers"},
    {"scope": "inference:anthropic:use", "resource_type": "inference", "path": ["anthropic"], "action": "use", "description": "Use any Anthropic model"}
  ]
}
```

The `scopes` array is optional vocabulary — services may register new scope entries alongside their routes in a single call. This is used by the inference gateway to register the static top-level inference scopes and to upsert per-model/family scopes dynamically as the catalog changes.

- Registration is global (included in every tenant bundle) when made by a `SYSTEM` or `SERVICE` principal
- Re-registration is idempotent — replaces the prior route set for the service, upserts vocabulary entries
- The identity-proxy sidecar exchanges the service's SPIFFE SVID for a platform token; the `routescopes` client retries until the sidecar is ready (typically 15–30s after pod start)

### Platform client

```
internal/platform/clients/routescopes/client.go
```

Services import this package and call `Register(ctx, serviceName, routes, scopes)` at startup. The policy-service URL is passed via each service's config (`POLICY_SERVICE_URL` env var). Retry with backoff is built into the client — the policy service or identity-proxy sidecar may not be ready when a service starts.

### In-memory store

Keyed by `(service_name, tenant_id)`. A global registration (`tenant_id = ""`) is merged into every tenant bundle at compile time. A tenant registration is merged only into that tenant's bundle.

### Bundle compilation

The compiler merges routes at compile time:

```
bundle_routes = global_registrations + tenant_registrations[tenant_id]
```

Written to `routes/data.json`. The Rego is unchanged — it still reads `data.routes.route_scopes`. Bundle recompilation is triggered immediately on any registration change, consistent with how grant writes trigger recompilation today.

### JWKS URL and JWT parameters

The JWKS URL, issuer, and audience are moved out of `authz.rego` into a config data document compiled into each bundle:

```
data/config/data.json → {"jwks_url": "...", "issuer": "orbit", "audience": "orbit-platform"}
```

`authz.rego` reads `data.config.jwks_url`, `data.config.issuer`, `data.config.audience`. Values are set via environment variables on the policy-service deployment (`POLICY_SERVICE_BUNDLE_JWKS_URL`, `POLICY_SERVICE_BUNDLE_ISSUER`, `POLICY_SERVICE_BUNDLE_AUDIENCE`), removing the last cluster-specific value from the Rego source.

### Restart behaviour

On policy-service restart, the in-memory store is empty. Bundles recompiled during this window contain no routes, so Layer 1 will deny all requests. Services re-register as they pass their own startup and readiness checks. The window is bounded by the slowest service's startup time — typically a few seconds in a healthy cluster.

If this window is unacceptable, a future iteration can persist registrations to the existing PostgreSQL database. The interface is identical; only the storage backend changes.

---

## Consequences

### Positive

- Adding a new platform service requires zero policy-service changes
- Tenant-published MCP servers and APIs can self-register with vocabulary-enforced scope containment
- No secrets or credentials required for registration
- All services use a single shared client — consistent retry, logging, and serialisation

### Negative

- Brief enforcement gap after policy-service restart until services re-register
- Services must be configured with the policy-service internal URL (one env var per service)

### Neutral

- Route→scope mappings move from a Go source file to per-service startup configuration
- The Rego file becomes cluster-agnostic; all environment-specific values are in bundle data documents

---

## Implementation

### New files

| Path | Purpose |
|---|---|
| `internal/platform/clients/routescopes/client.go` | Registration client used by all services |
| `internal/policyservice/routes/store.go` | In-memory route registration store |

### Modified files

| Path | Change |
|---|---|
| `internal/policyservice/api/handlers_routes.go` | `POST /policy/routes/register` handler (combined route + vocabulary upsert) |
| `internal/policyservice/bundle/compiler.go` | Read routes from store; merge global routes into every bundle; read JWKS URL from config |
| `internal/policyservice/bundle/authz.rego` | Read `data.config.jwks_url/issuer/audience` |
| `cmd/policy-service/config/config.go` | Add `Bundle.JWKSURL`, `Bundle.Issuer`, `Bundle.Audience` |
| `cmd/inference-gateway/main.go` | Call `routescopes.Register` at startup; re-register on catalog refresh via `SetCatalogUpdateHook` |

### Inference gateway catalog-driven vocabulary

The inference gateway extends the basic registration pattern with dynamic catalog-driven scope registration. On startup (after catalog warm-up) and on every `GET /v1/models` response, the router calls `SetCatalogUpdateHook` which triggers a re-registration with the full current set of per-family and per-model scopes:

```go
// main.go
router.SetCatalogUpdateHook(registerScopes)
go registerScopes(router.CatalogScopes())  // initial registration
```

`CatalogScopes()` returns one family-level scope (`inference:{family}:use`) and one model-level scope (`inference:{family}:{alias}:use`) per distinct model in the catalog. All scopes use the `use` action. The upsert is idempotent — re-registering unchanged scopes is a no-op at the database level.

---

## Related Decisions

- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy gateway and authorisation model this extends
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Envoy as the universal enforcement data plane
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — Cluster-internal identity; rationale for not requiring tokens on internal calls

---

**Last Updated:** 2026-04-10
