# User Profiles Table for Principal Attribution

**Status:** Accepted
**Date:** 2026-04-19
**Deciders:** Platform Engineering
**Technical Story:** PAT grants ([pat-scope-assignment-via-explicit-grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md)) are keyed on the PAT UUID (`principal_id=<pat_uuid>`). The policy administration dashboard ([policy-administration-dashboard](2026-04-15-policy-administration-dashboard.md)) can list these grants, but has no way to show which user created a PAT — the identity service never persists user profile information from OIDC logins. Attribution requires a persistent user store populated from the verified JWT on each login.

---

## Context

### Background

The identity service exchanges OIDC tokens for platform session JWTs. The session record captures the principal's OIDC `sub`, `display_name`, and `tenant_id`, but the session table is a volatile operational store: sessions expire and are pruned. There is no persistent record of which users have ever authenticated into a tenant.

[pat-scope-assignment-via-explicit-grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) introduced `user_pat_tokens`, linking each PAT to its issuing user via `user_id` (the OIDC `sub`). The policy grants table holds a corresponding row keyed on `(principal_type=PAT, principal_id=<pat_uuid>)`. To display "PAT created by alice@example.com" in the grants dashboard, the dashboard must be able to resolve:

```
policy_grants.principal_id (PAT UUID)
  → user_pat_tokens.user_id (OIDC sub)
    → ??? (email / display name)
```

There is currently no table to resolve the final hop.

### Problem Statement

The platform cannot answer basic questions about its principals:

1. Which users have authenticated into a tenant?
2. Who created a given PAT?
3. Who holds a given explicit grant (via a PAT they issued)?

These gaps affect auditability, operator visibility, and future admin tooling (user management, forced logout, grant revocation by user). Every one of these use-cases requires mapping an OIDC `sub` to a human-readable identity.

### Goals

- Persist a minimal, IdP-agnostic user profile (sub, email, display name) on every successful login.
- Keep the profile current without requiring SCIM or IdP webhooks — the login event itself is the source of truth.
- Enable downstream systems (policy dashboard, audit logs, admin UI) to resolve any `user_id` to a display identity.
- Remain additive: no existing flows break, no new required fields for callers.

### Non-Goals

- Full SCIM/directory synchronisation.
- Storing IdP-specific attributes beyond the OIDC standard claims.
- User management (suspend, invite, delete) — that is a separate initiative.
- Group or team membership — separate initiative.

---

## Decision Drivers

* **No SCIM dependency** — The platform must work with Dex + GLAuth locally and any OIDC provider in production. Relying on IdP push is ruled out.
* **Single source of truth** — Login is the only point where we hold a verified identity assertion. Capturing it there avoids polling or reconciliation jobs.
* **Minimal surface area** — Only store what is needed for attribution. Avoid modelling users as a first-class resource (with full CRUD) until that capability is explicitly required.
* **Forward compatibility** — The schema should accommodate future additions (avatar URL, timezone, preferred locale) without breaking existing consumers.

---

## Considered Options

### Option 1: Store metadata on the grant (per-grant display name)

At PAT creation time, record the issuing user's email and display name as JSONB metadata on the `policy_grants` row.

**Pros:** No new table; attribution is self-contained in the grant.

**Cons:** Denormalised — display name changes (e.g. legal name change, email migration) are not reflected. Duplicated across every PAT a user creates. Cannot answer "list all users in a tenant" without aggregating grants. Does not generalise beyond PATs.

**Verdict:** Rejected. Stale data and poor generalisability.

### Option 2: Query the IdP at resolution time

When the dashboard needs a display name, call the IdP's UserInfo endpoint using the platform service token.

**Pros:** Always returns the current IdP state.

**Cons:** Requires the IdP to be reachable from the dashboard backend; adds latency to every admin page load; breaks in air-gapped environments; requires storing IdP credentials server-side; does not work with GLAuth (no UserInfo endpoint for arbitrary subs).

**Verdict:** Rejected. Fragile and environment-dependent.

### Option 3: `user_profiles` table, upserted on login (chosen)

Add a `user_profiles` table to the identity service's database. On every successful session creation (device flow completion, OIDC id_token exchange, refresh), upsert a row for the authenticated principal with their current OIDC claims.

Downstream systems resolve `user_id → profile` by querying this table (directly, or via an identity service endpoint).

**Pros:** Simple write path (piggybacks on existing session creation). Profile is always as fresh as the most recent login. Generalises to any attribution use-case. No IdP dependency at read time. Works in all environments.

**Cons:** Profile may be stale between logins (e.g. display name changed in IdP but user has not re-authenticated). Acceptable: the upsert on next login will update it.

**Verdict:** Chosen.

---

## Decision

Add a `user_profiles` table to the identity service database. Upsert a profile row on every successful session creation using the verified claims from the OIDC token exchange or device flow completion.

### Schema

```sql
CREATE TABLE user_profiles (
    id           TEXT        NOT NULL,           -- OIDC sub
    tenant_id    TEXT        NOT NULL,
    email        TEXT        NOT NULL,
    display_name TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, tenant_id)
);
```

The composite primary key `(id, tenant_id)` reflects the multi-tenant model: a user's `sub` may not be globally unique across IdP configurations (e.g. two tenants using different Dex issuers could theoretically emit the same sub). Scoping by tenant_id eliminates the ambiguity.

### Upsert on Login

The upsert runs inside the existing `CreateSession` transaction (device flow) or immediately after the OIDC id_token is verified (browser flow). It is a single `INSERT ... ON CONFLICT DO UPDATE`:

```sql
INSERT INTO user_profiles (id, tenant_id, email, display_name, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (id, tenant_id) DO UPDATE
SET email        = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    updated_at   = NOW();
```

Failures are logged but non-fatal — a failed upsert does not prevent the session from being issued. The profile will be backfilled on the next successful login.

### Attribution Resolution

To resolve a PAT grant to a human identity, the policy dashboard follows this chain:

```
policy_grants (principal_type=PAT, principal_id=<pat_uuid>)
  → user_pat_tokens.user_id   (WHERE id = pat_uuid)
  → user_profiles.email       (WHERE id = user_id AND tenant_id = tenant_id)
```

This resolution is done by the policy service (or a new identity service endpoint) and does not require joining across service databases. The policy service calls the identity service's existing PAT list endpoint to retrieve `user_id`, then the user profiles endpoint to retrieve the profile.

### API Design

The identity service exposes three endpoints for user profile access, all scoped to the authenticated caller's tenant:

```
GET /api/v1/identity/users              List all users (paginated)
GET /api/v1/identity/users?ids=a,b,c   Filter to specific user IDs
GET /api/v1/identity/users/{id}        Single user by ID
```

`GET /identity/users` accepts optional query parameters:

| Parameter | Type   | Default | Description |
|-----------|--------|---------|-------------|
| `ids`     | string | —       | Comma-separated list of user IDs to filter to |
| `page`    | int    | 1       | 1-based page number |
| `limit`   | int    | 20      | Page size (max 100) |

The response includes a `total` field (total matching records) to support page-number UI:

```json
{
  "users": [{ "id": "...", "email": "...", "display_name": "..." }],
  "total": 42,
  "page": 1,
  "limit": 20
}
```

### Pagination Strategy: Offset vs Cursor

The usage logs endpoint ([inference-usage-logs](2026-04-17-inference-usage-logs.md)) uses **cursor-based pagination** because the dataset is unbounded (potentially millions of rows), append-only, and consumers only ever navigate forward.

User profiles use **offset pagination** because:

- **Bounded dataset** — even large enterprise tenants will have at most tens of thousands of users; offset is performant at this scale.
- **Admin UI needs page numbers** — user management tables want "page 2 of 5" and jump-to-page, which require a total count. Cursor pagination cannot provide this.
- **Simpler to consume** — `?page=2&limit=50` requires no client-side cursor state management.
- **Slow-changing data** — the "row inserted mid-scroll" problem that cursors solve is negligible for a user list; a user appearing or disappearing between pages is not a meaningful UX concern.

Offset pagination is implemented with a `COUNT(*) OVER()` window function so total count and page data are returned in a single query.

### What Is Not Stored

- Password hashes or credentials of any kind (the platform is an OIDC relying party only).
- IdP-specific attributes beyond standard OIDC claims (`email`, `name`).
- Roles or group memberships (managed by the policy service's grant model).
- Session or device state (handled by the `sessions` and `refresh_tokens` tables).

---

## Consequences

### Positive

- The policy grants dashboard can display human-readable attribution for every PAT grant without runtime IdP calls.
- Audit logs gain a stable, resolvable `user_id` → profile mapping.
- Future admin features (user listing, forced session revocation by admin) have a natural anchor table.
- Profile freshness is guaranteed to be ≤ one login cycle stale — acceptable for all current use-cases.

### Negative / Trade-offs

- A user who changes their email in the IdP will see the old email in Orbit until they next log in. This is acceptable given the use-case (attribution, not authentication).
- Adds one upsert to the login hot path. The upsert targets a single row by primary key and is negligible in practice.
- The `user_profiles` table is owned by the identity service but queried by the policy service (for grants attribution). This cross-service data access is mediated via HTTP, preserving service boundary ownership.

### Neutral

- No existing API contracts change.
- No migration required for existing sessions — profiles are populated lazily on next login.
- The schema is intentionally minimal; additional columns (avatar_url, locale) can be added in future migrations without affecting this decision.

---

## Implementation Plan

1. **Migration** — Add `user_profiles` table to the identity service database via a new migration file.
2. **Repository** — Add `UserProfileRepository` interface and `PostgresUserProfileRepository` implementation with `Upsert`, `GetByID`, and `List` (paginated) methods.
3. **Service** — Call `UpsertProfile` after successful session creation in both the device flow and OIDC id_token exchange paths. Expose `GetUserProfile`, `GetUserProfiles`, and `ListUsers` service methods.
4. **Endpoints** — Add `GET /api/v1/identity/users` (paginated list with optional `ids` filter), and `GET /api/v1/identity/users/{id}` scoped to the authenticated caller's tenant.
5. **Policy Dashboard** — Update the grants list to resolve PAT → user attribution via the new endpoint.
