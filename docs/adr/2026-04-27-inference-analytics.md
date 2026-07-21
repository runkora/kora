# Inference Analytics: My Usage and Analytics Tabs

**Status:** Accepted (Amended 2026-07-20)
**Date:** 2026-04-27
**Deciders:** Platform Engineering
**Technical Story:** The Model Gateway UI has a Usage tab populated entirely with mock data. Two distinct audiences need analytics: individual users wanting personal token/spend visibility, and platform/tenant admins wanting tenant-wide aggregates. This ADR defines the API design, access control model, and frontend architecture for both surfaces.

---

## Context

### Background

The inference gateway records a detailed `inference_usage` row for every request: model, provider, token counts (prompt, completion, cache read, cache creation), estimated cost, latency, routing strategy, and status. The Logs tab ([inference-usage-logs](2026-04-17-inference-usage-logs.md)) surfaces individual records. No aggregated time-series view exists for either audience.

The current Usage tab in the Model Gateway UI is entirely mocked — `_data/mock.ts` supplies all chart data. No backend analytics endpoint exists.

### Problem Statement

1. Individual users cannot see their aggregate token consumption, spend trends, or cache efficiency over time.
2. Platform and tenant admins cannot see tenant-wide usage aggregates, model breakdowns, or latency trends.
3. The mock Usage tab misleads users about the platform's observability capabilities.

### Goals

- Deliver a **My Usage** tab (personal scope) visible to all authenticated users.
- Deliver an **Analytics** tab (tenant scope) visible only to `platform-admin` and `tenant-admin`.
- Back both tabs with a real backend analytics endpoint that aggregates `inference_usage` via SQL.
- Gate the tenant-wide path behind a new `platform:analytics:read` policy scope.

### Non-Goals

- Materialized views or a separate analytics service (addressed if query latency becomes a problem).
- Budget alerts, export, or real-time streaming updates.
- Agent-attributed usage (agents not yet a platform concept).

---

## Decision Drivers

* **Correct data boundary** — the inference gateway owns `inference_usage`; analytics belongs there, not in a BFF or separate service.
* **SQL is the right aggregation tool** — `GROUP BY date_trunc` and `percentile_cont` handle the required bucketing and percentile calculations natively; iterating paginated records in TypeScript does not.
* **Consistency with existing policy model** — access control follows the established `platform:<resource>:<action>` scope pattern and `defaultRoleScopes` mechanism.
* **Shared components, different scope** — My Usage and Analytics are the same UX parameterised by data scope; a single set of chart components avoids duplication.
* **Security by default** — personal data is always server-enforced by the authenticated `principal_id`; the client cannot widen its own scope.

---

## Considered Options

### Option A: BFF-side aggregation

Fetch paginated raw records from `GET /v1/usage` in the Next.js BFF and aggregate into daily buckets in TypeScript.

**Pros:**
- No backend changes required.

**Cons:**
- A 90-day window for a busy user could involve thousands of records requiring many paginated fetches.
- Tenant-wide aggregation for an admin would be impractical — could involve tens of thousands of records.
- `percentile_cont` for latency percentiles has no ergonomic equivalent in JavaScript without loading all raw values into memory.

### Option B: Separate analytics microservice

Build a dedicated analytics service with its own database read replica and pre-materialised aggregation tables updated on a schedule.

**Pros:**
- Read workload isolated from the inference gateway's write path.
- Can support more complex queries without impacting gateway latency.

**Cons:**
- Premature for current scale — a new service, deployment, and infrastructure for a single SQL query.
- The inference gateway already owns `inference_usage`; splitting read access introduces cross-service coupling.
- Materialised views inside the existing database achieve the same read isolation at a fraction of the operational cost, and can be introduced later if needed.

### Option C: Single endpoint on the inference gateway with SQL aggregation (Chosen)

Add `GET /v1/analytics` to the inference gateway. The endpoint runs a single SQL query using `GROUP BY date_trunc` for bucketing and `percentile_cont` for latency percentiles. It filters by the authenticated `principal_id` for the personal path, or returns tenant-wide data for callers with `platform:analytics:read`.

**Pros:**
- SQL aggregation is the right tool — efficient, expressive, and already available.
- The inference gateway already owns the table and the authentication/authorization middleware.
- One endpoint, one contract — both tabs consume the same shape; scope determines the data.
- No new infrastructure.

**Cons:**
- Aggregation query runs on the primary database. Acceptable at current scale; a read replica or materialised view can be introduced later if needed.

---

## Decision

**Chosen Option: Option C — single `GET /v1/analytics` endpoint on the inference gateway with SQL aggregation.**

### Rationale

The inference gateway already owns `inference_usage`, runs the authentication middleware, and registers its routes with the policy service. Adding a read endpoint there requires no new service, no new infrastructure, and follows the same pattern as `GET /v1/usage`. PostgreSQL's `date_trunc` and `percentile_cont` aggregates handle the bucketing and percentile requirements natively and efficiently. BFF-side aggregation cannot scale to the tenant-wide case, and a separate service is premature.

---

## Consequences

### Positive

- Both tabs backed by real data with no mock data remaining.
- Personal data isolation is enforced server-side — the client cannot request another user's data regardless of query parameters.
- Shared chart components across My Usage and Analytics eliminates duplication.
- `platform:analytics:read` follows the established scope pattern; no new policy infrastructure required.
- *(2026-07-20)* Admins can drill into any principal's full usage breakdown directly from the Principals table, without a new scope, role, or infrastructure — `GET /v1/analytics/principals/{id}` reuses the personal-path aggregation query and the existing `platform:analytics:read` gate.

### Negative

- The aggregation query adds read load to the primary database. Needs monitoring; a read replica can be wired in later without API changes.
- `percentile_cont` requires all latency values within the window to be loaded into the aggregate — memory usage grows with query window size and record volume. If this becomes a problem, `percentile_disc` (approximate) or pre-bucketed histogram columns are viable mitigations without an API change.

### Neutral

- The Usage tab route (`/model-gateway/usage`) is renamed to `/model-gateway/analytics`; any existing bookmarks will break.
- My Usage is a new route (`/model-gateway/my-usage`) with no prior state to migrate.

---

## Implementation

### API endpoints

Two separate routes serve the two audiences. Each has a single, unambiguous responsibility.

#### `GET /v1/analytics/me` — personal (My Usage)

```
GET /v1/analytics/me?days=30&granularity=daily
Authorization: Bearer <session or PAT token>
```

Open to any authenticated caller — omitted from `inferenceRoutes()`, following the same pattern as `/v1/usage`. The handler always extracts `principal_id` and `tenant_id` from the token and enforces them as fixed SQL predicates. The caller cannot supply either as query parameters.

**Query parameters:**

| Parameter | Values | Default | Notes |
|---|---|---|---|
| `days` | `7`, `30`, `90` | `30` | Lookback window in days |
| `granularity` | `daily`, `weekly`, `monthly` | `daily` | Bucket size for time-series charts |
| `token_id` | UUID | — | Optional PAT filter; narrows results to a single PAT within the caller's own usage |

#### `GET /v1/analytics` — tenant-wide (Analytics)

```
GET /v1/analytics?days=30&granularity=daily
Authorization: Bearer <session or PAT token>
```

Requires `platform:analytics:read`, registered in `inferenceRoutes()`. Returns tenant-wide data with no `principal_id` filter. `tenant_id` is still always applied from the authenticated identity.

**Query parameters:**

| Parameter | Values | Default | Notes |
|---|---|---|---|
| `days` | `7`, `30`, `90` | `30` | Lookback window in days |
| `granularity` | `daily`, `weekly`, `monthly` | `daily` | Bucket size for time-series charts |
| `model_id` | string | — | Optional model filter |

**Response shape (identical for both endpoints):**

```json
{
  "scope": "personal",
  "period_days": 30,
  "granularity": "daily",
  "kpis": {
    "total_requests": 1420,
    "total_tokens": 8312400,
    "estimated_cost_usd": 24.94,
    "avg_latency_ms": 1840,
    "error_rate": 0.021,
    "cache_hit_rate": 0.34
  },
  "buckets": [
    {
      "date": "2026-04-01",
      "requests": 48,
      "successful_requests": 47,
      "failed_requests": 1,
      "prompt_tokens": 142000,
      "completion_tokens": 38000,
      "cache_read_tokens": 62000,
      "estimated_cost_usd": 0.83,
      "p50_latency_ms": 1600,
      "p95_latency_ms": 3200,
      "p99_latency_ms": 5100
    }
  ],
  "cost_by_model": [
    { "model_id": "claude-sonnet", "estimated_cost_usd": 18.40 },
    { "model_id": "gpt-4o", "estimated_cost_usd": 6.54 }
  ]
}
```

**SQL aggregation (daily buckets — personal path shown; tenant path omits the `principal_id` predicate):**

```sql
SELECT
    date_trunc('day', created_at AT TIME ZONE 'UTC') AS date,
    COUNT(*)                                          AS requests,
    COUNT(*) FILTER (WHERE status = 'success')        AS successful_requests,
    COUNT(*) FILTER (WHERE status != 'success')       AS failed_requests,
    SUM(prompt_tokens)                                AS prompt_tokens,
    SUM(completion_tokens)                            AS completion_tokens,
    SUM(cache_read_input_tokens)                      AS cache_read_tokens,
    SUM(estimated_cost_usd)                           AS estimated_cost_usd,
    percentile_cont(0.50) WITHIN GROUP (ORDER BY latency_ms) AS p50_latency_ms,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) AS p95_latency_ms,
    percentile_cont(0.99) WITHIN GROUP (ORDER BY latency_ms) AS p99_latency_ms
FROM inference_usage
WHERE tenant_id = $1
  AND created_at >= NOW() - ($2 || ' days')::INTERVAL
  AND principal_id = $3          -- personal path only; omitted for tenant-wide
  -- AND token_id = $4           -- optional PAT filter (personal path only)
  -- AND model_id = $4           -- optional model filter (tenant-wide path only)
GROUP BY 1
ORDER BY 1 ASC
```

**KPI derivations:**

- `cache_hit_rate` = `SUM(cache_read_input_tokens) / (SUM(prompt_tokens) + SUM(cache_read_input_tokens))` — zero when no cache tokens present.
- `error_rate` = `failed_requests / total_requests` — zero when no requests.
- `avg_latency_ms` = `AVG(latency_ms)` across the full window (not a daily average of daily averages).

#### `GET /v1/analytics/principals` — tenant-wide list (Principals table)

*Documented retroactively 2026-07-20 — this endpoint shipped after the original ADR date without an accompanying doc update.*

```
GET /v1/analytics/principals?days=30&page=1&limit=20&sort_by=cost&order=desc
Authorization: Bearer <session or PAT token>
```

Requires `platform:analytics:read`, same as `/v1/analytics`. Returns paginated, per-principal aggregate rows (not a time-series) — one row per `principal_id` with `request_count`, `total_tokens`, `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `estimated_cost_usd`, `error_rate`, `avg_latency_ms`, and `last_active_at`, aggregated via a single `GROUP BY principal_id` query (`PostgresInferenceUsageRepository.ListPrincipalUsage`). Sortable by any of those columns via an allowlisted `sort_by` → SQL column map; paginated via `page`/`limit` (default 20, max 100).

The Next.js BFF route (`/api/analytics/principals`) enriches each row with `email`/`display_name` by joining against `user_profiles` (see [user-profiles-table-for-principal-attribution](2026-04-19-user-profiles-table-for-principal-attribution.md)), falling back to the raw `principal_id` when no profile exists. It also injects synthetic zero-activity rows for tenant users with no usage in the window, when the result set fits on a single page.

#### `GET /v1/analytics/principals/{id}` — per-principal drill-down (added 2026-07-20)

```
GET /v1/analytics/principals/{id}?days=30&granularity=daily
Authorization: Bearer <session or PAT token>
```

Requires `platform:analytics:read` — the same scope already gating `/v1/analytics` and `/v1/analytics/principals`; no new scope or role mapping is introduced. `{id}` is the target `principal_id`; `tenant_id` is still always sourced from the authenticated caller's token, so a request can never cross tenants.

Returns the **same response shape as `GET /v1/analytics/me`** (KPIs, daily/weekly/monthly buckets, cost-by-model) but for the specified principal instead of the caller's own identity. Reuses the personal-path SQL aggregation, substituting the query-supplied `principal_id` for the JWT-derived one — the only place in the analytics surface where a caller can request another principal's breakdown, and it is only reachable with `platform:analytics:read`.

**Query parameters:**

| Parameter | Values | Default | Notes |
|---|---|---|---|
| `days` | `7`, `30`, `90` | `30` | Lookback window in days |
| `granularity` | `daily`, `weekly`, `monthly` | `daily` | Bucket size for time-series charts |

Frontend: each row in the Principals table links to a per-principal usage view built from the same chart components as My Usage, fed by this endpoint. The link carries the `display_name`/`email` already resolved by the `/v1/analytics/principals` BFF route so the view can show whose usage it is without a second identity lookup.

### Required Indexes

The aggregation query requires two indexes to meet the performance targets:

- `(tenant_id, created_at)` — already exists as a partial index; covers the tenant-wide path.
- `(tenant_id, principal_id, created_at)` — add if not present; covers the personal path and avoids a full tenant scan when `principal_id` is applied.

### Access Control: `platform:analytics:read`

`/v1/analytics/me` is omitted from `inferenceRoutes()` — open to any authenticated caller, consistent with `/v1/usage`.

`/v1/analytics` is registered in `inferenceRoutes()` with the required scope:

```go
// cmd/inference-gateway/main.go — inferenceRoutes()
{PathPrefix: "/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"},
```

This prefix match also covers `/v1/analytics/principals` and `/v1/analytics/principals/{id}` — both require `platform:analytics:read` and need no separate route-scope entry.

The scope is declared in `inferenceScopes()`:

```go
{Scope: "platform:analytics:read", ResourceType: "platform", PathSegments: []string{"analytics"}, Action: "read", Description: "Read tenant-wide inference analytics"},
```

The `defaultRoleScopes` map in `bundle/compiler.go` is updated:

```go
"tenant-admin":   {...existing..., "platform:analytics:read"},
"platform-admin": {...existing..., "platform:analytics:read"},
```

### Frontend Architecture

**Navigation changes:**

```
model-gateway/
├── page.tsx              (Overview)
├── models/               (Models)
├── my-usage/             ← new, all users
│   └── page.tsx
├── logs/                 (Logs)
├── catalog/              (admin only, existing)
└── analytics/            ← renamed from usage/
    └── page.tsx
```

**Analytics context:**

A new `AnalyticsContext` (mirroring `LogsContext`) fetches from `GET /v1/analytics` and exposes the response to child components. Both My Usage and Analytics pages use the same context; the page-level RSC passes the correct scope signal via props.

**Tab visibility gating:**

The Model Gateway nav reads `session.scopes` (already available in the BFF session) to conditionally render the Analytics tab link. The My Usage tab link is always rendered.

**Chart components:**

The six chart components in `usage/_components/` are de-coupled from `mock.ts` and accept the `AnalyticsResponse` shape directly. No new chart components are created — existing ones are rewired.

---

## Validation

### Success Metrics

- `GET /v1/analytics/me` p95 response time ≤ 500ms for a 90-day query.
- `GET /v1/analytics` p95 response time ≤ 1000ms for a 90-day tenant-wide query.
- A user querying their own analytics cannot retrieve another user's data (verified by integration test).
- A caller without `platform:analytics:read` receives 403 on the tenant-wide path.
- KPI card totals on My Usage match the sum of `estimated_cost_usd` across the same period in the Logs tab within floating-point rounding tolerance.

### Monitoring

- OTel span attribute `app.analytics.scope` = `personal` | `tenant` on every analytics request.
- Log warning when aggregation query exceeds 200ms, including `days`, `granularity`, and `scope`.

---

## Related Decisions

- [inference-usage-logs](2026-04-17-inference-usage-logs.md) — Inference usage logs (the per-request records this feature aggregates)
- [pat-scope-assignment-via-explicit-grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) — PAT scope grants (the `platform:analytics:read` scope follows the same vocabulary pattern)
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Route-to-scope registration used to wire `/v1/analytics` into the policy evaluator
- [user-profiles-table-for-principal-attribution](2026-04-19-user-profiles-table-for-principal-attribution.md) — `user_profiles` table used to resolve `display_name`/`email` for Principals table rows and the per-principal drill-down view

---

## References

- [PRD: Inference Analytics](../prd/inference-analytics.md)
- [PostgreSQL `date_trunc`](https://www.postgresql.org/docs/current/functions-datetime.html)
- [PostgreSQL `percentile_cont`](https://www.postgresql.org/docs/current/functions-aggregate.html)

---

## Notes

**Why are there two endpoints rather than one dual-mode endpoint?** A single endpoint that changes behaviour based on whether the caller holds `platform:analytics:read` cannot be properly registered in the route-scope vocabulary — the route either requires the scope (blocking personal users) or doesn't (leaving the admin path without policy enforcement). Two routes gives each path a clean, single responsibility and allows them to evolve independently.

**Why `days` not `from`/`to` date parameters?** Relative windows (7d, 30d, 90d) are what the UI controls expose and are safe against unbounded queries. Absolute date ranges can be added later once query performance is validated.

**Last Updated:** 2026-07-20





