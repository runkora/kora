# LLM Cost Tracking - Product Requirements Document

**Status:** Approved  
**Created:** 2026-05-29  
**Last Updated:** 2026-06-10  
**Owner:** Product  
**Stakeholders:** Platform Engineering, Security, Frontend

---

## Executive Summary

kora's inference gateway captures token counts on every request, but the dollar cost written to `inference_usage.estimated_cost_usd` is frequently `$0.00` because provider platforms like Bedrock do not expose pricing through their inference APIs. This PRD covers the complete feature set required to make cost data accurate and actionable: an automated pricing data pipeline to record correct dollar costs at spend time, per-principal monthly budget enforcement with hard-block semantics, budget assignment via OIDC roles and individual grants, in-app threshold alerts, and role-appropriate cost dashboards for platform operators, tenant admins, and individual developers.

---

## Problem Statement

### Current State

The inference gateway writes one `InferenceUsageRecord` to PostgreSQL per request. The `estimated_cost_usd` field is computed using prices stored in `virtual_model_routes.input_cost_per_1m` and `output_cost_per_1m`. In practice:

- The wired `PricingCache` implementation is `NewNopPricingCache()` — a no-op that always returns `$0.00`.
- The `virtual_model_routes` pricing columns are populated manually, making them stale by default and absent for most models. Pricing is coupled to the routing table rather than maintained as a first-class catalog.
- Providers such as AWS Bedrock, Google Vertex AI, and Azure OpenAI do not surface per-request dollar cost in their API responses — kora must resolve prices independently.
- The analytics API (`GET /v1/analytics`, `GET /v1/analytics/me`) already aggregates `SUM(estimated_cost_usd)` correctly, but the underlying values are almost always zero, making the data meaningless.
- ADR-0039 explicitly deferred budget controls. No budget enforcement exists. There is no mechanism to constrain or alert on agent or human spending.

### User Pain Points

- **Platform operators** have no way to see what any tenant, agent, or model is costing them. Chargeback and capacity planning are impossible.
- **Tenant admins** cannot set guardrails on agent spending. A runaway agent loop has no cost ceiling.
- **Individual developers** cannot see the real cost of their own inference calls or understand which models are cheaper.
- **On-call engineers** have no early warning when a tenant or agent is approaching an unusual spend level — they find out after the fact from the cloud bill.

### Business Impact

- Without accurate cost attribution, kora cannot support tenant chargeback or usage-based pricing tiers.
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
- `spend_limit_reached` correctly reflects `true` whenever `current_spend_usd >= effective_limit_usd` on the budget API.
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
- [ ] Admin can configure a monthly budget cap (USD) on any OIDC role or individual grant for principals in their tenant.
- [ ] Once a principal's monthly spend reaches 100% of their effective cap, all further inference requests from that principal are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, and recorded with `status = "policy_denied"`.
- [ ] The gateway returns `spend_limit_reached: true` on the budget API when the principal's spend meets or exceeds their effective limit. The frontend uses this flag to display a banner; no threshold percentage is involved.
- [ ] The budget resets automatically at the start of each calendar month (00:00 UTC on the 1st).
- [ ] Removing a budget from a role or grant removes the cap immediately; spend already recorded for the month is not adjusted, and in-flight requests complete normally.

---

**As a** tenant admin,  
**I want to** assign monthly budget caps to OIDC roles (e.g. "engineer", "agent") and set individual per-principal budget overrides on grants,  
**So that** I can apply a shared budget policy to a class of principals and add individual exceptions without configuring each one separately.

**Acceptance Criteria:**
- [ ] Admin can set a `budget` (monthly limit USD, reset cadence) on any OIDC role for their tenant; all principals whose OIDC token carries that role inherit the role's budget limit.
- [ ] Admin can set a `budget` on any individual grant for a principal; an individual grant budget raises the effective limit when it exceeds the role budget (MAX resolution).
- [ ] Budget assignment is part of the role/grant record — no separate quota object or table.
- [ ] Effective budget resolution follows MAX across all categories: the principal's effective limit is the highest budget limit configured across all their OIDC roles and individual grants. Grants never reduce a principal's effective limit below their role budget.
- [ ] Spend accumulates against every applicable category simultaneously (per-role counter, per-grant counter). Each category has its own reporting and reset schedule.
- [ ] Removing a budget from a role or grant removes that category's contribution to the effective limit immediately; the MAX across remaining categories becomes the new effective limit.

---

**As a** developer or agent operator,  
**I want to** view my own inference costs over time, broken down by model,  
**So that** I can understand my usage patterns and choose more cost-efficient models.

**Acceptance Criteria:**
- [ ] The existing `/model-gateway/my-usage` page displays accurate dollar costs for the authenticated principal once the pricing pipeline is live (no `$0.00` placeholders).
- [ ] Cost is broken down by virtual model.
- [ ] A time-series chart shows spend over the selected period.
- [ ] If the principal has a budget set and their current spend meets or exceeds it, a notification banner is visible on the page. The backend computes and returns `spend_limit_reached: bool` on the budget API; the frontend renders the banner when true.

---

**As a** platform operator,  
**I want to** pricing data to be automatically synchronized from an authoritative source and allow me to override any price,  
**So that** cost figures in the system remain accurate without requiring manual updates, and I can correct prices that are wrong or not yet in the catalog.

**Acceptance Criteria:**
- [ ] A background sync process fetches pricing from LiteLLM's `model_prices_and_context_window.json` on a daily schedule and writes to the `model_pricing` catalog table.
- [ ] At request time, the inference gateway writes four effective prices (`effective_input_cost_per_1m`, `effective_output_cost_per_1m`, `effective_cache_read_cost_per_1m`, `effective_cache_write_cost_per_1m`) and a `price_unknown` flag onto each `inference_usage` record. Each record is a self-contained audit entry — no separate snapshot table or join is required to reconstruct what was charged.
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
A background sync worker pulls model pricing from LiteLLM's `model_prices_and_context_window.json` on a daily schedule. Prices are written to a dedicated `model_pricing` table keyed by `(provider, model_id)` — decoupled from the routing table `virtual_model_routes`. The sync always refreshes the synced price columns across all four pricing dimensions (input, output, cache read, cache write); it never clears or overwrites the nullable override columns. Platform admins may set nullable override columns for any of the four dimensions on any `model_pricing` row. At inference request time the gateway resolves each effective price in order: override (if non-null) → synced price → `$0.00` with `price_unknown` flag. The four effective prices and the `price_unknown` flag are written directly onto each `inference_usage` record at request time — each record is a self-contained audit entry. A `StaticPricingCache` implementation already exists; this layer replaces `NewNopPricingCache()` with a real, database-backed, periodically-refreshed implementation.

**2. Budget Enforcement**  
Principals may be subject to a monthly budget cap via two mechanisms: a `budget` field on their OIDC role mapping (`policy_role_scopes`) and/or a `budget` field on an individual grant (`policy_grants`). Both carry `{ monthly_limit_usd, reset_cadence }` as an inline JSONB object — parallel to `scopes` on the same record. A principal's effective budget is the MAX across all budget assignments (OIDC roles and individual grants), consistent with how scopes resolve as UNION. Grants never restrict below a role's budget. `budget: null` on a record means no budget is assigned for that category.

Budget enforcement is OPA-native. Budget assignments are compiled into each tenant's OPA bundle as `data.budgets`, keyed by `principal_id`. The compiler pre-computes `effective_limit_usd` (MAX across all categories) and includes a `categories` array for per-source spend counter construction. The `kora.budget` Rego module evaluates the allow/deny decision: it compares the injected current spend against `effective_limit_usd`. The inference gateway reads current monthly spend from Redis (counters keyed by `budget:{principal_id}:role:{role_name}:{period}` and `budget:{principal_id}:grant:{grant_id}:{period}`), injects it into the OPA input document, and evaluates `data.kora.budget.budget_allow` via the policy evaluator's `CheckBudget` method. If the result is `false`, the request is rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, recorded with `status = "policy_denied"`. The resolution logic and enforcement rules live entirely in Rego — adding or changing enforcement rules requires no Go service changes.

Spend is written to every applicable category counter simultaneously so that per-category reporting, alerting, and reset tracking are all maintained independently.

A budget API endpoint returns `spend_limit_reached: bool` alongside `monthly_budget_usd` (the effective limit) and `current_spend_usd`; `spend_limit_reached` is derived from the OPA evaluation result. The frontend uses this flag directly with no client-side threshold logic. Redis is not the source of truth for spend — it is a fast read layer over `inference_usage`; on cache miss it is rehydrated using `SELECT SUM(estimated_cost_usd) FROM inference_usage WHERE DATE_TRUNC('month', created_at) = current_month`. Records where `estimated_cost_usd = 0` due to an unknown price contribute zero to the counter; `price_unknown` records are not backfilled into the budget retrospectively. Monthly spend is computed using `DATE_TRUNC('month', created_at)` — no mutable counter is reset at midnight. Cost is attributed to the month in which the request completes. Budget checks are fail-open: if OPA or Redis is unreachable, the request proceeds and the event is logged. This is a fixed platform behaviour and is not configurable per tenant.

**3. Frontend Dashboards**  
Role-appropriate views surface cost data from the existing analytics endpoints. No new API surface is required for the core dashboards — the existing `/v1/analytics`, `/v1/analytics/me`, and `/v1/analytics/principals` endpoints already return the needed aggregations. The frontend gap is that these views do not yet exist.

### User Experience

#### Platform Operator Flow

1. Operator navigates to **Model Gateway → Platform Usage** (`/model-gateway/platform-usage`) in the kora UI.
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

1. Admin navigates to **Model Gateway → Analytics** (`/model-gateway/analytics`) in the kora UI.
2. Views tenant-level KPIs (total spend, top model, top agent).
3. Navigates to **Principals** tab to see per-principal spend leaderboard.

#### Tenant Admin — Budget Management Flow

1. Admin navigates to **Policy → Roles** (`/policy/roles`) to set a monthly budget cap on an OIDC role (e.g. "engineer" → $50/month).
2. Admin navigates to **Policy → Grants** (`/policy/grants`) to set a monthly budget cap on an individual grant, raising the effective limit for that principal above their role budget.
3. Budget configuration is part of the role/grant editing UX — no separate Quotas page.
4. Admin can view per-principal spend, effective budget limit, and utilisation from the **Model Gateway → Analytics → Principals** tab.

#### Developer / Agent Operator Flow

1. Developer navigates to **Model Gateway → My Usage** (`/model-gateway/my-usage`).
2. Sees their own spend over time, broken down by virtual model, with accurate dollar costs.
3. If they have a budget set and `spend_limit_reached` is true, a notification banner is visible at the top of the page.

### Key Components

- **Pricing Sync Worker**: Background process that fetches model prices from LiteLLM's `model_prices_and_context_window.json`, writes all four synced price columns on `model_pricing`. Never touches override columns. Runs on the inference gateway binary on a daily schedule.
- **`model_pricing` table**: New global pricing catalog keyed by `(provider, model_id)`. Contains four synced columns (`synced_input_cost_per_1m`, `synced_output_cost_per_1m`, `synced_cache_read_cost_per_1m`, `synced_cache_write_cost_per_1m`) written by the sync worker, and four nullable override columns (same names, `override_` prefix) set by platform admins. Decoupled from `virtual_model_routes`.
- **DB-backed PricingCache**: Replaces `NewNopPricingCache()`. Loaded from `model_pricing` at startup, refreshed every 5 minutes in the background. Resolves each of the four effective prices via override-first COALESCE. Read by `service.go` at request dispatch.
- **Effective prices on `inference_usage`**: Four `effective_*_cost_per_1m` columns and a `price_unknown` flag are written onto each `inference_usage` record at request time. Every record is a self-contained audit entry — no join required to reconstruct what was charged.
- **Budget Store**: A nullable `budget JSONB` column on `policy_role_scopes` and `policy_grants`. Budget limits are compiled into each tenant's OPA bundle as `data.budgets` — same pipeline as grants and roles. Redis counters (`budget:{principal_id}:role:{role_name}:{period}` and `budget:{principal_id}:grant:{grant_id}:{period}`) are the fast spend read layer; rehydrated from `inference_usage` on miss using `DATE_TRUNC('month', created_at)` aggregation.
- **OPA Budget Module (`kora.budget`)**: A `budget.rego` policy module compiled into every tenant bundle. Evaluates `budget_allow` by comparing injected `current_spend_usd` against `data.budgets[principal_id].effective_limit_usd` (pre-computed MAX at bundle compile time). Enforced via the policy evaluator's `CheckBudget` method in the inference gateway.
- **Budget Check Gate**: A step in `Service.{ChatCompletion, AnthropicMessages, Embed, ...}` that reads current spend from Redis, injects it into OPA input, and evaluates `kora.budget.budget_allow`. Rejects with HTTP 429 and `error_code = "BUDGET_EXCEEDED"` if the result is `false`. Recorded with `status = "policy_denied"`. Fails open on OPA or Redis unavailability.
- **Budget API**: Returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached: bool }` for the authenticated principal. `spend_limit_reached` is derived from the OPA evaluation result. The frontend uses this flag directly to show or hide the banner — no client-side threshold logic.
- **Cost Dashboards (Frontend)**: Three surfaces: (1) new `/model-gateway/platform-usage` page for platform operators (cross-tenant aggregate + per-tenant breakdown, default 30-day range); (2) enhancements to the existing `/model-gateway/analytics` page for tenant admins (Principals tab with per-principal spend leaderboard, default 30-day range); (3) enhancements to the existing `/model-gateway/my-usage` page for developers (accurate cost data + budget alert banner).
- **Budget Management UI**: Budget configuration is part of the existing **Policy → Roles** and **Policy → Grants** pages — no separate Quotas page. The Roles edit form gains a `Monthly budget (USD)` field with a `reset_cadence` selector. The Grant edit form gains the same fields. Per-principal spend and effective budget utilisation is visible from the **Model Gateway → Analytics → Principals** tab.
- **Provider Catalog pricing enhancements**: The existing Provider Catalog tab gains an **Effective Price** column (override indicator / synced indicator / price-unknown warning) and a sync status banner on the Catalog page. The detail overlay surfaces read-only sync metadata, override price inputs with a clear action, and mirrors the enable/disable toggle. A price-over-time chart sourced from `inference_usage` is included as an experimental feature delivered as a dedicated atomic commit — only models with actual usage data appear.

---

## Scope

### In Scope

- Automated daily pricing sync from LiteLLM `model_prices_and_context_window.json` into a dedicated `model_pricing` catalog table (four pricing dimensions: input, output, cache read, cache write), decoupled from `virtual_model_routes`.
- Platform admin price overrides via nullable override columns on `model_pricing` for all four dimensions; sync worker never touches overrides; platform admin can clear an override to revert to the synced price.
- Effective prices (`effective_*_cost_per_1m`) and `price_unknown` flag written onto each `inference_usage` record at request time, providing a self-contained audit trail.
- Price resolution order at request time: platform admin override → synced catalog price → `$0.00` with `price_unknown` flag.
- Per-principal monthly budget caps with hard-block enforcement (HTTP 429, `BUDGET_EXCEEDED`).
- Budget assignment via a `budget JSONB` field inline on OIDC role mappings (`policy_role_scopes`) and individual grants (`policy_grants`). `budget: null` means no limit.
- Effective budget resolution uses MAX across all applicable OIDC role and grant budget assignments. Grants never reduce a principal's effective limit below their role budget.
- Spend accumulated against all applicable category counters simultaneously for per-category reporting and reset tracking.
- Server-computed `spend_limit_reached` flag on the budget API (`current_spend_usd >= effective_limit_usd`); shown as a banner on `/model-gateway/my-usage` when true.
- Monthly calendar reset of budget state using `DATE_TRUNC('month', created_at)` — no mutable counter.
- Cost attributed to the month in which the request completes.
- New `/model-gateway/platform-usage` page for platform operators: aggregate and per-tenant spend breakdown by model, provider, and time.
- Enhancements to existing `/model-gateway/analytics` page for tenant admins: Principals tab with per-principal spend leaderboard.
- Budget configuration on existing Policy → Roles and Policy → Grants pages: inline `budget` field on role/grant edit forms.
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
- [ ] Pricing sync worker fetches model prices from LiteLLM on a daily schedule, always writes to all four synced price columns on `model_pricing` (`synced_input_cost_per_1m`, `synced_output_cost_per_1m`, `synced_cache_read_cost_per_1m`, `synced_cache_write_cost_per_1m`), and never touches override columns.
- [ ] `model_pricing` is a dedicated table keyed by `(provider, model_id)` with separate synced columns and nullable override columns across all four pricing dimensions. Decoupled from `virtual_model_routes`.
- [ ] Sync worker never writes to override columns; platform admins may set or clear overrides via API.
- [ ] Price resolution at request time follows: override (if non-null) → synced price → `$0.00` + `price_unknown` flag.
- [ ] The active `PricingCache` implementation is database-backed and periodically refreshed; `NewNopPricingCache()` is no longer wired in production.
- [ ] `estimated_cost_usd` is non-zero for all models present in the pricing catalog on successful requests.
- [ ] Records where price is unknown at write time are written with `estimated_cost_usd = 0` and a `price_unknown` flag.
- [ ] A nullable `budget JSONB` column exists on `policy_role_scopes` and `policy_grants`. `budget: null` means no limit for that category.
- [ ] Budget schema on a role/grant record: `{ monthly_limit_usd: number, reset_cadence: "monthly" }`.
- [ ] Effective budget resolution uses MAX across all applicable OIDC role and individual grant budget assignments. Grants never reduce a principal's effective limit below their role budget. A principal with no budget assignments has no limit.
- [ ] Spend accumulates against all applicable category counters simultaneously. Per-category Redis counters keyed by `budget:{principal_id}:role:{role_name}:{period}` and `budget:{principal_id}:grant:{grant_id}:{period}`.
- [ ] Budget check reads per-category Redis counters before routing; counters are rehydrated from `SELECT SUM(estimated_cost_usd) ... WHERE DATE_TRUNC('month', created_at) = current_month` on cache miss. No mutable counter is reset at month boundary.
- [ ] Requests from a principal whose monthly spend ≥ `effective_limit_usd` are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"`, recorded with `status = "policy_denied"`.
- [ ] Cost is attributed to the month in which the request completes (when `estimated_cost_usd` is written), not when it starts.
- [ ] Monthly spend resets naturally via the `DATE_TRUNC` aggregation — no explicit reset job required.
- [ ] Tenant admin can set or clear a `budget` on any role or grant via the existing role/grant edit API (requires `platform:budgets:write` scope, tenant-scoped). Platform operators may manage budgets across tenants.
- [ ] Platform operator cost dashboard exists at `/model-gateway/platform-usage` (aggregate and per-tenant spend breakdown by model, provider, and time; default range 30 days). Requires `platform:catalog:write` scope.
- [ ] Existing `/model-gateway/analytics` page is enhanced with a Principals tab showing per-principal spend leaderboard and effective budget for tenant admins (default range 30 days). Requires `platform:analytics:read` scope.
- [ ] A budget API endpoint returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached: bool }` for the authenticated principal. `spend_limit_reached` is computed server-side via OPA `CheckBudget`.
- [ ] Existing `/model-gateway/my-usage` page displays accurate dollar costs (no `$0.00` placeholders) and shows a budget limit banner when `spend_limit_reached` is true.

- [ ] Provider Catalog tab gains an Effective Price column with synced/override/unknown indicators; model rows are clickable and open a detail overlay with sync metadata, override inputs (all four dimensions), clear action, and mirrored enable/disable toggle.

**Should Have (P1):**
- [ ] Budget utilisation progress bar on the Analytics → Principals view showing spend vs effective limit per principal.
- [ ] Pricing sync status banner on the Catalog page header (last sync time, models updated, error count) with expandable run log.
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
- Budget administration (setting/clearing a budget on roles and grants) requires `platform:budgets:write` scope; reads require `platform:budgets:read`. All budget API operations are tenant-scoped — a tenant admin cannot read or write budgets for principals outside their own tenant.
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
- **Hades Design System** (fully implemented): All frontend cost dashboard components must use Hades primitives.

### External Dependencies

- **Pricing data source**: LiteLLM `model_prices_and_context_window.json` (open-source, community-maintained, updated approximately daily, covers Bedrock/Vertex/Azure/OpenAI/Anthropic). The sync worker must be designed against an abstracted `PricingSource` interface so the source can be swapped in future. **Note:** LiteLLM keys Bedrock models by their bare base ID (e.g. `anthropic.claude-3-5-sonnet-20241022-v2:0`). kora's Bedrock connector uses the routable cross-region inference profile ID (e.g. `us.anthropic.claude-3-5-sonnet-20241022-v2:0`) as the canonical model ID. The `PricingCache` normalises these by stripping the region prefix before lookup; the `model_pricing` table and sync worker are unaffected.

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
- kora is an operator-deployed platform; pricing is a platform operator concern, not a per-tenant configuration. The operator (e.g. My First Bank's platform team) sets prices for all BUs.
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

7. **Multi-instance sync coordination:** ~~Resolved~~ — The sync worker runs on the inference gateway binary. With multiple replicas, each will independently fetch from LiteLLM and write to `model_pricing`. Since writes are idempotent upserts and LiteLLM is a raw GitHub file fetch with no rate limits, redundant syncs are acceptable and no distributed lock is required. Documented in the ADR.

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
- Modify migration `20260609000001` in place: drop `quota_roles`, `principal_quota_role_memberships`, `principal_quotas`; add nullable `budget JSONB` to `policy_role_scopes` and `policy_grants`.
- Update `UpsertRole` and grant write operations to explicitly preserve the `budget` field when only scopes are being updated.
- Extend policy-service bundle compiler: replace `data/quotas/data.json` with `data.budgets` document keyed by `principal_id`; pre-compute `effective_limit_usd` (MAX) and include `categories` array at compile time; recompile on every role or grant write.
- Rewrite `policy/budget.rego` (`kora.budget` module) for the new `data.budgets` shape: compare `current_spend_usd` against `data.budgets[principal_id].effective_limit_usd`.
- Add `CheckBudget` method to `platform/policy.Evaluator` interface and in-process engine (already present — update implementation for new bundle shape).
- Rewrite inference gateway budget check: read all per-category Redis counters for the principal, inject total into OPA input, evaluate `kora.budget.budget_allow`.
- Write spend to all per-category Redis counters simultaneously on each usage write.
- Remove old `quota_roles`, `principal_quota_role_memberships`, `principal_quotas` tables and their handlers; remove quota CRUD API routes.
- Add budget status API endpoint (`spend_limit_reached`, `current_spend_usd`, `monthly_budget_usd`) — the effective values derived from `data.budgets` in the OPA bundle.
- Execute full `quota` → `budget` terminology rename across all surviving files (see `BUDGET-REDESIGN.md` Removal & Rename Plan for file-by-file detail).

**Phase 3: Frontend Dashboards** (Target: TBD)
- New `/model-gateway/platform-usage` page (platform operator: aggregate + per-tenant spend breakdown, by model, provider, time; default 30-day range).
- Enhance `/model-gateway/analytics` with Principals tab (per-principal spend leaderboard for tenant admins, with effective budget column; default 30-day range).
- Budget configuration fields (`Monthly budget (USD)`, `Reset cadence`) added to existing Policy → Roles and Policy → Grants edit forms.
- Enhance `/model-gateway/my-usage` with accurate cost data and budget limit banner.
- Provider Catalog tab: Effective Price column (synced/override/unknown indicators) and clickable row detail overlay (sync metadata, override inputs for all four dimensions, clear action, mirrored enable/disable toggle).
- Catalog page header: sync status banner (last sync time, models updated, error count, expandable run log).

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

- [ADR-0023](../adr/2026-04-01-inference-gateway-architecture.md) - Inference gateway architecture and usage write path
- [ADR-0039](../adr/2026-04-17-virtual-model-catalog-and-platform-routing.md) - Virtual model catalog; budget controls deferral decision
- [ADR-0040](../adr/2026-04-17-inference-usage-logs.md) - Inference usage logs and PAT attribution
- [ADR-0043](../adr/2026-04-27-inference-analytics.md) - Analytics endpoints and aggregation design
- [LLM Cost Tracking ADR](../adr/2026-06-05-llm-cost-tracking.md) - Pricing pipeline, budget storage, and enforcement decisions (Decisions 1–4 accepted; Decision 5 superseded by [Budget Model Redesign ADR](../adr/2026-06-10-budget-model-redesign.md))
- [Budget Model Redesign ADR](../adr/2026-06-10-budget-model-redesign.md) - Inline budget JSONB on roles/grants, MAX resolution, per-category spend accumulation

### Glossary

- **`estimated_cost_usd`:** The dollar cost of an inference request, computed at write time from token counts and per-model pricing across four dimensions (input, output, cache read, cache write). Stored as `NUMERIC(18,10)` in `inference_usage`.
- **`model_pricing`:** New global pricing catalog table keyed by `(provider, model_id)`. Contains four synced price columns (written by the sync worker) and four nullable override columns (set by platform admins), all using `*_cost_per_1m` naming. Decoupled from the routing table `virtual_model_routes`.
- **`effective_*_cost_per_1m` columns:** Four columns added to `inference_usage` recording the prices actually used to compute `estimated_cost_usd` for that request. Together with `price_unknown`, they make each record a self-contained audit entry.
- **`virtual_model_routes`:** Database table mapping a virtual model to ordered provider targets. Pricing columns on this table are superseded by `model_pricing`.
- **`PricingCache`:** The in-process interface in `internal/inferencegw/pricing.go` that the service layer calls to resolve model prices. Currently wired to a no-op implementation; to be replaced with a DB-backed implementation that resolves override → synced → unknown across all four dimensions.
- **Budget assignment:** A `budget: { monthly_limit_usd, reset_cadence }` object stored inline on a `policy_role_scopes` or `policy_grants` record, parallel to `scopes`. `budget: null` means no limit for that category. Budget assignments are the source of truth for per-principal spend limits; compiled into the tenant's OPA bundle as `data.budgets`.
- **Effective budget:** The `effective_limit_usd` value pre-computed at bundle compile time as the MAX across all applicable budget assignments (OIDC role budgets and individual grant budgets) for a principal. Grants never reduce a principal's effective limit below a role budget.
- **Budget category counter:** A per-source-per-principal Redis counter. Roles use `budget:{principal_id}:role:{role_name}:{period}`; grants use `budget:{principal_id}:grant:{grant_id}:{period}`. Spend is written to all applicable category counters simultaneously on each inference request. Each category has its own reporting and reset tracking.
- **`kora.budget` Rego module:** The OPA policy module compiled into every tenant bundle. Evaluates `budget_allow` by comparing injected `current_spend_usd` against `data.budgets[principal_id].effective_limit_usd`. The enforcement logic lives entirely in Rego.
- **Fail-open:** Fixed platform behaviour when the budget store is unreachable — the request proceeds rather than is blocked, and the event is logged. Not configurable per tenant.
- **Hard block:** Enforcement mode where requests are rejected with HTTP 429 and `error_code = "BUDGET_EXCEEDED"` (`status = "policy_denied"`) once a principal's monthly spend meets or exceeds their effective budget limit.
- **`spend_limit_reached`:** A boolean flag returned by the budget API, derived from the OPA evaluation of `data.kora.budget.budget_allow`. It indicates whether the principal's current monthly spend has triggered the budget enforcement policy and further requests will be disallowed. The specific logic is defined in the `kora.budget` Rego module — not a hardcoded client-side comparison. The frontend uses this flag to show or hide the budget limit banner with no client-side logic.
- **`price_unknown` flag:** Indicator on an `inference_usage` record that no price was available at write time, causing `estimated_cost_usd = 0`. These records contribute zero to budget enforcement counters and are not retroactively recomputed.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
