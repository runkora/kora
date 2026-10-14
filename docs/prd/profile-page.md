# Profile Page - Product Requirements Document

**Status:** Draft
**Created:** 2026-05-11
**Last Updated:** 2026-05-11
**Owner:** Frontend Team
**Stakeholders:** Frontend Engineering, Platform Engineering

---

## Executive Summary

kora has no centralised view where users can inspect their identity, permissions, or API tokens. The profile page gives every authenticated user a single screen answering "who am I, what can I do, and what tokens have I issued?" — accessible from a popover menu in the sidebar footer.

---

## Problem Statement

### Current State

The sidebar footer displays the user's email and display name alongside a standalone logout button. There is no dedicated page where users can view their principal ID, tenant, OIDC provider, assigned roles, effective scopes, session status, or personal access tokens. The only way to inspect these values today is to decode the JWT manually or ask an admin to look up grants in the Policy UI.

### User Pain Points

- Users cannot see their principal ID — they must ask an admin or inspect JWT tokens manually to share it when requesting access grants.
- There is no visibility into which roles grant which scopes, making it hard to understand why access is allowed or denied.
- Users have no way to view their API keys after creation — the API exists but there is no UI surface.
- Session health (token expiry, scope freshness) is invisible, leading to confusion when permissions change but the session hasn't refreshed.

### Business Impact

- Support overhead: admins field requests for principal IDs and permission debugging that users should be able to self-serve.
- Security: users cannot audit their own tokens or identify compromised credentials.
- Onboarding friction: new users have no self-service way to understand their access level within the platform.

---

## Goals and Objectives

### Primary Goals

1. Provide a read-only profile page showing user identity, permissions breakdown, and API keys.
2. Add a sidebar popover menu replacing the current standalone logout button, with navigation to the profile page.

### Success Metrics

- A user can copy their principal ID from the profile page and share it with an admin without needing to decode a JWT.
- A user can view their complete permission breakdown (roles, grants, effective scopes) without leaving the profile page.
- A user can see all their API keys and their expiry status.

### Non-Goals

- Profile editing (name, email, avatar) — identity is managed by the upstream OIDC provider.
- Role or grant modification — handled via the existing Policy pages.
- API key creation or revocation from the profile page. Deferred to Phase 2.
- Session listing and revocation. Deferred to Phase 2; requires new backend endpoints.
- UI for API key management beyond read-only listing.

---

## User Stories

### Primary User Stories

**As a** platform user,
**I want to** click my user area in the sidebar and see a menu,
**So that** I can access my profile, settings, and logout from one place.

**Acceptance Criteria:**
- [ ] Clicking the user footer area opens a popover with "View Profile", "Settings", and "Log out"
- [ ] "View Profile" navigates to `/profile`
- [ ] "Log out" calls the existing logout flow
- [ ] "Settings" is rendered as disabled (no `/settings` route exists yet)

---

**As a** platform user,
**I want to** see my identity details on the profile page,
**So that** I can share my principal ID with admins and understand my identity source.

**Acceptance Criteria:**
- [ ] Profile page displays: name, email, principal ID, tenant ID, OIDC provider
- [ ] Principal ID has a click-to-copy interaction with visual feedback

---

**As a** platform user,
**I want to** see a breakdown of my roles and effective scopes,
**So that** I understand what I can access and why.

**Acceptance Criteria:**
- [ ] Roles section shows OIDC roles with distinct styling
- [ ] Grants section shows direct grants with grant ID and scope count
- [ ] Effective scopes section shows the full scope set with a total count
- [ ] Scopes are displayed as badges; if more than 6, remaining are collapsed behind "+ N more"

---

**As a** platform user,
**I want to** see my API keys,
**So that** I can audit which keys exist and when they expire.

**Acceptance Criteria:**
- [ ] API Keys card lists all non-revoked keys for the current user with: name, scopes, expiry date, and status

---

## Proposed Solution

### High-Level Approach

Replace the standalone logout button in the sidebar footer with a clickable user area that opens a popover menu. Add a new `/profile` route under the dashboard layout that renders a card-based profile page. All data is fetched server-side from existing APIs.

### User Experience

The existing user footer (icon, email, name) becomes clickable. On click, a popover appears above the footer with three menu items: "View Profile", "Settings" (disabled), and "Log out" (separated by a divider).

The profile page displays three cards in a single-column layout:

1. **User Identity** — name, email, principal ID (copyable), tenant, OIDC provider
2. **Roles & Permissions** — OIDC roles, direct grants, and effective scopes with a visual breakdown of where each scope originates
3. **API Keys** — read-only list of the user's API keys with name, scopes, and expiry

#### User Flow

1. User clicks the user area in the sidebar footer
2. Popover menu appears with "View Profile", "Settings", and "Log out"
3. User clicks "View Profile"
4. Browser navigates to `/profile`
5. Profile page loads with identity, permissions, and API Keys cards
6. User can click-to-copy their principal ID
7. User can expand collapsed scope lists to see all effective scopes

### Key Components

- **Sidebar User Menu**: Replaces the current `SidebarUserFooter`, wraps user info in a `Popover` trigger
- **Profile Page**: Server Component at `/profile` that fetches data server-side in parallel
- **User Identity Card**: Displays identity details from session and identity service
- **Roles & Permissions Card**: Displays permission breakdown from the policy explain endpoint
- **API Keys Card**: Displays API keys from the identity service

---

## Scope

### In Scope

- Sidebar popover menu replacing the standalone logout button
- Profile page route (`/profile`) within the dashboard layout
- User identity card with copyable principal ID
- Roles and permissions breakdown card
- API keys read-only listing card
- Server-side PAT listing via existing `listPATsFromIdentity()` helper (no new BFF route needed)

### Out of Scope

- Profile editing (identity is managed by the upstream OIDC provider)
- API key creation or deletion from the profile page (deferred to Phase 2)
- Session listing and revocation (deferred to Phase 2 — requires new backend endpoints)
- Avatar or profile picture upload
- Notification or display preferences
- Admin-level profile viewing (viewing other users' profiles)

### Future Considerations

- **Last Active Timestamp** — Display when the user was last active. Requires adding a `last_active_at` column to `user_profiles` and write-on-login tracking in the identity service.
- **Member Since Timestamp** — Display when the user account was created. Requires extending `GET /api/v1/identity/users/{id}` to include `created_at` (already stored in `user_profiles` table).
- **Active Sessions** — List all user sessions with revoke capability. Requires new backend endpoints.
- **API Key Management** — Create and revoke API keys directly from the profile page.
- **Tenant Info Card** — Show tenant display name, state, and OIDC configuration.
- **Scope Descriptions** — Enrich scope badges with human-readable descriptions from the vocabulary endpoint.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Sidebar user area opens a popover menu on click
- [ ] Popover contains "View Profile", "Settings", and "Log out"
- [ ] `/profile` route renders within the dashboard layout
- [ ] User Identity card displays: name, email, principal ID (copyable), tenant, OIDC provider
- [ ] Roles & Permissions card displays: OIDC roles, direct grants, effective scopes
- [ ] API Keys card listing user's keys via server-side `listPATsFromIdentity()` call

**Should Have (P1):**
- [ ] Click-to-copy on principal ID field with visual confirmation

**Nice to Have (P2):**
- [ ] Expandable scope list ("+ N more" that reveals all scopes on click)
- [ ] Relative time formatting for timestamps ("2 minutes ago", "in 47 minutes")

### Non-Functional Requirements

**Performance:**
- Profile page initial load < 1s (server-side data fetching with parallel API calls)
- No layout shift — use skeleton loading states for each card

**Security:**
- Access token is never exposed to the client (server-side fetch only, sealed in session cookie)
- Profile page requires an authenticated session (enforced by existing Edge middleware)
- API key list endpoint is scoped to the current user only — no cross-user visibility

**Reliability:**
- Profile page renders gracefully when individual API calls fail — each card handles its own error state independently
- If the policy explain endpoint is unreachable, the Roles & Permissions card shows cached scopes from the session

**Scalability:**
- No additional scalability concerns — profile page reads from existing endpoints with no new write paths

**Observability:**
- Server-side `listPATsFromIdentity()` call reuses existing `orbitClient` logging and error handling

---

## Dependencies

### Internal Dependencies

- **Hades Design System** (`@runkora/hades`): Popover, Card, Badge, Stack, Text, Skeleton, Icon components (all exist)
- **Session infrastructure** (`@orbit/server/session`): `getSession()` server-side, `useSession()` client-side (exists)
- **Policy evaluate/explain endpoint**: `GET /api/policy/evaluate/explain` (exists)
- **Identity users endpoint**: `GET /identity/users/{id}` (exists)

> **Note:** Identity service paths (`/identity/...`) are relative to `ORBIT_API_URL` (which includes the `/api/v1` base path) as resolved by `orbitClient`.

### External Dependencies

- **Server-side token service**: `listPATsFromIdentity()` in `tokens-server.ts` — calls the Go identity service `GET /identity/tokens` endpoint directly via `orbitClient`. Used by existing Model Gateway pages; no BFF route required.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| OIDC provider issuer not available in session cookie | Medium | Medium | Source the upstream OIDC issuer URL from tenant configuration or discovery metadata stored in session. If that value is not currently available, add a dedicated session field for it and fall back to "Unknown" when absent. |
| User has no roles or grants (empty state) | Low | Medium | Render empty states with explanatory text linking to Policy documentation. |
| User may lack required scope to list API keys | Medium | Low | `GET /api/v1/identity/tokens` is scoped to the authenticated caller. If the user's session lacks the required token-listing scope, show an empty state with a message explaining the missing permission. |
| Large number of effective scopes causes UI overflow | Low | Medium | Collapse scope badges with "+ N more" and expandable section. |
| Policy explain endpoint requires `platform:grants:read` scope | Medium | Low | If user lacks this scope, show cached scopes from session instead of the full breakdown. |

---

## Alternatives Considered

### Alternative 1: Embed profile in the Settings page

**Description:** Add a "Profile" tab to the existing Settings page rather than creating a new route.

**Why not chosen:**
- The Settings page is WIP and has a different purpose (system-level configuration).
- Profile is user-specific; settings is system-wide. Conflating them creates a confusing hierarchy.
- A separate `/profile` route is cleaner for navigation and bookmarking.

### Alternative 2: Side panel instead of a full page

**Description:** Open profile as a side panel from the sidebar rather than navigating to a new page.

**Why not chosen:**
- Too much content for a side panel (3 cards in Phase 1, growing to 5+ with Phase 2 additions like sessions and API key management).
- Doesn't accommodate Phase 2 features (session listing, API key management).
- A full page is consistent with how other dashboard sections present detailed data.

### Alternative 3: Inline dropdown from sidebar footer

**Description:** Show profile data directly in a large dropdown, without a separate page.

**Why not chosen:**
- The sidebar is 260px wide — not enough space for permission breakdowns, API key lists, and usage stats.
- No URL means the profile state can't be bookmarked, shared, or deep-linked.

---

## Open Questions

1. **PAT endpoint filtering:** `GET /api/v1/identity/tokens` is caller-scoped and lists PATs for the authenticated caller only. It does not expose a `user_id` filter parameter; the server derives and enforces `tenant_id` and `user_id` from auth claims.
   - **Status:** Resolved — filtering is not supported or needed for this UI.

2. **OIDC issuer availability:** Is the OIDC issuer URL reliably present in the JWT claims across all configured providers?
   - **Status:** Open
   - **Owner:** Frontend Team

3. **Scope descriptions:** Should scopes show human-readable descriptions from the vocabulary endpoint?
   - **Status:** Deferred — raw scope strings in Phase 1; vocabulary-enriched descriptions as a future enhancement.
   - **Owner:** Product

---

## Timeline and Milestones

### Phases

**Phase 1: Core Profile Page** (Target: TBD)
- Sidebar popover menu
- Profile page with User Identity, Roles & Permissions, and API Keys cards

**Phase 2: Session & API Key Management** (Target: TBD)
- Active sessions listing (requires new backend endpoint)
- Session revocation
- API key create/revoke from profile page

### Key Milestones

- [ ] PRD Approved: TBD
- [ ] ADR Created: TBD
- [ ] Implementation Started: TBD
- [ ] Alpha/Beta: TBD
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [ADR-0037](../adr/2026-04-14-frontend-policy-projection.md) - Frontend policy projection
- [Personal Access Tokens PRD](./personal-access-tokens.md) - PAT backend API (consumed by this feature)

### References

- [Hades Design System](../prd/design-system.md) - Component library used for all UI elements
- [Browser Auth Flow](../prd/browser-auth-flow.md) - OIDC authentication flow and session model

### Glossary

- **Principal ID:** Unique identifier for a user in kora, derived from the OIDC `sub` claim.
- **Scope:** A permission string in `resource:path:action` format (e.g., `platform:grants:read`).
- **Grant:** A direct assignment of scopes to a principal, stored in `policy_grants`.
- **Role:** An OIDC role name mapped to a set of scopes via `policy_role_scopes`.
- **API Key (PAT):** A user-bound, longer-lived, inference-scoped platform token. The backend refers to these as Personal Access Tokens (PATs); the frontend uses "API Keys" for consistency with the Model Gateway UI.
- **BFF:** Backend for Frontend — Next.js API routes that proxy requests to Go backend services.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
