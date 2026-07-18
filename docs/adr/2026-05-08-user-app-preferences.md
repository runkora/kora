# User App Preferences

**Status:** Proposed
**Date:** 2026-05-08
**Deciders:** Platform Engineering
**Technical Story:** [PRD: User App Preferences](../prd/user-app-preferences.md)

---

## Context

### Background

All UI state in Orbit is currently ephemeral. A user who hides a column in the Inference Logs table, switches to a compact table layout, or adjusts the analytics time window must redo that configuration on every new session and every device. There is no mechanism for the application to remember how a user has left it.

The identity service already owns the `user_profiles` table (ADR-0042), which provides a persistent, tenant-scoped record per human principal. The infrastructure for writing to and reading from a `(user_id, tenant_id)` store is established; what is missing is a preferences layer built on top of it.

### Problem Statement

Users cannot persist UI customisations across sessions or devices. This affects power users most acutely — analysts who work across machines, users who share a browser profile, and anyone who configures Orbit carefully only to find it reset on next login.

### Goals

- Provide a server-backed store for typed user preferences scoped to `(user_id, tenant_id)`.
- Synchronise preferences across all devices and browsers without user action.
- Support both deliberate user settings and silently remembered interaction state (column visibility, analytics window).

### Non-Goals

- Admin-level or tenant-wide default preferences.
- Preference inheritance or cascading (tenant default → user override).
- Real-time cross-tab sync (React context within a session is sufficient).
- Data retention, deletion, or inclusion in user data exports (deferred — no retention pipeline exists).

---

## Decision Drivers

* **Identity service already owns the pattern** — `user_profiles` establishes `(user_id, tenant_id)` scoping, a PostgreSQL-backed identity store, and the repository pattern. Preferences are a natural extension.
* **Schema must accommodate unknown future keys** — new preference categories will be added over time. The storage format must not require a migration for every new key.
* **Cross-device sync is a hard requirement** — `localStorage` is ruled out.
* **Two distinct UX modes** — some preferences are deliberate settings (user expects confirmation); others are remembered interaction state (user expects the app to just remember). The implementation must handle both, but the storage layer need not distinguish them.

---

## Considered Options

### Option 1: `localStorage` keyed by `user_id`

Store all preferences in the browser's `localStorage`, keyed by the user's `user_id`.

**Pros:**
- Zero backend changes.
- No latency on read.

**Cons:**
- Does not sync across devices or browsers — fails the core requirement.
- Lost when the user clears browser storage or uses incognito.
- `localStorage` is unavailable during SSR, blocking theme application on first paint.

**Verdict:** Rejected. Fails cross-device sync.

### Option 2: Flat key-value table (`user_id, key, value`)

A relational table with one row per preference key.

```sql
CREATE TABLE user_preferences_kv (
    user_id    TEXT NOT NULL,
    tenant_id  TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      JSONB NOT NULL,
    PRIMARY KEY (user_id, tenant_id, key)
);
```

**Pros:**
- Simple to query individual keys.
- Easy to add new keys without schema changes.

**Cons:**
- Reading the full preference set requires `SELECT *` and reassembly in application code.
- Atomically updating multiple preferences (e.g. an entire column visibility map) requires a multi-row transaction.
- A user with N preference keys produces N rows — unnecessary overhead for what is effectively a single document per user.

**Verdict:** Rejected. JSONB document is simpler and performs equally well.

### Option 3: Dedicated `preferences` microservice

Stand up a new service (e.g. a generic key-value store or a custom preferences API) separate from the identity service.

**Pros:**
- Clean separation of concerns.
- Could be reused by other services.

**Cons:**
- Significant operational overhead (new deployment, new database, new service-to-service auth) for what is one table and two HTTP handlers.
- The identity service already owns the `(user_id, tenant_id)` scoping pattern and the `user_profiles` table — preferences sit naturally alongside it.
- Introduces a new service-to-service dependency on the critical SSR path.

**Verdict:** Rejected. Over-engineering at current scale.

### Option 4: JSONB `preferences` column on `user_profiles`

Add a `preferences JSONB` column directly to the existing `user_profiles` table, avoiding a new table entirely.

**Pros:**
- Zero new tables; preferences ride the existing profile upsert.

**Cons:**
- Mixes attribution concerns (identity, display name, email) with UI state in a single row — the `user_profiles` table is upserted on every login from OIDC claims, making it an awkward home for user-controlled preferences.
- The profile upsert would need to be careful not to overwrite the preferences column, adding conditional logic to an already-hot login path.
- Makes future separation harder if preferences grow in complexity.

**Verdict:** Rejected. Concerns are too different to share a row.

### Option 5: JSONB document in the identity service (chosen)

Add a `user_preferences` table to the identity service database with one JSONB row per `(user_id, tenant_id)`. Expose `GET`, `PUT`, and `DELETE` endpoints on the identity service. The frontend fetches the document once at session start and writes back the full sparse document via a debounced `PUT`.

**Pros:**
- Single row read/write per user operation — simple and fast.
- JSONB allows additive schema evolution without migrations.
- The backend is deliberately schema-agnostic — it treats the document as opaque JSONB with no knowledge of specific preference keys or defaults. Adding, removing, or changing a preference key requires no backend changes.
- Lives in the existing identity service alongside `user_profiles`, reusing established patterns (repository interface, `(user_id, tenant_id)` primary key, multi-tenant scoping).
- No new service, no new database.

**Cons:**
- Preferences and identity are loosely related concerns co-located in one service. Acceptable at current scale; can be separated later if the service grows unwieldy.
- JSONB renaming a key requires a one-off data migration (additive changes and removals do not).

**Verdict:** Chosen.

---

### Write Strategy

Separately from the storage format, the mechanism for how the frontend persists preference changes to the backend was considered.

#### Option A: Partial PATCH with recursive merge

The frontend sends only the changed keys; the backend recursively merges the payload into the stored document, leaving unmentioned keys untouched.

**Pros:**
- Smaller write payloads.
- Concurrent writes from separate tabs changing different keys both survive — changes are merged rather than one overwriting the other.

**Cons:**
- Requires non-trivial recursive JSONB merge logic on the backend.
- Orphaned keys accumulate permanently. With a schema-agnostic backend there is no mechanism to strip keys absent from the current schema, and a partial PATCH never removes existing keys.

**Verdict:** Rejected. Orphaned key accumulation is unresolvable without either backend schema awareness (explicitly rejected) or a separate data migration mechanism (also rejected).

#### Option B: Full document PUT (chosen)

The frontend serialises `PreferencesContext` **sparsely** — only keys whose values differ from the hardcoded defaults in `preferences.schema.ts` are included — and sends it as a complete document replacement on every write.

**Pros:**
- Orphaned key accumulation is impossible — the payload is derived from `preferences.schema.ts` and contains only canonical keys. A removed preference key disappears from the stored document on the user's next write, with no migration required.
- Default value changes in `preferences.schema.ts` propagate automatically to any user who has not explicitly overridden that key, because absent keys always fall back to the current default on read.
- The stored document reflects only deliberate deviations from defaults — absent keys unambiguously mean "user has not changed this".
- Backend write handler is a plain upsert — no merge logic required.
- Multiple rapid changes are naturally batched — the debounce window collapses them into one PUT of the final sparse state.

**Cons:**
- Slightly larger write payloads than a partial patch. Negligible in practice given document size.
- The two-tab race condition is marginally more aggressive: two tabs making simultaneous changes to different keys will have one overwrite the other's full document, rather than both surviving via a merge. Accepted for a non-critical personalisation store.

**Verdict:** Chosen.

---

## Decision

Store user preferences as a JSONB document in a `user_preferences` table in the identity service database, with `GET /identity/preferences`, `PUT /identity/preferences`, and `DELETE /identity/preferences` endpoints scoped to the authenticated session's `(user_id, tenant_id)`.

The backend is deliberately schema-agnostic: it stores, replaces, and returns the document without knowledge of specific preference keys or their defaults. All schema concerns live on the frontend.

### Responsibilities

| Responsibility | Owner |
|---|---|
| Persistent storage (`user_preferences` table) | Backend |
| Multi-tenant scoping (`user_id`, `tenant_id` from session token) | Backend |
| Lazy row creation on first `PUT` | Backend |
| Full document replacement on `PUT` (upsert) | Backend |
| Document size enforcement (≤ 64 KB) | Backend |
| Authentication and authorisation | Backend |
| Observability (metrics, structured logs) | Backend |
| Canonical preference type (`UserPreferences`) | Frontend |
| Hardcoded defaults for all preference keys | Frontend |
| Merging stored document with defaults on read | Frontend |
| SSR fetch with 500 ms timeout fallback | Frontend |
| Optimistic updates | Frontend |
| Debounced writes (500 ms) | Frontend |
| BFF proxy route (`/api/preferences`) | Frontend |
| Wiring preferences to UI components | Frontend |

### Schema

```sql
CREATE TABLE user_preferences (
    user_id    TEXT        NOT NULL,
    tenant_id  TEXT        NOT NULL,
    prefs      JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, tenant_id),
    CHECK (octet_length(prefs::text) <= 65536)
);
```

`tenant_id` is sourced exclusively from the verified platform session token — never from the request body — consistent with all other identity service endpoints.

> **Note on column naming:** `user_profiles` (ADR-0042) uses `id` for the OIDC `sub`; this table uses `user_id` for the same value. The divergence is intentional — `user_id` is more self-descriptive in a table that is not itself a profile record. Joins between the two tables use `user_profiles.id = user_preferences.user_id`.

### Lazy Row Creation

Preference rows are created lazily on the first write, not the first read. The `GET` handler performs a plain `SELECT` and returns `{}` if no row exists — no row is written until the user first changes a preference. The `PUT` handler upserts, replacing the document in its entirety.

A user who opens the app but never changes a preference generates no database row, and no backfill is needed for existing users.

### API Design

All three endpoints are authenticated via the platform session token. `tenant_id` and `user_id` are extracted from the verified token claims.

```
GET    /identity/preferences         Returns the stored document as-is, or `{}` if no row exists yet. Missing keys are filled with hardcoded defaults by the frontend.
PUT    /identity/preferences         Replaces the stored document in its entirety; upserts a row if none exists.
DELETE /identity/preferences         Removes the stored document; subsequent GET returns `{}`.
```

> **On `DELETE` scope:** `DELETE /identity/preferences` removes only the stored preferences document for the authenticated user. Full data-export and right-to-erasure obligations are out of scope for this ADR (no retention pipeline exists yet); that is a separate initiative.

> **Rate limiting:** Write endpoints are debounced client-side (500 ms). Server-side rate limiting is handled by the existing gateway-level OPA enforcement (ADR-0044) and applies uniformly to all identity service endpoints.

#### GET Response Shape

`GET /identity/preferences` returns the stored document verbatim, or `{}` if no row exists. Missing keys are filled with hardcoded defaults by the frontend — the backend has no knowledge of what constitutes a complete preferences document.

Example response for a user who has changed only their analytics period:

```json
{
  "analytics": {
    "defaultPeriod": "30d"
  }
}
```

#### Document Size Limit

The stored document is capped at **64 KB**, enforced at the handler layer before any upsert and additionally by the `CHECK` constraint on the column. Per-field array cardinality guards (e.g. for `modelGateway.favouriteModels`) are determined during implementation to stay comfortably within this limit.

### Orphaned Key Cleanup

Orphaned keys cannot accumulate. Every `PUT` is serialised sparsely from `preferences.schema.ts`, which contains only canonical keys. A key removed from the schema will no longer appear in any subsequent PUT, so it disappears from the stored document on the user's next write.

### Preference Schema

All v1 preferences are **remembered state** — persisted silently, configured inline at the point of use. There are no explicit setting preferences in v1.

The full v1 schema:

| Key | Mode | Type | Default |
|-----|------|------|---------|
| `tables.columns.<tableId>` | Remembered state | `Record<columnId, boolean>` | `{}` (all visible) |
| `analytics.defaultPeriod` | Remembered state | `"7d" \| "30d" \| "90d"` | `"7d"` |
| `myUsage.defaultPeriod` | Remembered state | `"7d" \| "30d" \| "90d"` | `"7d"` |
| `modelGateway.favouriteModels` | Remembered state | `string[]` | `[]` |

Unknown keys in the stored document are silently ignored on read. Missing keys fall back to hardcoded defaults. Renaming a key requires a one-off data migration; adding or removing a key does not.

### Frontend Architecture

#### Session Initialisation

Preferences are fetched in the **root Next.js server component** on every SSR request. A **500 ms timeout** is applied: if the identity service does not respond within 500 ms, the page renders with hardcoded defaults. This keeps preferences off the critical render path under degraded conditions without blocking the user.

The fetched document is stored in a `PreferencesContext` and accessed via a `usePreferences` hook throughout the component tree.

#### Write Strategy

All writes use an **optimistic update + debounced PUT** pattern:

1. The changed preference is applied to `PreferencesContext` immediately.
2. A PUT is dispatched after **500 ms of inactivity** (debounce window resets on each change).
3. Multiple changes within the window are batched — `PreferencesContext` is serialised **sparsely** (only keys whose values differ from the hardcoded defaults in `preferences.schema.ts` are included) and sent as a single PUT.
4. Failed writes are silently dropped. The user's local context retains the intended value for the session; the change is re-sent the next time the user adjusts a preference.

On remembered-state controls (column pickers), persistence is invisible by design — no save confirmation is shown.

#### BFF Proxy

The BFF exposes `/api/preferences` as a **simple authenticated proxy**: request and response bodies are forwarded unmodified to the identity service. No client-side caching or response transformation is applied in v1.

Error behaviour:
- **`GET`**: on identity service error or timeout, the BFF returns the hardcoded defaults document with a `200` status rather than propagating a `5xx`. This keeps the client-side and SSR fallback paths identical and avoids frontend error-handling complexity for a non-critical endpoint.
- **`PUT`**: errors are propagated as-is. The frontend silently ignores non-2xx responses (see Write Strategy).

#### Table Column Visibility

Column visibility is built into an app-level `DataTable` wrapper component that sits over Terra's underlying table primitive. `tableId` is a required prop on the wrapper; it connects to `usePreferences` internally and manages column visibility. Terra has no awareness of preferences — the wrapper is the app-level concern. Every table in the application that uses the wrapper gets preference-backed column visibility automatically, with no additional wiring required.

The `tables.columns` key in the preferences document is typed as `Record<string, Record<string, boolean>>`. The wrapper component is the enforcement boundary; no enumeration of known table IDs is required at the schema level.

#### Period Selector

The `PeriodSelector` is an app-level component wrapping Terra's period button controls (7d/30d/90d). It accepts a `prefKey` prop identifying which preference key to read and write (e.g. `"analytics.defaultPeriod"` or `"myUsage.defaultPeriod"`). It connects to `usePreferences` internally using that key — Terra has no awareness of preferences. All period selectors in the application use this component; the preference behaviour is implicit in using it.

---

## Consequences

### Positive

- Preferences sync across all devices and browsers with no user action.
- Column layouts, analytics windows, and other interaction state are remembered silently — UX improvement requiring no explicit user action.
- The app-level `DataTable` wrapper and `PeriodSelector` components mean new tables and period selectors automatically get preference support with no additional wiring.
- The schema-agnostic backend means preference keys can be added, changed, or removed with no backend changes — the frontend `preferences.schema.ts` is the sole source of truth.

### Negative

- A user with two browser tabs open making simultaneous preference changes will experience last-write-wins at the database. Because writes are full document replacements, the second write overwrites the first tab's full document rather than merging changes. Accepted — the non-critical nature of preference data makes this an appropriate trade-off.
- A failed write is silently dropped — the user's local context retains the intended value for the session.
- The preferences `GET` is on the critical SSR path (mitigated by the 500 ms timeout and default fallback).
- Renaming a preference key requires a one-off data migration.

### Neutral

- Preferences are co-located with identity in the same service and database. This is a pragmatic decision at current scale, not a permanent commitment.
- No existing API contracts change.
- Existing users' preferences default to the hardcoded defaults until they first interact with a preference control.

---

## Implementation

This feature spans three sequential phases across backend and frontend. The phased structure departs from the flat numbered list used in ADR-0042 to reflect the distinct deliverables in each phase.

### Phase 1: Backend Foundation

1. PostgreSQL migration: `user_preferences` table.
2. `UserPreferencesRepository` interface and `PostgresUserPreferencesRepository` implementation (`Get`, `Upsert`, `Delete`).
3. `GET /identity/preferences`, `PUT /identity/preferences`, `DELETE /identity/preferences` handlers.
4. Unit and integration tests covering: lazy row creation, concurrent first-write race, 64 KB document limit, cross-tenant isolation.

### Phase 2: Frontend Wiring

1. BFF `/api/preferences` proxy route.
2. Centralised `preferences.schema.ts` — canonical `UserPreferences` type and hardcoded defaults.
3. `PreferencesContext` and `usePreferences` hook.
4. App-level `DataTable` wrapper with required `tableId` prop, connected to `usePreferences` for column visibility.
5. App-level `PeriodSelector` component accepting a `prefKey` prop, connected to `usePreferences`; wired to `analytics.defaultPeriod` on model gateway analytics pages and `myUsage.defaultPeriod` on the `/my-usage` page.
6. `modelGateway.favouriteModels` remembered state wired to the `/model-gateway/models` page — toggling the heart icon on a model card adds or removes the model ID from the stored array.

---

## Validation

### Success Metrics

- `GET /identity/preferences` p95 latency < 50 ms.
- `PUT /identity/preferences` p95 latency < 100 ms.
- Zero cross-tenant preference leakage (verified by integration test).
- ≥ 60 % of weekly-active users have at least one non-default preference set within 30 days of launch.
- Support tickets relating to "lost column settings" drop to zero.

### Monitoring

- Structured log lines on each handler with `user_id`, `tenant_id`, and `latency_ms`.
- The "≥ 60 % of WAU" metric is measured by querying `user_preferences` for rows with non-empty documents and joining against the WAU set from analytics.

---

## Related Decisions

- [ADR-0042](2026-04-19-user-profiles-table-for-principal-attribution.md) — Establishes `user_profiles` and the `(user_id, tenant_id)` scoping pattern this ADR extends.
- [ADR-0030](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — BFF session architecture; `user_id` and `tenant_id` are sourced from the platform session token managed here.
- [ADR-0019](2026-03-17-frontend-scaffolding-and-toolchain.md) — Next.js App Router; server component fetch and SSR hydration patterns apply here.

---

## References

- [PRD: User App Preferences](../prd/user-app-preferences.md)

---

**Last Updated:** 2026-05-08
