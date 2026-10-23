# Policy Administration Dashboard

**Status:** Accepted (Amended 2026-10-22)
**Date:** 2026-04-15
**Deciders:** Engineering Team  
**Technical Story:** Build a full-featured policy administration dashboard that enables tenants to manage grants, roles, vocabulary, and routes — and to inspect effective permissions and test policy decisions — using the live policy-service and policy-gateway APIs.

---

## Context

[policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) established the two-layer OPA enforcement model. [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) added dynamic route-scope registration. [frontend-policy-projection](2026-04-14-frontend-policy-projection.md) defined the frontend permission projection model via cached scope sets in the sealed session cookie. The backend policy plane is complete: grants CRUD, roles CRUD, scope vocabulary, route registrations, bundle compilation, and policy evaluation are all operational.

The frontend currently has a placeholder page at `/policy` wrapped in a `ScopeGuard` requiring `platform:grants:read`. There is no UI for viewing or managing policy objects, no way to inspect effective permissions for a principal, and no dry-run capability for testing policy decisions without making live requests.

### Problem Statement

1. Tenant administrators have no self-service UI for managing grants and roles — all policy changes require direct API calls or CLI tooling.
2. There is no visibility into the effective permissions of a principal (agent, human, or service) broken down by source (roles vs. direct grants).
3. There is no safe way to test whether a policy change will produce the expected result before committing it.
4. The scope vocabulary and route registrations are invisible to administrators, making it difficult to understand what scopes exist and what they protect.

### Goals

- Full CRUD for grants (create, list, detail, revoke) with scope-aware gating
- Full CRUD for roles (create/edit via upsert, list, delete) with reserved role protection
- Read-only vocabulary browser with search, grouping, and usage counts
- Read-only route registration viewer
- Overview dashboard with aggregate stats and bundle sync status
- Evaluate tool for inspecting effective permissions and dry-running policy checks
- All pages use live API data — no mocked or hardcoded content
- Follow established frontend patterns (model-gateway nested routes, Hades design system, BFF proxy)

### Non-Goals

- Editing or overriding OPA Rego rules from the UI
- Managing bundle compilation parameters (debounce, polling interval)
- Multi-tenant switching within the dashboard (uses session tenant)
- Real-time WebSocket updates for policy changes (polling or refetch-on-action is sufficient)
- Audit log / activity timeline (requires a separate audit service not yet built)

---

## Decision Drivers

- Follow existing patterns: the model-gateway feature is the reference implementation for nested route features with sidebar navigation
- BFF boundary: all backend URLs remain server-side; the browser never learns policy-service or policy-gateway addresses
- Permission-aware: write actions gated by `platform:grants:write` and `platform:roles:write`; read actions by `platform:grants:read` and `platform:roles:read`
- Backend is authoritative: the frontend is advisory; all mutations are validated and enforced server-side
- Live data from day one: every page fetches from real API endpoints; no local state masquerading as backend state

---

## Considered Options

### Option 1: Policy gateway as unified frontend API

Route all frontend requests through the policy gateway. The gateway would proxy admin operations (grants, roles, vocabulary) to the policy service internally, presenting a single backend URL to the BFF.

**Pros:** Single backend URL in frontend config; gateway becomes the only external surface for the policy plane  
**Cons:** Adds proxy complexity to the gateway; gateway's purpose is evaluation, not administration; couples gateway availability to admin operations; adds latency to every admin call

### Option 2: Direct BFF-to-policy-service for admin, gateway for evaluation

The BFF talks to two backends: the policy service (port 8083) for CRUD operations on grants, roles, vocabulary, and routes; and the policy gateway (port 8084) for evaluation operations (check, scopes). Each backend serves its own domain.

**Pros:** Clean separation of concerns; no proxy-through-proxy; each service owns its own API surface; BFF already talks to multiple backends (orbit API, inference gateway, policy gateway)  
**Cons:** One additional env var (`POLICY_SERVICE_URL`) in frontend config

### Option 3: Single catch-all BFF proxy route

A single `app/api/policy/[...path]/route.ts` that forwards all requests to the policy service based on path matching.

**Pros:** Minimal boilerplate; one route handler covers everything  
**Cons:** Loses type safety; harder to add per-route validation or transformation; error handling is generic; doesn't handle the split between policy-service and policy-gateway

---

## Decision

**Chosen Option:** Option 2 — Direct BFF-to-policy-service for admin, gateway for evaluation

### Rationale

The BFF already connects to three backends (`KORA_API_URL`, `INFERENCE_GATEWAY_URL`, `POLICY_GATEWAY_URL`). Adding `POLICY_SERVICE_URL` as a fourth is consistent with the established pattern. The policy service and policy gateway have distinct responsibilities — administration vs. evaluation — and routing through one to reach the other would create unnecessary coupling. Individual BFF route handlers per resource type (grants, roles, vocabulary, routes, evaluate) provide type-safe request/response handling and per-endpoint error messages.

---

## Design

### 1. New Backend Endpoints

Four additions to existing services to support the dashboard:

#### 1a. Policy Service: `GET /api/v1/policy/stats`

Returns aggregate counts for the overview dashboard. Avoids fetching all grants/roles/vocabulary just to count them.

```
GET /api/v1/policy/stats
Authorization: Bearer <platform-token>

→ 200 OK
{
  "grants": {
    "total": 24,
    "by_principal_type": { "AGENT": 14, "HUMAN": 8, "SERVICE": 2 }
  },
  "roles": {
    "total": 5,
    "reserved": 2,
    "custom": 3
  },
  "vocabulary": {
    "total": 31,
    "by_resource_type": { "inference": 4, "mcp": 5, "platform": 6, "credentials": 1 }
  },
  "routes": {
    "total": 8
  },
  "bundles": {
    "tenants": [
      { "tenant_id": "acme-prod", "last_compiled_at": "2026-04-14T10:30:00Z", "status": "current" },
      { "tenant_id": "acme-staging", "last_compiled_at": "2026-04-14T10:28:00Z", "status": "current" }
    ]
  }
}
```

**Scope required:** `platform:grants:read`

#### 1b. Policy Service: `GET /api/v1/policy/vocabulary` — Enhanced with Usage Counts

Extend the existing vocabulary list response to include usage counts per scope. This is computed at query time by joining against the grants and roles tables.

```
GET /api/v1/policy/vocabulary?resource_type=inference

→ 200 OK
{
  "scopes": [
    {
      "scope": "inference:anthropic:invoke",
      "resource_type": "inference",
      "path_segments": ["anthropic"],
      "action": "invoke",
      "description": "Call Anthropic models",
      "grant_count": 8,
      "role_count": 3,
      "created_at": "2026-04-10T08:00:00Z"
    }
  ]
}
```

The `grant_count` and `role_count` fields are new. They count the number of grants and roles that include each scope (exact match — not hierarchy-aware). This is a read-time join, not a denormalized counter, so it is always consistent.

#### 1c. Policy Gateway: `GET /v1/policy/scopes/explain`

Returns the effective scope set for a specified principal, broken down by source. The existing `GET /v1/policy/scopes` endpoint is preserved unchanged — it is used by the session layer and returns a flat deduplicated list for the authenticated principal. The new `/explain` variant accepts query parameters specifying which principal to inspect and adds provenance information for the evaluate tab.

```
GET /v1/policy/scopes/explain?principal_type=HUMAN&principal_id=auth0|6a7b8c9d0e1f2a3b4c
Authorization: Bearer <platform-token>

→ 200 OK
{
  "principal_type": "HUMAN",
  "principal_id": "auth0|6a7b8c9d0e1f2a3b4c",
  "tenant_id": "acme-prod",
  "from_roles": [
    {
      "role": "engineer",
      "scopes": ["inference:anthropic:invoke", "mcp:github:read", "platform:agents:read"]
    }
  ],
  "from_grants": [
    {
      "grant_id": "550e8400-e29b-41d4-a716-446655440000",
      "scopes": ["platform:grants:read"]
    }
  ],
  "effective_scopes": [
    "inference:anthropic:invoke",
    "mcp:github:read",
    "platform:agents:read",
    "platform:grants:read"
  ],
  "evaluated_at": "2026-04-14T10:30:00Z"
}
```

The principal to inspect is specified via `principal_type` and `principal_id` query parameters. When omitted, the endpoint falls back to the authenticated principal's own identity from the JWT claims. Inspecting another principal requires `platform:grants:read`.

**Scope required:** `platform:grants:read`

#### 1d. Policy Gateway: `POST /v1/policy/check/simulate`

The existing `POST /v1/policy/check` endpoint accepts a raw platform JWT in its request body — it is designed for service-to-service use where the caller already has the principal's token. The dashboard evaluate page needs a different flow: an admin specifies a principal by type + ID and tests whether they would be allowed to perform an action, without possessing that principal's token.

The new `/check/simulate` endpoint accepts `principal_type` + `principal_id` instead of a raw token. The gateway resolves the principal's effective scopes internally (via the same OPA evaluation used by `/scopes/explain`) and runs the check against those scopes.

```
POST /v1/policy/check/simulate
Authorization: Bearer <platform-token>
Content-Type: application/json

{
  "principal_type": "AGENT",
  "principal_id": "agt_01jt7m4kx2n8pq5rv6w",
  "resource": {
    "type": "inference",
    "path_segments": ["anthropic", "claude-sonnet-4-6"]
  },
  "action": "invoke"
}

→ 200 OK
{
  "allow": true,
  "reason": "allow"
}
```

This is a POST because it performs an evaluation (side-effect-free but semantically a computation, consistent with the existing `/check` endpoint). The existing `/check` endpoint is unchanged — services continue to use it with raw tokens.

**Scope required:** `platform:grants:read`

### 2. BFF Proxy Architecture

#### New environment variable

```
POLICY_SERVICE_URL=http://policy-service.orbit.svc:8083
```

Added to `frontend/src/server/config.ts`:

```typescript
policyServiceUrl: requireEnv('POLICY_SERVICE_URL').replace(/\/$/, ''),
```

#### BFF API Routes

Each resource type gets its own Next.js route handler under `app/api/policy/`. All handlers follow the same pattern: validate session, extract `accessToken`, proxy to the appropriate backend, return the response.

| BFF Route | Method(s) | Proxies To | Backend |
|---|---|---|---|
| `/api/policy/stats` | GET | `/api/v1/policy/stats` | policy-service |
| `/api/policy/grants` | GET, POST | `/api/v1/policy/grants` | policy-service |
| `/api/policy/grants/[id]` | GET, DELETE | `/api/v1/policy/grants/{id}` | policy-service |
| `/api/policy/roles` | GET, PUT | `/api/v1/policy/roles` | policy-service |
| `/api/policy/roles/[role]` | GET, DELETE | `/api/v1/policy/roles/{role}` | policy-service |
| `/api/policy/vocabulary` | GET | `/api/v1/policy/vocabulary` | policy-service |
| `/api/policy/routes` | GET | `/api/v1/policy/routes` | policy-service |
| `/api/policy/check` | POST | `/v1/policy/check/simulate` | policy-gateway |
| `/api/policy/scopes/explain` | GET | `/v1/policy/scopes/explain` | policy-gateway |

Query parameters (`tenant_id`, `principal_type`, `principal_id`, `resource_type`, `limit`, `offset`) are forwarded as-is. The BFF injects the `Authorization: Bearer <accessToken>` header from the sealed session and the `X-Tenant-ID` header from `session.tenantId`.

#### ApiRoute Registry

Added to `frontend/src/lib/routing/api-routes.ts`:

```typescript
policy: {
  stats:      { GET: () => '/api/policy/stats' },
  grants:     { GET: (params?) => '/api/policy/grants' + qs(params),
                POST: () => '/api/policy/grants' },
  grant:      { GET: (id) => `/api/policy/grants/${id}`,
                DELETE: (id) => `/api/policy/grants/${id}` },
  roles:      { GET: (params?) => '/api/policy/roles' + qs(params),
                PUT: () => '/api/policy/roles' },
  role:       { GET: (role, tenantId) => `/api/policy/roles/${role}?tenant_id=${tenantId}`,
                DELETE: (role, tenantId) => `/api/policy/roles/${role}?tenant_id=${tenantId}` },
  vocabulary: { GET: (params?) => '/api/policy/vocabulary' + qs(params) },
  routes:     { GET: () => '/api/policy/routes' },
  check:      { POST: () => '/api/policy/check' },
  explain:    { GET: (params?) => '/api/policy/scopes/explain' + qs(params) },
},
```

### 3. Frontend Routing and Navigation

#### Route definitions

Added to `APP_ROUTES` in `routes.ts`, following the model-gateway pattern:

```typescript
policy: {
  overview:   '/policy/overview',
  evaluate:   '/policy/evaluate',
  grants:     '/policy/grants',
  roles:      '/policy/roles',
  vocabulary: '/policy/vocabulary',
  routes:     '/policy/routes',
},
```

The existing `policy: '/policy'` flat entry is replaced with this nested object. The root `/policy` page redirects to `/policy/overview`.

#### Sidebar navigation

A new `POLICY_NAV` array in `nav-entries.ts`, added as a nav group:

```typescript
const POLICY_NAV: NavEntry[] = [
  { label: 'Overview',   href: APP_ROUTES.policy.overview,   icon: BarChart3,    requiredScope: 'platform:grants:read' },
  { label: 'Evaluate',   href: APP_ROUTES.policy.evaluate,   icon: FlaskConical, requiredScope: 'platform:grants:read' },
  { label: 'Grants',     href: APP_ROUTES.policy.grants,     icon: ShieldCheck,  requiredScope: 'platform:grants:read' },
  { label: 'Roles',      href: APP_ROUTES.policy.roles,      icon: Users,        requiredScope: 'platform:roles:read' },
  { label: 'Vocabulary', href: APP_ROUTES.policy.vocabulary,  icon: BookOpen,     requiredScope: 'platform:grants:read' },
  { label: 'Routes',     href: APP_ROUTES.policy.routes,     icon: Route,        requiredScope: 'platform:grants:read' },
];
```

The existing single "Policy Engine" entry in `SECURITY_NAV` is removed. `POLICY_NAV` becomes its own nav group titled "Policy Engine" in `NAV_GROUPS`, positioned after "Security".

#### File structure

```
src/app/(dashboard)/policy/
├── layout.tsx                    # ScopeGuard wrapper for all /policy/* routes
├── page.tsx                      # redirect('/policy/overview')
├── overview/
│   ├── page.tsx                  # RSC — fetches stats server-side
│   └── loading.tsx               # Skeleton
├── evaluate/
│   ├── page.tsx                  # Client component — interactive forms
│   └── loading.tsx
├── grants/
│   ├── page.tsx                  # RSC — fetches grants list server-side
│   └── loading.tsx
├── roles/
│   ├── page.tsx                  # RSC — fetches roles list server-side
│   └── loading.tsx
├── vocabulary/
│   ├── page.tsx                  # RSC — fetches vocabulary server-side
│   └── loading.tsx
├── routes/
│   ├── page.tsx                  # RSC — fetches routes server-side
│   └── loading.tsx
├── _components/
│   ├── overview-panel.tsx        # Stats cards, bundle sync status
│   ├── evaluate-panel.tsx        # Effective permissions + policy check forms
│   ├── grants-table.tsx          # Grant list with filters, pagination
│   ├── grant-create-modal.tsx    # Create grant form with grouped scope picker
│   ├── grant-detail-panel.tsx    # Slide-out detail view
│   ├── grant-revoke-confirm.tsx  # Revoke confirmation dialog
│   ├── roles-grid.tsx            # Role cards grid (reserved + custom)
│   ├── role-upsert-modal.tsx     # Create/edit role form
│   ├── role-delete-confirm.tsx   # Delete confirmation dialog
│   ├── vocabulary-browser.tsx    # Grouped scope tables with search
│   └── routes-table.tsx          # Route registrations table
├── _services/
│   ├── policy-server.ts          # Server-side fetch helpers (RSC data loading)
│   └── policy-client.ts          # Client-side fetch helpers (mutations, refetches)
└── _types/
    ├── api.ts                    # Wire types — mirror Go JSON response shapes
    └── index.ts                  # UI-derived types (table rows, form state, etc.)
```

### 4. Hades Design System Components

Three new components added to the Hades package for reuse across features:

#### 4a. ScopeChip

Renders a scope string with color-coded segments. The resource type is tinted with the accent color, separators are dimmed, and the action segment uses the foreground color. Monospace font, compact sizing.

```tsx
<ScopeChip scope="inference:anthropic:invoke" />
```

Props: `scope: string`, `size?: 'sm' | 'md'`, `onClick?: () => void`

#### 4b. PrincipalBadge

Renders a principal type label with a tinted background. Color varies by type:
- AGENT: blue
- HUMAN: green
- SERVICE: amber

```tsx
<PrincipalBadge type="AGENT" />
```

Props: `type: 'AGENT' | 'HUMAN' | 'SERVICE'`

#### 4c. GroupedPicker

A data-agnostic multi-select picker that renders items organized into collapsible groups with checkboxes. Includes search/filter input and "select all in group" toggles. The component knows nothing about scopes, policies, or any specific API — it operates on a generic `GroupedPickerGroup` data shape.

```tsx
type GroupedPickerItem = {
  value: string;
  label: string;
  description?: string;
};

type GroupedPickerGroup = {
  key: string;
  label: string;
  items: GroupedPickerItem[];
};

<GroupedPicker
  groups={groups}
  selected={selectedValues}
  onChange={setSelectedValues}
/>
```

Props: `groups: GroupedPickerGroup[]`, `selected: string[]`, `onChange: (values: string[]) => void`, `disabled?: boolean`, `placeholder?: string`

The policy feature code in `_services/policy-client.ts` is responsible for fetching vocabulary from the BFF and transforming the `ScopeEntry[]` response into `GroupedPickerGroup[]` — grouping by `resource_type`, mapping `scope` to `value`, `description` to `description`, etc. This transformation is domain logic and belongs in the feature module, not in Hades.

### 5. Type Ownership

Policy types are **feature-local**, not shared. The `shared/api.ts` file is reserved for types that are genuinely consumed by multiple features (models, health, PATs). Policy types are only consumed by the policy feature and belong in its `_types/` directory.

**Type location rule:** Wire types (matching backend JSON shapes exactly) go in `_types/api.ts`. UI-derived types (table row transforms, form state, component props, picker adapters) go in `_types/index.ts`.

#### `_types/api.ts` — Wire Types

```typescript
// ── Wire types — mirror Go JSON response shapes exactly ─────────────────────

export type PrincipalType = 'AGENT' | 'HUMAN' | 'SERVICE';

export interface Grant {
  id: string;
  tenant_id: string;
  principal_type: PrincipalType;
  principal_id: string;
  scopes: string[];
  created_at: string;
}

export interface CreateGrantRequest {
  tenant_id: string;
  principal_type: PrincipalType;
  principal_id: string;
  scopes: string[];
}

export interface GrantsListResponse {
  grants: Grant[];
  total: number;
}

export interface RoleEntry {
  id: string;
  tenant_id: string;
  role: string;
  scopes: string[];
  created_at: string;
}

export interface UpsertRoleRequest {
  tenant_id: string;
  role: string;
  scopes: string[];
}

export interface RolesListResponse {
  roles: RoleEntry[];
  total: number;
}

export interface ScopeEntry {
  scope: string;
  resource_type: string;
  path_segments: string[];
  action: string;
  description: string;
  grant_count: number;
  role_count: number;
  created_at: string;
}

export interface VocabularyResponse {
  scopes: ScopeEntry[];
}

export interface RouteScope {
  path_prefix: string;
  method: string;
  required_scope: string;
}

export interface RouteRegistration {
  service: string;
  tenant_id: string;
  routes: RouteScope[];
  registered_at: string;
}

export interface RoutesListResponse {
  registrations: RouteRegistration[];
}

export interface PolicyStatsResponse {
  grants: { total: number; by_principal_type: Record<PrincipalType, number> };
  roles: { total: number; reserved: number; custom: number };
  vocabulary: { total: number; by_resource_type: Record<string, number> };
  routes: { total: number };
  bundles: {
    tenants: Array<{
      tenant_id: string;
      last_compiled_at: string;
      status: 'current' | 'stale';
    }>;
  };
}

export interface SimulateCheckRequest {
  principal_type: PrincipalType;
  principal_id: string;
  resource: { type: string; path_segments?: string[]; attributes?: Record<string, string> };
  action: string;
}

export interface CheckResponse {
  allow: boolean;
  reason: string;
}

export interface ScopesExplainResponse {
  principal_type: PrincipalType;
  principal_id: string;
  tenant_id: string;
  from_roles: Array<{ role: string; scopes: string[] }>;
  from_grants: Array<{ grant_id: string; scopes: string[] }>;
  effective_scopes: string[];
  evaluated_at: string;
}
```

#### `_types/index.ts` — UI-Derived Types

UI-specific types such as table row models, filter state, form validation shapes, and the `ScopeEntry[]` → `GroupedPickerGroup[]` adapter types live here. These are defined during implementation as component needs emerge.

If a policy type is later needed by another feature (e.g., `PrincipalType` used by an agents dashboard), it is promoted to `shared/api.ts` at that point — not preemptively.

### 6. Scope Gating

| Page / Action | Required Scope | Enforcement |
|---|---|---|
| All `/policy/*` pages | `platform:grants:read` | `ScopeGuard` in `policy/layout.tsx` |
| Roles page | `platform:roles:read` | Additional `ScopeGuard` in `roles/page.tsx` |
| Create grant button | `platform:grants:write` | `disabled` prop + `hasScope()` |
| Revoke grant action | `platform:grants:write` | `disabled` prop + `hasScope()` |
| Create/edit role button | `platform:roles:write` | `disabled` prop + `hasScope()` |
| Delete role action | `platform:roles:write` | `disabled` prop + `hasScope()` |
| Evaluate — policy check | `platform:grants:read` | Page guard is sufficient |
| Vocabulary — read-only | `platform:grants:read` | Page guard is sufficient |
| Routes — read-only | `platform:grants:read` | Page guard is sufficient |

Backend enforces all of these independently. Frontend gating is advisory UX.

### 7. Error Handling

All pages follow the model-gateway error patterns:

- **Server-side fetch failures** (RSC): return a normalized `{ data: null, error: string }` result. The page component renders an inline error state with a retry prompt — never an unhandled exception.
- **Client-side mutation failures**: display a toast or inline error message with the upstream error reason. Mutations (create grant, revoke grant, upsert role, delete role) surface the backend's error message directly when it is a validation error (e.g., "scope not in vocabulary"), and show a generic "something went wrong" for unexpected failures.
- **Service unavailable**: if the policy-service or policy-gateway is unreachable, the BFF returns a 502. Pages show a "Policy service unavailable" state with a retry button, consistent with how the model-gateway handles inference-gateway outages.
- **403 from backend**: should not happen if frontend gating is correct, but if it does (stale scope cache), show the standard forbidden state from `ScopeGuard`.

### 8. Bundle Staleness Semantics

A bundle's status in the `/stats` response is determined by comparing the most recent grant, role, or route registration write timestamp against the bundle's `last_compiled_at` timestamp:

- **`current`**: `last_compiled_at >= max(last_grant_write, last_role_write, last_route_registration)` — the bundle reflects all known policy state.
- **`stale`**: `last_compiled_at < max(last_grant_write, last_role_write, last_route_registration)` — a write has occurred since the last compilation. This should be transient (the compiler's 500ms debounce window), but if it persists, it indicates a compilation failure.

The overview page displays stale bundles with a warning indicator. This is informational — the debounce window means a brief stale state after any write is normal and expected.

---

## Implementation Order

The implementation is structured in four phases. Each phase produces working, testable functionality. All phases use live API data.

### Phase 1: Foundation (Backend Endpoints + BFF + Types + Hades Components)

**Goal:** Establish all infrastructure before building any pages.

1. **Backend: `GET /api/v1/policy/stats` endpoint** on policy-service
2. **Backend: Vocabulary usage counts** — add `grant_count` and `role_count` to vocabulary list response
3. **Backend: `GET /v1/policy/scopes/explain` endpoint** on policy-gateway
4. **Backend: `POST /v1/policy/check/simulate` endpoint** on policy-gateway
5. **Frontend: Add `POLICY_SERVICE_URL`** to server config
6. **Frontend: Policy wire types** in `policy/_types/api.ts`
7. **Frontend: `ApiRoute.policy.*`** entries in `api-routes.ts`
8. **Frontend: BFF proxy route handlers** under `app/api/policy/`
9. **Frontend: `APP_ROUTES.policy.*`** nested routes in `routes.ts`
10. **Frontend: `POLICY_NAV`** sidebar nav group in `nav-entries.ts`
11. **Frontend: Hades components** — `ScopeChip`, `PrincipalBadge`, `GroupedPicker`
12. **Frontend: Policy `_types/index.ts`** and `_services/` (server + client fetch helpers)

### Phase 2: Overview + Core CRUD (Overview + Grants + Roles)

**Goal:** The landing page works immediately, and administrators can manage grants and roles.

The overview page is built first — it is the redirect target for `/policy` and consumes the `/stats` endpoint from Phase 1. It is simpler than the CRUD pages (read-only stat cards) and gives immediate visual feedback that the foundation is working.

1. **Overview page** — stat cards (grants, roles, vocabulary, routes counts), bundle sync status per tenant
2. **Grants page** — table with search, filters (principal type, scope category), pagination
3. **Grant create modal** — form with principal type selector, ID input, grouped scope picker
4. **Grant detail panel** — slide-out with full grant info and scopes
5. **Grant revoke confirm** — destructive confirmation dialog with impact warning
6. **Roles page** — card grid with reserved roles (read-only) and custom roles (editable)
7. **Role upsert modal** — create/edit form with name input and grouped scope picker
8. **Role delete confirm** — destructive confirmation with principal impact count

### Phase 3: Read-Only Views (Vocabulary + Routes)

**Goal:** Administrators can browse the scope vocabulary and route registrations.

1. **Vocabulary page** — grouped tables by resource type, search filter, chip row filter, click-to-copy
2. **Routes page** — registration table with method badges, service grouping, info banner

### Phase 4: Evaluate

**Goal:** Policy evaluation and inspection tooling.

1. **Evaluate page** — two-panel layout:
   - **Effective permissions:** principal lookup → scope breakdown by source (roles vs. grants) via `/scopes/explain`
   - **Policy check:** dry-run form with principal type + ID, resource, and action → allow/deny result with reason via `/check/simulate`

---

## Consequences

### Positive

- Tenant administrators can self-serve policy management without API tooling
- Effective permissions inspection reduces support burden for debugging access issues
- Policy check dry-run enables safe testing before committing changes
- Vocabulary browser provides discoverability for available scopes
- Follows established frontend patterns — minimal new conventions to learn
- All data is live — no risk of UI/backend divergence from stale mocks

### Negative

- Six new pages and associated components increase the frontend surface area
- Four new backend endpoints need implementation and testing
- `GroupedPicker` is a complex interactive component that needs careful UX attention
- Overview stats query may need optimisation if grant/role tables grow large (index coverage)

### Neutral

- The BFF gains a fourth backend URL (`POLICY_SERVICE_URL`), consistent with existing pattern
- Hades gains three new components, expanding the design system's scope
- The sidebar nav adds a new group with six items, matching the model-gateway group size

---

## Amendment 1 (2026-10-22): Human-only grant creation and platform-owned grants

**Status:** Accepted

Touches *What can it access?* and *Who is this agent?* Grants for a HUMAN principal are still written by tenant admins. Grants for any other principal type (AGENT, SERVICE, PAT) can now be written only by a SYSTEM caller: the platform services (orbit-agents, orbit-triggers, identity) that own those identities. What a grant allows is unchanged. Tracked in [#443](https://github.com/runkora/kora/issues/443).

**Context.** The grants page offers a principal type selector and a free-text principal ID (Phase 2, "Grant create modal"). For three of the four types that produces a grant that is wrong or short-lived:

- **AGENT.** The form asks for an agent UUID. OPA matches agent grants on `spiffe://<td>/tenant/<ns>/agent/<slug>`, so a grant created here never matches a request. With a correct ID, `ensureAgentGrant` (`backend/internal/agents/k8s.go`) deletes the extra grant on the next agent save.
- **SERVICE.** A SERVICE grant is a trigger identity. orbit-triggers creates it with fixed scopes (`backend/internal/triggers/`) and deletes only the grant IDs it recorded. A manual one extends a trigger beyond its one agent and outlives the trigger.
- **PAT.** The identity PAT service creates and replaces a PAT's grant when its scopes change (`backend/internal/identity/pat_service.go`). A manual write bypasses the token flow.

**Decision.**

*Grants page*

- Creating a grant is a human-only wizard. It offers no other principal type.
- AGENT, SERVICE and PAT grants stay in the list with no revoke action, each linking to its owner: the agent builder, the trigger page, or the token screens. Editing an agent grant from this page is out of scope. The Grants tab under Model Gateway Budgets lists the same grants and likewise offers no edit-budget action on them.
- `PrincipalBadge` shows the `SERVICE` principal type as "Trigger". The wire value stays `SERVICE`.

*Policy-service rule*

- Only a SYSTEM caller may create a grant whose `principal_type` is not `HUMAN`. Delete and set-budget look up the grant and apply the same check to its stored `principal_type`. Reads are unchanged. A write by any other caller returns 403, tenant admins included.
- SYSTEM is the only principal type that may write them. Any SYSTEM caller qualifies; there is no per-component allowlist. A SERVICE principal is refused: it is a tenant-scoped identity (`/tenant/<id>/service/<name>`, or a trigger source's token), the kind of identity these grants protect, not a platform component. Platform components such as orbit-agents and tool-gateway present SYSTEM (`/system/<component>`), not SERVICE.
- This is the same SYSTEM-versus-the-rest split `enforceScopeCeiling` and `resolveTenantID` already use. The three platform writers each present SYSTEM: identity mints it with `SystemToken`, and orbit-triggers and orbit-agents exchange their SVID through `SelfTokenSource` for a token whose principal comes from `/system/<component>`.
- The dashboard BFF and the orbit MCP tools (`policy.grants.create`, `policy.grants.delete`, `policy.grants.set_budget`) call the same endpoints as a person, so the rule covers them with no change to either. The `policy.grants.create` tool's `principal_type` enum narrows to `HUMAN` so it no longer advertises types the API refuses.
- PAT grants are unaffected where they are written properly: the model-gateway keys screen and `pats.update_scopes` go to the identity service (`/identity/tokens`), which writes the grant as SYSTEM. Only a direct write to the policy-service grants API is refused.

**Consequences.** A grant of any non-human type can no longer be written through the dashboard or an MCP tool, including to clean up one a failed trigger or agent cleanup left behind; only a SYSTEM caller can remove it. Set-budget follows the same rule, so a per-grant budget on those grants, including a trigger's per-run limit, is set only by a SYSTEM caller; the cap an admin keeps over PAT and trigger spend is the tenant cap. Grants of those types already created by hand are untouched and become read-only on the page. The Phase 2 create modal and revoke action, and the "full CRUD for grants" goal, now apply to HUMAN grants only. A new platform component that writes grants must call the policy-service as SYSTEM or its writes return 403.

---

## Related Decisions

- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy gateway and authorisation model
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Dynamic route-scope registration
- [frontend-policy-projection](2026-04-14-frontend-policy-projection.md) — Frontend policy projection via BFF
- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — Browser auth flow and BFF session architecture
- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) — Hades frontend design system
- [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — The provisioner writes agent grants
- [agent-triggers-and-webhook-ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) — orbit-triggers writes trigger SERVICE grants
- [pat-scope-assignment-via-explicit-grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) — The identity PAT service writes PAT grants

---

**Last Updated:** 2026-10-22 (Amendment 1: human-only grant creation and platform-owned grants)
