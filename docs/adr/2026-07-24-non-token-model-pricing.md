# Non-Token Model Pricing: Full Cost Dimension Pipeline

**Status:** Accepted  
**Date:** 2026-07-24  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD: Non-Token Model Pricing](../prd/non-token-model-pricing.md)

---

## Context

### Background

The pricing pipeline established in [LLM Cost Tracking](2026-06-05-llm-cost-tracking.md) and extended in [Model Cost Visibility](2026-06-22-model-cost-visibility.md) is built around four per-token cost dimensions: `input_cost_per_token`, `output_cost_per_token`, `cache_read_input_token_cost`, and `cache_creation_input_token_cost`. These map to four `synced_*` columns on `model_pricing` and four fields on `ModelPriceEntry`.

LiteLLM publishes eight additional cost fields covering per-image, per-request, per-second, and per-audio-token billing. The `liteLLMEntry` struct does not parse them — Go's `json.Unmarshal` silently ignores unrecognised fields — so every non-token-priced model is synced with all four `synced_*_cost_per_1m` columns at `0`.

169 models across Orbit's supported providers carry concurrent token and non-token cost dimensions. These are not competing pricing schemes — they are additive billing for different input types within the same request. `gemini-2.5-flash`, for example, bills text input per token and audio input per audio token simultaneously. There is no single "primary" cost unit for these models; all active dimensions apply.

Two additional dimensions — cache read and cache write pricing — are already stored and used for cost attribution on Anthropic-compatible models but are never surfaced in the UI.

### Problem Statement

1. Every non-token-priced model reaches the API with `pricing: { effective_input_cost_per_1m: 0, effective_output_cost_per_1m: 0 }` — a non-nil object. The frontend "Unknown" guard fires only when `pricing === undefined`; it never fires for these models, which render as `$0.00 / $0.00` instead of "Unknown".
2. `vmEffectivePricing` selects the lowest-cost route by comparing `input + output` per-1M token totals. A zero-cost route always wins, polluting the effective price for any virtual model that mixes token-priced and unpriced routes.
3. Cache read/write pricing for Anthropic-compatible models is invisible to platform admins in the provider catalog and model detail view, even though the values are stored and used for cost attribution.

### Goals

- Parse and store all eight non-token cost dimensions from LiteLLM for every supported provider.
- Expose all active (non-zero) cost dimensions through the API and frontend with correct unit labels.
- Surface cache read/write pricing in the provider catalog and model detail view.
- Fix `vmEffectivePricing` so unpriced routes do not pollute effective price selection.
- Render "Unknown" for models with no pricing data in any dimension.

### Non-Goals

- Mixed-modality request handling (audio/image content in request messages).
- Per-request attribution of non-token cost dimensions in `inference_usage`.
- Admin override columns for the new non-token dimensions.
- Budget enforcement changes.

---

## Decision Drivers

* **Additive compatibility** — the four existing token dimensions are the backbone of cost attribution and budget enforcement; changes must not regress them.
* **No collapsing** — models with concurrent dimensions must expose all of them independently; a single "primary" unit loses information and misleads users comparing multimodal models.
* **Consistent naming** — the `synced_*` column convention is established; deviating without cause adds cognitive load.
* **Backwards-compatible wire format** — `GET /v1/models` and `GET /v1/catalog/virtual-models` are consumed by external clients; additions must be additive.
* **No new infrastructure** — the pricing catalog, sync worker, and in-process cache are the right abstraction layer; this feature extends them.

---

## Decisions

This ADR covers five distinct decisions that collectively extend the pricing pipeline end-to-end.

---

## Decision 1: `ModelPriceEntry` Extension — Flat Fields vs. Map vs. Collapsed Pair

### Considered Options

#### Option A: Flat independent fields (one Go field per non-token dimension)

Add eight new fields to `ModelPriceEntry`, one per non-token dimension, following the existing naming convention:

```go
type ModelPriceEntry struct {
    Provider             string
    ModelID              string
    // Existing token fields (unchanged):
    InputCostPer1M       float64
    OutputCostPer1M      float64
    CacheReadCostPer1M   float64
    CacheWriteCostPer1M  float64
    // New non-token dimensions:
    ImageInputCost       float64  // per image input
    ImageOutputCost      float64  // per image output
    RequestInputCost     float64  // flat per-request input
    RequestOutputCost    float64  // flat per-request output
    SecondInputCost      float64  // per-second input
    SecondOutputCost     float64  // per-second output
    AudioInputCostPer1M  float64  // per 1M audio tokens input
    AudioOutputCostPer1M float64  // per 1M audio tokens output
}
```

Audio token costs are scaled to per-1M (×1,000,000) for consistency with regular token pricing. Image, request, and second costs are stored as absolute per-unit values (per image, per request, per second) — the column suffix makes the unit explicit.

**Pros:**
- Consistent with the existing four-field shape; no structural change to how the type is consumed.
- Each dimension is independently addressable in Go and SQL without parsing or type assertion.
- Zero-value semantics are clean: a 0 field means the dimension is absent or free.

**Cons:**
- Eight new fields; struct grows. Acceptable — `ModelPriceEntry` is an internal type with no wire format impact.

#### Option B: `NonTokenDimensions map[string]float64`

Replace the eight individual fields with a single map keyed by dimension name.

**Pros:**
- Struct stays compact regardless of how many dimensions LiteLLM adds in future.

**Cons:**
- String keys require runtime enumeration; no compile-time safety on field names.
- Maps do not map cleanly to SQL columns without JSONB, breaking the existing `UpsertSynced` pattern.
- Serialisation order is non-deterministic.

#### Option C: Collapsed primary/secondary pair

Resolve all non-token dimensions to a single `NonTokenInputCost` / `NonTokenOutputCost` pair, selecting whichever non-token dimension is non-zero (first-wins or highest).

**Pros:**
- Minimal struct change; no fan-out to consumers.

**Cons:**
- Models with concurrent token + non-token dimensions carry both simultaneously; collapsing loses one set with no canonical priority ordering across image/request/second/audio.
- Consumers rendering the cost still need the unit label, which cannot be inferred from a collapsed value.

### Decision 1: Flat independent fields

**Chosen Option: Option A**

Flat fields extend the established pattern cleanly. Each dimension is independently testable, storeable, and zero-safe. The eight additions are a mechanical extension of the four existing fields — no new abstraction layer is warranted.

---

## Decision 2: `model_pricing` Schema — New Columns vs. JSONB vs. Separate Table

### Considered Options

#### Option A: Eight new `synced_*` columns

Add eight `NUMERIC(10,6) NOT NULL DEFAULT 0` columns to `model_pricing` following the established convention:

```sql
-- Per-unit costs (image, request, second — stored as cost per individual unit)
synced_image_input_cost_per_image   NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_image_output_cost_per_image  NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_request_input_cost           NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_request_output_cost          NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_second_input_cost            NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_second_output_cost           NUMERIC(10,6) NOT NULL DEFAULT 0,
-- Audio token costs (scaled to per-1M for consistency with token pricing)
synced_audio_input_cost_per_1m      NUMERIC(10,6) NOT NULL DEFAULT 0,
synced_audio_output_cost_per_1m     NUMERIC(10,6) NOT NULL DEFAULT 0,
```

**Pros:**
- Consistent with the four existing `synced_*` columns in type, nullability, and default.
- Each dimension is independently queryable without JSON operators.
- `DEFAULT 0` means existing rows are valid immediately after migration; no backfill needed.
- Clean path to per-dimension override columns in future (matching the nullable override column pattern established for token dimensions).

**Cons:**
- Schema grows by eight columns. Acceptable — `model_pricing` is a narrow, low-volume catalog table (hundreds of rows) written once per day.

#### Option B: `non_token_costs JSONB DEFAULT '{}'`

A single JSONB column stores all non-token dimensions as a keyed object.

**Pros:**
- Schema stays compact regardless of future LiteLLM dimension additions.

**Cons:**
- Dimensions are not independently queryable without JSON operators.
- JSONB parsing adds overhead in `LoadAll`, which runs on every cache refresh (5-minute interval).
- Per-dimension overrides — a plausible future requirement — cannot follow the existing nullable column pattern.
- Breaks the established naming and access pattern used by `UpsertSynced`.

#### Option C: Separate `model_pricing_non_token` table keyed by `(provider, model_id, dimension)`

One row per non-zero dimension per model.

**Pros:**
- Open-ended; new dimensions require no schema change.

**Cons:**
- `LoadAll` requires a JOIN or separate query; the single-query bulk load becomes more complex.
- `UpsertSynced` must manage rows (delete-and-reinsert or upsert-by-dimension), not columns — introducing race conditions that the current ON CONFLICT approach avoids.
- Overkill for eight dimensions defined by a stable external schema.

### Decision 2: Eight new `synced_*` columns

**Chosen Option: Option A**

Columns are additive with `DEFAULT 0` — no backfill, no dual-write period. The `UpsertSynced` conflict target (`ON CONFLICT (provider, model_id) DO UPDATE`) is unchanged; it simply writes twelve `synced_*` columns instead of four. Override columns can be added per-dimension in a future ADR using the exact same nullable column pattern established for token dimensions.

---

## Decision 3: `vmEffectivePricing` — Definition of Effective Price and Route Eligibility

### What "effective price" means

Effective price is defined as **the per-1M token rate for a standard text request, after any platform overrides are applied.** It is intentionally scoped to token dimensions only.

Non-token dimensions (per-image, per-request, per-second, per-audio-token) are incommensurable with token pricing — `$0.04 / image` cannot be meaningfully compared to `$0.50 / 1M tokens`. Including them in a single effective price number would require an arbitrary exchange rate. Effective price is therefore the minimum `effective_input_cost_per_1m + effective_output_cost_per_1m` across eligible routes — the same metric used today.

Routes without token pricing are ineligible for the comparator. They have real costs in other dimensions, but those costs cannot be expressed in the token unit the comparator uses.

### Considered Options

#### Option A: Filter out routes without token pricing; return `nil` when no token-priced route exists

Routes where both `effective_input_cost_per_1m` and `effective_output_cost_per_1m` are zero are excluded from the comparator. The existing token-to-token minimum runs over the eligible subset. `vmEffectivePricing` returns `nil` when no eligible route remains.

**Pros:**
- Zero-token-cost routes no longer pollute effective price selection.
- A virtual model with only non-token routes correctly displays "Unknown" — it has costs, just not in a token-comparable unit.
- Token-to-token comparison is preserved and semantically clean.
- The definition is simple to explain: effective price = cheapest token-priced route.

**Cons:**
- A virtual model with a mix of token-priced and non-token-only routes displays "Unknown" despite the non-token routes having real costs. This is an accurate representation of the unit constraint, not a pipeline gap. Revisit when image/audio endpoints ship and a multi-unit effective price can be computed per modality.

#### Option B: Keep current behaviour — zero-token-cost routes participate in comparator

**Pros:**
- No behaviour change for existing virtual models.

**Cons:**
- Zero-token-cost routes always win the minimum comparison, producing `$0.00` effective price for any virtual model that includes one. Incorrect.

#### Option C: Return `nil` for any virtual model with any non-token-only route

**Pros:**
- Simple rule; no edge cases.

**Cons:**
- Overcorrects: a virtual model with ten token-priced routes and one non-token route shows "Unknown" even though the ten token routes have a meaningful effective price.

### Decision 3: Filter on token pricing; non-token fallback with `non_token_only` label

**Chosen Option: Option A (extended)**

Routes where both token dimensions are zero are excluded from the comparator. When no token-priced route remains, `vmEffectivePricing` does not immediately return `nil` — it performs a second pass to find the first non-token-priced route and returns a pricing object with `NonTokenOnly: true`. This signals to the frontend that the virtual model has real costs, just not in the token unit effective price uses.

`vmEffectivePricing` returns `nil` only when no route has pricing data in any dimension — genuinely unpriced models with no token or non-token costs.

```go
func lacksTokenPricing(p ModelPricing) bool {
    return p.InputCostPerMillion == 0 && p.OutputCostPerMillion == 0
}

func hasNonTokenPricing(p ModelPricing) bool {
    return p.ImageInputCost > 0 || p.ImageOutputCost > 0 ||
        p.RequestInputCost > 0 || p.RequestOutputCost > 0 ||
        p.SecondInputCost > 0 || p.SecondOutputCost > 0 ||
        p.AudioInputCostPer1M > 0 || p.AudioOutputCostPer1M > 0
}
```

The `non_token_only: true` field on the API response is the frontend's machine-readable signal. When set, `EffectivePriceCell` and `VmEffectivePriceCell` render the alternative-pricing indicator (muted `—` + info icon) rather than "Unknown" — conveying "priced, but not in token units" rather than "no data."

---

## Decision 4: API Wire Format for Non-Token Dimensions

### Considered Options

#### Option A: Named fields with `omitempty` alongside existing token fields

Extend `modelObjectPricing` with one named `omitempty` field per non-token dimension. The struct is returned as `nil` when all dimensions are zero — fixing the root cause of the `$0.00` display.

```go
type modelObjectPricing struct {
    // Existing (unchanged):
    EffectiveInputCostPer1M  float64 `json:"effective_input_cost_per_1m,omitempty"`
    EffectiveOutputCostPer1M float64 `json:"effective_output_cost_per_1m,omitempty"`
    CacheReadCostPer1M       float64 `json:"cache_read_cost_per_1m,omitempty"`
    CacheWriteCostPer1M      float64 `json:"cache_write_cost_per_1m,omitempty"`
    // New:
    ImageInputCost            float64 `json:"image_input_cost,omitempty"`
    ImageOutputCost           float64 `json:"image_output_cost,omitempty"`
    RequestInputCost          float64 `json:"request_input_cost,omitempty"`
    RequestOutputCost         float64 `json:"request_output_cost,omitempty"`
    SecondInputCost           float64 `json:"second_input_cost,omitempty"`
    SecondOutputCost          float64 `json:"second_output_cost,omitempty"`
    AudioInputCostPer1M       float64 `json:"audio_input_cost_per_1m,omitempty"`
    AudioOutputCostPer1M      float64 `json:"audio_output_cost_per_1m,omitempty"`
}
```

**Pros:**
- Purely additive — existing clients that parse only `effective_input_cost_per_1m` and `effective_output_cost_per_1m` are unaffected.
- Named fields are statically typed end-to-end from Go struct to TypeScript interface.
- `omitempty` keeps the payload compact for token-only models; `pricing` is `nil` when all dimensions are zero.

**Cons:**
- Adding a new LiteLLM dimension in future requires a Go struct field, a DB column, and a TS type update. Acceptable — this is a schema-defined list, not an open-ended extensibility point.

#### Option B: `dimensions` array `[{unit, input_cost, output_cost}]`

Return active dimensions as an array of typed objects.

**Pros:**
- Open-ended; new dimensions require no struct change.

**Cons:**
- Frontend must enumerate an array; no static type safety per dimension.
- Unit strings become a stringly-typed contract — a typo on the backend produces a silently incorrect frontend render.
- Existing token fields do not fit cleanly into the array shape without special-casing effective/override semantics.

#### Option C: Separate `/v1/models/pricing` endpoint

**Pros:**
- Existing `/v1/models` payload is unchanged.

**Cons:**
- Two API calls for a single page render.
- Regular users cannot call `/v1/catalog` endpoints — the models page cannot be enriched for non-admins (the same constraint that ruled out this option in the Model Cost Visibility ADR).

### Decision 4: Named fields with `omitempty`

**Chosen Option: Option A**

Named fields preserve static typing end-to-end. The `omitempty` guarantee means `pricing` is `nil` only when LiteLLM carries no cost data for the model — resolving the root cause of the "$0.00" display. The `pricing === undefined` frontend guard already works correctly; the fix is in the backend nil gate, not the frontend condition.

---

## Decision 5: Frontend Rendering of Multi-Dimensional Pricing

### Considered Options

#### Option A: All active dimensions as separate labelled rows; token pricing prominent in table cells

Detail views (model detail modal, virtual model detail panel, provider catalog row detail) enumerate each active cost dimension as a separate row with its unit label. Table cells show token input/output pricing prominently when present; non-token dimensions are appended as additional labelled rows. For non-token-only models, the non-token dimension is the only row shown.

**Pros:**
- No information loss — all active billing dimensions are visible.
- Multi-dimensional models are distinguishable from single-dimension models.
- Token pricing, where present, remains the primary comparator for users evaluating relative costs.

**Cons:**
- Table cells with multiple dimensions are taller than single-value cells; column sort remains on token input cost.

#### Option B: Collapse to single "primary" unit

**Pros:**
- Table cells are uniform height.

**Cons:**
- No canonical priority ordering across image/request/second/audio dimensions.
- Loses information for multi-dimensional models; users comparing a per-image model to a per-token model see incompatible values with no warning.

#### Option C: Show token pricing only; badge indicating "additional costs apply"

**Pros:**
- Table layout is predictable.

**Cons:**
- Non-token-only models (no token pricing) would show nothing in the token row — requires a special case.
- The badge communicates existence but gives no usable cost information.

### Decision 5: Full dimension display in detail views; token pricing prominent in table cells; alternative-pricing indicator in effective price cells

**Chosen Option: Option A**

Detail views render all active dimensions with unit labels. Table cells show token pricing in the first row when present (consistent with the sort accessor and existing `EffectivePriceCell` shape); non-token dimensions follow as additional labelled rows. For non-token-only models, the non-token row is the only row shown.

**Three-state effective price cell:**

The "Effective Price" cell has three distinct states:

| State | Condition | Display |
|---|---|---|
| Known | Token pricing present | Cost value(s) with `/1M` unit |
| Unknown | `pricing === undefined` | Amber icon — no data |
| Alternative pricing | `pricing` present but no token dimensions | Indicator directing user to detail view |

The third state is new. A model with `pricing.image_input_cost` set but both token fields absent has real cost data — "Unknown" is wrong. The cell renders a visual indicator (e.g. a muted `—` with a small icon or label) that conveys "priced, but not in token units — see detail view." The exact visual treatment is a frontend implementation decision; the requirement is that it is visually distinct from both the cost-value state and the amber Unknown state.

This state applies to both the provider catalog / models tab `EffectivePriceCell` and the virtual models `VmEffectivePriceCell`.

Cache read/write costs are rendered as a distinct secondary section in the provider catalog and model detail view — separate from effective pricing, rendered only when at least one cache field is non-zero.

The "Effective Price" column header in all three pricing tables (provider catalog, virtual models, models tab) carries a `headerTooltip` info icon with the definition of effective price. The tooltip strings differ by surface:

- **Provider catalog and models tab:** "The per-1M token price for a standard text request, after any platform overrides are applied."
- **Virtual models catalog:** "The lowest per-1M token price across all token-priced enabled routes, after overrides. Routes without token pricing are excluded."

The strings are set as `headerTooltip` on the column definition and rendered by an optional `headerTooltip?: string` field added to `DataTableColumn` in Terra. Updating the strings is a frontend-only change with no backend impact.

---

## Implementation

### Backend Changes

**`pricing_source.go` — `liteLLMEntry`:**

All eight new fields use `*float64` (pointer) so absent fields are nil rather than 0 — the same pattern as the existing token fields. The `Fetch` conversion loop populates non-nil values on `ModelPriceEntry`; audio tokens are scaled ×1,000,000 to per-1M; image, request, and second costs are stored as-is (per unit).

**`pricing_catalog.go` — migration and `UpsertSynced`:**

A Goose migration under `cmd/inference-gateway/migrations/` adds eight columns with `DEFAULT 0`. The `UpsertSynced` INSERT/ON CONFLICT statement writes all twelve `synced_*` columns atomically. `PricingCatalogEntry`, `LoadAll`, and the in-memory `ModelPricing` struct are updated to carry all twelve dimensions.

**`handlers_openai.go` — `modelObjectPricing` and `vmEffectivePricing`:**

`modelObjectPricing` is returned as `nil` when all pricing dimensions are zero for the resolved entry (Decision 4 nil gate). `vmEffectivePricing` filters routes using `lacksTokenPricing` (routes where both token dimensions are zero are excluded from the token-priced comparator). When no token-priced route remains, a second pass returns the first enabled route carrying non-token data with `NonTokenOnly: true`; `nil` is returned only when no eligible route has pricing data in any dimension (Decision 3).

### Frontend Changes

**`src/shared/api.ts` — `ModelObjectPricing`:**

All new fields are optional (`?`), consistent with `omitempty` on the backend.

**Pricing display components:**

`EffectivePriceCell`, `VmEffectivePriceCell`, and the model detail modal render active non-token dimensions as additional labelled rows. A static `PRICING_ROWS` map drives rendering order and unit labels — only rows where at least one of the input/output keys is non-zero are rendered.

`EffectivePriceCell` and `VmEffectivePriceCell` implement the three-state logic from Decision 5: cost value when token pricing is present; amber Unknown when `pricing === undefined`; alternative-pricing indicator when `pricing` is present but both token dimensions are zero.

**Cache pricing:**

`cache_read_cost_per_1m` and `cache_write_cost_per_1m` are surfaced in the provider catalog row and model detail view as a "Cache pricing" secondary section. Rows are omitted entirely when both are zero.

**`DataTable` — `headerTooltip`:**

`DataTableColumn` gains `headerTooltip?: string`. The Terra `DataTable` renders an inline `<Icon name="info" size="2xs">` wrapped in a `<Tooltip>` when the field is set. The "Effective Price" column in all three pricing tables uses this field with the following strings:

- `provider-catalog-section.tsx` and `models-tab.tsx`: `"The per-1M token price for a standard text request, after any platform overrides are applied."`
- `virtual-models-section.tsx`: `"The lowest per-1M token price across all token-priced enabled routes, after overrides. Routes without token pricing are excluded."`

### Migration Path

1. Add Goose migration for the eight new `synced_*` columns (`DEFAULT 0`; no backfill required).
2. Deploy backend — next sync run populates the new columns for all supported provider models.
3. Deploy frontend — non-token dimension rows appear as data becomes available.

No dual-write period. `UpsertSynced` is idempotent; running the sync worker twice produces identical DB state.

---

## Consequences

### Positive

- Models with non-token pricing display accurate costs with unit labels instead of `$0.00`.
- Cache read/write pricing is visible to platform admins for models that carry it.
- `vmEffectivePricing` correctly excludes routes without token pricing; effective price for virtual models is no longer polluted by zero-token-cost entries.
- `pricing === undefined` fires for genuinely unpriced models — the existing frontend guard works correctly without modification.
- All twelve pricing dimensions are independently stored; per-dimension override columns can be added in a future ADR using the established nullable column pattern.

### Negative

- `model_pricing` gains eight columns (twelve `synced_*` total). `UpsertSynced` grows correspondingly. Acceptable — the table has hundreds of rows and is written once per day.
- Virtual models where no route has token pricing render the alternative-pricing indicator (`non_token_only: true`) rather than "Unknown". If no route has pricing data in any dimension, "Unknown" is shown. This distinction is accurate: non-token-only models have real costs, just not expressible in the token unit effective price uses.
- Table cells for multi-dimensional models are taller than single-dimension cells.

### Neutral

- `ModelPriceEntry` grows by eight fields; it is an internal type with no wire format impact.
- The `PricingSource` interface contract gains eight new fields on `ModelPriceEntry`; any alternative `PricingSource` implementation must be updated.
- The frontend "Unknown" guard condition (`pricing === undefined`) is unchanged — the fix is in the backend nil gate.

---

## Related Decisions

- [2026-06-05-llm-cost-tracking](2026-06-05-llm-cost-tracking.md) — Established the pricing pipeline, `ModelPriceEntry`, `PricingSource` interface, and `model_pricing` schema this feature extends.
- [2026-06-22-model-cost-visibility](2026-06-22-model-cost-visibility.md) — Added `pricing` to the models and virtual models API responses; `modelObjectPricing` shape extended here.
- [2026-06-10-budget-model-redesign](2026-06-10-budget-model-redesign.md) — Budget enforcement layer; non-token cost attribution to `inference_usage` is explicitly deferred and does not affect budget enforcement in this ADR.

---

**Last Updated:** 2026-07-24
