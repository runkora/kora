# Admin Inference Logs — Cross-Principal, Tenant-Scoped Usage Read

**Date:** 2026-08-18
**Status:** Proposed
**Deciders:** Qj Stout-Spykers, Platform Engineering
**PRD:** [admin-inference-logs](../prd/admin-inference-logs.md)

---

## Context

The Inference Gateway writes one `inference_usage` row per request (model, provider, token counts, cost, latency, HTTP status, error code, principal attribution). `GET /v1/usage` reads them back, but `Service.ListUsage` forces the scope from verified claims — `filter.TenantID = p.TenantID; filter.PrincipalID = &p.PrincipalID` — so a caller sees only their own rows. The [Inference Usage Logs ADR](2026-04-17-inference-usage-logs.md) made that self-isolation a hard requirement and deliberately located it in the service layer "at a layer that can be bypassed (with appropriate authorization) for a future admin view." This is that view.

The pattern already exists next door. Analytics splits `AnalyticsMe` (self-scoped, no scope required) from `Analytics` / `AnalyticsPrincipals` (tenant-wide: `TenantID` from claims, `PrincipalID` not forced to self), gated by `platform:analytics:read`. `GET /v1/analytics/principals/{id}` is already an admin per-principal drill-down under that scope. The route→scope layer (`cmd/inference-gateway/main.go`) maps each path+method to exactly one `RequiredScope`, and the `/v1/usage` tree is deliberately split by path into personal (`RequiredScope: ""`) and privileged reads rather than branching on scope inside one handler.

**What can it access?** This is a new privileged, tenant-wide read. It is default-deny: the `platform:analytics:read` scope is enforced by the route-scope middleware *before* the handler runs, exactly as for the tenant-wide analytics reads. Tenant isolation is preserved unconditionally — `TenantID` is always sourced from the verified JWT and never from a query parameter, so the SQL predicate is self-sufficient. The optional principal filter can only *narrow* within the tenant; a caller-supplied `principal_id` from another tenant simply matches no rows. The feature touches neither identity minting/delegation (*Who is this agent?*) nor outbound credentials (*How does it connect?*); those invariants are unaffected.

The `tenant-admin` role's hardcoded bundle (`policyservice/bundle/compiler.go`) already includes `platform:analytics:read`.

---

## Options Considered

### Option A: Scope-conditional widening on the existing `GET /v1/usage`

Keep one route; widen `ListUsage` from self to tenant when the caller holds the admin scope.

**Pros:** One path, one handler.
**Cons:** The route-scope layer maps a path to exactly one `RequiredScope` and cannot express "optional scope," so authorization would move into handler logic — against the codebase's explicit personal/privileged split by path. It also risks regressing the self-isolation the original ADR guards, and every existing `ListUsage` test now has to reason about two modes.

**Not chosen.**

### Option B: New privileged route `GET /v1/usage/all`, gated by `platform:analytics:read` (chosen)

A tenant-wide sibling of `/v1/usage`, mirroring `/analytics` vs `/analytics/me`. New `ListUsageAll` service method; `/v1/usage` is untouched.

**Pros:**
- Self-scoped `/v1/usage` and its tests are untouched — no regression surface.
- Reuses the existing filter struct, repository query, response shape, table UI, and detail modal.
- `platform:analytics:read` is already held by `tenant-admin`: zero role change, zero vocabulary change, usable on deploy.
- Tenant scoping stays sourced from claims; the privileged/personal boundary stays expressed as separate paths, consistent with the rest of the gateway.

**Cons:** Row-level log access is welded to the analytics scope — an org cannot grant one without the other.

**Chosen.**

### Option C: New `platform:inference-logs:read` scope on the new route

As Option B, but with a dedicated scope registered in the vocabulary and added to the reserved-role bundles.

**Pros:** Decouples row-level log access from aggregate analytics; matches the repo's "scope follows the resource" philosophy (catalog, budgets each own a scope).
**Cons:** Edits the hardcoded `tenant-admin` / `platform-admin` bundles and the scope vocabulary for a scope that, in practice, will always be co-granted with `platform:analytics:read`. Premature separation.

**Not chosen** — but it is the clean upgrade path if row-level log visibility ever needs to be grantable independently of analytics.

---

## Decision

**Option B.** Add `GET /v1/usage/all`: a tenant-scoped, cross-principal inference usage read with an optional principal filter, gated by `platform:analytics:read`. The self-scoped `GET /v1/usage` is unchanged.

---

## Implementation

### 1. Backend — inference-gateway service

New `Service.ListUsageAll(ctx, filter)` beside `ListUsage`:

```go
// ListUsageAll returns inference usage across every principal in the caller's
// tenant. TenantID is always sourced from the verified JWT; PrincipalID is an
// optional caller-supplied filter that can only narrow within the tenant. The
// caller must carry platform:analytics:read — enforced at route registration,
// mirroring Analytics(). Contrast ListUsage(), which forces PrincipalID to self.
func (s *Service) ListUsageAll(ctx context.Context, filter InferenceUsageFilter) ([]InferenceUsageRecord, bool, error) {
    p, err := principalFromClaims(ctx)
    if err != nil {
        return nil, false, fmt.Errorf("%w: %w", ErrTokenParseFailed, err)
    }
    filter.TenantID = p.TenantID   // always from claims, never a query param
    // PrincipalID left as parsed from the query: nil = whole tenant, set = one principal.
    filter.IncludeDelegated = false // admin sees each row as-is (agent rows stay the agent's)
    return s.usage.List(ctx, filter)
}
```

No new repository method: `InferenceUsageRepository.List` already honours `TenantID`, `PrincipalID`, `PrincipalType`, model/status/time filters, and cursor pagination.

### 2. Handler

`handleListUsageAll` reuses `parseUsageFilter` (already parses `principal_id`, `principal_type`, `model`, `status`, `from`/`to`, and the cursor) and returns the same `listUsageResponse` shape as `handleListUsage`.

### 3. Route registration (`controller.go`)

```go
mux.Handle("GET /usage/all", c.handleListUsageAll) // before "GET /usage" — segment-bounded prefix match, most-specific wins
```

### 4. Route scope (`cmd/inference-gateway/main.go`)

Add, listed **before** the `/v1/usage` entry so the more-specific path wins:

```go
{PathPrefix: "/v1/usage/all", Method: "GET", RequiredScope: "platform:analytics:read"},
```

No `inferenceScopes` change (the scope already exists); no role change (`tenant-admin` already holds it).

### 5. Frontend — admin logs surface

A cross-principal logs view under Model Gateway: a **principal column** and **principal filter** added to the existing logs table, reusing the existing detail modal. The BFF route handler proxies `GET /v1/usage/all`. Visibility is gated on `platform:analytics:read` via the [frontend policy projection](2026-04-14-frontend-policy-projection.md), so non-admins never see the surface.

### 6. Schema

None. No migration; no new column. Metadata-only, consistent with the original ADR.

---

## Consequences

**Positive:**
- A tenant admin can locate another principal's failed or expensive request without local reproduction or direct DB access.
- Minimal surface: no schema, no migration, no new scope, no role change; the self-scoped path and its tests are untouched.
- Consistent with the established analytics `me` vs tenant-wide precedent and the gateway's path-based privilege split.

**Negative / Trade-offs:**
- Row-level log visibility is welded to `platform:analytics:read`. If an org ever needs to grant it independently, Option C (a dedicated scope) is the migration path.
- The principal filter is caller-supplied and only narrows; because the tenant predicate is self-sufficient, a foreign or bogus `principal_id` yields an empty result, never a cross-tenant leak — this must be covered by a test.
- Cross-tenant visibility and real-time streaming remain out of scope (PRD non-goals); a platform-admin cross-tenant view would be a separate decision.

---

## Notes

- Extends [Inference Usage Logs](2026-04-17-inference-usage-logs.md), whose deferred "admin view of cross-user logs" non-goal this fulfils.
- Reuses the detail view from [Inference Log Detail Modal](2026-06-05-inference-log-details.md).
- Authorization model and the route-scope enforcement layer: [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md), [Dynamic Route Scope Registration](2026-04-14-dynamic-route-scope-registration.md).
