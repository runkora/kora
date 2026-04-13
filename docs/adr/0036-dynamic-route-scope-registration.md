# 0036. Dynamic Route-Scope Registration

**Status:** Proposed  
**Date:** 2026-04-10  
**Deciders:** Engineering Team  
**Technical Story:** Eliminate hardcoded route→scope mappings from the policy service so that any service — platform or tenant-published — can declare the scopes required to access its own routes without a policy-service code change or redeploy.

---

## Context

ADR-0035 established the two-layer OPA enforcement model. Layer 1 (OPA Envoy sidecar) intercepts requests before they reach a service and derives the required scope from `data.routes.route_scopes` in the compiled bundle. Layer 2 (in-process `platform/policy.Evaluator`) makes fine-grained resource-level decisions inside the service handler.

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
- Per-route allow/deny overrides (grant model from ADR-0035 handles this)
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

The cluster network is already the security boundary for east-west traffic (enforced via network policy and the Envoy sidecar). Adding a secret or token for an internal registration call would require secret distribution, rotation, and management — contradicting the platform's design principle of no manual secret handling (ADR-0010, ADR-0018). In-memory storage is sufficient because services register at startup anyway; the policy service restart window is brief and bounded by service readiness probes.

---

## Design

### Registration tiers

| Tier | Declared by | Compiled into | Scope constraint |
|---|---|---|---|
| **Global** | Platform services (inference-gw, policy-gw) | Every tenant bundle | None — platform scopes are not in any tenant vocabulary |
| **Per-tenant** | Tenant-published services | That tenant's bundle only | Every `required_scope` must exist in the tenant's scope vocabulary |

The vocabulary is the trust anchor for tenant registration. Since vocabulary writes require an authenticated API call with `platform:grants:write`, a tenant cannot register a route that maps to a scope they have not explicitly defined.

### Registration request

Sent by each service at startup to the policy service internal port:

```
POST /internal/routes/register
Content-Type: application/json

{
  "service":   "inference-gateway",
  "tenant_id": "",
  "routes": [
    {"path_prefix": "/v1/inference/", "method": "POST", "required_scope": "inference:invoke"},
    {"path_prefix": "/v1/inference/", "method": "GET",  "required_scope": "inference:list"}
  ]
}
```

- `tenant_id` empty → global registration, included in every tenant bundle
- `tenant_id` set → tenant-scoped, included only in that tenant's bundle; all `required_scope` values validated against the tenant's vocabulary before acceptance
- Re-registration is idempotent and replaces the previous entry for `(service, tenant_id)`

### Platform client

```
internal/platform/routereg/client.go
```

Services import this package and call `Register(ctx, ...)` at startup. The internal policy-service URL is passed via each service's config (`POLICY_SERVICE_INTERNAL_URL` env var). Retry with backoff is built into the client — the policy service may not be ready when a service starts.

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
| `internal/platform/routereg/client.go` | Registration client used by all services |
| `internal/policyservice/routes/store.go` | In-memory registration store |

### Modified files

| Path | Change |
|---|---|
| `internal/policyservice/api/handlers.go` | Add `POST /internal/routes/register` handler |
| `internal/policyservice/bundle/compiler.go` | Read routes from store + merge global/tenant; read JWKS URL from config |
| `internal/policyservice/bundle/authz.rego` | Read `data.config.jwks_url/issuer/audience` |
| `cmd/policy-service/config/config.go` | Add `Bundle.JWKSURL`, `Bundle.Issuer`, `Bundle.Audience` |
| `internal/inferencegw/`, `internal/policygw/` | Call `routereg.Register` at startup |

---

## Related Decisions

- [ADR-0035](0035-policy-gateway-and-authorisation-model.md) — Policy gateway and authorisation model this extends
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Envoy as the universal enforcement data plane
- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — Cluster-internal identity; rationale for not requiring tokens on internal calls

---

**Last Updated:** 2026-04-10
