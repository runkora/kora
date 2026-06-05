# 61. LLM Cost Tracking: Pricing Pipeline, Budget Enforcement, and Quota Groups

**Status:** Proposed  
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
- Decouple pricing from the routing table into a dedicated, provider-keyed catalog with admin override support and full price history.
- Enforce per-principal monthly budget caps via a low-latency check on the inference hot path.
- Enable quota groups so a single budget policy can be applied to a range of principals, with per-principal overrides where needed.

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
* **Auditability** — price history must be queryable point-in-time to support retroactive cost analysis and audit.
* **Operational simplicity** — Redis is already in the stack; no new infrastructure should be introduced unless it provides a clear advantage.
* **Separation of concerns** — pricing is a first-class domain concern and must not be coupled to routing configuration.
* **Scalability of quota management** — a tenant with thousands of principals needs a policy-by-group model, not individual record creation.

---

## Decisions

This ADR covers five distinct but related decisions. Each is addressed in its own section.

---

## Decision 1: Pricing Data Source

### Considered Options

#### Option A: LiteLLM `model_prices_and_context_window.json`

A community-maintained JSON file in the LiteLLM GitHub repository. Covers Bedrock, Vertex AI, Azure OpenAI, Anthropic, and OpenAI. Updated approximately daily, sometimes multiple times per day. Fetched as a raw GitHub file — no authentication, no rate limits.

**Pros:**
- Broad provider coverage matching Orbit's supported providers.
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

LiteLLM's file has the broadest coverage of Orbit's supported providers, is updated at the cadence Orbit needs (daily), and introduces no infrastructure dependency or cost. The sync worker must be implemented against a `PricingSource` interface so the source can be swapped if LiteLLM becomes unavailable or a better source emerges.

**`PricingSource` interface contract:**

The interface must not leak LiteLLM-specific field names. The minimal contract the sync worker depends on:

```go
type ModelPriceEntry struct {
    Provider           string
    ModelID            string
    InputCostPer1M     float64
    OutputCostPer1M    float64
}

type PricingSource interface {
    Fetch(ctx context.Context) ([]ModelPriceEntry, error)
}
```

The LiteLLM implementation maps the JSON fields `litellm_provider`, `input_cost_per_token`, and `output_cost_per_token` to this struct internally. Any replacement source implements the same interface.

**Sync schedule:** Daily. The sync worker runs on the inference gateway binary. With multiple replicas, each independently fetches and upserts — writes are idempotent (`ON CONFLICT (provider, model_id) DO UPDATE` on synced columns only). This produces redundant syncs but no correctness issues. The snapshot table (see Decision 2) will receive multiple rows if replicas sync near-simultaneously; the ADR accepts this as a known minor noisiness in the history table. A distributed lock is not added at this time — the cost of the coordination infrastructure is not justified by the benefit. If snapshot duplication becomes a problem, a `UNIQUE` constraint on `(provider, model_id, date_trunc('hour', recorded_at))` can be added to deduplicate at the DB layer.

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

A new table keyed by `(provider, model_id)` with separate synced columns and nullable override columns. The sync worker writes only to synced columns. Platform admins write only to override columns. The `PricingCache` reads both and applies override-first resolution.

**Pros:**
- Single source of truth for a given `(provider, model_id)` — no fan-out.
- Synced and override values coexist on the same row; override semantics are transparent.
- Override removal is a NULL set — no separate flag, no footgun.
- Clean path to per-tenant pricing in future (add `tenant_model_pricing` keyed by `(tenant_id, provider, model_id)`).
- Snapshot table is a natural append-only companion.

**Cons:**
- New table; requires migration to remove pricing columns from `virtual_model_routes`.

### Decision 2: Dedicated `model_pricing` table

**Chosen Option: Option B**

**Schema:**

```sql
CREATE TABLE model_pricing (
    provider                TEXT           NOT NULL,
    model_id                TEXT           NOT NULL,
    synced_input_per_1m     NUMERIC(10, 6) NOT NULL DEFAULT 0,
    synced_output_per_1m    NUMERIC(10, 6) NOT NULL DEFAULT 0,
    override_input_per_1m   NUMERIC(10, 6),          -- NULL = no override
    override_output_per_1m  NUMERIC(10, 6),          -- NULL = no override
    last_synced_at          TIMESTAMPTZ    NOT NULL,
    created_at              TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model_id)
);

CREATE TABLE model_pricing_snapshots (
    id          TEXT        PRIMARY KEY,
    provider    TEXT        NOT NULL,
    model_id    TEXT        NOT NULL,
    source      TEXT        NOT NULL CHECK (source IN ('sync', 'override')),
    input_per_1m  NUMERIC(10, 6) NOT NULL,
    output_per_1m NUMERIC(10, 6) NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON model_pricing_snapshots (provider, model_id, recorded_at DESC);
```

**Price resolution at request time:**

```
effective_input  = COALESCE(override_input_per_1m,  synced_input_per_1m)
effective_output = COALESCE(override_output_per_1m, synced_output_per_1m)
```

If both synced columns are 0 and both override columns are NULL, the price is unknown: `estimated_cost_usd = 0`, `price_unknown = true`.

**Snapshot writes:**
- Sync run: one row per model with `source = 'sync'`, recording synced prices.
- Override set/clear: one row with `source = 'override'`, recording the resulting effective price at the time of the change.

**Migration:** The `input_cost_per_1m` and `output_cost_per_1m` columns are dropped from `virtual_model_routes` once `model_pricing` is populated and all read paths have been updated. The `PricingCache` is updated to load from `model_pricing` rather than `virtual_model_routes`. This is a Phase 1 milestone.

**`price_unknown` flag:** A `price_unknown BOOLEAN NOT NULL DEFAULT false` column is added to `inference_usage`. This distinguishes a genuinely `$0.00` model from a model with no price data, and supports the P2 "unknown pricing" report cleanly. `estimated_cost_usd = 0` alone is ambiguous.

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

**Fail-open:** If Redis is unreachable during a budget check, the request proceeds and a structured log event is emitted. This is a fixed platform behaviour — agent continuity takes precedence over budget cap guarantees during infrastructure outages. It is not configurable per tenant.

---

## Decision 5: Quota Model — Groups and Resolution Order

### Considered Options

#### Option A: Per-principal quotas only

Each principal has at most one `PrincipalQuota` record. Tenant admins create individual records for each principal they want to constrain.

**Pros:**
- Simple schema — one table.
- Straightforward enforcement — one lookup.

**Cons:**
- Does not scale. A tenant admin managing hundreds or thousands of principals (especially agent fleets) must create and maintain individual records. There is no way to apply a default policy to a class of principals.

#### Option B: Quota groups with per-principal override (Chosen)

A `quota_groups` table defines named policies (e.g. "engineer", "agent-fleet") scoped to a tenant. A `principal_quota_group_memberships` join table assigns principals to groups. A `principal_quotas` table holds per-principal overrides that take precedence over any group. Enforcement resolves: individual quota → group quota → no limit.

**Pros:**
- Scales to large principal counts — assign a group once, all members inherit the policy.
- Per-principal overrides remain available for exceptions.
- Each group member gets their own individual limit — the group budget is not a shared pool.
- Clean path for future UI: groups are manageable from `/policy/quotas` alongside individual overrides.

**Cons:**
- Three tables instead of one.
- Quota resolution requires two lookups (individual, then group) instead of one; both can be served from Redis/cache.

#### Option C: Hierarchical roles with inherited quotas

Reuse the existing `policy_role_scopes` mechanism to attach quota metadata to OIDC roles.

**Pros:**
- Leverages existing role infrastructure.

**Cons:**
- Roles are an auth concern; budget policy is an operational concern. Coupling them makes both harder to evolve independently.
- OIDC role claims are managed externally (in Entra/Okta); quota management would require coordinating with the external IdP.

### Decision 5: Quota groups with per-principal override

**Chosen Option: Option B**

**Schema:**

```sql
CREATE TABLE quota_groups (
    id                 TEXT           PRIMARY KEY,
    tenant_id          TEXT           NOT NULL,
    name               TEXT           NOT NULL,
    monthly_budget_usd NUMERIC(12, 4) NOT NULL,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE principal_quota_group_memberships (
    principal_id  TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    group_id      TEXT NOT NULL REFERENCES quota_groups(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, tenant_id)  -- one group per principal per tenant
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

```
1. Look up principal_quotas WHERE principal_id = ? AND tenant_id = ?
   → if found: use this quota (individual override)
2. Look up principal_quota_group_memberships → quota_groups
   WHERE principal_id = ? AND tenant_id = ?
   → if found: use the group quota
3. No quota found → no limit, request proceeds
```

Both lookups can be served from a short-lived in-process cache (e.g. 60-second TTL per principal) to avoid DB reads on the hot path. The Redis counter check only happens if a quota exists.

**Budget limit banner semantics:** The quota API returns `spend_limit_reached: bool`, computed server-side as `current_spend_usd >= monthly_budget_usd`. This is the same condition as the hard block — the principal cannot make further requests, and the banner informs them of this state. The frontend uses the flag directly with no threshold logic. There is no separate configurable alert threshold.

**Quota deletion:** Deleting a `principal_quotas` record or removing a principal from a group removes the cap immediately. Spend already recorded for the month is not adjusted. In-flight requests that have already passed the budget check complete normally.

**API scopes:**
- `platform:quotas:read` — list groups, list memberships, read individual quotas.
- `platform:quotas:write` — create/update/delete groups, assign/remove memberships, set/clear individual quotas.
- All quota API operations are tenant-scoped. A tenant admin cannot read or write quotas for principals outside their own tenant. Platform operators may manage quotas across tenants.

---

## Consequences

### Positive

- `estimated_cost_usd` becomes meaningful for all models in the LiteLLM catalog — covering Bedrock, Vertex AI, Azure OpenAI, Anthropic, and OpenAI.
- Pricing is decoupled from routing; the `model_pricing` table is a clean, single source of truth keyed by `(provider, model_id)`.
- Full price history is queryable point-in-time from `model_pricing_snapshots`.
- Budget enforcement adds < 1 ms to the inference hot path (single Redis GET when quota exists).
- Quota groups scale budget management to large principal fleets without per-principal configuration.
- Monthly budget counters cannot race at the midnight boundary — `DATE_TRUNC` aggregation is stateless.

### Negative

- `virtual_model_routes.input_cost_per_1m` and `output_cost_per_1m` must be migrated away and dropped — a schema migration with a brief dual-write period.
- The 5-minute PricingCache TTL means a platform admin price override takes up to 5 minutes to propagate across all gateway replicas.
- Redis counter drift (bounded to one request's cost) is possible on usage write failures. Self-corrects on rehydration but means the enforced limit may occasionally be off by a small amount.
- Quota resolution now requires two lookups (individual + group) instead of one. Mitigated by the per-principal quota cache.

### Neutral

- `NewNopPricingCache()` is removed from production wiring. Tests that depend on it must be updated to use `NewStaticPricingCache(nil)` or a test double.
- The `price_unknown` column is added to `inference_usage`. Existing records will have `price_unknown = false` — consistent with them having been written with whatever price was available at the time (zero in practice, but not flagged as unknown retroactively).

---

## Implementation

### Phase 1: Pricing Data Pipeline

1. Create `model_pricing` and `model_pricing_snapshots` migrations.
2. Implement `PricingSource` interface and `LiteLLMPricingSource` implementation.
3. Implement `PricingSyncWorker` — fetches, upserts synced columns, writes snapshot rows.
4. Implement `DatabasePricingCache` — loads from `model_pricing`, background refresh at 5-minute interval.
5. Wire `DatabasePricingCache` into `main.go`, replacing `NewNopPricingCache()`.
6. Add `price_unknown` column to `inference_usage`.
7. Add platform admin API routes for price override set/clear (`PUT /v1/catalog/pricing/:provider/:model_id/override`, `DELETE /v1/catalog/pricing/:provider/:model_id/override`), registered under `platform:catalog:write`.
8. Migrate and drop pricing columns from `virtual_model_routes`.
9. Verify `estimated_cost_usd > 0` on test requests for all supported providers.

### Phase 2: Budget Enforcement

1. Create `quota_groups`, `principal_quota_group_memberships`, and `principal_quotas` migrations.
2. Implement `QuotaRepository` — wraps both the DB tables and the Redis counter. Exposes `EffectiveQuota(ctx, principalID, tenantID)` and `IncrementSpend(ctx, principalID, tenantID, amount)`.
3. Implement budget check in `Service.{ChatCompletion, AnthropicMessages, Embed, ...}` — resolve effective quota, read Redis counter, reject with HTTP 429 if met or exceeded.
4. Implement Redis increment in the usage write path.
5. Implement quota group and individual quota CRUD API routes under `platform:quotas:read`/`write` scopes.
6. Add quota API endpoint returning `{ monthly_budget_usd, current_spend_usd, spend_limit_reached }` for the authenticated principal.

### Phase 3: Frontend

See PRD Phase 3 milestones.

### Migration Path

`virtual_model_routes` pricing column migration:

1. Deploy Phase 1 with `model_pricing` populated.
2. Verify `PricingCache` is reading from `model_pricing` in all environments.
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
- OTel span `budget.quota_check` — wraps the Redis read and effective quota resolution on every request for a principal with a quota.

---

## Related Decisions

- [ADR-0039](0039-virtual-model-catalog-and-platform-routing.md) — Virtual model catalog; pricing columns on `virtual_model_routes` are superseded by this ADR.
- [ADR-0023](0023-inference-gateway-architecture.md) — Inference gateway architecture and usage write path.
- [ADR-0040](0040-inference-usage-logs.md) — Inference usage logs; `inference_usage` table is the source of truth for spend aggregation.
- [ADR-0043](0043-inference-analytics.md) — Analytics endpoints consumed by cost dashboards.
- [ADR-0035](0035-policy-gateway-and-authorisation-model.md) — Policy model; `platform:quotas:*` and `platform:catalog:write` scopes follow the established vocabulary pattern.

---

## References

- [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)
- [LiteLLM model_prices_and_context_window.json](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)

---

## Notes

**Why not a shared spending pool for quota groups?** A shared pool requires atomic cross-principal accounting — every request from any group member must decrement a shared counter. This is a distributed coordination problem with no clean Redis primitive. It also creates unpredictable UX: a single high-traffic agent could exhaust the pool for all other members. Per-member individual limits are both simpler to implement and more predictable for operators.

**Why drop `virtual_model_routes` pricing columns rather than leave them?** Leaving them creates an ongoing confusion risk — engineers may write to or read from them not knowing they are superseded. The columns are also populated with stale data that could silently re-emerge if someone wires them back in. A clean drop in Phase 1 is safer than deprecation-in-place.

**Why is the budget limit state a server-computed flag rather than a client-side calculation?** The frontend should not contain business logic. If the definition of "limit reached" ever changes — for example, if a grace amount or grace period is introduced — a client-side calculation would require updates in every frontend surface. The backend computes `spend_limit_reached = current_spend_usd >= monthly_budget_usd` and returns it on the quota API. The frontend reads the flag and renders the banner. There is no configurable alert threshold percentage — the limit and the banner are the same concept at the same boundary.

**Last Updated:** 2026-06-05
