# LLM Cost Tracking: Pricing Pipeline, Budget Enforcement, and Quota Roles

**Status:** Proposed (Decision 5 superseded by [Budget Model Redesign ADR](2026-06-10-budget-model-redesign.md) 2026-06-10)  
**Date:** 2026-06-05  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)

---

## Context

### Background

The inference gateway records one `inference_usage` row per request including `estimated_cost_usd`, but the value is always `$0.00` in practice. The `PricingCache` interface exists in `internal/inferencegw/pricing.go` but is wired to `NewNopPricingCache()` — a no-op that returns nothing. The `virtual_model_routes` table carries `input_cost_per_1m` and `output_cost_per_1m` columns that were intended as the price source but are unpopulated for most models. Providers such as AWS Bedrock, Google Vertex AI, and Azure OpenAI do not expose per-request dollar cost in their APIs, so prices must be resolved independently.

No budget enforcement exists. ADR-0039 explicitly deferred quota controls. There is no mechanism to constrain or alert on per-principal spending.

### Problem Statement

1. `estimated_cost_usd` is `$0.00` on virtually every `inference_usage` record, making cost analytics meaningless and budget enforcement impossible.
2. Pricing is structurally coupled to the routing table (`virtual_model_routes`), which is the wrong key for price lookup and creates fan-out writes when a provider model appears on multiple routes.
3. No per-principal budget cap or quota enforcement exists. Runaway agent loops have no financial ceiling.
4. Tenant admins managing large numbers of principals (hundreds or thousands) have no scalable way to assign budget policies — every principal would need individual configuration.

### Goals

- Record accurate `estimated_cost_usd` on every inference request for all supported providers, sourced from an automatically-maintained pricing catalog.
- Decouple pricing from the routing table into a dedicated, provider-keyed catalog with four pricing dimensions (input, output, cache read, cache write), admin override support, and effective prices written directly onto inference records for full auditability.
- Enforce per-principal monthly budget caps via a low-latency check on the inference hot path.
- Enable quota roles so a single budget policy can be applied to a range of principals, with per-principal overrides where needed.

### Non-Goals

- Real-time per-request price fetching from provider APIs.
- Per-tenant pricing overrides (deferred; see PRD future considerations).
- Tenant-level aggregate budget caps.
- Invoice generation or payment integration.
- RPM / TPM rate limiting.

---

## Decision Drivers

* **Hot-path latency** — the budget check runs on every inference request; it must add < 5 ms p99 and must never synchronously block on a database read.
* **Correctness over complexity** — the pricing catalog must stay current automatically; manual admin updates are not a sustainable approach.
* **Auditability** — the effective price used for each request must be queryable without joining to a separate table; self-contained records are simpler and more robust.
* **Operational simplicity** — Redis is already in the stack; no new infrastructure should be introduced unless it provides a clear advantage.
* **Separation of concerns** — pricing is a first-class domain concern and must not be coupled to routing configuration.
* **Scalability of quota management** — a tenant with thousands of principals needs a policy-by-role model, not individual record creation.

---

## Decisions

This ADR covers five distinct but related decisions. Each is addressed in its own section.

---

## Decision 1: Pricing Data Source

### Considered Options

#### Option A: LiteLLM `model_prices_and_context_window.json`

A community-maintained JSON file in the LiteLLM GitHub repository. Covers Bedrock, Vertex AI, Azure OpenAI, Anthropic, and OpenAI. Updated approximately daily, sometimes multiple times per day. Fetched as a raw GitHub file — no authentication, no rate limits.

**Pros:**
- Broad provider coverage matching kora's supported providers.
- Community-maintained with active contributions; price changes and new model additions tracked promptly.
- No API key, no rate limits, no cost.
- Single file fetch — trivially simple sync implementation.

**Cons:**
- No formal SLA or versioning guarantee; the file could change structure without notice.
- No explicit `last_updated` field — staleness must be inferred from sync timestamp.
- Community-sourced; individual prices may lag provider announcements by hours to days.

#### Option B: Scrape provider pricing pages (AWS, Google, Azure, Anthropic)

Scrape each provider's published pricing page on a schedule.

**Pros:**
- Authoritative source for each provider.

**Cons:**
- Four separate scrapers to build and maintain; each provider's page structure changes without notice.
- Scraper breakage is silent — prices go stale with no signal.
- Significant ongoing maintenance burden.

#### Option C: Helicone pricing API

A third-party API that maintains model pricing data.

**Pros:**
- Structured API response.

**Cons:**
- External service dependency with potential rate limits, pricing, or availability risk.
- Less broad coverage than LiteLLM for Bedrock and Vertex models.
- Adds a runtime dependency on a third-party service.

### Decision 1: LiteLLM `model_prices_and_context_window.json`

**Chosen Option: Option A**

LiteLLM's file has the broadest coverage of kora's supported providers, is updated at the cadence kora needs (daily), and introduces no infrastructure dependency or cost. The sync worker must be implemented against a `PricingSource` interface so the source can be swapped if LiteLLM becomes unavailable or a better source emerges.

**`PricingSource` interface contract:**

The interface must not leak LiteLLM-specific field names. The minimal contract the sync worker depends on:

```go
type ModelPriceEntry struct {
    Provider              string
    ModelID               string
    InputCostPer1M        float64
    OutputCostPer1M       float64
    CacheReadCostPer1M    float64  // 0 if not provided by source
    CacheWriteCostPer1M   float64  // 0 if not provided by source
}

type PricingSource interface {
    Fetch(ctx context.Context) ([]ModelPriceEntry, error)
}
```

The LiteLLM implementation maps `litellm_provider`, `input_cost_per_token`, `output_cost_per_token`, `cache_read_input_token_cost`, and `cache_creation_input_token_cost` to this struct (converting per-token to per-1M). Any replacement source implements the same interface.

**Bedrock cross-region profile ID normalisation:** The Bedrock connector uses cross-region inference profile IDs as canonical model IDs — e.g. `us.anthropic.claude-3-5-sonnet-20241022-v2:0` — because those are the routable forms. LiteLLM only carries the bare base form: `anthropic.claude-3-5-sonnet-20241022-v2:0`. `PricingCache.Price` strips the known region prefix (`us.`, `eu.`, `apac.`, etc.) before performing the lookup so that every Bedrock model with a cross-region profile resolves correctly. This normalisation is applied at the `PricingCache` read layer — the `model_pricing` table and the sync worker are unaffected.

**Sync schedule:** Daily. The sync worker runs on the inference gateway binary. With multiple replicas, each independently fetches and upserts — writes are idempotent (`ON CONFLICT (provider, model_id) DO UPDATE` on synced columns only). This produces redundant syncs but no correctness issues. A distributed lock is not added at this time — the cost of the coordination infrastructure is not justified by the benefit.

---

## Decision 2: Pricing Table Design

### Considered Options

#### Option A: Keep pricing on `virtual_model_routes`

Add a `price_admin_override` flag column and a sync worker that writes to `virtual_model_routes`.

**Pros:**
- No schema migration of the existing pricing columns.
- No new tables.

**Cons:**
- `virtual_model_routes` is keyed by `(virtual_model_id, provider, model_id)` — a route key, not a pricing key. The same `(provider, model_id)` pair appears on multiple routes; syncing requires a fan-out write to every matching row.
- Pricing and routing are different concerns with different lifecycles, admin UIs, and access scopes. Coupling them to the same table makes both harder to evolve.
- Per-tenant pricing (future consideration) cannot be added cleanly to a routing table.
- History / snapshot for pricing requires either a separate table anyway or pollutes the routing table with temporal data.

#### Option B: Dedicated `model_pricing` table with override columns (Chosen)

A new table keyed by `(provider, model_id)` with separate synced columns and nullable override columns across all four pricing dimensions. The sync worker writes only to synced columns. Platform admins write only to override columns. Effective prices are written onto `inference_usage` at request time — each record is a self-contained audit entry.

**Pros:**
- Single source of truth for a given `(provider, model_id)` — no fan-out.
- Synced and override values coexist on the same row; override semantics are transparent.
- Override removal is a NULL set — no separate flag, no footgun.
- Effective prices on `inference_usage` make every record fully auditable without joining to a separate table.
- Clean path to per-tenant pricing in future (add `tenant_model_pricing` keyed by `(tenant_id, provider, model_id)`).

**Cons:**
- New table; requires migration to remove pricing columns from `virtual_model_routes`.

### Decision 2: Dedicated `model_pricing` table with effective prices on `inference_usage`

**Chosen Option: Option B**

**`model_pricing` schema:**

```sql
CREATE TABLE model_pricing (
    provider                         TEXT           NOT NULL,
    model_id                         TEXT           NOT NULL,
    -- Synced columns: always written by the sync worker
    synced_input_cost_per_1m         NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_output_cost_per_1m        NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_cache_read_cost_per_1m    NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_cache_write_cost_per_1m   NUMERIC(10, 6) NOT NULL DEFAULT 0,
    -- Override columns: set by platform admins; NULL = use synced value
    override_input_cost_per_1m       NUMERIC(10, 6),
    override_output_cost_per_1m      NUMERIC(10, 6),
    override_cache_read_cost_per_1m  NUMERIC(10, 6),
    override_cache_write_cost_per_1m NUMERIC(10, 6),
    last_synced_at                   TIMESTAMPTZ    NOT NULL,
    created_at                       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at                       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model_id)
);
```

**`inference_usage` additions:**

```sql
ALTER TABLE inference_usage
    ADD COLUMN price_unknown                  BOOLEAN        NOT NULL DEFAULT false,
    ADD COLUMN effective_input_cost_per_1m    NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_output_cost_per_1m   NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_cache_read_cost_per_1m  NUMERIC(10, 6) NOT NULL DEFAULT 0,
    ADD COLUMN effective_cache_write_cost_per_1m NUMERIC(10, 6) NOT NULL DEFAULT 0;
```

The four effective columns record the prices actually used to compute `estimated_cost_usd` for that request. When `price_unknown = true` all four are 0. This makes every record a complete, self-contained audit entry — no join to `model_pricing` is required to reconstruct what was charged.

**Price resolution at request time:**

```
effective_input_cost_per_1m      = COALESCE(override_input_cost_per_1m,      synced_input_cost_per_1m)
effective_output_cost_per_1m     = COALESCE(override_output_cost_per_1m,     synced_output_cost_per_1m)
effective_cache_read_cost_per_1m = COALESCE(override_cache_read_cost_per_1m, synced_cache_read_cost_per_1m)
effective_cache_write_cost_per_1m = COALESCE(override_cache_write_cost_per_1m, synced_cache_write_cost_per_1m)
```

If all synced columns are 0 and all override columns are NULL: `price_unknown = true`, `estimated_cost_usd = 0`.

**Cache pricing fallback:** When a model's LiteLLM entry has no explicit cache pricing (common for non-Anthropic models), `synced_cache_read_cost_per_1m` and `synced_cache_write_cost_per_1m` are stored as 0. The `EstimatedCostUSD` method in the codebase already applies industry-standard fallbacks at compute time (cache read = 10% of input, cache write = 125% of input) when these are 0. This is a computation convenience, not a stored price — the stored value remains 0, and `price_unknown` is not set because input and output prices are known.

**`price_unknown` flag:** Distinguishes a genuinely $0.00 model from a model with no price data. Supports the P2 "unknown pricing" report. `estimated_cost_usd = 0` alone is ambiguous.

**No separate snapshot table:** Effective prices are written to `inference_usage` at request time. The `(provider, model_id, created_at)` index makes cost reconstruction efficient without a separate snapshot table.

**Migration:** The `input_cost_per_1m` and `output_cost_per_1m` columns are dropped from `virtual_model_routes` once `model_pricing` is populated and all read paths updated. The `ModelPricing` Go struct is updated to rename `CacheCreationCostPerMillion` → `CacheWriteCostPerMillion` for consistency with the DB naming convention.

---

## Decision 3: PricingCache Design and TTL

### Considered Options

#### Option A: In-process cache loaded at startup, background refresh

The `PricingCache` is loaded from `model_pricing` at gateway startup into an in-memory map. A background goroutine refreshes it on a fixed interval. All inference request paths read from the in-memory map — zero database reads on the hot path.

**Pros:**
- Sub-microsecond price lookups — no I/O on the hot path.
- Refresh is fully decoupled from request handling.
- Consistent with how `virtualCatalog` is managed in the existing service.

**Cons:**
- Stale by up to the refresh interval. A price override set by an admin takes up to TTL to propagate.
- All gateway replicas maintain independent caches; momentarily different effective prices across replicas is possible within the TTL window.

#### Option B: Redis-backed cache

Store resolved prices in Redis, keyed by `(provider, model_id)`. Gateway instances read from Redis on each request with a short TTL.

**Pros:**
- Single shared cache; all replicas see the same prices immediately after a write.

**Cons:**
- Adds a Redis read to every inference request — contrary to the goal of keeping the pricing lookup off the network.
- Redis is already used for quota counters; mixing pricing and quota data in the same store couples two concerns.
- Price lookups are read-only and do not need cross-replica consistency within a short TTL window.

### Decision 3: In-process cache with 5-minute background refresh

**Chosen Option: Option A**

A 5-minute TTL is chosen as the refresh interval. This means a price override set by a platform admin takes effect within 5 minutes across all replicas — acceptable for an admin operation. The refresh runs in a background goroutine using a `time.Ticker`; it must never block request handling. On refresh failure, the previous in-memory state is retained and a structured log event is emitted.

The existing `StaticPricingCache` and `PricingCache` interface in `internal/inferencegw/pricing.go` are replaced with a `DatabasePricingCache` that implements the same interface. The constructor accepts a DB reader and a refresh interval; the `NewNopPricingCache()` wiring in `main.go` is replaced with `NewDatabasePricingCache(db, 5*time.Minute)`.

---

## Decision 4: Budget State Storage

### Considered Options

#### Option A: Redis increment counter

On each `inference_usage` write, increment a Redis key `quota:{principal_id}:{year_month}` by `estimated_cost_usd`. The budget check reads this key. On cache miss, rehydrate from `inference_usage`.

**Pros:**
- Sub-millisecond budget check — single Redis `GET`.
- Monthly reset is implicit: the `{year_month}` component in the key means each month has its own key; old keys can be allowed to expire via Redis TTL.
- No mutable counter to reset at midnight; no race condition at month boundary.

**Cons:**
- Counter can drift from the DB if a usage write succeeds but the Redis increment fails (or vice versa). Drift is bounded by one request's cost and is self-correcting on next rehydration.
- Redis is not the source of truth; `inference_usage` is. Any discrepancy is resolved by rehydration.

#### Option B: DB-computed with in-process cache

On cache miss or periodically, run `SELECT SUM(estimated_cost_usd) FROM inference_usage WHERE DATE_TRUNC('month', created_at) = current_month AND principal_id = $1`. Cache the result in-process with a short TTL.

**Pros:**
- Always accurate to the DB; no separate state to drift.
- No Redis writes on the usage write path.

**Cons:**
- Cache miss triggers a database query — under high traffic this creates DB load spikes.
- In-process cache is per-replica; different replicas may have different cached spend values simultaneously, making enforcement inconsistent within the cache window.
- Redis is already in the stack, eliminating the main argument for DB-computed.

### Decision 4: Redis increment counter with DB rehydration

**Chosen Option: Option A**

**Key design:**

```
quota:{principal_id}:{YYYY-MM}
```

- Type: Redis string storing a float (USD).
- TTL: 35 days from last write — ensures the key outlives the calendar month by a safe margin without requiring explicit expiry management.
- Increment: `INCRBYFLOAT quota:{principal_id}:{YYYY-MM} {cost}` on every `inference_usage` write, performed best-effort after the DB write. If the Redis increment fails, the DB write is not rolled back — the counter will be corrected on next rehydration.
- Rehydration query (on cache miss):

```sql
SELECT COALESCE(SUM(estimated_cost_usd), 0)
FROM inference_usage
WHERE principal_id = $1
  AND DATE_TRUNC('month', created_at AT TIME ZONE 'UTC') =
      DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')
```

- `price_unknown` records (`estimated_cost_usd = 0`) contribute zero to the counter. They are not backfilled retroactively. The pricing pipeline is the correct fix for coverage gaps.
- Cost is attributed to the month in which the request completes (when `estimated_cost_usd` is written), not when it starts. Requests crossing the month boundary at 00:00 UTC on the 1st are attributed to the month of their completion timestamp.

**Fail-open:** If Redis is unreachable during a budget check, or if OPA evaluation fails (e.g. bundle not yet loaded, evaluation error), the request proceeds and a structured log event is emitted. This is a fixed platform behaviour — agent continuity takes precedence over budget cap guarantees during infrastructure outages. It is not configurable per tenant.

**Role of Redis in enforcement:** Redis provides the current-month spend value (`current_spend_usd`). The inference gateway reads this value, injects it into the OPA input document alongside the principal identity, and evaluates `data.orbit.budget.budget_allow` via `pol.CheckBudget`. Redis does not make the enforcement decision — OPA does. Redis is simply the fast data source for spend.

---

## Decision 5: Quota Model — Roles and Resolution Order

> **Superseded 2026-06-10.** See [Budget Model Redesign ADR](2026-06-10-budget-model-redesign.md). The three-table quota model below was replaced with inline `budget JSONB` on `policy_role_scopes` and `policy_grants`, MAX resolution, and per-category Redis spend counters. Decisions 1–4 remain accepted.

### Considered Options

#### Option A: Per-principal quotas only

Each principal has at most one `PrincipalQuota` record. Tenant admins create individual records for each principal they want to constrain.

**Pros:**
- Simple schema — one table.
- Straightforward enforcement — one lookup.

**Cons:**
- Does not scale. A tenant admin managing hundreds or thousands of principals (especially agent fleets) must create and maintain individual records. There is no way to apply a default policy to a class of principals.

#### Option B: Quota roles with per-principal override (Chosen)

A `quota_roles` table defines named policies (e.g. "engineer", "agent-fleet") scoped to a tenant. A `principal_quota_role_memberships` join table assigns principals to roles. A `principal_quotas` table holds per-principal overrides that take precedence over any role. Enforcement resolves: individual quota → role quota → no limit.

**Pros:**
- Scales to large principal counts — assign a role once, all members inherit the policy.
- Per-principal overrides remain available for exceptions.
- Each role member gets their own individual limit — the role budget is not a shared pool.
- Clean path for future UI: roles are manageable from `/policy/quotas` alongside individual overrides.

**Cons:**
- Three tables instead of one.
- Quota resolution requires two lookups (individual, then role) instead of one; both can be served from Redis/cache.

#### Option C: Hierarchical roles with inherited quotas

Reuse the existing `policy_role_scopes` mechanism to attach quota metadata to OIDC roles.

**Pros:**
- Leverages existing role infrastructure.

**Cons:**
- Roles are an auth concern; budget policy is an operational concern. Coupling them makes both harder to evolve independently.
- OIDC role claims are managed externally (in Entra/Okta); quota management would require coordinating with the external IdP.

### Decision 5: Quota roles with per-principal override

**Chosen Option: Option B**

**Schema (tables live in the policy-service database):**

These tables are owned by the policy-service, not the inference-gateway. Budget limits are a policy concern — co-locating them with grants and roles keeps quota management within the policy service boundary and allows bundle compilation to include quota data without a cross-service fetch.

```sql
CREATE TABLE quota_roles (
    id                 TEXT           PRIMARY KEY,
    tenant_id          TEXT           NOT NULL,
    name               TEXT           NOT NULL,
    monthly_budget_usd NUMERIC(12, 4) NOT NULL,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE principal_quota_role_memberships (
    principal_id  TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    role_id      TEXT NOT NULL REFERENCES quota_roles(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)  -- one role per principal per tenant
);

CREATE TABLE principal_quotas (
    principal_id          TEXT           NOT NULL,
    tenant_id             TEXT           NOT NULL,
    monthly_budget_usd    NUMERIC(12, 4) NOT NULL,
    created_at            TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)
);
```

**Effective quota resolution:**

Resolution is implemented in Rego (`policy/budget.rego`), not in Go. Quota limits and role memberships are compiled into the tenant bundle as `data/quotas/data.json`. The inference gateway reads current spend from Redis, injects it into the OPA input, and evaluates `data.orbit.budget.budget_allow`. The Rego rule applies the resolution order:

```rego
package orbit.budget

import rego.v1

default budget_allow := true  # fail-open: no quota = allow

budget_allow := false if {
    limit := data.quotas.principal_quotas[input.principal.id]
    input.current_spend_usd >= limit
}

budget_allow := false if {
    not data.quotas.principal_quotas[input.principal.id]
    role_id := data.quotas.memberships[input.principal.id]
    limit := data.quotas.roles[role_id].monthly_budget_usd
    input.current_spend_usd >= limit
}
```

The OPA input document for budget checks:

```json
{
  "principal": { "id": "usr_abc123", "type": "HUMAN", "tenant_id": "tenant_1" },
  "current_spend_usd": 48.75
}
```

The bundle data document (`data/quotas/data.json`) shape compiled by policy-service:

```json
{
  "principal_quotas": {
    "usr_abc123": 50.00
  },
  "roles": {
    "grp_agents": { "name": "agents", "monthly_budget_usd": 5.00 }
  },
  "memberships": {
    "agt_001": "grp_agents"
  }
}
```

This eliminates the in-process `QuotaCache` (60-second TTL) and `QuotaRepository` in the inference-gateway. Bundle refresh (triggered on every quota write, polled every 5 minutes) is the propagation mechanism. Because quota data lives in the policy-service database alongside grants, bundle compilation requires no cross-service fetch.

**Budget limit banner semantics:** The quota API returns `spend_limit_reached: bool`, derived from the OPA evaluation result. This is the same condition as the hard block — the principal cannot make further requests, and the banner informs them of this state. The frontend uses the flag directly with no threshold logic. There is no separate configurable alert threshold.

**Quota deletion:** Deleting a `principal_quotas` record or removing a principal from a role removes the cap immediately. Spend already recorded for the month is not adjusted. In-flight requests that have already passed the budget check complete normally.

**API scopes:**
- `platform:quotas:read` — list roles, list memberships, read individual quotas.
- `platform:quotas:write` — create/update/delete roles, assign/remove memberships, set/clear individual quotas.
- All quota API operations are tenant-scoped. A tenant admin cannot read or write quotas for principals outside their own tenant. Platform operators may manage quotas across tenants.

---

## Consequences

### Positive

- `estimated_cost_usd` becomes meaningful for all models in the LiteLLM catalog — covering Bedrock, Vertex AI, Azure OpenAI, Anthropic, and OpenAI across all four pricing dimensions.
- Pricing is decoupled from routing; the `model_pricing` table is a clean, single source of truth keyed by `(provider, model_id)`.
- Every `inference_usage` record is a self-contained audit entry — effective prices are written at request time, no join required to reconstruct costs.
- No separate snapshot table to maintain or write to on every sync and override.
- Budget enforcement adds < 1 ms to the inference hot path (single Redis GET + in-process OPA evaluation).
- Quota roles scale budget management to large principal fleets without per-principal configuration.
- Monthly budget counters cannot race at the midnight boundary — `DATE_TRUNC` aggregation is stateless.
- Budget enforcement logic lives entirely in Rego — adding or changing enforcement rules (e.g. grace periods, tenant-level caps) requires no Go service changes.
- Quota tables co-located with grants and roles in policy-service — bundle compilation requires no cross-service fetch for quota data.

### Negative

- `virtual_model_routes.input_cost_per_1m` and `output_cost_per_1m` must be migrated away and dropped — a schema migration with a brief dual-write period.
- `inference_usage` gains five new columns (`price_unknown` + four `effective_*_cost_per_1m`) — a schema migration on a potentially large table.
- The 5-minute PricingCache TTL means a platform admin price override takes up to 5 minutes to propagate across all gateway replicas.
- Redis counter drift (bounded to one request's cost) is possible on usage write failures. Self-corrects on rehydration but means the enforced limit may occasionally be off by a small amount.
- Quota tables moving to policy-service requires migrating data out of the inference-gateway DB and updating all quota admin API routes and handlers to the policy-service binary. The existing `QuotaCache`, `QuotaRepository`, and `handlers_quota.go` in the inference-gateway are removed.

### Neutral

- `NewNopPricingCache()` is removed from production wiring. Tests that depend on it must be updated to use `NewStaticPricingCache(nil)` or a test double.
- The `ModelPricing` Go struct field `CacheCreationCostPerMillion` is renamed to `CacheWriteCostPerMillion`. Call sites must be updated.
- The `QuotaCache` (60-second in-process TTL) is eliminated; bundle refresh is the propagation mechanism for quota limit changes.

---

## Implementation

### Phase 1: Pricing Data Pipeline

1. Create `model_pricing` migration with eight pricing columns (four synced, four nullable override) using `*_cost_per_1m` naming.
2. Add `price_unknown` and four `effective_*_cost_per_1m` columns to `inference_usage`.
3. Update `ModelPricing` Go struct: rename `CacheCreationCostPerMillion` → `CacheWriteCostPerMillion`; add `CacheReadCostPerMillion` and `CacheWriteCostPerMillion` to all serialisation paths.
4. Implement `PricingSource` interface and `LiteLLMPricingSource` — maps four LiteLLM fields to `ModelPriceEntry`.
5. Implement `PricingSyncWorker` — fetches, upserts all eight synced columns.
6. Implement `DatabasePricingCache` — loads from `model_pricing`, background refresh at 5-minute interval. Resolves all four effective prices via COALESCE at load time.
7. Wire `DatabasePricingCache` into `main.go`, replacing `NewNopPricingCache()`.
8. Update inference request handlers to write four `effective_*_cost_per_1m` values and `price_unknown` flag onto each `inference_usage` record at write time.
9. Add platform admin API routes for price override set/clear (`PUT /v1/catalog/pricing/:provider/:model_id/override`, `DELETE /v1/catalog/pricing/:provider/:model_id/override`), registered under `platform:catalog:write`.
10. Migrate and drop pricing columns from `virtual_model_routes`.
11. Verify `estimated_cost_usd > 0` on test requests for all supported providers.

### Phase 2: Budget Enforcement — OPA-Native

Budget enforcement is split into two sub-phases: migrating quota ownership to policy-service and wiring OPA evaluation into the gateway.

#### Phase 2a: Quota tables and bundle data in policy-service

1. Create migrations in `cmd/policy-service/migrations/` for `quota_roles`, `principal_quota_role_memberships`, and `principal_quotas` — identical schema to Phase 2 original, but owned by the policy-service database.
2. Implement quota repository in `internal/policyservice/quotas/` — `postgres.go` with CRUD methods; `repository.go` interface.
3. Add quota CRUD API routes to policy-service under `platform:quotas:read`/`write` scopes, replacing the inference-gateway quota handlers. Quota admin API is now co-located with grants admin.
4. Extend the bundle compiler (`internal/policyservice/bundle/compiler.go`) to load quota data and include `data/quotas/data.json` in every tenant bundle. Recompile triggers on every quota write — same mechanism as grant writes.
5. Write `policy/budget.rego` and embed it in the bundle alongside `authz.rego`. Update the bundle manifest `Roots` to include `"orbit/budget"` and `"quotas"` alongside the existing roots (`"orbit/authz"`, `"grants"`, `"vocabulary"`, `"roles"`, `"routes"`, `"config"`). Both new roots are required: `"orbit/budget"` for the Rego package, `"quotas"` for the `data/quotas/data.json` document. Without both declared, OPA will not load them from the bundle.
6. Add a `CheckBudget(ctx, tenantID, principalID string, currentSpendUSD float64) (allow bool, err error)` method to `internal/platform/policy.Evaluator` and the in-process engine — evaluates `data.orbit.budget.budget_allow`.
7. Migrate any existing quota data from the inference-gateway database to policy-service.
8. Drop quota tables from the inference-gateway database.

#### Phase 2b: Budget check in the inference gateway

1. Remove `QuotaCache`, `QuotaRepository`, and `handlers_quota.go` from `internal/inferencegw/`. Keep `RedisQuotaCounter` (spend read/write) and `SpendCounter` interface unchanged.
2. Remove `quotaCache` and `quotaCounter` fields from the `Service` struct in `service.go`; update `NewService` constructor to drop those parameters. Update all call sites in `cmd/inference-gateway/main.go`.
3. Rewrite `checkBudget` in `service.go`: read spend from Redis via `SpendCounter.GetSpend`, inject into OPA input, call `pol.CheckBudget`. Fail open on Redis error or OPA error — same semantics as before.
4. Remove quota route registrations from `cmd/inference-gateway/main.go`. Remove `QuotaCache` and `QuotaRepository` wiring.
5. Add a new Goose migration in `cmd/inference-gateway/migrations/` that drops the `quota_roles`, `principal_quota_role_memberships`, and `principal_quotas` tables. Do not delete the earlier `20260608015333_create_quota_tables.sql` migration — Goose requires the full history to remain intact. The new down migration is the correct mechanism for removing the tables.
6. Delete the inference-gateway quota test files: `handlers_quota_test.go`, `quota_cache_test.go` (if present), and `quota_repository_test.go` (if present). Quota behaviour is now covered by policy-service tests.
7. Add quota status API endpoint to **policy-service**: returns `{ monthly_budget_usd, current_spend_usd, spend_limit_reached }` for the authenticated principal. Policy-service reads current spend from the Redis counter (same key format as the gateway: `quota:{principal_id}:{YYYY-MM}`) and evaluates `orbit.budget.budget_allow` to derive `spend_limit_reached`. This keeps the quota read path co-located with quota data ownership and avoids a cross-service call from the gateway for the status response.
8. Verify: integration test confirms zero successful inference requests after a principal's monthly spend meets their effective budget.

### Phase 3: Frontend

See PRD Phase 3 milestones.

### Migration Path

`virtual_model_routes` pricing column migration:

1. Deploy Phase 1 with `model_pricing` populated and `DatabasePricingCache` reading from it.
2. Verify effective prices are being written to `inference_usage` in all environments.
3. Run migration to drop `input_cost_per_1m` and `output_cost_per_1m` from `virtual_model_routes`.

---

## Validation

### Success Metrics

- `estimated_cost_usd > 0` on ≥ 99% of successful inference requests where `(provider, model_id)` is present in `model_pricing`.
- Budget check adds < 5 ms p99 to the inference hot path (measured via OTel span on the quota check step).
- Zero successful inference requests processed after a principal's monthly spend meets or exceeds their effective `monthly_budget_usd` (verified by integration test).
- `PricingSyncWorker` completes a full sync of the LiteLLM catalog within 5 minutes.

### Monitoring

- OTel span `pricing.cache.refresh` — emitted on every background cache refresh; attribute `pricing.models_loaded` records catalog size.
- Structured log event `pricing.sync.completed` — emitted on every sync run with fields: `models_upserted`, `models_skipped` (override present), `errors`, `duration_ms`.
- Structured log event `budget.hard_block` — emitted whenever a request is rejected for budget exceeded; fields: `principal_id`, `tenant_id`, `monthly_spend_usd`, `monthly_budget_usd`.
- Structured log event `budget.fail_open` — emitted whenever a request proceeds because the quota store was unreachable; fields: `principal_id`, `tenant_id`, `error`.
- OTel span `budget.quota_check` — wraps the Redis spend read and OPA `CheckBudget` evaluation on every request for a principal with a quota.

---

## Related Decisions

- [virtual-model-catalog-and-platform-routing](2026-04-17-virtual-model-catalog-and-platform-routing.md) — Virtual model catalog; pricing columns on `virtual_model_routes` are superseded by this ADR.
- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference gateway architecture and usage write path.
- [inference-usage-logs](2026-04-17-inference-usage-logs.md) — Inference usage logs; `inference_usage` table is the source of truth for spend aggregation.
- [inference-analytics](2026-04-27-inference-analytics.md) — Analytics endpoints consumed by cost dashboards.
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy model; `platform:quotas:*` and `platform:catalog:write` scopes follow the established vocabulary pattern.

---

## References

- [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)
- [LiteLLM model_prices_and_context_window.json](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)

---

## Notes

**Why move quota tables to policy-service rather than keep them in the inference-gateway?** Budget limits are a policy concern — they define what a principal is *allowed* to spend, in the same way grants define what a principal is *allowed* to access. Co-locating them with grants and roles in policy-service means bundle compilation can include quota data without a cross-service fetch. It also means quota admin sits alongside grant admin in a single service boundary rather than being split across two binaries. The inference-gateway is the *enforcement* point, not the *authority* on budget limits; this mirrors the existing pattern where the gateway evaluates OPA decisions but does not own the grant data. Future budget rules (tenant-level caps, grace periods, per-model limits) are Rego changes, not Go changes.

**Why not a shared spending pool for quota roles?** A shared pool requires atomic cross-principal accounting — every request from any role member must decrement a shared counter. This is a distributed coordination problem with no clean Redis primitive. It also creates unpredictable UX: a single high-traffic agent could exhaust the pool for all other members. Per-member individual limits are both simpler to implement and more predictable for operators.

**Why drop `virtual_model_routes` pricing columns rather than leave them?** Leaving them creates an ongoing confusion risk — engineers may write to or read from them not knowing they are superseded. The columns are also populated with stale data that could silently re-emerge if someone wires them back in. A clean drop in Phase 1 is safer than deprecation-in-place.

**Why normalise Bedrock model IDs at the cache read layer rather than at sync or write time?** The `model_pricing` table is keyed on the bare LiteLLM form (`anthropic.claude-…`). Normalising at write time would require `PricingSyncWorker` to know about the Bedrock profile ID scheme — coupling two independent concerns. Normalising at the cache read layer in `PricingCache.Price` is the minimal change: one function, no schema impact, no migration, and covers both the inference request path and any future consumer of the same cache.

**Why store effective prices on `inference_usage` rather than a separate snapshot table?** A snapshot table requires writes on every sync run and every override change, adds a join to cost reconstruction, and creates a second source of truth that can diverge from the actual prices used. Writing effective prices directly onto `inference_usage` at request time makes every record self-contained — the prices used to compute `estimated_cost_usd` are stored alongside the token counts, no join required.

**Why is the budget limit state a server-computed flag rather than a client-side calculation?** The frontend should not contain business logic. If the definition of "limit reached" ever changes — for example, if a grace amount or grace period is introduced — a client-side calculation would require updates in every frontend surface. The backend computes `spend_limit_reached = current_spend_usd >= monthly_budget_usd` and returns it on the quota API. The frontend reads the flag and renders the banner. There is no configurable alert threshold percentage — the limit and the banner are the same concept at the same boundary.

**Last Updated:** 2026-06-10 (Decision 5 superseded by Budget Model Redesign ADR)
