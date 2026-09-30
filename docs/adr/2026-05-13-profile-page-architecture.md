# Profile Page Architecture

**Status:** Proposed
**Date:** 2026-05-13
**Deciders:** Frontend Team, Platform Engineering
**Technical Story:** [Profile Page PRD](../prd/profile-page.md) — kora has no centralised view where users can inspect their identity, permissions, or API tokens. The profile page gives every authenticated user a single screen answering "who am I, what can I do, and what tokens have I issued?" — accessible from a popover menu in the sidebar footer.

---

## Context

### Background

The kora control plane frontend follows the BFF (Backend for Frontend) architecture with iron-session sealed cookies (ADR-0030). Permissions are projected from OPA into the session as a cached scope set (ADR-0037). User profiles are persisted on login via the identity service's `user_profiles` table (ADR-0042). Personal access tokens (PATs) are listed via the identity service's `GET /identity/tokens` endpoint (ADR-0033).

The frontend golden path pattern (`frontend/docs/golden-paths/frontend-state-pattern.md`) defines a 10-step structure for data-driven dashboard pages: types → server service → BFF route → API route registry → client service → hook → context → tab shell → RSC page → feature components. This pattern is designed for pages with client-side state management, mutations, and reactive updates.

These building blocks exist but have no user-facing surface. Users cannot inspect their principal ID, permission breakdown, or API tokens without decoding JWTs manually or asking an admin.

### Problem Statement

Users need a self-service view of their identity, permissions, and API tokens. The sidebar footer currently shows email and name alongside a standalone logout button, with no navigation to any profile or account page.

Two architectural questions must be resolved:
1. Which golden path steps does a read-only page with no client-initiated state require?
2. How should the profile page consume data from endpoints already used by other features (policy explain, PAT listing) without duplicating server-side API call logic?

### Goals

- Provide a read-only profile page showing user identity, permissions breakdown, and API keys
- Replace the standalone logout button with a popover menu that provides navigation to profile, future settings, and logout
- Fetch all data server-side from existing APIs — no new backend endpoints required
- Handle individual API failures gracefully without blocking the entire page
- Follow established frontend conventions (Terra enforcement, component tiers, file naming, lint rules)

### Non-Goals

- Profile editing (identity is managed by the upstream OIDC provider)
- API key creation or revocation (deferred to Phase 2)
- Session listing and revocation (deferred to Phase 2 — requires new backend endpoints)
- New backend endpoints or BFF routes

---

## Decision Drivers

* **Reuse existing infrastructure** — session, identity service, policy explain, and PAT listing endpoints all exist. The profile page composes them; it does not extend them.
* **Server-side data fetching** — access tokens must never reach the browser (BFF pattern). All API calls use the server-side `orbitClient`.
* **Independent error boundaries** — the three data sources (session, policy gateway, identity service) have different availability profiles. A policy gateway outage should not prevent the user from seeing their identity or API keys.
* **Feature boundary isolation** — per the golden path pattern, each feature owns its client-initiated data lifecycle (Steps 3–8: BFF route, API route registry, client service, hook, context, tab shell) and its components (Step 10). Server-side API call helpers that wrap backend endpoints are infrastructure, not feature-specific concerns — they belong in a shared location when multiple features call the same endpoint.
* **Terra component enforcement** — all UI must use Terra (`@orbit/terra`) or app-shared (`@orbit/components`) primitives. Raw HTML is banned by ESLint with zero escape hatches (`AGENTS.md` § Frontend Development).
* **Golden path scoping** — the golden path's Steps 3–8 exist for client-initiated data fetching and state management (BFF routes, API route registry, client services, hooks, context, tab shells). A read-only page with no client-initiated data fetching correctly omits these steps while still following the golden path (Steps 1, 2, 9, 10).

---

## Considered Options

### Option A: All 10 golden path steps regardless of need

**Description:** Implement every golden path step: types → server service → BFF route → API route registry → client service → hook → context → tab shell → RSC page → feature components. Create BFF routes at `/api/profile/identity`, `/api/profile/permissions`, `/api/profile/tokens` for client-side refetching.

**Pros:**
- All 10 steps present — identical file structure to data-driven pages (models, logs, policy)
- Built-in client-side refresh via hook's `refresh()` function
- Steps 3–8 already in place when Phase 2 adds mutations

**Cons:**
- Creates 3 BFF routes, a client service, a hook, a context, and a tab shell for a page that has no client-initiated data fetching
- The profile page is read-only — no mutations, no polling, no filters, no pagination
- Steps 3–8 exist to manage client-side state lifecycle; the profile page has no client-side state
- More code to write and maintain for no functional benefit in Phase 1

### Option B: Golden path Steps 1, 2, 9, 10 — omit Steps 3–8 (chosen)

**Description:** The `/profile` page follows the golden path, implementing the steps that serve a purpose: types (Step 1), server service (Step 2), RSC page (Step 9), and feature components (Step 10). Steps 3–8 (the client-initiated data lifecycle: BFF route, API route registry, client service, hook, context, tab shell) are omitted because the page has no client-initiated data fetching. Data is fetched server-side via `Promise.all` and passed as props to Tier 3 client components that handle only local UI state (copy-to-clipboard, expand/collapse).

**Pros:**
- Follows the golden path — only the steps that apply to a read-only page
- Zero client-side JavaScript for data fetching; page is fully rendered server-side
- Parallel fetches minimise total load time
- Each card receives its own result and handles errors independently
- Establishes a reusable decision rule: read-only pages don't need the client-side stack (Steps 3–8)

**Cons:**
- No client-side refresh without full page reload (acceptable for read-only data that changes infrequently)
- Phase 2 mutations (API key management) will require implementing Steps 3–8 at the page level — the page already follows the golden path, Phase 2 adds the client-side stack

### Option C: Hybrid — server fetch with per-card client refresh

**Description:** Server component for initial fetch (like Option B), but each card is wrapped in a client component that can independently re-fetch via a BFF route on user action (e.g. a "Refresh" button).

**Pros:**
- Per-card refresh without full page reload

**Cons:**
- Requires BFF routes for each data source, increasing the surface area
- Over-engineered for Phase 1 — refresh is not a stated requirement
- Per-card refresh complicates error handling (must handle both server-rendered errors and client-fetched errors)

---

## Decision

**Chosen Option: B — Golden path Steps 1, 2, 9, 10 — omit Steps 3–8**

### Rationale

The profile page follows the golden path. It implements Steps 1 (types), 2 (server service), 9 (RSC page), and 10 (feature components). Steps 3–8 (the client-initiated data lifecycle: BFF route, API route registry, client service, hook, context, tab shell) are correctly omitted because their purpose — client-initiated data fetching and state management — does not apply to a read-only page with no mutations, polling, filters, or client-initiated refetches.

This follows the established pattern used by all six policy feature pages (grants, roles, evaluate, overview, routes, vocabulary), which fetch server-side and pass props directly to tab components without implementing Steps 3–8. The golden path's Step 9 (RSC page) supports this directly — Steps 3–8 are added when a page needs client-initiated data fetching and state management. Only pages with complex client-side state (models, logs, analytics, my-usage) implement the full 10-step stack. The decision rule generalises cleanly: **read-only pages don't need the client-initiated data lifecycle.**

When Phase 2 adds API key mutations, the page adopts Steps 3–8 at the page level — a `useProfile` hook, `ProfileProvider` context, and tab shell wrapping the entire page. Individual cards (including the still-read-only identity and permissions cards) consume data from the page-level context rather than receiving props directly from the RSC.

### Server-Side API Helpers: Shared Infrastructure

The golden path's feature boundary convention governs client-initiated data fetching and state management (Steps 3–8) and component ownership (Step 10). Server-side API call helpers — functions that wrap `GET` requests to backend services — are infrastructure, not feature-specific concerns. When multiple features call the same backend endpoint, duplicating the helper creates a maintenance hazard without meaningful isolation benefit.

The profile page calls three backend endpoints:

| Endpoint | Defined in | Consumers | Approach |
|----------|-----------|-----------|----------|
| `GET /identity/users/{id}` | ADR-0042 (not yet consumed in frontend) | 0 (profile page will be first consumer) | New: `profile/_services/profile-server.ts` (single consumer) |
| `GET /policy/scopes/explain` | `policy/_services/policy-server.ts` | 2 routes | Promote to `src/server/services/policy-gateway-server.ts` |
| `GET /identity/tokens` | `model-gateway/_services/tokens-server.ts` | 4 routes | Promote to `src/server/services/identity-server.ts` |

> **Prerequisite:** `GET /identity/users/{id}` is defined in ADR-0042 but currently only the list variant (`GET /identity/users?ids=...`) is consumed in the frontend. Confirm the single-resource endpoint is implemented in the identity service backend before this work begins.

**Promotion strategy:**

- `listPATsFromIdentity()` and its `ListPATsResult` type are extracted from `model-gateway/_services/tokens-server.ts` into `src/server/services/identity-server.ts`. The existing return type (`{ ok: true; tokens: PAT[] }`) is preserved — normalising to the generic `Result<T>` would require updating all existing consumers for no functional benefit. Both the model-gateway feature and the profile page import from this shared module. Feature-specific PAT CRUD functions (`createPATInIdentity`, `revokePATInIdentity`, etc.) remain in `model-gateway/_services/tokens-server.ts`. Note: `fetchUsersByIDs` and `searchUsers` also call identity service endpoints (`GET /identity/users`). `searchUsers` is already consumed cross-feature by the policy grants tab (`policy/_actions/grants.ts`, `policy/_components/grants-tab.tsx`), making it an existing candidate for promotion to `src/server/services/identity-server.ts`. This promotion is deferred to avoid scope creep but should be addressed in a follow-up to eliminate the cross-feature `_services/` import.
- `fetchScopesExplain()` (signature: `(accessToken: string, principalType?: string, principalId?: string)`) and its internal `policyGatewayFetch` helper are extracted from `policy/_services/policy-server.ts` into `src/server/services/policy-gateway-server.ts`. Both the policy feature and the profile page import from this shared module. Other PDP functions that use `policyGatewayFetch` (e.g., `checkSimulate`) will also import it from the shared module. Only PAP functions using the separate `policyServiceFetch` helper (e.g., `fetchGrants`, `upsertRole`) are unaffected and remain in `policy/_services/policy-server.ts`.
- `fetchUserProfile()` remains in `profile/_services/profile-server.ts` — there is only one consumer.

This approach preserves feature isolation where it matters (client-side state, components) while eliminating duplication of server-side infrastructure. Cross-feature imports between `_services/` directories are still prohibited — the shared location (`src/server/services/`) is a neutral, feature-agnostic layer.

---

## Design

### Route Structure

```
frontend/src/app/(dashboard)/profile/
├── page.tsx                          # Server Component — session + parallel fetch
├── loading.tsx                       # Suspense skeleton (mirrors real layout)
├── _services/
│   └── profile-server.ts            # Server-only: fetchUserProfile + re-exports listPATsFromIdentity, fetchScopesExplain
├── _types/
│   └── index.ts                     # Feature-local view types
└── _components/
    ├── user-identity-card.tsx        # Tier 3 client component
    ├── roles-permissions-card.tsx    # Tier 3 client component
    └── api-keys-card.tsx             # Tier 3 component (server — no client interactivity)
```

The route is nested under the `(dashboard)` layout group, inheriting the sidebar, session validation, and `PermissionsProvider` context.

### Sidebar User Menu

The existing `SidebarUserFooter` client component (`(dashboard)/_components/sidebar-user-footer.tsx`) is modified to wrap the user info area in a Terra `Popover` trigger. The current component renders an `Icon`, email `Text`, name `Text`, and `LogoutButton` in a horizontal `Stack`.

The modification:
1. Wrap the `Stack` (minus `LogoutButton`) in a `Popover` trigger
2. Move `LogoutButton` into the popover content, separated by a `Separator`
3. Add "View Profile" as an `AppLink` (`@orbit/components/links`) — type-safe internal navigation via the `APP_ROUTES` registry — and "Settings" as disabled text

| Menu item | Behaviour |
|-----------|-----------|
| View Profile | `<AppLink to="/profile">` — type-safe internal navigation via `APP_ROUTES` registry |
| Settings | Rendered as disabled `Text` (no `/settings` route exists yet) |
| Log out | Existing `LogoutButton` component, separated by `Separator` |

### Server Service

`profile/_services/profile-server.ts` — the profile feature's server-side data access module. It imports shared helpers for PAT listing and permissions explain, and defines only `fetchUserProfile()` locally (no other consumer).

`fetchUserProfile()` calls `GET /identity/users/{id}` (ADR-0042) to retrieve the backend-persisted identity record — the canonical source of truth for user display attributes. The session also carries `user.sub`, `user.email`, `user.name`, and `tenantId`, which serve as the fallback when the profile fetch fails. Following the established golden path Step 2 pattern, the page fetches from the backend service rather than relying solely on session state:

```typescript
import 'server-only';

import { ApiError, orbitClient } from '@orbit/server/client';

// ── Shared imports (promoted to src/server/services/) ────────────────────────

export { listPATsFromIdentity } from '@orbit/server/services/identity-server';
export { fetchScopesExplain } from '@orbit/server/services/policy-gateway-server';

// ── Result type (matches established convention) ─────────────────────────────

type Result<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

function fromApiError(err: unknown): { ok: false; status: number; message: string } {
  if (err instanceof ApiError) {
    return { ok: false, status: err.status, message: err.message };
  }
  return { ok: false, status: 502, message: 'Unexpected error communicating with backend service' };
}

// ── User profile (single consumer — lives here) ─────────────────────────────

// Matches ADR-0042 GET /identity/users/{id} response shape.
// tenant_id is implicitly scoped by the caller's tenant and available via session.tenantId.
// oidc_issuer is deferred until PRD Open Question #2 is resolved and a backing column exists.
interface UserProfileResponse {
  id: string;
  email: string;
  display_name: string;
}

export async function fetchUserProfile(
  accessToken: string,
  userId: string,
): Promise<Result<UserProfileResponse>> {
  try {
    const data = await orbitClient.get<UserProfileResponse>(
      `/identity/users/${userId}`,
      { Authorization: `Bearer ${accessToken}` },
    );
    if (!data) {
      return { ok: false, status: 502, message: 'Empty response from identity service' };
    }
    return { ok: true, data };
  } catch (err) {
    return fromApiError(err);
  }
}
```

### RSC Page

`profile/page.tsx` — follows the golden path Step 9 pattern (RSC page):

```typescript
// No 'use client' directive — this is a server component
import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchUserProfile, listPATsFromIdentity, fetchScopesExplain } from './_services/profile-server';
import { UserIdentityCard } from './_components/user-identity-card';
import { RolesPermissionsCard } from './_components/roles-permissions-card';
import { ApiKeysCard } from './_components/api-keys-card';

export default async function ProfilePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    // Redirect or show session-expired state
  }

  const [profileResult, explainResult, tokensResult] = await Promise.all([
    fetchUserProfile(session.accessToken, session.user.sub),
    fetchScopesExplain(session.accessToken),
    listPATsFromIdentity(session.accessToken),
  ]);

  return (
    <Stack gap="comfortable">
      <Heading variant="title">Profile</Heading>
      <UserIdentityCard
        session={session}
        profile={profileResult}
      />
      <RolesPermissionsCard
        explain={explainResult}
        cachedScopes={session.permissions.scopes}
      />
      <ApiKeysCard tokens={tokensResult} />
    </Stack>
  );
}
```

### Error Handling Strategy

Each card component receives its own result object (discriminated union) and handles errors independently:

| Card | Primary data | Fallback on failure |
|------|-------------|-------------------|
| User Identity | `fetchUserProfile()` (canonical backend record) | Session fields (`user.name`, `user.email`, `user.sub`, `tenantId`) always render as fallback. If the profile fetch succeeds, prefer its `display_name` and `email` over session values (backend record is source of truth). |
| Roles & Permissions | `fetchScopesExplain()` | Fall back to `session.permissions.scopes` (flat list, no role/grant breakdown). Show note: "Showing cached permissions — detailed breakdown unavailable." |
| API Keys | `listPATsFromIdentity()` | Show error state using `EmptyState` from `@orbit/components`. No fallback data available. |

### Component Architecture

All components use existing Terra (Tier 1) and app-shared (Tier 2) primitives. New components are Tier 3 (feature-scoped) with `@tier 3` JSDoc annotations.

**Reused Tier 2 components (`@orbit/components`):**

| Component | File | Used by profile card |
|-----------|------|---------------------|
| `ScopeChip` | `src/components/scope-chip.tsx` | `RolesPermissionsCard`, `ApiKeysCard` — renders scope strings as syntax-highlighted badges (resource type in accent, separator in subtle, action in foreground). Already used by the policy evaluate tab and model-gateway keys tab. |
| `StatusBadge` | `src/components/status-badge.tsx` | `ApiKeysCard` — renders PAT expiry status. The `PAT` type (`shared/api.ts`) has no `status` field; derive from `expires_at` using the existing `isExpired()` utility (`src/utils/format.ts`). Mapping: not expired → `status="healthy" label="Active"`, expired → `status="error" label="Expired"`. Note: the existing keys tab (`model-gateway/_components/keys-tab.tsx`) uses a raw Terra `Badge` for this; the profile page uses `StatusBadge` as the preferred Tier 2 abstraction. |
| `EmptyState` | `src/components/empty-state.tsx` | `ApiKeysCard` — zero-state when no PATs exist or the listing fails. |
| `CopyButton` | `src/components/copy-button.tsx` | `UserIdentityCard` — click-to-copy with visual feedback. Consolidates 5 existing clipboard implementations across model-gateway (`token-created-modal`, `model-detail-modal`, `setup-tab`), policy (`vocabulary-tab`), and chat (`message-content`). Adopts the `useRef` + `useEffect` timer cleanup pattern from `vocabulary-tab.tsx`. |

**Existing pattern — `ScopesExplainResponse` rendering:**

The policy evaluate tab (`policy/_components/evaluate-tab.tsx`) already renders the same `ScopesExplainResponse` shape that the profile page's `RolesPermissionsCard` will display: `from_roles` as cards with role name + `ScopeChip` list, `from_grants` as cards with grant ID + scope count, and `effective_scopes` as a flat `ScopeChip` list. The profile card should follow this established rendering pattern.

Both the evaluate tab and the profile page now render `ScopesExplainResponse` — if the rendering logic diverges or a third consumer appears, extract a shared `ScopesExplainBreakdown` Tier 2 component into `src/components/`. For Phase 1, the profile card pattern-matches the evaluate tab's layout without extracting a shared component (two consumers with different surrounding context — the evaluate tab is part of a larger simulate/explain flow, the profile card is standalone).

**Tier 3 — `profile/_components/`:**

| Component | Client? | Purpose | Key Terra/Tier 2 components |
|-----------|---------|---------|----------------------------|
| `UserIdentityCard` | Yes — copy-to-clipboard | Name, email, principal ID (copyable), tenant | `Card`, `CardHeader`, `CardContent`, `Stack`, `Text`, `Icon`, `Code`, `CopyButton` (Tier 2) |
| `RolesPermissionsCard` | Yes — expand/collapse | OIDC roles, direct grants, effective scopes | `Card`, `CardHeader`, `CardContent`, `Stack`, `Text`, `ScopeChip` (Tier 2), `Separator`, `Button` |
| `ApiKeysCard` | No | Read-only PAT listing | `Card`, `CardHeader`, `CardContent`, `Stack`, `Text`, `ScopeChip` (Tier 2), `StatusBadge` (Tier 2), `EmptyState` (Tier 2) |

All client components use `'use client'` as the first line. All components use Terra imports only — no raw HTML elements.

**Example JSDoc annotation:**

```typescript
/** @tier 3 — profile-scoped. Promote to src/components/ if a second feature needs a user identity card. */
```

### Loading State

`profile/loading.tsx` — Suspense skeleton that mirrors the real layout:

```typescript
import { Card, Skeleton, Stack } from '@orbit/terra';

export default function ProfileLoading() {
  return (
    <Stack gap="comfortable">
      <Skeleton width="12rem" height="2rem" />
      <Card><Skeleton height="10rem" /></Card>
      <Card><Skeleton height="14rem" /></Card>
      <Card><Skeleton height="10rem" /></Card>
    </Stack>
  );
}
```

---

## Consequences

### Positive

- Users can self-serve their principal ID, permission breakdown, and API key audit — reducing admin support burden
- No new backend endpoints or BFF routes — the feature is purely a frontend composition of existing APIs
- Shared server helpers (`src/server/services/`) eliminate duplication while preserving feature boundary isolation where it matters (client-side state, components)
- Server-side rendering means zero client JavaScript for data fetching; fast initial load
- Independent card error handling means a single API outage degrades one section, not the entire page
- The sidebar popover establishes the navigation pattern for future account-level pages (settings, sessions)
- Follows the established pattern for read-only pages: golden path without the client-initiated data lifecycle (Steps 3–8), consistent with how the six policy feature pages already work

### Negative

- Promoting `listPATsFromIdentity` and `fetchScopesExplain` to `src/server/services/` creates a shared dependency — changes to these helpers require checking all consumers (model-gateway, policy, profile).
- No client-side refresh — users must reload the page to see updated data. Acceptable for read-only content that changes infrequently.
- Phase 2 mutations (API key management) will require implementing Steps 3–8 at the page level (the client-initiated data lifecycle: BFF route, API route registry, client service, hook, context, tab shell).

### Neutral

- The profile page has no scope gate (`requiredScope`) — all authenticated users can view their own profile. Backend endpoints already scope data to the authenticated caller.
- The sidebar popover replaces the inline `LogoutButton` — the logout flow itself is unchanged.

---

## Implementation

### New Files

| Path | Purpose |
|------|---------|
| `frontend/src/server/services/identity-server.ts` | Shared: `listPATsFromIdentity()` and `ListPATsResult` promoted from model-gateway (new `src/server/services/` directory — shared server-side infrastructure layer) |
| `frontend/src/server/services/policy-gateway-server.ts` | Shared: `fetchScopesExplain()` and `policyGatewayFetch` promoted from policy |
| `frontend/src/app/(dashboard)/profile/page.tsx` | Profile page server component (RSC) |
| `frontend/src/app/(dashboard)/profile/loading.tsx` | Suspense skeleton |
| `frontend/src/app/(dashboard)/profile/_services/profile-server.ts` | Server-only: `fetchUserProfile()` + re-exports shared helpers |
| `frontend/src/app/(dashboard)/profile/_types/index.ts` | Feature-local view types |
| `frontend/src/app/(dashboard)/profile/_components/user-identity-card.tsx` | User identity card (Tier 3, client) |
| `frontend/src/app/(dashboard)/profile/_components/roles-permissions-card.tsx` | Roles & permissions card (Tier 3, client) |
| `frontend/src/app/(dashboard)/profile/_components/api-keys-card.tsx` | API keys card (Tier 3) |
| `frontend/src/components/copy-button.tsx` | Click-to-copy button (Tier 2, client) — consolidates 5 existing clipboard implementations across model-gateway, policy, and chat. Uses `useRef` + `useEffect` cleanup pattern from `vocabulary-tab.tsx`. |

### Modified Files

| Path | Change |
|------|--------|
| `frontend/src/app/(dashboard)/model-gateway/_services/tokens-server.ts` | Remove `listPATsFromIdentity()` and its result type; re-export from `@orbit/server/services/identity-server` |
| `frontend/src/app/(dashboard)/policy/_services/policy-server.ts` | Remove `fetchScopesExplain()` and `policyGatewayFetch()`; import `policyGatewayFetch` from `@orbit/server/services/policy-gateway-server` for remaining PDP functions (e.g., `checkSimulate`); re-export `fetchScopesExplain` from shared |
| `frontend/src/app/(dashboard)/_components/sidebar-user-footer.tsx` | Wrap user info in Terra `Popover` trigger; move `LogoutButton` into popover content; add "View Profile" `AppLink` and "Settings" (disabled) |
| `frontend/src/lib/routing/routes.ts` | Add `profile: '/profile'` to `APP_ROUTES` registry (required for `AppLink` type-safe routing) |
| `frontend/src/components/index.ts` | Add `CopyButton` export |

### Implementation Order

1. **Prerequisite check** — verify `GET /identity/users/{id}` is implemented in the identity service backend (defined in ADR-0042, but only the list variant `GET /identity/users?ids=...` is currently consumed in the frontend)
2. **Register profile route** — add `profile: '/profile'` to `APP_ROUTES` in `src/lib/routing/routes.ts` (required before sidebar popover work; enables type-safe `AppLink` navigation)
3. **Promote shared helpers** — create `src/server/services/` directory; extract `listPATsFromIdentity()` and `ListPATsResult` to `src/server/services/identity-server.ts`; extract `fetchScopesExplain()` and `policyGatewayFetch` to `src/server/services/policy-gateway-server.ts`
4. **Update existing consumers** — update `model-gateway/_services/tokens-server.ts` and `policy/_services/policy-server.ts` to import from the shared location
5. **Profile server service** — create `profile/_services/profile-server.ts` with `fetchUserProfile()` and re-exports of shared helpers
6. **Types** — create `profile/_types/index.ts` with view types for profile, permissions, and tokens
7. **Profile route scaffold** — create `profile/page.tsx` (RSC) with session validation and parallel data fetching
8. **Loading skeleton** — create `profile/loading.tsx` mirroring the real card layout
9. **CopyButton (Tier 2)** — create `src/components/copy-button.tsx` as a Tier 2 shared component; export from `src/components/index.ts`; adopt `useRef` + `useEffect` timer cleanup pattern from `vocabulary-tab.tsx`
10. **User Identity card** — display session + profile data with copyable principal ID
11. **Roles & Permissions card** — display explain response with role/grant/scope breakdown and expand/collapse
12. **API Keys card** — display PAT listing with status badges and empty state
13. **Sidebar popover** — modify `SidebarUserFooter` to add the popover menu with `AppLink` navigation
14. **Verification** — run `pnpm lint && pnpm tsc --noEmit`, fix all issues

### Verification Checklist

Per the golden path and `AGENTS.md` conventions:

- [ ] All `_services/*.ts` and `src/server/services/*.ts` files start with `import 'server-only'`
- [ ] Shared helpers (`identity-server.ts`, `policy-gateway-server.ts`) return discriminated unions (note: `listPATsFromIdentity` uses `{ ok: true; tokens }` not generic `{ ok: true; data }`), never throw
- [ ] `model-gateway/_services/tokens-server.ts` and `policy/_services/policy-server.ts` import from shared — no duplicated endpoint calls
- [ ] Server service returns discriminated union (`{ ok: true; data } | { ok: false; status; message }`), never throws
- [ ] RSC page has NO `'use client'` directive and NO React hooks
- [ ] RSC page calls `isSessionValid(session)` before any fetch
- [ ] All client components have `'use client'` as first line
- [ ] All components use Terra (`@orbit/terra`) or `@orbit/components` — no raw HTML elements
- [ ] Scopes rendered via `ScopeChip` (`@orbit/components`) — not raw `Badge`
- [ ] PAT status rendered via `StatusBadge` (`@orbit/components`) — not raw `Badge`
- [ ] No `import { X } from 'lucide-react'` — use `<Icon name="..." />` only
- [ ] No `className` props on Terra components
- [ ] No `eslint-disable` comments targeting Terra rules
- [ ] All Tier 3 components have `@tier 3` JSDoc annotation with promotion trigger
- [ ] All filenames are kebab-case
- [ ] `loading.tsx` mirrors real layout exactly
- [ ] `pnpm lint` passes with zero errors
- [ ] `pnpm tsc --noEmit` passes with zero errors

---

## Validation

### Success Metrics

- Profile page loads in < 1 second with all three cards populated
- A user can copy their principal ID without leaving the profile page
- A user can see the source of each scope (which role or grant provides it)
- If the policy gateway is unreachable, the Roles & Permissions card falls back to cached session scopes
- If the identity service PAT endpoint fails, only the API Keys card shows an error — other cards render normally
- Sidebar popover opens and navigates to `/profile` correctly

### Testing

- **Integration tests** (Vitest): `SidebarUserFooter` popover rendering and menu item interactions
- **Integration tests** (Vitest): Each profile card component with mock data (success and error result variants)
- **E2E test** (Playwright): Navigate from sidebar → profile page → verify card content renders

### Monitoring

- Track profile page server-side rendering latency via existing Next.js OTEL traces; alert if p95 exceeds 2s
- Monitor policy gateway fallback rate — if the Roles & Permissions card frequently falls back to cached session scopes, investigate policy gateway availability
- Monitor identity service error rate for `GET /identity/users/{id}` and `GET /identity/tokens` via existing backend observability

---

## Related Decisions

- [ADR-0019](2026-03-17-frontend-scaffolding-and-toolchain.md) — Next.js App Router; server component fetch and SSR hydration patterns apply here
- [ADR-0030](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — BFF session architecture (iron-session, sealed cookie, server-only access token)
- [ADR-0033](2026-04-02-personal-access-tokens.md) — PAT model and listing endpoint
- [ADR-0037](2026-04-14-frontend-policy-projection.md) — Permission projection into session; `GET /policy/scopes` for session enrichment (the `/scopes/explain` variant is an extension of this endpoint, implemented in `policy/_services/policy-server.ts`)
- [ADR-0042](2026-04-19-user-profiles-table-for-principal-attribution.md) — `user_profiles` table and `GET /identity/users/{id}` endpoint
- [ADR-0046](2026-05-04-frontend-component-consolidation.md) — Three-tier component model; Terra and app-shared components

---

## References

- [Profile Page PRD](../prd/profile-page.md) — Full requirements, user stories, and acceptance criteria
- [Golden Path: Frontend State Pattern](../../frontend/docs/golden-paths/frontend-state-pattern.md) — 10-step data-driven page pattern (profile page implements Steps 1, 2, 9, 10; omits Steps 3–8 — no client-initiated data fetching)
- [Terra Design System Reference](../../frontend/docs/reference/terra-design-system.md) — Component props, examples, ESLint rules
- [App Components Reference](../../frontend/docs/reference/app-components.md) — Tier 2 shared components (EmptyState, StatusBadge)
- [Component Placement Reference](../../frontend/docs/reference/component-placement.md) — Three-tier model and placement decision tree

---

**Last Updated:** 2026-05-15
