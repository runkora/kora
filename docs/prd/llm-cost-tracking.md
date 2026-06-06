# LLM Cost Tracking - Product Requirements Document

**Status:** Approved  
**Created:** 2026-05-29  
**Last Updated:** 2026-06-05  
**Owner:** Product  
**Stakeholders:** Platform Engineering, Security, Frontend

---

## Executive Summary

Orbit's inference gateway captures token counts on every request, but the dollar cost written to `inference_usage.estimated_cost_usd` is frequently `$0.00` because provider platforms like Bedrock do not expose pricing through their inference APIs. This PRD covers the complete feature set required to make cost data accurate and actionable: an automated pricing data pipeline to record correct dollar costs at spend time, per-principal monthly budget enforcement with hard-block semantics, quota groups for scalable budget assignment, in-app threshold alerts, and role-appropriate cost dashboards for platform operators, tenant admins, and individual developers.

---

## Problem Statement

### Current State

The inference gateway writes one `InferenceUsageRecord` to PostgreSQL per request. The `estimated_cost_usd` field is computed using prices stored in `virtual_model_routes.input_cost_per_1m` and `output_cost_per_1m`. In practice:

- The wired `PricingCache` implementation is `NewNopPricingCache()` — a no-op that always returns `$0.00`.
- The `virtual_model_routes` pricing columns are populated manually, making them stale by default and absent for most models. Pricing is coupled to the routing table rather than maintained as a first-class catalog.
- Providers such as AWS Bedrock, Google Vertex AI, and Azure OpenAI do not surface per-request dollar cost in their API responses — Orbit must resolve prices independently.
- The analytics API (`GET /v1/analytics`, `GET /v1/analytics/me`) already aggregates `SUM(estimated_cost_usd)` correctly, but the underlying values are almost always zero, making the data meaningless.
- ADR-0039 explicitly deferred budget controls. No quota enforcement exists. There is no mechanism to constrain or alert on agent or human spending.

### User Pain Points

- **Platform operators** have no way to see what any tenant, agent, or model is costing them. Chargeback and capacity planning are impossible.
- **Tenant admins** cannot set guardrails on agent spending. A runaway agent loop has no cost ceiling.
- **Individual developers** cannot see the real cost of their own inference calls or understand which models are cheaper.
- **On-call engineers** have no early warning when a tenant or agent is approaching an unusual spend level — they find out after the fact from the cloud bill.

### Business Impact

- Without accurate cost attribution, Orbit cannot support tenant chargeback or usage-based pricing tiers.
- The inability to set budget limits is a blocker for production use of autonomous agents — operators will not deploy agents they cannot financially constrain.
- Inaccurate `$0.00` costs undermine trust in the analytics feature that has already been built.

---

## Goals and Objectives

### Primary Goals

1. Record accurate dollar cost on every inference request at the moment of spend, for all supported providers.
2. Enable per-principal monthly budget caps with hard-block enforcement.
3. Surface cost data through role-appropriate dashboards for operators, tenant admins, and individual developers.

### Success Metrics

- `estimated_cost_usd > 0` on ≥ 99% of successful inference requests where the model is in the pricing catalog.
- Budget hard-block fires correctly: zero successful inference requests are processed after a principal's monthly budget is exceeded.
- `spend_limit_reached` correctly reflects `true` whenever `current_spend_usd >= monthly_budget_usd` on the quota API.
- Cost dashboard p95 load time < 500 ms.

### Non-Goals

- Real-time per-request price fetching from provider APIs (not feasible — Bedrock, Vertex, Azure do not expose this).
- Tenant-level budget caps (out of scope for this release; per-principal caps are sufficient).
- Invoice generation, billing integration, or payment processing.
- Cross-tenant comparative reporting (ranking tenants by spend against each other). The platform operator dashboard shows aggregate totals and per-tenant breakdowns, but does not surface one tenant's data to another tenant or produce cross-tenant league tables.
- Cost allocation tags on individual inference requests.
- RPM / TPM rate limiting (a separate concern, deferred per ADR-0039).

---

## User Stories

### Primary User Stories

**As a** platform operator,  
**I want to** see a dashboard of total inference spend broken down by model, provider, and time period,  
**So that** I can understand the cost profile of the platform and identify unexpectedly expensive models or traffic spikes.

**Acceptance Criteria:**
- [ ] Operator dashboard shows total spend (USD) over a selectable time range.
- [ ] Spend is broken down by virtual model and by provider.
- [ ] A time-series chart shows spend bucketed by day or hour.
- [ ] Data reflects real dollar costs, not `$0.00` placeholders.

---

**As a** tenant admin,  
**I want to** set a monthly USD budget cap on individual agents or users,  
**So that** a misbehaving or looping agent cannot accumulate unbounded costs.

**Acceptance Criteria:**
- [ ] Admin can configure a monthly budget cap (USD) for any principal in their tenant.
- [ ] Once a principal's monthly spend reaches 100% of their cap, all further inference requests from that principal are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, and recorded with `status = "policy_denied"`.
- [ ] The gateway returns `spend_limit_reached: true` on the quota API when the principal's spend meets or exceeds their budget. The frontend uses this flag to display a banner; no threshold percentage is involved.
- [ ] The budget resets automatically at the start of each calendar month (00:00 UTC on the 1st).
- [ ] Deleting a quota removes the cap immediately; spend already recorded for the month is not adjusted, and in-flight requests complete normally.

---

**As a** tenant admin,  
**I want to** define quota groups (e.g. "engineer", "agent") and assign principals to them,  
**So that** I can apply a shared budget policy to a range of principals without configuring each one individually.

**Acceptance Criteria:**
- [ ] Admin can create, update, and delete named quota groups within their tenant.
- [ ] Each quota group defines a `monthly_budget_usd` that applies individually to every principal assigned to the group (not a shared pool).
- [ ] Admin can assign any principal to a quota group.
- [ ] Admin can set a principal-level quota override that takes precedence over the group quota for that principal.
- [ ] Budget enforcement at request time follows: individual quota (if set) → group quota (if assigned) → no limit.
- [ ] Removing a principal's individual override reverts them to their group quota.

---

**As a** developer or agent operator,  
**I want to** view my own inference costs over time, broken down by model,  
**So that** I can understand my usage patterns and choose more cost-efficient models.

**Acceptance Criteria:**
- [ ] The existing `/model-gateway/my-usage` page displays accurate dollar costs for the authenticated principal once the pricing pipeline is live (no `$0.00` placeholders).
- [ ] Cost is broken down by virtual model.
- [ ] A time-series chart shows spend over the selected period.
- [ ] If the principal has a budget set and their current spend meets or exceeds it, a notification banner is visible on the page. The backend computes and returns `spend_limit_reached: bool` on the quota API; the frontend renders the banner when true.

---

**As a** platform operator,  
**I want to** pricing data to be automatically synchronized from an authoritative source and allow me to override any price,  
**So that** cost figures in the system remain accurate without requiring manual updates, and I can correct prices that are wrong or not yet in the catalog.

**Acceptance Criteria:**
- [ ] A background sync process fetches pricing from LiteLLM's `model_prices_and_context_window.json` on a daily schedule and writes to the `model_pricing` catalog table.
- [ ] Each sync run writes a timestamped snapshot of every model's synced prices to a `model_pricing_snapshots` table. Override changes also write a snapshot entry at the time of the change, recording the effective price at that moment. Together these preserve a complete, point-in-time-queryable price history for audit and retroactive cost recomputation.
- [ ] Platform admins can set a manual price override for any `(provider, model_id)` pair; the sync always refreshes the synced price columns but never clears or overwrites override column values.
- [ ] Platform admins can clear an override to revert a model to the synced price.
- [ ] At request time, price resolution follows this order: platform admin override → synced catalog price → `$0.00` with `price_unknown` flag.
- [ ] If the sync source does not have a price for a model, `estimated_cost_usd` records `$0.00` and the record is flagged.

---

### Secondary User Stories

**As a** tenant admin,  
**I want to** see a per-principal cost leaderboard for my tenant,  
**So that** I can identify which agents or users are the highest spenders.

**Acceptance Criteria:**
- [ ] A "principals" view shows each principal in the tenant ranked by total spend for the selected period.
- [ ] The view displays spend, token count, and request count per principal.

---

## Proposed Solution

### High-Level Approach

The feature is delivered in three layers:

**1. Pricing Data Pipeline**  
A background sync worker pulls model pricing from LiteLLM's `model_prices_and_context_window.json` on a daily schedule. Prices are written to a dedicated `model_pricing` table keyed by `(provider, model_id)` — decoupled from the routing table `virtual_model_routes`. The sync always refreshes the synced price columns; it never clears or overwrites the nullable override columns. Each sync run appends a timestamped row to `model_pricing_snapshots` recording the synced prices; override changes also write a snapshot entry at the time of the change. Together these preserve a complete, point-in-time-queryable price history for audit and retroactive cost recomputation. Platform admins may set nullable override columns (`override_input_per_1m`, `override_output_per_1m`) on any `model_pricing` row. At inference request time the gateway resolves price in order: override (if non-null) → synced price → `$0.00` with `price_unknown` flag. A `StaticPricingCache` implementation already exists; this layer replaces `NewNopPricingCache()` with a real, database-backed, periodically-refreshed implementation.

**2. Budget Enforcement**  
Principals may be subject to a monthly budget cap via two mechanisms: a principal-level `PrincipalQuota` record, or membership in a `QuotaGroup`. A quota group defines a `monthly_budget_usd` that applies individually to each member principal — it is not a shared spending pool. A principal-level quota always takes precedence over a group quota. Before the gateway routes an inference request, it resolves the effective quota in order: individual quota → group quota → no limit. If the principal's monthly spend meets or exceeds their effective `monthly_budget_usd`, the request is rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, recorded with `status = "policy_denied"`. A quota API endpoint returns `spend_limit_reached: bool` (computed server-side as `current_spend_usd >= monthly_budget_usd`) alongside `monthly_budget_usd` and `current_spend_usd`; the frontend uses this flag directly with no client-side threshold logic. Budget state is maintained in Redis (a counter keyed by `quota:{principal_id}:{year_month}`). Redis is not the source of truth — it is a fast read layer over `inference_usage`; on cache miss it is rehydrated using `SELECT SUM(estimated_cost_usd) FROM inference_usage WHERE DATE_TRUNC('month', created_at) = current_month`. Records where `estimated_cost_usd = 0` due to an unknown price contribute zero to the counter; `price_unknown` records are not backfilled into the budget retrospectively. Monthly spend is computed using `DATE_TRUNC('month', created_at)` — no mutable counter is reset at midnight. Cost is attributed to the month in which the request completes. Budget checks are fail-open: if the quota store is unreachable, the request proceeds and the event is logged. This is a fixed platform behaviour and is not configurable per tenant.

**3. Frontend Dashboards**  
Role-appropriate views surface cost data from the existing analytics endpoints. No new API surface is required for the core dashboards — the existing `/v1/analytics`, `/v1/analytics/me`, and `/v1/analytics/principals` endpoints already return the needed aggregations. The frontend gap is that these views do not yet exist.

### User Experience

#### Platform Operator Flow

1. Operator navigates to **Model Gateway → Platform Usage** (`/model-gateway/platform-usage`) in the Orbit UI.
2. Selects a time range (default: last 30 days) and sees: total spend across all tenants (KPI card), spend by model (bar chart), spend by provider, spend over time (line chart).
3. Can view spend broken down per tenant as well as aggregate totals.
4. Can drill into a specific model to see aggregate spend per principal ID within that model's usage. Principal spend shown at this level is an aggregate cost figure, not personal cost data belonging to another tenant's principals.

#### Platform Operator — Pricing Management Flow

1. Operator navigates to **Model Gateway → Catalog → Provider Catalog** tab.
2. The table gains an **Effective Price** column showing the active price per 1M input/output tokens. A visual indicator distinguishes synced prices from platform admin overrides; a warning indicator marks rows where no price is known.
3. The enable/disable toggle remains inline in the table for fast access.
4. Operator clicks a model row to open a detail overlay showing:
   - Model ID, provider, family, capabilities (read-only).
   - Last synced timestamp and current synced prices (read-only).
   - Override price input fields (input / output per 1M) with a **Clear override** action to revert to the synced price.
   - A `price_unknown` warning if no synced price is available and no override is set.
   - Enable/disable toggle (mirrored from the table).

#### Tenant Admin Flow

1. Admin navigates to **Model Gateway → Analytics** (`/model-gateway/analytics`) in the Orbit UI.
2. Views tenant-level KPIs (total spend, top model, top agent).
3. Navigates to **Principals** tab to see per-principal spend leaderboard.

#### Tenant Admin — Budget Management Flow

1. Admin navigates to **Policy → Quotas** (`/policy/quotas`) in the Orbit UI.
2. Views all principals in their tenant with their current monthly spend, effective budget cap (individual or group), and utilisation progress bar.
3. Navigates to **Groups** tab to create, edit, and delete quota groups (e.g. "engineer", "agent") and assign principals to them.
4. Clicks a principal → can set or edit an individual quota override (`monthly_budget_usd`) or remove the override to revert the principal to their group quota.

#### Developer / Agent Operator Flow

1. Developer navigates to **Model Gateway → My Usage** (`/model-gateway/my-usage`).
2. Sees their own spend over time, broken down by virtual model, with accurate dollar costs.
3. If they have a budget set and `spend_limit_reached` is true, a notification banner is visible at the top of the page.

### Key Components

- **Pricing Sync Worker**: Background process that fetches model prices from LiteLLM's `model_prices_and_context_window.json`, writes all four synced price columns on `model_pricing`. Never touches override columns. Runs on the inference gateway binary on a daily schedule.
- **`model_pricing` table**: New global pricing catalog keyed by `(provider, model_id)`. Contains four synced columns (`synced_input_cost_per_1m`, `synced_output_cost_per_1m`, `synced_cache_read_cost_per_1m`, `synced_cache_write_cost_per_1m`) written by the sync worker, and four nullable override columns (same names, `override_` prefix) set by platform admins. Decoupled from `virtual_model_routes`.
- **DB-backed PricingCache**: Replaces `NewNopPricingCache()`. Loaded from `model_pricing` at startup, refreshed every 5 minutes in the background. Resolves each of the four effective prices via override-first COALESCE. Read by `service.go` at request dispatch.
- **Effective prices on `inference_usage`**: Four `effective_*_cost_per_1m` columns and a `price_unknown` flag are written onto each `inference_usage` record at request time. Every record is a self-contained audit entry — no join required to reconstruct what was charged.
- **Quota Store**: A `principal_quotas` table for individual overrides and a `quota_groups` table for named groups, each with `monthly_budget_usd`. A `principal_quota_group_memberships` join table assigns principals to groups. Effective quota resolution: individual quota → group quota → no limit. Redis counter (`quota:{principal_id}:{year_month}`) is the fast read layer; rehydrated from `inference_usage` on miss using `DATE_TRUNC('month', created_at)` aggregation.
- **Budget Check Middleware / Pre-routing Gate**: A step in `Service.{ChatCompletion, AnthropicMessages, ...}` that resolves the principal's effective quota (individual → group → none) and rejects the request with HTTP 429 and `error_code = "BUDGET_EXCEEDED"` if the Redis counter meets or exceeds the budget. Recorded with `status = "policy_denied"`.
- **Quota API**: Returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached: bool }` for the authenticated principal. `spend_limit_reached` is computed server-side as `current_spend_usd >= monthly_budget_usd`. The frontend uses this flag directly to show or hide the banner — no client-side threshold logic.
- **Cost Dashboards (Frontend)**: Three surfaces: (1) new `/model-gateway/platform-usage` page for platform operators (cross-tenant aggregate + per-tenant breakdown, default 30-day range); (2) enhancements to the existing `/model-gateway/analytics` page for tenant admins (Principals tab with per-principal spend leaderboard, default 30-day range); (3) enhancements to the existing `/model-gateway/my-usage` page for developers (accurate cost data + budget alert banner).
- **Budget Management UI**: New `/policy/quotas` page in the Policy section. Principals tab lists all principals with effective quota, current spend, and utilisation progress bar (individual or group quota indicated). Groups tab manages quota group definitions and principal assignments. Individual override set/clear available on the principal detail view.
- **Provider Catalog pricing enhancements**: The existing Provider Catalog tab gains an **Effective Price** column (override indicator / synced indicator / price-unknown warning) and a sync status banner on the Catalog page. The detail overlay surfaces read-only sync metadata, override price inputs with a clear action, and mirrors the enable/disable toggle. A price-over-time chart sourced from `inference_usage` is included as an experimental feature delivered as a dedicated atomic commit — only models with actual usage data appear.

---

## Scope

### In Scope

- Automated daily pricing sync from LiteLLM `model_prices_and_context_window.json` into a dedicated `model_pricing` catalog table (four pricing dimensions: input, output, cache read, cache write), decoupled from `virtual_model_routes`.
- Platform admin price overrides via nullable override columns on `model_pricing` for all four dimensions; sync worker never touches overrides; platform admin can clear an override to revert to the synced price.
- Effective prices (`effective_*_cost_per_1m`) and `price_unknown` flag written onto each `inference_usage` record at request time, providing a self-contained audit trail.
- Price resolution order at request time: platform admin override → synced catalog price → `$0.00` with `price_unknown` flag.
- Per-principal monthly budget caps with hard-block enforcement (HTTP 429, `BUDGET_EXCEEDED`).
- Quota groups: named groups with a shared `monthly_budget_usd` applied individually per member principal. Individual overrides take precedence over group quotas.
- Server-computed `spend_limit_reached` flag on the quota API (`current_spend_usd >= monthly_budget_usd`); shown as a banner on `/model-gateway/my-usage` when true.
- Monthly calendar reset of budget state using `DATE_TRUNC('month', created_at)` — no mutable counter.
- Cost attributed to the month in which the request completes.
- New `/model-gateway/platform-usage` page for platform operators: aggregate and per-tenant spend breakdown by model, provider, and time.
- Enhancements to existing `/model-gateway/analytics` page for tenant admins: Principals tab with per-principal spend leaderboard.
- New `/policy/quotas` page for tenant admins: budget cap management per principal and group, with utilisation progress bars.
- Enhancements to existing `/model-gateway/my-usage` page for developers: accurate cost data and budget limit banner.

### Out of Scope

- Tenant-level budget caps.
- Email or webhook budget alerts.
- Invoice generation or payment integration.
- RPM / TPM rate limiting.
- Cost allocation tags on requests.
- Real-time (per-request) price resolution from provider APIs.
- Cross-tenant comparative reporting (ranking tenants by spend against each other). The platform operator dashboard shows aggregate totals and per-tenant breakdowns, but does not surface one tenant's data to another tenant or produce cross-tenant league tables.
- Exportable reports (CSV / PDF).

### Future Considerations

- Tenant-level aggregate budget caps.
- Webhook / email delivery of budget alerts.
- Per-tenant price overrides — allowing individual BUs to apply internal chargeback rates that differ from the platform-wide pricing.
- Cost allocation tags (project, environment, team) on inference requests for multi-team cost attribution.
- Exported reports for chargeback to internal teams or customers.
- Provider-level cost reconciliation against actual cloud bills (spot-check accuracy).

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Pricing sync worker fetches model prices from LiteLLM on a daily schedule, always writes to synced price columns on `model_pricing` (never touching override columns), and appends a timestamped snapshot row to `model_pricing_snapshots`.
- [ ] Override set/clear operations also write a snapshot entry at the time of change, recording the resulting effective price. Together sync and override snapshots provide a complete point-in-time price history.
- [ ] `model_pricing` is a dedicated table keyed by `(provider, model_id)` with separate synced columns (`synced_input_per_1m`, `synced_output_per_1m`) and nullable override columns (`override_input_per_1m`, `override_output_per_1m`). Decoupled from `virtual_model_routes`.
- [ ] Sync worker never writes to override columns; platform admins may set or clear overrides via API.
- [ ] Price resolution at request time follows: override (if non-null) → synced price → `$0.00` + `price_unknown` flag.
- [ ] The active `PricingCache` implementation is database-backed and periodically refreshed; `NewNopPricingCache()` is no longer wired in production.
- [ ] `estimated_cost_usd` is non-zero for all models present in the pricing catalog on successful requests.
- [ ] Records where price is unknown at write time are written with `estimated_cost_usd = 0` and a `price_unknown` flag.
- [ ] A `principal_quotas` table stores individual per-principal budget overrides (`monthly_budget_usd`).
- [ ] A `quota_groups` table stores named quota groups (`name`, `monthly_budget_usd`) scoped to a tenant.
- [ ] A `principal_quota_group_memberships` join table assigns principals to quota groups.
- [ ] Effective quota resolution at request time: individual quota (if set) → group quota (if assigned) → no limit.
- [ ] Budget check reads a Redis counter (`quota:{principal_id}:{year_month}`) before routing; counter is rehydrated from `SELECT SUM(estimated_cost_usd) ... WHERE DATE_TRUNC('month', created_at) = current_month` on cache miss. No mutable counter is reset at month boundary.
- [ ] Requests from a principal whose monthly spend ≥ effective `monthly_budget_usd` are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, recorded with `status = "policy_denied"`.
- [ ] Cost is attributed to the month in which the request completes (when `estimated_cost_usd` is written), not when it starts.
- [ ] Monthly spend resets naturally via the `DATE_TRUNC` aggregation — no explicit reset job required.
- [ ] Tenant admin can create, update, and delete quota groups for their tenant via API (requires `platform:quotas:write` scope, tenant-scoped).
- [ ] Tenant admin can assign and remove principals from quota groups via API (requires `platform:quotas:write` scope, tenant-scoped).
- [ ] Tenant admin can set, update, and delete individual principal quota overrides for principals in their tenant via API (requires `platform:quotas:write` scope, tenant-scoped). Platform operators may manage quotas across tenants.
- [ ] Platform operator cost dashboard exists at `/model-gateway/platform-usage` (aggregate and per-tenant spend breakdown by model, provider, and time; default range 30 days). Requires `platform:catalog:write` scope.
- [ ] Existing `/model-gateway/analytics` page is enhanced with a Principals tab showing per-principal spend leaderboard for tenant admins (default range 30 days). Requires `platform:analytics:read` scope.
- [ ] New `/policy/quotas` page exists for tenant admins: Principals tab (effective quota, spend, utilisation per principal), Groups tab (create/edit/delete groups, assign principals). Requires `platform:quotas:read`/`write` scope.
- [ ] A quota API endpoint returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached: bool }` for the authenticated principal. `spend_limit_reached` is computed server-side as `current_spend_usd >= monthly_budget_usd`.
- [ ] Existing `/model-gateway/my-usage` page displays accurate dollar costs (no `$0.00` placeholders) and shows a budget limit banner when `spend_limit_reached` is true.

- [ ] Provider Catalog tab gains an Effective Price column with synced/override/unknown indicators; model rows are clickable and open a detail overlay with sync metadata, override inputs (all four dimensions), clear action, and mirrored enable/disable toggle.

**Should Have (P1):**
- [ ] Budget utilization progress bar on the `/policy/quotas` per-principal view.
- [ ] Pricing sync status banner on the Catalog page header (last sync time, models updated, error count) with expandable run log.
- [ ] Price history chart in the Provider Catalog detail overlay, sourced from `inference_usage` effective price columns. Only models with actual usage data appear. Delivered as a dedicated atomic commit (experimental — may be reverted independently).

**Nice to Have (P2):**
- [ ] A "price unknown" report in the admin UI listing models with no pricing data.
- [ ] A per-principal budget history log (budget changes, reset events).

### Non-Functional Requirements

**Performance:**
- Budget check must add < 5 ms p99 latency to the inference request path (use Redis or in-process cache, not a synchronous DB read per request).
- Pricing cache refresh must not block the inference request path.
- Cost dashboard API responses < 500 ms p95 for a 30-day window with up to 10,000 principals.

**Security:**
- Principals may only view their own cost data; tenant admins may view and manage within their own tenant only; platform operators may view across tenants.
- Budget quota administration (principal quotas and quota groups) requires `platform:quotas:write` scope; reads require `platform:quotas:read`. All quota API operations are tenant-scoped — a tenant admin cannot read or write quotas for principals outside their own tenant.
- Pricing override administration requires `platform:catalog:write` scope.
- Budget check must be evaluated inside the trust boundary of the inference gateway — never client-supplied.

**Reliability:**
- Pricing sync failure must not degrade inference request throughput; fall back to the cached price from the last successful sync.
- Budget check is fail-open: if the quota store is unreachable, the request proceeds and the event is logged. This is a fixed platform behaviour and is not configurable per tenant — agent continuity takes precedence over budget cap guarantees during infrastructure outages. All fail-open events must be logged and observable.

**Scalability:**
- Budget state must support at least 10,000 principals per tenant without degrading request latency.
- Pricing sync must complete within 5 minutes for a catalog of 1,000 models.

**Observability:**
- Pricing sync emits a structured log event on every run (models updated, models skipped due to override, errors, duration).
- Budget check enforcement emits a structured log event whenever a request is hard-blocked.
- Fail-open events (quota store unreachable) are logged as structured events with principal ID and request context.
- OTel spans wrap pricing sync and budget check operations.

---

## Dependencies

### Internal Dependencies

- **Virtual Model Catalog** (ADR-0039, fully implemented): `virtual_model_routes` table remains the routing layer. Pricing is decoupled from it into the new `model_pricing` table; the existing pricing columns on `virtual_model_routes` are superseded.
- **Inference Usage Repository** (ADR-0023, ADR-0040, fully implemented): `inference_usage` table is the source of truth for spend aggregation.
- **Analytics Endpoints** (ADR-0043, fully implemented): `GET /v1/analytics`, `GET /v1/analytics/me`, `GET /v1/analytics/principals` are the API layer for all three dashboard scopes.
- **Platform Token / Claims** (`internal/platform/token/claims.go`, fully implemented): `PrincipalType` and `TenantID` are used to scope budget checks.
- **OTel Observability** (`internal/platform/observability/`, fully implemented): Wraps new operations with spans.
- **Terra Design System** (fully implemented): All frontend cost dashboard components must use Terra primitives.

### External Dependencies

- **Pricing data source**: LiteLLM `model_prices_and_context_window.json` (open-source, community-maintained, updated approximately daily, covers Bedrock/Vertex/Azure/OpenAI/Anthropic). The sync worker must be designed against an abstracted `PricingSource` interface so the source can be swapped in future.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| External pricing source is stale or inaccurate for a specific provider/model | Medium — cost figures are wrong | High — providers change pricing without notice | Log and surface "last synced" timestamp; allow admin override; reconcile against cloud bills periodically |
| Budget check adds latency to inference hot path | High — every inference request is affected | Medium | Cache quota state in Redis or in-process with short TTL; never do a synchronous DB read per request |
| Fail-open budget check allows overspend during quota store outage | Medium — spend may exceed cap during an outage | Low | Fixed platform behaviour by design — agent continuity takes precedence. Log all fail-open events; alert if fail-open rate exceeds threshold |
| Monthly reset logic has a race condition near midnight UTC on 1st | Medium — requests near reset may be incorrectly blocked or allowed | Low | Use `DATE_TRUNC('month', created_at)` aggregation on the DB side; do not maintain a mutable counter that resets |
| Pricing sync source unavailable at startup | Low — inference still works, costs recorded as $0 | Medium | Seed pricing from last-known DB state at startup; retry sync on schedule |
| `price_unknown` records accumulate without admin awareness | Low — data quality degrades silently | Medium | Surface count of `price_unknown` records in operator dashboard; add P2 "unknown pricing" report |

---

## Alternatives Considered

### Alternative 1: Pull live prices from provider SDKs per request

**Description:** Query each provider's pricing API inline on every inference call to get the exact cost at the moment of spend.

**Why not chosen:**
- AWS Bedrock, Google Vertex AI, and Azure OpenAI do not expose per-request cost in their inference APIs.
- Even if they did, this would add unacceptable latency to every inference request.

### Alternative 2: Nightly backfill job for `$0.00` records

**Description:** Record `$0.00` at request time and run a nightly job to backfill missing costs by looking up the price for the model.

**Why not chosen:**
- Budget enforcement requires an accurate running cost total in near-real-time. A nightly backfill means budgets would be evaluated against stale spend data, allowing significant overspend within a 24-hour window.
- A lookup-at-request-time approach with a warm in-process cache achieves the same result without the lag.

### Alternative 3: Tenant-level budget caps (vs. per-principal)

**Description:** Apply budget caps at the tenant level rather than per-principal.

**Why not chosen:**
- Per-principal caps are the higher-value control for autonomous agent safety — the primary use case is constraining a specific runaway agent, not the entire tenant.
- Tenant-level caps can be added in a future iteration once per-principal caps are established.

### Alternative 4: DB-computed budget state with short-TTL cache (vs. Redis counter)

**Description:** Compute monthly spend on-the-fly from `inference_usage` with a short in-process cache TTL, rather than maintaining a Redis increment counter.

**Why not chosen:**
- Redis is already in the stack, eliminating the infrastructure dependency argument for DB-computed.
- A Redis counter provides sub-millisecond reads with no database touch on the hot path. DB-computed introduces a query on every cache miss, which under high traffic can create load spikes.
- The Redis counter approach is simpler to reason about for enforcement latency guarantees.

### Alternative 5: Store pricing on `virtual_model_routes` (vs. dedicated `model_pricing` table)

**Description:** Continue storing pricing on `virtual_model_routes` (the existing routing table), adding an admin override flag and automated sync to that table.

**Why not chosen:**
- `virtual_model_routes` is keyed by virtual model route, not by `(provider, model_id)`. The same physical model can appear on many routes; syncing prices would require fanning out to every matching row — writing the same price multiple times.
- Pricing is a first-class concern with its own history, override semantics, and admin UX. Coupling it to the routing table violates separation of concerns and makes the schema harder to extend (e.g. per-tenant pricing in future).
- A dedicated `model_pricing` table keyed by `(provider, model_id)` is a clean, single source of truth.

### Alternative 6: Per-tenant pricing table (vs. platform-wide pricing with no tenant overrides)

**Description:** Allow each tenant to maintain their own pricing catalog, enabling BU-level internal chargeback rates that differ from the platform-wide LiteLLM prices.

**Why not chosen for this release:**
- Orbit is an operator-deployed platform; pricing is a platform operator concern, not a per-tenant configuration. The operator (e.g. My First Bank's platform team) sets prices for all BUs.
- Per-tenant pricing adds a third lookup layer and a `tenant_model_pricing` table, significantly increasing the complexity of price resolution, cache design, and admin UX.
- Deferred to future considerations — the `model_pricing` table design does not preclude adding a tenant override layer later.

1. **Pricing data source selection:** ~~Resolved~~ — LiteLLM `model_prices_and_context_window.json`. To be documented in the ADR.

2. **Pricing sync frequency:** ~~Resolved~~ — Daily default. LiteLLM updates approximately daily; hourly would add no meaningful accuracy benefit. Manual trigger available for out-of-band price changes. To be documented in the ADR.

3. **Budget state storage:** ~~Resolved~~ — Redis counter keyed by `quota:{principal_id}:{year_month}`. Redis is already in the stack. Counter is best-effort; rehydrated from `inference_usage` on miss. `inference_usage` remains the source of truth. To be documented in the ADR.

4. **`price_unknown` flag column:** Should the flag be a new `price_unknown BOOLEAN NOT NULL DEFAULT false` column on `inference_usage`, or is `estimated_cost_usd = 0` on a successful request sufficient as the signal? A dedicated column is cleaner for the P2 "unknown pricing" report and distinguishes "priced at $0.00" from "price missing."
   - **Status:** Open — to be resolved in the ADR.
   - **Owner:** Platform Engineering.

5. **Retroactive recomputation of `price_unknown` records:** ~~Resolved~~ — `price_unknown` records (where `estimated_cost_usd = 0`) contribute zero to budget enforcement counters and are not backfilled retroactively. The pricing pipeline is the correct fix for coverage gaps; budget enforcement does not attempt to derive historical spend from token counts. Accurate ongoing pricing is the mitigation.

6. **Budget check fail-open vs. fail-closed:** ~~Resolved~~ — Fail-open is a fixed platform behaviour. Agent continuity takes precedence over budget cap guarantees during infrastructure outages. This is not configurable per tenant.

7. **Multi-instance sync coordination:** The sync worker runs on the inference gateway binary. With multiple replicas, each will independently fetch from LiteLLM and write to `model_pricing`. Since writes are idempotent upserts and LiteLLM is a raw GitHub file fetch with no rate limits, redundant syncs are likely acceptable — but the ADR should make this explicit and decide whether to add a distributed lock or leader-election pattern. Multiple simultaneous syncs could also write duplicate snapshot rows to `model_pricing_snapshots`.
   - **Status:** Open — to be resolved in the ADR.
   - **Owner:** Platform Engineering.

8. **Alert notification delivery mechanism:** ~~Resolved~~ — No server-side alert state. The frontend derives banner visibility at render time from the current spend-to-budget ratio. No polling interval or push mechanism required for correctness; banner reflects live data on page load/refresh.

9. **PricingSource interface coupling:** The sync worker should be designed against an abstracted `PricingSource` interface. The interface must not leak LiteLLM-specific field names or structures so that the source can be swapped without changing the cache or gateway code. The ADR should define the minimal interface contract.
   - **Status:** Open — to be resolved in the ADR.
   - **Owner:** Platform Engineering.

10. **QuotaRepository pattern:** The quota check combines a Redis fast path and a PostgreSQL fallback. The ADR should define whether the `QuotaRepository` abstraction wraps both (presenting a single interface to the gateway) or whether the gateway orchestrates them directly. It should also specify the rehydration query and whether the Redis TTL is time-based or key-expiry-based.
    - **Status:** Open — to be resolved in the ADR.
    - **Owner:** Platform Engineering.

11. **PricingCache refresh TTL:** The PRD requires the cache refresh must not block the inference request path, but does not specify the refresh interval or staleness tolerance. A short TTL (e.g. 1 minute) minimises the window between a price override and it taking effect; a longer TTL (e.g. 1 hour) reduces database load. The ADR should specify the TTL and the background refresh strategy.
    - **Status:** Open — to be resolved in the ADR.
    - **Owner:** Platform Engineering.

---

## Timeline and Milestones

### Phases

**Phase 1: Pricing Data Pipeline** (Target: TBD)
- Document pricing source and sync architecture in ADR.
- Introduce `model_pricing` table with synced and nullable override columns.
- Introduce `model_pricing` table with four synced and four nullable override columns using `*_cost_per_1m` naming; migrate pricing off `virtual_model_routes` and drop the old pricing columns.
- Add `price_unknown` and four `effective_*_cost_per_1m` columns to `inference_usage`.
- Rename `CacheCreationCostPerMillion` → `CacheWriteCostPerMillion` in `ModelPricing` Go struct.
- Implement `PricingSource` interface and LiteLLM sync worker (four pricing dimensions).
- Replace `NewNopPricingCache()` with the database-backed implementation.
- Implement platform admin API for price override set/clear across all four dimensions.
- Verify `estimated_cost_usd > 0` on test requests for all supported providers.

**Phase 2: Budget Enforcement** (Target: TBD)
- Design and migrate `principal_quotas`, `quota_groups`, and `principal_quota_group_memberships` tables.
- Implement quota resolution service (individual → group → no limit).
- Wire budget check into inference gateway request path with HTTP 429 rejection.
- Implement Redis counter with `DATE_TRUNC`-based rehydration from `inference_usage`.
- Implement `BUDGET_EXCEEDED` rejection with correct status and error code.
- Implement quota API endpoint returning `spend_limit_reached` flag.
- Admin API for quota group CRUD, principal group assignment, and individual override CRUD.

**Phase 3: Frontend Dashboards** (Target: TBD)
- New `/model-gateway/platform-usage` page (platform operator: aggregate + per-tenant spend breakdown, by model, provider, time; default 30-day range).
- Enhance `/model-gateway/analytics` with Principals tab (per-principal spend leaderboard for tenant admins; default 30-day range).
- New `/policy/quotas` page: Principals tab (effective quota, spend, utilisation progress bar per principal; individual/group source indicated), Groups tab (create/edit/delete quota groups, assign principals, manage overrides).
- Enhance `/model-gateway/my-usage` with accurate cost data and budget limit banner.
- Provider Catalog tab: Effective Price column (synced/override/unknown indicators) and clickable row detail overlay (sync metadata, override inputs for all four dimensions, clear action, mirrored enable/disable toggle).
- Catalog page header: sync status banner (last sync time, models updated, error count, expandable run log).
- **[Experimental — separate atomic commit]** Price history chart in Provider Catalog detail overlay, sourced from `inference_usage` effective price columns; only models with actual usage data appear.

### Key Milestones

- [ ] PRD Approved
- [ ] ADR Created (pricing source + budget state)
- [ ] Phase 1 Implementation Complete
- [ ] Phase 2 Implementation Complete
- [ ] Phase 3 Implementation Complete
- [ ] Launch

---

## Appendix

### Related Documents

- [ADR-0023](../adr/0023-inference-gateway-architecture.md) - Inference gateway architecture and usage write path
- [ADR-0039](../adr/0039-virtual-model-catalog-and-platform-routing.md) - Virtual model catalog; budget controls deferral decision
- [ADR-0040](../adr/0040-inference-usage-logs.md) - Inference usage logs and PAT attribution
- [ADR-0043](../adr/0043-inference-analytics.md) - Analytics endpoints and aggregation design

### Glossary

- **`estimated_cost_usd`:** The dollar cost of an inference request, computed at write time from token counts and per-model pricing across four dimensions (input, output, cache read, cache write). Stored as `NUMERIC(18,10)` in `inference_usage`.
- **`model_pricing`:** New global pricing catalog table keyed by `(provider, model_id)`. Contains four synced price columns (written by the sync worker) and four nullable override columns (set by platform admins), all using `*_cost_per_1m` naming. Decoupled from the routing table `virtual_model_routes`.
- **`effective_*_cost_per_1m` columns:** Four columns added to `inference_usage` recording the prices actually used to compute `estimated_cost_usd` for that request. Together with `price_unknown`, they make each record a self-contained audit entry.
- **`virtual_model_routes`:** Database table mapping a virtual model to ordered provider targets. Pricing columns on this table are superseded by `model_pricing`.
- **`PricingCache`:** The in-process interface in `internal/inferencegw/pricing.go` that the service layer calls to resolve model prices. Currently wired to a no-op implementation; to be replaced with a DB-backed implementation that resolves override → synced → unknown across all four dimensions.
- **`PrincipalQuota`:** A per-principal budget override record storing `monthly_budget_usd`. Takes precedence over any group quota. Does not yet exist in the codebase.
- **`QuotaGroup`:** A named quota policy (e.g. "engineer", "agent") storing `monthly_budget_usd`, scoped to a tenant. Applied individually to each member principal — not a shared spending pool. Does not yet exist in the codebase.
- **Effective quota:** The quota that applies to a principal at enforcement time, resolved as: individual `PrincipalQuota` (if set) → `QuotaGroup` membership (if assigned) → no limit.
- **Redis quota counter:** A key `quota:{principal_id}:{year_month}` incremented on each inference usage write by the `estimated_cost_usd` of that request. The fast read layer for budget checks; rehydrated from `SELECT SUM(estimated_cost_usd) FROM inference_usage WHERE DATE_TRUNC(...)` on miss. Records where `estimated_cost_usd = 0` (price unknown) contribute zero to the counter. Not the source of truth.
- **Fail-open:** Fixed platform behaviour when the quota store is unreachable — the request proceeds rather than is blocked, and the event is logged. Not configurable per tenant.
- **Hard block:** Enforcement mode where requests are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"` (`status = "policy_denied"`) once a principal's monthly spend meets or exceeds their effective budget cap.
- **`spend_limit_reached`:** A boolean field returned by the quota API, computed server-side as `current_spend_usd >= monthly_budget_usd`. The frontend uses this flag to show or hide the budget limit banner with no client-side logic.
- **`price_unknown` flag:** Indicator on an `inference_usage` record that no price was available at write time, causing `estimated_cost_usd = 0`. These records contribute zero to budget enforcement counters and are not retroactively recomputed.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
