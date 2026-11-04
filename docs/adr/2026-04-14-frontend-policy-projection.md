# Frontend Policy Projection via BFF

**Status:** Accepted  
**Date:** 2026-04-14  
**Deciders:** Engineering Team  
**Technical Story:** The frontend has no authorization model — every authenticated user sees every page, button, and navigation entry. Backend services enforce policy on every API call, but the UI provides no feedback until a 403 arrives. The frontend needs a permission-aware rendering model driven by the same OPA policies that protect the backend, without embedding OPA in the browser or duplicating policy logic in TypeScript.

---

## Context

[policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) established the two-layer OPA enforcement model for backend services. [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) added dynamic route-scope registration so services declare their own protected routes. Together these give the platform a single, centrally managed policy model evaluated by OPA — grants, roles, scope vocabulary, and route-scope mappings compiled into per-tenant bundles.

The frontend (Next.js, BFF pattern via iron-session) currently has zero authorization awareness. The session stores `sub`, `email`, `name`, and `tenantId` but no permission data. All authenticated users see the same sidebar, the same pages, and the same action buttons. When a user lacks the required scope for an operation, the backend returns 403 — but only after the user has navigated to the page, filled in a form, and submitted it.

### Problem Statement

1. The UI should reflect what the user is actually allowed to do — hide pages they cannot access, disable actions they cannot perform, show only tenants they can administer.
2. Authorization decisions must come from the same OPA policy model that protects the backend. Hardcoding role checks in frontend code creates drift, duplication, and a second source of truth.
3. Tokens must not reach the browser (BFF pattern constraint). OPA bundles must not be shipped to the client (leaks the policy model). The solution must work within the existing server-side-only security boundary.

### Goals

- Single source of truth: OPA policies drive both backend enforcement and frontend rendering
- No policy logic in the browser — all evaluation happens server-side via the policy gateway
- Permission data is cached in the sealed session cookie for zero-latency page renders
- Bounded staleness — permission set refreshes automatically without requiring re-login
- Works for both tenant-admins (single tenant) and platform-admins (multi-tenant)

### Non-Goals

- Real-time permission updates (sub-second propagation of grant changes to active sessions)
- Embedding OPA or WASM policy evaluation in the browser or Next.js edge runtime
- Per-request policy evaluation from the BFF on every page load
- Field-level or row-level authorization in the frontend (backend handles this)

---

## Decision Drivers

- **No token leakage:** the BFF pattern means tokens and policy data stay server-side
- **No policy duplication:** hardcoding `if (role === 'admin')` in TypeScript drifts from OPA and creates a second enforcement surface to maintain
- **Acceptable staleness:** permissions refreshing every ~15 minutes (on token refresh) is sufficient — the backend is always authoritative
- **Minimal new infrastructure:** reuse the existing policy gateway rather than adding a new service or sidecar
- **Consistent with existing patterns:** the frontend already fetches data via server components and proxies through BFF API routes

---

## Considered Options

### Option 1: Hardcoded role checks in the frontend

The BFF extracts roles from the JWT at login and the frontend checks `roles.includes('tenant-admin')` to gate pages and actions.

**Pros:**
- Simple to implement, no new endpoints
- Zero latency — roles are in the session

**Cons:**
- Duplicates policy logic — the OPA bundle defines what `tenant-admin` can do, and now the frontend has a parallel definition
- Breaks when scope assignments change — if `tenant-admin` loses `platform:roles:write`, the UI still shows the Roles page
- Cannot express grant-based access (a user with a direct `platform:grants:read` grant but no admin role would be invisible to this model)
- Every policy change potentially requires a frontend code change

### Option 2: Per-request policy evaluation from the BFF

On every page load or action, the BFF calls the policy gateway's `/v1/policy/check` endpoint to ask "can this user do X?"

**Pros:**
- Always fresh — no staleness window
- Uses OPA as the single source of truth

**Cons:**
- Adds latency to every page render — a page showing grants, roles, and vocabulary needs 3+ policy checks before it can render
- The sidebar needs to know visibility for ~10 nav entries — that's 10 round-trips on every navigation, or a batch endpoint that doesn't exist yet
- Unnecessary load on the policy gateway for data that changes infrequently (a user's permissions typically change less than once a day)

### Option 3: Cached permission projection via `/v1/policy/scopes`

At login (and on each token refresh), the BFF calls a new policy gateway endpoint that evaluates the authenticated principal against OPA and returns the complete set of scopes they hold. This scope set is cached in the sealed session cookie and drives all frontend rendering decisions. On failure, the BFF falls back to an empty scope set (fail closed) — the user sees only ungated pages until the next successful refresh.

**Pros:**
- Single source of truth — OPA evaluates once, frontend consumes the result
- Zero per-page latency — permissions are in the session, no network call to render
- Bounded staleness — re-evaluated on token refresh (~15 min) without user action
- Works for all access paths — grants, roles, reserved roles all resolve to scopes
- Scope-based gating is granular: nav entries, page guards, and button visibility all use the same `scopes.includes('platform:grants:write')` check
- No policy logic in TypeScript — the frontend only checks set membership, never interprets policy rules

**Cons:**
- Staleness window of up to one token refresh cycle (~15 min) after a grant/role change
- New endpoint on the policy gateway (small scope — one new Rego query rule and one handler)
- Scope set grows with the vocabulary — could become large for principals with broad access (mitigated: the set is string[], compressed well in the sealed cookie)
- If the policy gateway is unavailable at login, the user gets an empty permission set (fail closed) — they must wait for a successful token refresh to see gated pages

---

## Decision

**Chosen Option:** Option 3 — Cached permission projection via `/v1/policy/scopes`

### Rationale

Option 3 is the only approach that satisfies all three core requirements simultaneously: OPA as single source of truth, no per-page latency cost, and no policy logic in the frontend. The staleness window is acceptable because the backend is always the authoritative enforcement point — the frontend permission set is advisory, driving UI rendering only. A user who somehow sees a button they shouldn't will still be rejected by the backend.

Piggybacking the refresh on token renewal reuses existing infrastructure (the `proxy.ts` middleware already handles token refresh inline) and avoids adding a new background job or polling mechanism.

---

## Design

### New endpoint: `GET /v1/policy/scopes`

Added to the policy gateway. The principal is extracted from the `Authorization: Bearer` header by the existing auth middleware — there is no request body. The handler evaluates the authenticated principal against the OPA bundle for their tenant and returns the effective scope set.

```
GET /v1/policy/scopes
Authorization: Bearer <platform JWT>

→ 200 OK
{
  "scopes": [
    "platform:grants:read",
    "platform:grants:write",
    "platform:roles:read",
    "platform:roles:write",
    "inference:invoke",
    "inference:anthropic:use"
  ],
  "tenant_id": "acme-corp",
  "evaluated_at": "2026-04-14T10:30:00Z"
}
```

The scope set is the union of:
1. Scopes from explicit grants for this principal
2. Scopes from roles the principal holds (OIDC role claim → role→scope mappings)
3. Scopes from reserved roles (`tenant-admin`, `platform-admin`)

This is a new Rego query rule in the bundle:

```rego
principal_scopes contains scope if {
    some grant in data.grants
    grant.principal_id == input.principal.id
    some scope in grant.scopes
}

principal_scopes contains scope if {
    some role_name in input.principal.roles
    some role_entry in data.roles
    role_entry.role == role_name
    some scope in role_entry.scopes
}
```

The policy gateway calls this rule and returns the collected set. Parent scope containment is preserved: if a principal holds `inference:invoke`, the frontend can check for it directly. Child scope checks (e.g. `inference:anthropic:use`) are also in the set if granted. The frontend does not need to implement scope hierarchy resolution — the Rego rule returns the fully expanded set.

### Session enrichment

The `SessionPayload` is extended:

```typescript
export interface SessionPayload {
  user: SessionUser;
  accessToken: string;
  refreshToken: string;
  expiresAt: number;
  tenantId: string;
  permissions: {
    scopes: string[];
    evaluatedAt: number;  // UTC Unix seconds
  };
}
```

The public session response (`GET /api/auth/session`) includes permissions so client components can use them:

```typescript
// Response shape — no tokens, just identity + permissions
{
  user: { sub, email, name },
  permissions: { scopes: [...], evaluatedAt: 1713091800 },
  expiresAt: 1713095400
}
```

### Permission evaluation on login

After the identity service issues the access token, the BFF session creation handler (`POST /api/auth/session`) makes a second call:

```
BFF → GET /v1/policy/scopes  (Authorization: Bearer <accessToken>)
    ← { scopes: [...], tenant_id, evaluated_at }
```

Both the token and permission set are sealed into the iron-session cookie.

### Failure mode: fail closed

If the `/v1/policy/scopes` call fails (policy gateway unreachable, OPA evaluation error, network timeout), the BFF seals an **empty permission set** (`scopes: []`) into the session. The user's session is still valid — they can authenticate and navigate — but:

- Nav entries with a `requiredScope` are hidden (no scopes match)
- Page-level guards with `requiredScope` render the forbidden page
- Pages with no `requiredScope` gate remain visible (e.g. the dashboard home, settings)

This is a deliberate fail-closed design. The user sees a degraded UI rather than an over-privileged one. On the next token refresh cycle, the BFF retries the scopes call — if the policy gateway is back, the full permission set is restored without re-login.

The BFF logs the failure so operators can detect and respond to policy gateway availability issues.

### Permission refresh on token renewal

The existing token refresh logic in `proxy.ts` runs when the access token is within 5 minutes of expiry. After refreshing the token, it also refreshes the permission set:

```
proxy.ts middleware detects token expiring
  → POST /api/v1/identity/session { grant_type: refresh_token, ... }
  ← new access_token
  → GET /v1/policy/scopes  (Authorization: Bearer <newAccessToken>)
  ← updated scopes
  → seal both into session cookie
```

This gives a worst-case staleness window equal to the access token lifetime (default ~15 minutes). In practice, most sessions refresh multiple times per hour. If the scopes call fails during refresh, the existing permission set is preserved (not cleared) — the user keeps their current permissions until a successful refresh or re-login.

### Frontend consumption

**Navigation filtering:**

```typescript
// nav-entries.ts
export type NavEntry = {
  label: string;
  href: AppRoute;
  icon: React.ComponentType<{ className?: string }>;
  requiredScope?: string;  // hidden if user's scopes don't include this
  wip?: boolean;
};

// Sidebar filters entries:
const visible = entry.requiredScope
  ? session.permissions.scopes.includes(entry.requiredScope)
  : true;
```

**Page-level guards (server components):**

```typescript
// policy/layout.tsx
export default async function PolicyLayout({ children }) {
  const session = await getSession();
  if (!session.permissions.scopes.includes('platform:grants:read')) {
    return <ForbiddenPage />;
  }
  return <>{children}</>;
}
```

**Action-level gating (client components):**

```typescript
// Client components receive permissions via context or props
const { scopes } = usePermissions();
<Button disabled={!scopes.includes('platform:grants:write')}>
  Create Grant
</Button>
```

**Scope helper:**

```typescript
// lib/permissions.ts
export function hasScope(scopes: string[], required: string): boolean {
  return scopes.includes(required);
}

export function hasAnyScope(scopes: string[], required: string[]): boolean {
  return required.some(s => scopes.includes(s));
}
```

The frontend performs set membership checks only. It never interprets scope hierarchy, evaluates Rego rules, or makes policy decisions. If the scope is in the set, the user has access. If not, they don't.

### Multi-tenant (platform-admin)

Platform-admins can administer multiple tenants. The `/v1/policy/scopes` endpoint returns scopes for the principal's own tenant. For cross-tenant administration:

- The session stores the platform-admin's own scope set (which includes `platform:tenants:manage`)
- When the admin selects a different tenant via the tenant selector, the BFF proxies API calls to the policy service with `?tenant_id=<selected>` — the backend authorises based on the SYSTEM principal type
- The tenant selector is visible only when `scopes.includes('platform:tenants:manage')`
- Per-tenant data (grants, roles) is fetched on navigation, not cached in the session

### What the frontend does NOT do

- **No policy evaluation:** the frontend never runs OPA, Rego, or WASM
- **No scope hierarchy resolution:** `inference:invoke` covering `inference:anthropic:invoke` is resolved by the Rego rule, not by frontend code
- **No token inspection:** the frontend never decodes or reads the JWT — permissions come from the session
- **No enforcement:** if a stale permission set shows a button that the user no longer has access to, the backend rejects the action. The UI is advisory.

---

## Consequences

### Positive

- OPA is the single source of truth for both backend enforcement and frontend rendering
- No policy logic in TypeScript — the frontend only checks scope set membership
- Zero per-page latency for permission checks — the scope set is in the session
- Policy changes propagate to the UI automatically via token refresh — no frontend deploy needed
- The pattern works uniformly across all features (policy admin, model gateway, agents, etc.)

### Negative

- Staleness window of up to one token refresh cycle (~15 min) after a permission change
- One additional HTTP call on login and on each token refresh (to the policy gateway)
- The sealed session cookie grows by the size of the scope array (typically <1KB)

### Neutral

- A new Rego query rule is added to the bundle for scope collection
- A new handler is added to the policy gateway (`/v1/policy/scopes`)
- The public session response shape changes (additive — new `permissions` field)
- Navigation entries gain an optional `requiredScope` field

---

## Implementation

### New files

| Path | Purpose |
|---|---|
| `internal/policygw/handlers_scopes.go` | `GET /v1/policy/scopes` handler |
| `frontend/src/lib/permissions.ts` | `hasScope`, `hasAnyScope` helpers |
| `frontend/src/contexts/permissions-context.tsx` | React context for client component access |

### Modified files

| Path | Change |
|---|---|
| `internal/policyservice/bundle/authz.rego` | Add `principal_scopes` rule |
| `internal/policygw/router.go` | Mount `/v1/policy/scopes` route |
| `internal/policygw/models.go` | Add `ScopesResponse` type |
| `frontend/src/server/auth-config.ts` | Add `permissions` to `SessionPayload` |
| `frontend/src/app/api/auth/session/route.ts` | Fetch scopes on session creation; include in public response |
| `frontend/src/proxy.ts` | Refresh scopes alongside token refresh |
| `frontend/src/app/(dashboard)/_utils/nav-entries.ts` | Add `requiredScope` to nav entries |
| `frontend/src/app/(dashboard)/_components/dashboard-sidebar.tsx` | Filter entries by permission |

### Prerequisite

The identity service must propagate OIDC role claims into the platform token's `kora.claims.roles` field. Without this, the OPA bundle cannot match the principal's OIDC roles to role→scope mappings. This is a backend change to the token issuance flow, independent of this ADR.

---

## Related Decisions

- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy gateway and authorisation model
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Dynamic route-scope registration
- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — BFF pattern and iron-session architecture

---

**Last Updated:** 2026-04-14
