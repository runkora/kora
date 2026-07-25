# Inference Analytics - Product Requirements Document

**Status:** Implemented (Amended 2026-07-20)
**Created:** 2026-04-21
**Last Updated:** 2026-07-20
**Owner:** Platform Engineering
**Stakeholders:** Platform Engineering, Tenant Admins, Platform Admins, Individual API Consumers

---

## Executive Summary

The Orbit Model Gateway currently has a Usage tab populated entirely with mock data and no analytics visibility for individual users. This PRD defines two new analytics experiences: **My Usage** (personal token consumption for any authenticated user) and **Analytics** (tenant-wide aggregated analytics for platform and tenant admins). Both surfaces share the same chart components but operate over different data scopes, gated by a new `platform:analytics:read` policy scope for the admin view.

---

## Problem Statement

### Current State

The inference gateway records a detailed usage row for every request — capturing model, provider, token counts (including cache tokens), estimated cost, latency, routing strategy, and status. The Logs tab surfaces individual records to users. However:

- There is no aggregated time-series view of usage for individual users.
- There is no platform-wide aggregated analytics view for admins.
- The existing Usage tab in the Model Gateway UI is entirely mocked — no real data is displayed.
- There is no backend endpoint that aggregates usage into the daily/weekly/monthly buckets required by charts.

### User Pain Points

**Individual users:**
- Cannot see their total token consumption or estimated spend over time.
- Cannot identify trends in their usage (growing cost, shifting model mix, degrading cache hit rate).
- Must manually count rows in the Logs tab to understand their overall footprint.

**Platform and tenant admins:**
- Have no visibility into aggregate platform usage, spend, or error rates.
- Cannot identify which users or models are driving the most consumption.
- Cannot assess whether routing strategies are performing as intended.
- Have no latency trend data to detect provider degradation.

### Business Impact

- Without personal usage visibility, users cannot self-serve cost management or optimise their integration.
- Without admin analytics, operators cannot make informed decisions about model routing, policy, or capacity.
- The mock Usage tab erodes trust — users who discover it is fake lose confidence in the platform's observability story.

---

## Goals and Objectives

### Primary Goals

1. Give every authenticated user a personal **My Usage** tab showing their own token consumption, spend, and request trends over time.
2. Give platform and tenant admins an **Analytics** tab showing tenant-wide aggregates across all users and models.
3. Replace all mocked data in the current Usage tab with real data backed by a new analytics API.
4. Gate the Analytics tab behind a `platform:analytics:read` policy scope automatically assigned to `platform-admin` and `tenant-admin` roles.

### Success Metrics

- My Usage loads real data for any authenticated user within 2 seconds.
- Analytics loads real tenant-wide data for authorised admins within 2 seconds.
- Zero mock data remains in any production chart or KPI card.
- Unauthorised users receive a 403 on the analytics endpoint; the Analytics tab is not visible in the UI.
- Cost estimates in charts match the sum of `estimated_cost_usd` in the Logs tab to within rounding tolerance.

### Non-Goals

- Log export or CSV download.
- Budget alerts or spend thresholds.
- Real-time / push-based chart updates (polling on page load is sufficient for v1).
- Agent-attributed usage (agents are not yet a platform concept).
- Log retention policy or data archiving.

---

## User Stories

### My Usage — Individual User

**As a** Model Gateway user,
**I want to** see my total token consumption, estimated spend, and request counts over time,
**So that** I can understand my usage footprint and catch unexpected cost growth early.

**Acceptance Criteria:**
- [ ] My Usage tab is visible to all authenticated users in the Model Gateway.
- [ ] All charts and KPI cards show data scoped to the authenticated user's `principal_id` only.
- [ ] Data from other users is never returned, regardless of query parameters.
- [ ] KPI cards display: total requests, total tokens (prompt + completion + cache), estimated spend, average latency, error rate, and cache hit rate for the selected period.
- [ ] A spend-over-time chart shows daily estimated cost for the last 30 days, broken down by prompt vs output tokens.
- [ ] A tokens-over-time chart shows daily token counts for the last 30 days.
- [ ] A requests-over-time chart shows daily request counts broken down by status (success vs error) for the last 30 days.
- [ ] A latency chart shows daily p50/p95/p99 latency percentiles for the selected period.
- [ ] A cost-by-model breakdown shows the top models by estimated spend.
- [ ] All charts support granularity controls (daily, weekly, monthly) and period controls (7d, 30d, 90d) where applicable.

---

**As a** Model Gateway user,
**I want to** see which of my PATs is driving the most usage,
**So that** I can identify runaway keys or understand usage per integration.

**Acceptance Criteria:**
- [ ] My Usage charts are filterable by PAT (matching the filter behaviour on the Logs tab).
- [ ] When a PAT filter is active, all charts and KPI cards update to reflect only that key's activity.

---

### Analytics — Platform and Tenant Admin

**As a** platform or tenant admin,
**I want to** see tenant-wide usage, spend, and error trends aggregated across all users,
**So that** I can make informed decisions about model routing, policy, and capacity.

**Acceptance Criteria:**
- [ ] The Analytics tab is only visible to users with the `platform:analytics:read` scope.
- [ ] Users without the scope receive a 403 from the analytics API endpoint; the tab is hidden in the UI.
- [ ] All charts and KPI cards are identical in structure to My Usage but show data across the entire tenant.
- [ ] The `platform:analytics:read` scope is automatically assigned to the `platform-admin` and `tenant-admin` roles in the default role→scope mappings.
- [ ] Admins can filter charts by model to understand per-model trends.

---

**As a** platform admin,
**I want to** click a principal row on the Analytics tab and see that individual's full usage breakdown,
**So that** I can investigate a specific user's or agent's cost, token, and error trends without asking them to self-report.

**Acceptance Criteria:**
- [ ] Each row in the Principals table on the Analytics tab links to a per-principal usage view.
- [ ] The per-principal view shows the same time-series, KPIs, and cost-by-model breakdown as My Usage, scoped to the selected principal.
- [ ] The view is fed by a new endpoint, gated by `platform:analytics:read` — the same scope already required for the Analytics tab and Principals table, so no new scope or role is introduced.
- [ ] The per-principal view displays the principal's resolved `display_name`/`email` (already available from the Principals table's identity enrichment) so it's clear whose usage is shown; falls back to the raw `principal_id` when no profile exists.
- [ ] A caller without `platform:analytics:read` is denied at the policy layer, consistent with the existing tenant-wide analytics endpoints.

### Secondary User Stories

**As a** platform or tenant admin,
**I want to** filter the Analytics tab by model,
**So that** I can understand the cost and usage profile of a specific model or virtual model family.

**Acceptance Criteria:**
- [ ] A model filter dropdown is available on the Analytics tab.
- [ ] When a model filter is active, all charts and KPI cards update to reflect only requests for that model.

---

**As a** Model Gateway user with no prior usage,
**I want to** see a clear empty state on My Usage,
**So that** I understand the tab is working and know what to expect once I start making requests.

**Acceptance Criteria:**
- [ ] When a user has zero usage records, My Usage shows zero-state charts (axes present, no data points) rather than blank or error states.

---

## Proposed Solution

### High-Level Approach

`GET` endpoints on the inference gateway aggregate `inference_usage` records into daily buckets via SQL, returning pre-computed time-series data suitable for direct chart rendering:

- **`GET /v1/analytics/me`** — personal scope. Open to any authenticated caller (omitted from route-scope registration, following the `/v1/usage` pattern). The server always extracts `principal_id` and `tenant_id` from the token and enforces them as fixed SQL predicates — the client cannot supply either. Supports an optional `token_id` query parameter to narrow results to a single PAT.
- **`GET /v1/analytics`** — tenant-wide scope. Requires `platform:analytics:read`, registered in the route-scope vocabulary. Returns data across all users in the tenant. Supports an optional `model_id` filter.
- **`GET /v1/analytics/principals`** — tenant-wide scope, requires `platform:analytics:read`. Returns paginated, per-principal aggregate rows (cost, tokens, requests, error rate, latency) for the Principals table. Enriched with `email`/`display_name` in the BFF route via a join against `user_profiles`.
- **`GET /v1/analytics/principals/{id}`** *(added 2026-07-20)* — tenant-wide scope, requires `platform:analytics:read`. Returns the same response shape as `/v1/analytics/me` (time-series, KPIs, cost-by-model) but for the specified `principal_id` instead of the caller's own identity. Backs the admin drill-down from a Principals table row into that principal's full usage view.

`/v1/analytics/me` and `/v1/analytics/principals/{id}` return the same response shape. The My Usage tab calls `/v1/analytics/me`; the Analytics tab's Overview calls `/v1/analytics`; the Principals table calls `/v1/analytics/principals`; clicking a principal row calls `/v1/analytics/principals/{id}`.

The frontend replaces all mock data in the current Usage tab (renamed Analytics) with real API calls, and adds a new My Usage tab after Models in the Model Gateway nav using the same chart components.

### Navigation Structure

```
Model Gateway
├── Overview
├── Models
├── My Usage          ← new tab, all authenticated users
├── Logs
├── Catalog           ← admin only (existing)
└── Analytics         ← replaces Usage tab, admin only
```

### Chart Components (shared between My Usage and Analytics)

Both tabs render the same set of charts. Components are parameterised by data — they do not know whether the data is personal or tenant-wide.

| Chart | Description |
|---|---|
| KPI Cards | 6 cards: total requests, total tokens, estimated spend, avg latency, error rate, cache hit rate |
| Spend over time | Daily estimated cost (USD), prompt vs output breakdown, 30d default |
| Tokens over time | Daily token counts (prompt, completion, cache read), 30d default |
| Requests over time | Daily request counts, success vs error breakdown, 30d default |
| Latency percentiles | Daily p50/p95/p99, 30d default |
| Cost by model | Top models by estimated spend, bar chart |

### Access Control

A new scope `platform:analytics:read` is added to the platform vocabulary and baked into `defaultRoleScopes` for both `tenant-admin` and `platform-admin`. The inference gateway registers the analytics route with this required scope. The frontend reads the user's effective scopes (already available in the session) to conditionally render the Analytics tab.

### User Experience

#### My Usage — User Flow

1. User navigates to Model Gateway → **My Usage**.
2. The page loads with the default 30-day period and daily granularity.
3. KPI cards render at the top showing totals for the selected period.
4. Charts render below: spend over time, tokens over time, requests over time, latency percentiles, cost by model.
5. User can change the period (7d / 30d / 90d) or granularity (daily / weekly / monthly) using controls at the top — all charts update simultaneously.
6. User can select a PAT from the filter dropdown to narrow all charts to a single key.
7. If the user has no usage records, an empty state is shown with copy explaining that usage will appear once requests are made.

#### Analytics — Admin Flow

1. Admin navigates to Model Gateway → **Analytics** (tab only visible to `platform-admin` and `tenant-admin`).
2. Page loads with the default 30-day period showing tenant-wide aggregates.
3. All charts reflect usage across all users and PATs in the tenant.
4. Admin can optionally filter by model using the model dropdown.
5. Admin can change period and granularity controls identically to My Usage.

### Key Components

- **`GET /v1/analytics/me` endpoint** — personal analytics, open to any authenticated caller. Aggregates `inference_usage` via SQL for the authenticated `principal_id`; supports optional `token_id` PAT filter.
- **`GET /v1/analytics` endpoint** — tenant-wide analytics, requires `platform:analytics:read`. Aggregates across all users; supports optional `model_id` filter. Both endpoints return the same response shape.
- **`GET /v1/analytics/principals` endpoint** — tenant-wide per-principal aggregate rows backing the Principals table; requires `platform:analytics:read`.
- **`GET /v1/analytics/principals/{id}` endpoint** — per-principal drill-down, same response shape as `/v1/analytics/me`; requires `platform:analytics:read`.
- **`platform:analytics:read` scope** — new vocabulary entry; baked into `defaultRoleScopes` for `tenant-admin` and `platform-admin`; enforced by the inference gateway's OPA policy middleware.
- **Analytics context / hook** — frontend data-fetching layer, mirrors the pattern established by `LogsContext`. Shared by both My Usage and Analytics tabs, parameterised by scope.
- **Chart components** — the six chart components from the current (mocked) Usage tab, de-coupled from mock data and accepting real API response shapes.
- **My Usage tab** — new route at `/model-gateway/my-usage`, visible to all authenticated users.
- **Analytics tab** — existing `/model-gateway/usage` route renamed to `/model-gateway/analytics`, visibility gated on session scope.

---

## Scope

### In Scope

- `GET /v1/analytics/me` backend endpoint (personal, any authenticated caller)
- `GET /v1/analytics` backend endpoint (tenant-wide, requires `platform:analytics:read`)
- `GET /v1/analytics/principals` backend endpoint (tenant-wide per-principal rows, requires `platform:analytics:read`)
- `GET /v1/analytics/principals/{id}` backend endpoint (per-principal full breakdown, same shape as `/v1/analytics/me`, requires `platform:analytics:read`)
- Analytics tab: clicking a principal row on the Principals table links to that principal's full usage breakdown (reuses My Usage chart components)
- `platform:analytics:read` scope added to vocabulary and default role mappings
- **My Usage** tab: personal analytics scoped to `principal_id`, visible to all users
- **Analytics** tab: tenant-wide analytics, visible only to `platform-admin` and `tenant-admin`
- All 6 chart types listed above, wired to real data in both tabs
- PAT filter support on My Usage (matching Logs tab behaviour)
- Model filter support on Analytics
- Period and granularity controls (7d / 30d / 90d, daily / weekly / monthly)
- Removal of all mock data from `_data/mock.ts` used by charts

### Out of Scope

- Spend alerts or budget thresholds
- CSV export or data download
- Real-time updates (page-load fetch is sufficient)
- Cache token cost breakdown as a separate chart series (included in total cost but not split out in v1)
- Hourly granularity

### Future Considerations

- Anomaly detection or cost spike alerts
- Comparison view: current period vs previous period deltas on KPI cards
- Provider-level breakdown chart
- Export to CSV / BI tool integration

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `GET /v1/analytics/me` endpoint returning daily aggregated time-series data for the authenticated principal
- [ ] `GET /v1/analytics` endpoint returning tenant-wide aggregated time-series data (requires `platform:analytics:read`)
- [ ] Tenant-wide aggregation path requiring `platform:analytics:read` scope
- [ ] `platform:analytics:read` scope registered in vocabulary and assigned to `tenant-admin` and `platform-admin` by default
- [ ] My Usage tab visible to all authenticated users, data scoped to their `principal_id`
- [ ] Analytics tab visible only when `platform:analytics:read` scope is present in session
- [ ] KPI cards: total requests, total tokens, estimated spend, avg latency, error rate, cache hit rate
- [ ] Spend-over-time chart (daily, 30d default, prompt vs output breakdown)
- [ ] Tokens-over-time chart (daily, 30d default)
- [ ] Requests-over-time chart (daily, 30d default, success vs error)
- [ ] Latency percentiles chart (daily p50/p95/p99, 30d default)
- [ ] Cost-by-model chart (top models by spend)
- [ ] Period selector: 7d, 30d, 90d
- [ ] Granularity selector: daily, weekly, monthly
- [ ] PAT filter on My Usage
- [ ] All mock data removed from chart components
- [ ] `GET /v1/analytics/principals/{id}` endpoint returning the same shape as `/v1/analytics/me` for the specified principal, scoped to the tenant, requiring `platform:analytics:read`
- [ ] Analytics tab: clicking a principal row navigates to that principal's full usage breakdown, reusing My Usage chart components and carrying the resolved `display_name`/`email`

**Should Have (P1):**
- [ ] Model filter on Analytics tab
- [ ] Cache hit rate calculated from `cache_read_input_tokens` vs total input tokens
- [ ] Empty state handling when a user has no usage records

**Nice to Have (P2):**
- [ ] Period-over-period delta indicators on KPI cards (e.g. "+12% vs last period")
- [ ] Tooltip on charts showing exact values per day

### Non-Functional Requirements

**Performance:**
- Analytics endpoint response time ≤ 500ms p95 for 90-day personal queries
- Analytics endpoint response time ≤ 1000ms p95 for 90-day tenant-wide queries
- SQL aggregation uses indexes on `(tenant_id, created_at)` and `(tenant_id, principal_id, created_at)`

**Security:**
- My Usage endpoint enforces `principal_id` filtering server-side; client-supplied `principal_id` is ignored in favour of the authenticated identity
- Analytics endpoint returns 403 for any principal without `platform:analytics:read`
- No prompt or completion content is ever included in analytics responses

**Observability:**
- Analytics endpoint emits OTel spans with `app.analytics.scope` attribute (`personal` vs `tenant`)
- Slow queries (> 200ms) logged with query parameters for investigation

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Notes |
|---|---|---|
| `inference_usage` table | Available | All required columns exist including cache token columns (this branch) |
| Policy scope vocabulary + role mappings | Available | Adding `platform:analytics:read` follows the same pattern as existing scopes |
| Inference gateway route registration | Available | Self-registration pattern established in `cmd/inference-gateway/main.go` |
| Frontend session scopes | Available | Effective scopes already available in the BFF session for tab visibility gating |

### External Dependencies

None. The analytics endpoint aggregates data already stored in PostgreSQL — no new infrastructure required.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| SQL aggregation is slow on large datasets | Medium | Medium | Partial index on `(tenant_id, created_at)` already exists; add composite index on `(tenant_id, principal_id, created_at)` if needed |
| Cache hit rate definition is ambiguous | Low | Low | Define clearly: `SUM(cache_read_input_tokens) / (SUM(prompt_tokens) + SUM(cache_read_input_tokens))` across the period (sum-then-divide, not per-request average) |
| Latency percentile calculation in SQL is non-trivial | Medium | Low | Use PostgreSQL `percentile_cont` aggregate; test against representative dataset |
| Users expect real-time data but get stale-on-load | Low | Medium | Add a Refresh button (already built on the Logs tab) and document that data reflects committed requests |

---

## Alternatives Considered

### Alternative 1: BFF-side aggregation (fetch raw records, aggregate in Node)

Fetch pages of raw `inference_usage` records from `/v1/usage` in the BFF and aggregate into buckets server-side in TypeScript.

**Why not chosen:**
- 90-day queries could involve thousands of records per user and tens of thousands tenant-wide
- Pagination overhead makes this impractical for anything beyond a few hundred records
- SQL aggregation with `GROUP BY date_trunc` is the correct tool for this problem

### Alternative 2: Separate analytics service

Build a dedicated analytics microservice with its own read replica and pre-materialised aggregation tables.

**Why not chosen:**
- Premature for current scale
- The inference gateway already owns the `inference_usage` table; adding a read endpoint there is the right boundary
- Materialised views or a separate service can be introduced later if query latency becomes a problem

---

## Open Questions

1. **Should My Usage be visible when a user has zero records, or hidden until first use?**
   - **Status:** Decided — always visible; show zero-state charts.
   - **Owner:** Platform Engineering

2. **Should Analytics show a "no data" empty state or be hidden entirely for tenants with no usage?**
   - **Status:** Open
   - **Owner:** Platform Engineering

3. **Is 90 days the right maximum lookback for admins, or should further history be queryable?**
   - **Status:** Open
   - **Owner:** Platform Engineering

---

## Timeline and Milestones

### Phases

**Phase 1: Backend analytics endpoint** (Target: 2026-04-28)
- `GET /v1/analytics/me` — personal scope, authenticated-only
- `GET /v1/analytics` — tenant-wide scope, requires `platform:analytics:read`
- Personal scope (filtered by authenticated `principal_id`)
- Tenant-wide scope (requires `platform:analytics:read`)
- `platform:analytics:read` added to vocabulary and default role mappings
- Route registered as authenticated-only (scope enforcement handled inside the handler)

**Phase 2: My Usage tab** (Target: 2026-05-05)
- New tab after Models in Model Gateway nav, visible to all users
- All 6 chart components wired to real `/v1/analytics` data
- PAT filter support
- Mock data removed from chart components

**Phase 3: Analytics tab** (Target: 2026-05-09)
- Existing Usage tab renamed to Analytics, route updated to `/model-gateway/analytics`
- Tab visibility gated on `platform:analytics:read` in session
- All 6 chart components wired to tenant-wide `/v1/analytics` data
- Model filter support
- All remaining mock data removed

### Key Milestones

- [ ] PRD Approved: 2026-04-21
- [ ] ADR Created: 2026-04-21
- [ ] Phase 1 Complete (Backend endpoint): 2026-04-28
- [ ] Phase 2 Complete (My Usage tab): 2026-05-05
- [ ] Phase 3 Complete (Analytics tab): 2026-05-09

---

## Appendix

### Related Documents

- [PRD: Inference Logs](inference-logs.md) — per-request log visibility that this feature extends with aggregation
- [PRD: Inference Gateway](inference-gateway.md) — the underlying gateway whose usage data is being surfaced
- [inference-analytics](../adr/2026-04-27-inference-analytics.md) — Inference Analytics Architecture

### References

- [PostgreSQL `date_trunc` function](https://www.postgresql.org/docs/current/functions-datetime.html) — used for daily/weekly/monthly bucketing in SQL aggregation
- [PostgreSQL `percentile_cont`](https://www.postgresql.org/docs/current/functions-aggregate.html) — ordered-set aggregate for p50/p95/p99 latency calculation

### Glossary

| Term | Definition |
|---|---|
| KPI Card | A summary statistic displayed prominently at the top of an analytics page (e.g. total requests, estimated spend) |
| My Usage | The personal analytics tab scoped to the authenticated user's own inference activity |
| Analytics | The tenant-wide analytics tab visible only to platform and tenant admins |
| `principal_id` | The unique identifier of the authenticated caller (user sub or PAT UUID) recorded on every inference usage record |
| Cache Hit Rate | The ratio of `cache_read_input_tokens` to total input tokens (`prompt_tokens + cache_read_input_tokens`), indicating how effectively prompt caching is being utilised |
| `platform:analytics:read` | The policy scope that grants access to the tenant-wide Analytics tab and API endpoint |
| Granularity | The time bucket size used in chart aggregation: daily, weekly, or monthly |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______

---

**Last Updated:** 2026-04-21
