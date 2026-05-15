# User App Preferences - Product Requirements Document

**Status:** Approved  
**Created:** 2026-05-08  
**Last Updated:** 2026-05-08 (post-review revision)  
**Owner:** Product  
**Stakeholders:** Frontend Engineering, Identity/Backend Engineering, Platform

---

## Executive Summary

Orbit users currently have no way to persist UI choices across sessions or devices. This PRD defines a lightweight, backend-synced user preferences system that allows users to customise their Orbit experience — column layouts, analytics time windows, favourited models, and more — with changes that follow them across browsers and devices without any additional action on their part.

---

## Problem Statement

### Current State

All UI state in Orbit is ephemeral. Column visibility choices, analytics time windows, and any other display customisations are lost the moment a user closes a tab or switches devices. There is no mechanism for a user to declare "I always want to see these columns" and have that be honoured everywhere they use Orbit.

### User Pain Points

- Reconfiguring table columns on every session is friction that disproportionately affects power users who open Orbit from multiple machines or browsers.
- Analysts who work primarily with certain metrics have no way to de-clutter views by hiding irrelevant columns or charts.
- Users who share a machine with colleagues cannot each have their own layout without overwriting the other's local storage.

### Business Impact

- UX friction increases time-to-insight for analysts using the logs, usage, and analytics tables daily.
- Missing personalisation reinforces a perception of Orbit as a prototype rather than a production-grade platform.

---

## Goals and Objectives

### Primary Goals

1. Provide a server-backed store for arbitrary, typed user preferences scoped to a `(user_id, tenant_id)` pair.
2. Synchronise preferences across all devices and browsers without requiring manual import/export.
3. Ship an initial set of preference categories that deliver immediate, visible value.

### Success Metrics

- Preferences API p95 read latency < 50 ms.
- Preferences API p95 write latency < 100 ms.
- Zero cross-tenant preference leakage (verified by integration test).
- After launch, ≥ 60 % of weekly-active users have at least one non-default preference set within 30 days.
- Support ticket volume related to "lost column settings" drops to zero.

### Non-Goals

- Admin-level preferences or org-wide defaults (addressed separately if needed).
- Preference *inheritance* or cascading (tenant default → user override) — YAGNI for v1.
- Exporting or sharing preference profiles between users.
- Real-time preference sync across open tabs in the same browser (local state sync via React context is sufficient).
- Data retention, deletion, or inclusion in user data exports (deferred — no retention pipeline exists).

---

## User Stories

### Primary User Stories

**As a** platform user,  
**I want to** choose which columns are visible in the Inference Logs table,  
**So that** I can focus on the fields relevant to my workflow without visual clutter.

**Acceptance Criteria:**
- [ ] Column visibility choices persist across sessions and devices.
- [ ] Resetting to defaults restores the original column set.
- [ ] Preference is applied on initial render via the server-fetched preferences document.

### Secondary User Stories

**As a** developer,  
**I want to** access a stable API for reading and writing preferences,  
**So that** I can wire new UI preferences to the system without designing storage myself.

**Acceptance Criteria:**
- [ ] If `GET /identity/preferences` fails or times out, the frontend renders with hardcoded defaults and no error is shown to the user.
- [ ] Write failures are silently dropped; no error state or rollback is triggered in the UI.

---

## Proposed Solution

### High-Level Approach

Extend the identity service with a `user_preferences` table keyed on `(user_id, tenant_id)`. Preferences are stored as a JSONB document, allowing schema evolution without migrations for each new preference key. A strongly-typed TypeScript type (`UserPreferences`) in a centralised `preferences.schema.ts` file acts as the canonical frontend schema. The backend is deliberately schema-agnostic — it stores and returns the JSONB document as-is, with no knowledge of specific preference keys or defaults. Unknown keys in the stored document are ignored by the frontend; missing keys fall back to hardcoded TypeScript defaults.

Preference rows are created **lazily** — a row is created on the first `PUT` (the user's first preference write), not at account creation or first `GET`. The `GET` handler returns `{}` if no row exists; the `PUT` handler upserts.

The frontend fetches the preferences document once at session start (via a server component), stores it in a React context, and writes back the full sparse document via a debounced `PUT /identity/preferences`. A `DELETE /identity/preferences` endpoint resets all preferences to defaults by removing the stored document.

### Two Modes of Persistence

Preferences fall into two distinct UX modes, both backed by the same storage layer:

- **Explicit settings** — deliberate choices a user makes consciously. The user expects a confirmation that their choice was saved.
- **Remembered state** — interaction state the system persists silently on the user's behalf. The user configures these inline and simply expects the app to remember — there is no "settings panel" for these, and no save confirmation.

All v1 preference keys are remembered state.

### User Experience

#### Remembered State Controls

For preferences that are remembered state (e.g. column visibility), controls remain entirely in-context — a column picker on the table toolbar. The user interacts with these controls as they normally would; the system silently persists the state so it is restored on next visit from any device.

#### Write Strategy

When a user changes a preference, the frontend applies it to local context immediately (optimistic update) so the UI responds without waiting for the network. A `PUT` to the backend is then debounced at **500 ms** — rapid changes collapse into a single request rather than firing one per interaction.

**Failure behaviour:** Preference writes are best-effort. If the `PUT` fails, the UI does not roll back or surface an error. The in-flight change is silently dropped. The user's local context retains the intended value, so their session is unaffected; the change will be re-sent the next time they adjust a preference. This data-loss trade-off is intentional for a non-critical personalisation store.

#### User Flow

1. User adjusts a preference (e.g. hides a table column).
2. Frontend applies the change to local context immediately.
3. After 500 ms of inactivity, a sparse `PUT` is sent to the backend.
4. On next login from any device, the preference is fetched and applied before render.

### Key Components

- **`user_preferences` table** — PostgreSQL, JSONB document per `(user_id, tenant_id)`.
- **Preferences API** — `GET /identity/preferences`, `PUT /identity/preferences`, `DELETE /identity/preferences` on the identity service.
- **`PreferencesContext`** — React context + custom hook (`usePreferences`) for frontend reads/writes.
- **`preferences.schema.ts`** — Centralised TypeScript file containing the canonical `UserPreferences` type and all hardcoded defaults. The sole source of truth for what constitutes a valid or complete preferences document; the backend returns the stored document as-is and has no knowledge of defaults.
- **`DataTable` wrapper** — App-level component wrapping Terra's table primitive. `tableId` is a required prop; column visibility is managed internally via `usePreferences`. Terra has no awareness of preferences.
- **`PeriodSelector`** — App-level component wrapping Terra's period button controls (7d/30d/90d). Accepts a `prefKey` prop identifying which preference key to read and write (e.g. `"analytics.defaultPeriod"` or `"myUsage.defaultPeriod"`). Connects to `usePreferences` internally. Terra has no awareness of preferences.

---

## Preference Categories

The following categories and keys are in scope for v1. Each key is tagged as either **explicit setting** or **remembered state** (persisted silently, configured inline).

### 1. Tables — Per-Table Column Visibility

Each table gets its own key under `tables.columns.<tableId>` mapping column IDs to a boolean (visible/hidden). Unset columns default to visible.

Column visibility is built into the app-level `DataTable` wrapper component. `tableId` is a required prop; the wrapper connects to `usePreferences` internally and manages column visibility. Terra's underlying table primitive has no awareness of preferences. Every table in the application that uses the wrapper gets preference-backed column visibility with no additional wiring — there is no opt-in step. The tables below are those present at launch; any future table using the wrapper inherits the behaviour for free.

Proposed `tableId` values for v1:

| Table ID | View |
|----------|------|
| `inferenceLogs` | Model Gateway → Logs |
| `gatewayModels` | Model Gateway → Models |
| `analyticsRequests` | Model Gateway → Analytics → Requests |
| `analyticsSpend` | Model Gateway → Analytics → Spend |
| `analyticsTokens` | Model Gateway → Analytics → Tokens |
| `analyticsPrincipals` | Model Gateway → Analytics → Users |
| `usageRequests` | Model Gateway → My Usage → Requests |
| `policyGrants` | Policy → Grants |
| `policyRoles` | Policy → Roles |
| `patTokens` | Model Gateway → Keys |
| `catalogVirtualModels` | Model Gateway → Catalog → Virtual Models |
| `catalogProviders` | Model Gateway → Catalog → Provider Models |

### 2. Analytics & Dashboard

| Key | Mode | Type | Default | Description |
|-----|------|------|---------|-------------|
| `analytics.defaultPeriod` | Remembered state | `"7d" \| "30d" \| "90d"` | `"7d"` | Default time window for model gateway analytics dashboards. |

### 3. My Usage

| Key | Mode | Type | Default | Description |
|-----|------|------|---------|-------------|
| `myUsage.defaultPeriod` | Remembered state | `"7d" \| "30d" \| "90d"` | `"7d"` | Default time window for the `/my-usage` page. |

### 4. Model Gateway

| Key | Mode | Type | Default | Description |
|-----|------|------|---------|-------------|
| `modelGateway.favouriteModels` | Remembered state | `string[]` | `[]` | IDs of models the user has favourited on the `/model-gateway/models` page. Toggling the heart icon adds or removes the model ID from the stored array. Arrays are replaced in full on each `PUT`. |

---

## Scope

### In Scope

- `user_preferences` table and repository in the identity service.
- `GET`, `PUT`, and `DELETE` endpoints on the identity service, authenticated via the existing platform session token.
- Frontend `PreferencesContext` and `usePreferences` hook.
- BFF API route proxying preferences reads/writes — a simple authenticated proxy; request/response bodies are forwarded unmodified to the identity service with no client-side caching or transformation in v1.
- Wiring column visibility preferences via the app-level `DataTable` wrapper.
- Wiring analytics period preferences via the app-level `PeriodSelector` component.

### Out of Scope

- Admin console for setting org-wide preference defaults (future).
- Preference history or audit log.
- Data retention, deletion, or inclusion in user data exports (deferred; no retention/deletion pipeline exists yet).
- Bulk preference reset (other than a "Reset all to defaults" action).
- Mobile/native client support (no mobile client currently exists).

### Future Considerations

- Tenant-level default preferences that users inherit and can override.
- Exportable preference profiles (useful for onboarding teammates to a shared layout).
- Per-dashboard pinned chart configurations.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `GET /identity/preferences` returns the stored document as-is, or `{}` if no row exists; missing keys are filled with hardcoded defaults by the frontend.
- [ ] `PUT /identity/preferences` accepts a complete preferences document and replaces the stored document in its entirety; upserts a row if none exists.
- [ ] Preferences are scoped to `(user_id, tenant_id)` — one user in two tenants has independent preference stores.
- [ ] Frontend fetches preferences once at session initialisation and hydrates context.
- [ ] Column visibility is built into the app-level `DataTable` wrapper — `tableId` is a required prop; the wrapper manages column visibility via `usePreferences` internally. Every table using the wrapper gets preference-backed column visibility with no additional wiring.
- [ ] The twelve tables listed in the Preference Categories section use the `DataTable` wrapper and are therefore wired at launch.
- [ ] `DELETE /identity/preferences` resets all stored preferences; the frontend redirects to a fresh state.

**Should Have (P1):**
- [ ] `analytics.defaultPeriod` applied as the initial window on model gateway analytics dashboards via the app-level `PeriodSelector` component.
- [ ] `myUsage.defaultPeriod` applied as the initial window on the `/my-usage` page via the app-level `PeriodSelector` component.
- [ ] `modelGateway.favouriteModels` wired to the `/model-gateway/models` page.

**Nice to Have (P2):**

### Non-Functional Requirements

**Performance:**
- `GET /identity/preferences` p95 < 50 ms (single row read from PostgreSQL with primary key lookup).
- `PUT /identity/preferences` p95 < 100 ms.
- Frontend must not block the critical render path waiting for preferences — preferences are fetched in the root server component.

**Security:**
- Preferences endpoint must reject requests without a valid platform session token.
- Users may only read and write their own preferences — no cross-user access.
- The stored JSONB document must not contain secrets (enforced by schema design; no free-form string fields that could be misused).
- `tenant_id` must be sourced from the verified session token, never the request body.

**Reliability:**
- Preference writes are best-effort; a transient write failure must not block the UI action it accompanied. Failed writes are silently dropped — the user's local context retains the intended value for the session, and the change is re-sent on the next preference update.
- If the preferences endpoint is unavailable at session start, the frontend falls back to hardcoded defaults silently.
- The server-side preferences fetch (root server component) must apply a **500 ms timeout**. If the identity service does not respond within 500 ms, the request falls back to hardcoded defaults so SSR is not stalled.
- Concurrent writes from multiple open browser tabs are resolved as last-write-wins at the database level. This is an intentional trade-off given the non-critical nature of preference data.

**Scalability:**
- The `user_preferences` table primary key is `(user_id, tenant_id)`; a secondary index on `(tenant_id, user_id)` supports tenant-wide administrative queries.
- No fan-out or pub/sub required for v1 (single-writer, single-reader per session).

**Observability:**
- Preference reads and writes emit structured log lines with `user_id`, `tenant_id`, and `latency_ms`.

---

## Dependencies

### Internal Dependencies

- **Identity service** (Go) — adds the new repository, handlers, and migration. No changes to the auth flow required.
- **Frontend BFF API routes** — new `/api/preferences` route proxying to the identity service.
- **Terra Design System** — app-level wrappers (`DataTable`, `PeriodSelector`) sit over Terra primitives; Terra itself has no awareness of preferences.
- **Session context** — `user_id` and `tenant_id` must be available in the BFF session (they already are via `PlatformClaims`).

### External Dependencies

- **PostgreSQL** — existing managed instance; a new table and migration only.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| JSONB schema drift — stored documents diverge from the TypeScript schema over time | Low | Medium | The backend is schema-agnostic and has no knowledge of specific keys — adding, removing, or changing a preference key requires no backend changes. The frontend `preferences.schema.ts` is the sole source of truth; unknown keys in stored documents are silently ignored on read. Renaming a key requires a one-off data migration. |
| Preference fetch adds latency to initial page load | High | Low | Server-side fetch in Next.js layout; preference document is small (< 2 KB). |
| Users unknowingly lose preferences if `user_id` changes (e.g. IdP migration) | Medium | Low | `user_id` is the stable OIDC `sub` claim — document this as a constraint; handle in any future IdP migration runbook. |
| Noisy `PUT` calls from rapid UI interactions (e.g. dragging column widths) | Low | High | Debounce writes at 500 ms on the frontend; batch key changes in a single request. |
| Scope creep — teams request preference keys during implementation | Low | High | All new keys must go through a design review before being added to the canonical schema. |

---

## Alternatives Considered

### Alternative 1: Browser `localStorage` only

**Description:** Store all preferences client-side in `localStorage`, keyed by `user_id`.

**Why not chosen:**
- Does not satisfy the cross-device sync requirement.
- Preferences are lost when the user clears browser storage.
- Breaks in incognito sessions.

### Alternative 2: Separate `preferences` microservice

**Description:** Stand up a dedicated service for preferences rather than extending the identity service.

**Why not chosen:**
- Over-engineering for what is a single table and two API endpoints.
- The identity service already owns the `user_profiles` table and the `(user_id, tenant_id)` scoping pattern — preferences are a natural extension.

### Alternative 3: Flat key-value table (`user_id, key, value`)

**Description:** One row per preference key rather than a JSONB document.

**Why not chosen:**
- Read requires `SELECT *` and re-assembly in application code.
- Atomic update of multiple preferences (e.g. column set) requires a transaction with N rows.
- JSONB document with a single upsert is simpler and performs equally well at this scale.

---

## Resolved Questions

1. **Should the preferences endpoint be on the identity service or a new platform service?**
   - **Resolution:** Identity service, as it already manages `user_profiles` with the same `(user_id, tenant_id)` pattern.

2. **What is the maximum allowed size of the preferences JSONB document?**
   - **Resolution:** 64 KB, enforced at the handler layer before upsert. `modelGateway.favouriteModels` and any other unbounded arrays should have a per-field cardinality cap determined during implementation to stay comfortably within this limit.

---

## Timeline and Milestones

### Phases

**Phase 1: Backend Foundation** (Target: 2026-05-22)
- PostgreSQL migration: `user_preferences` table.
- `UserPreferencesRepository` interface + PostgreSQL implementation in the identity service.
- `GET /identity/preferences`, `PUT /identity/preferences`, and `DELETE /identity/preferences` handlers.
- Unit + integration tests.

**Phase 2: Frontend Wiring** (Target: 2026-06-05)
- BFF `/api/preferences` route.
- Centralised `preferences.schema.ts` — `UserPreferences` type and hardcoded defaults.
- `PreferencesContext` + `usePreferences` hook.
- App-level `DataTable` wrapper with required `tableId` prop, connected to `usePreferences` for column visibility.
- App-level `PeriodSelector` component accepting a `prefKey` prop, connected to `usePreferences`; wired to `analytics.defaultPeriod` on model gateway analytics pages and `myUsage.defaultPeriod` on the `/my-usage` page.
- `modelGateway.favouriteModels` remembered state wired to the `/model-gateway/models` page.

### Key Milestones

- [ ] PRD Approved
- [ ] ADR created (storage approach)
- [ ] Phase 1 complete + merged
- [ ] Phase 2 complete + merged
- [ ] Launch

---

## Appendix

### Related Documents

- [ADR-0061](../adr/0061-user-app-preferences.md) — Architecture decision for storage approach, API design, and SSR hydration strategy
- [`docs/design/inference-logs.md`](../design/inference-logs.md) — Logs table column inventory
- [`docs/design/inference-analytics.md`](../design/inference-analytics.md) — Analytics table column inventory

### Glossary

- **Preference key:** A dot-separated path within the preferences document (e.g. `tables.columns.inferenceLogs`).
- **Default:** The hardcoded fallback value used when a preference key is absent from the stored document.
- **Optimistic update:** Writing the new preference value to local React context immediately, before the API write completes, to keep the UI responsive.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
