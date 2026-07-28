# Non-Token Model Pricing — Product Requirements Document

**Status:** Draft  
**Created:** 2026-07-16  
**Last Updated:** 2026-07-16  

**Owner:** Platform Engineering  
**Stakeholders:** Platform Admins, Tenant Users

---

## Executive Summary

The inference gateway's pricing pipeline today is built entirely around per-token cost dimensions (`input_cost_per_token`, `output_cost_per_token`, etc.). LiteLLM publishes eight additional cost fields covering per-image, per-request, per-second, and per-audio-token billing — all of which are silently ignored by the current parser. Because zero is a valid-looking value, the frontend renders `$0.00` instead of the "Unknown" state, misrepresenting affected models as free.

Importantly, many of these models are not purely non-token-priced: LiteLLM carries 169 models with concurrent token and non-token dimensions, where each dimension applies to a different modality in the same request (e.g. a realtime model that bills text input at `$X / 1M tokens` and audio input at `$Y / audio token` simultaneously). These are not competing schemes — they are additive billing for different input types.

Orbit currently exposes endpoints for chat completions, messages, embeddings, and responses. There is no live endpoint for image generation or audio — models with those capabilities are catalogued and discoverable but not yet routable. Per-request cost attribution for multi-dimensional models is therefore deferred until the relevant endpoints exist.

Additionally, Anthropic-compatible models carry two cache pricing dimensions (`cache_read_input_token_cost`, `cache_creation_input_token_cost`) that are already captured in the DB and used for request-cost attribution — but are never surfaced in any UI. Platform admins have no way to see cache pricing in the provider catalog or model detail view.

This feature extends the pricing pipeline to store all active cost dimensions per model and surfaces them in the UI. Mixed-modality request handling and per-request attribution of non-token dimensions are explicitly out of scope.

---

## Problem Statement

### Current State

The `liteLLMEntry` struct in `pricing_source.go` parses only four token-based fields from LiteLLM's JSON:

- `input_cost_per_token`
- `output_cost_per_token`
- `cache_read_input_token_cost` — captured and stored, but not surfaced in the UI
- `cache_creation_input_token_cost` — captured and stored, but not surfaced in the UI

LiteLLM publishes eight additional cost fields for non-token-priced models:

| LiteLLM field | Unit |
|---|---|
| `input_cost_per_image` | Per image input |
| `output_cost_per_image` | Per image output |
| `input_cost_per_request` | Flat per-request input |
| `output_cost_per_request` | Flat per-request output |
| `input_cost_per_second` | Per-second input (audio/video) |
| `output_cost_per_second` | Per-second output |
| `input_cost_per_audio_token` | Per audio token input |
| `output_cost_per_audio_token` | Per audio token output |

Go's `json.Unmarshal` silently ignores fields not present in the target struct. Every non-token-priced model is therefore synced with all four `synced_*_cost_per_1m` columns at `0`.

Many non-token-priced models in LiteLLM are not exclusively non-token: 169 models across Orbit's supported providers carry concurrent token and non-token cost dimensions. These represent multimodal billing — a single request may include text and audio, each billed by a different unit — rather than competing or conflicting pricing schemes. For example, `gemini-2.5-flash` carries both `input_cost_per_token` and `input_cost_per_audio_token` because a request can contain both text and audio input. There is no single "primary" cost unit for these models; all active dimensions apply.

### User Pain Points

- **Platform admins** browsing the provider catalog see `$0.00 / $0.00` for image and embedding models. There is no way to distinguish a genuinely free model from one that is unpriced due to a pipeline gap.
- **Platform admins** also cannot see cache read/write pricing for Anthropic-compatible models anywhere in the UI, even though those values are stored and used for cost attribution.
- **Users** on the models page see `$0.00` for models like `Stable Image Fast Upscale`. The "Unknown" guard that displays the amber warning icon fires only when `pricing` is `nil` — it never fires for these models because the backend returns a non-nil `pricing` object with zero values.
- **Virtual model effective pricing** (`vmEffectivePricing`) selects the lowest-cost route by comparing `input + output` token totals. A zero-cost route always wins, polluting the effective price for any virtual model that mixes token-priced and unpriced routes.

### Business Impact

- Cost transparency is incorrect for a visible class of provider models.
- Budget attribution for non-token requests cannot be accurate while their costs are stored as zero. This is deferred until the relevant request endpoints exist, but correct storage is a prerequisite.
- Incorrect effective pricing on virtual models undermines the `lowest_cost` routing strategy when unpriced routes exist alongside token-priced routes.

---

## Goals and Objectives

### Primary Goals

1. Accurately capture and store all non-token cost dimensions published by LiteLLM for every supported provider, including models with concurrent token and non-token dimensions.
2. Surface all active cost dimensions and their unit labels in model pricing display surfaces — models page, virtual models catalog, provider catalog, and model detail modal. For multi-dimensional models, display all active dimensions rather than collapsing to a single "primary" unit.
3. Surface cache read/write pricing in the provider catalog and model detail view for models that carry it.
4. Render "Unknown" (amber warning icon) for models that genuinely have no pricing data in any dimension.
5. Fix `vmEffectivePricing` so routes where all known cost fields are zero are excluded from route selection when non-zero-cost routes are available.

### Non-Goals

- Mixed-modality request handling — accepting audio or image content in request messages, routing it correctly through each provider connector, and verifying round-trip fidelity. This is not yet implemented or tested.
- Per-request attribution of non-token cost dimensions. This includes per-audio-token costs (audio input travels through `POST /chat/completions` but Orbit does not yet extract audio token counts from provider usage responses), per-image costs (no image generation endpoint exists), and any other non-token dimension. Storing and displaying all dimensions is in scope; attributing them from live requests is not.
- Budget enforcement changes dependent on attribution accuracy.
- Admin override columns for the new cost dimensions; the existing token-dimension override flow is unchanged.

### Success Metrics

- Zero `$0.00` pricing display for models whose LiteLLM entry contains a non-zero non-token cost field.
- Cache read/write costs visible in the provider catalog and model detail view for all models that carry them.
- `pricing` is `nil` in the API response only when LiteLLM carries no cost data whatsoever for that model.
- `vmEffectivePricing` never selects a route where all cost fields are zero when a non-zero-cost route is available.
- `pnpm lint` and `make check` pass with zero errors.

---

## User Stories

**As a** platform admin,  
**I want to** see an accurate cost and unit label (e.g. `$0.04 / image`) for image-generation and embedding models in the provider catalog,  
**So that** I can evaluate the real cost of these models relative to text models.

**Acceptance Criteria:**
- [ ] Provider catalog table shows the primary non-token cost with its unit label for models whose only pricing is non-token-based.
- [ ] Models with no pricing data in any dimension show the "Unknown" indicator (amber icon).
- [ ] Models with non-token pricing but no token pricing show a visual indicator in the "Effective Price" cell that alternative pricing exists, directing the user to the detail view.

---

**As a** user browsing the models page,  
**I want to** see a real cost and unit for non-token models instead of `$0.00 / $0.00`,  
**So that** I can make informed model choices without being misled about cost.

**Acceptance Criteria:**
- [ ] Models with only non-token pricing display the cost and correct unit label.
- [ ] Models with no pricing data in any dimension display the amber "Unknown" indicator.
- [ ] Models with non-token pricing but no token pricing show a visual indicator in the "Effective Price" cell that alternative pricing exists, directing the user to the detail view.
- [ ] Token-priced models are unaffected.

---

**As a** platform admin configuring virtual models,  
**I want** effective pricing for virtual models to reflect only routes with real cost data,  
**So that** zero-cost entries from unpriced models do not pollute the effective price display.

**Acceptance Criteria:**
- [ ] `vmEffectivePricing` skips routes without token pricing.
- [ ] A virtual model with no token-priced routes returns `nil` pricing (displays "Unknown").
- [ ] A virtual model with at least one token-priced route shows "Effective Price" computed from those routes only.
- [ ] A virtual model whose routes include non-token-only models shows a visual indicator in the "Effective Price" cell that some routes have alternative pricing not reflected in the effective price.

---

**As a** platform admin,  
**I want to** see cache read and write pricing for Anthropic-compatible models in the provider catalog and model detail view,  
**So that** I can understand the full cost profile of a model before routing traffic to it.

**Acceptance Criteria:**
- [ ] Provider catalog row shows cache read and write costs where non-zero.
- [ ] Model detail view shows cache read and write costs where non-zero.
- [ ] Models without cache pricing do not show empty or `$0.00` cache rows — the rows are omitted entirely.

---

## Proposed Solution

### High-Level Approach

The fix is a pipeline extension that adds all non-token cost dimensions at every layer, from the LiteLLM JSON decoder to the API response. The frontend "Unknown" guard (`pricing === undefined`) already works correctly — the root cause is that it never fires because the backend emits `{0, 0}` instead of `nil`.

The key design constraint is that many models carry **concurrent** cost dimensions: token pricing for text input/output alongside audio-token or image pricing for other modalities in the same request. These are not competing — they are additive billing for different content types. The schema and API must store and expose all active dimensions independently rather than collapsing to a single "primary" unit.

For the `vmEffectivePricing` comparator, routes where all cost fields across all dimensions are zero are treated as unpriced and excluded from selection. Routes with any non-zero cost dimension are eligible. Since cross-dimension cost comparison is not meaningful (tokens vs. images vs. audio tokens are incommensurable), effective pricing for virtual models is only computed when all eligible routes share a token-priced dimension — the common case. Mixed-unit virtual models display each route's cost individually in the detail view rather than a single effective price.

### Key Components

**`pricing_source.go` — `liteLLMEntry` and `Fetch`**  
Extend `liteLLMEntry` with all eight non-token fields. `ModelPriceEntry` gains eight new fields, one per non-token dimension. All non-zero fields are populated; no collapsing or priority selection occurs at the source layer. The existing four token fields are unchanged.

**`pricing_catalog.go` and DB schema**  
Add one column per non-token dimension to `model_pricing` (eight `synced_*` columns), following the same naming convention as the existing token columns. This keeps every dimension independently queryable and overridable in future without a schema change. The `UpsertSynced` method and `PricingCatalogEntry` struct are updated accordingly.

**`handlers_openai.go` — `vmEffectivePricing` and `modelObjectPricing`**  
`modelObjectPricing` is extended to carry all active cost dimensions as named fields alongside the existing token fields. `vmEffectivePricing` is updated to skip routes where all dimensions are zero. Effective price comparisons remain token-based (input + output per 1M); routes without token pricing are excluded from the comparator and shown as individual rows in the detail view.

**API response `pricing` object**  
The `pricing` field on `GET /v1/models` and `GET /v1/catalog/virtual-models` exposes all non-zero cost dimensions as named fields. Dimensions that are zero are omitted (`omitempty`). This is a purely additive change — existing token fields are retained.

**Frontend**  
Pricing display surfaces (models page, virtual models catalog, provider catalog, model detail view) are updated to render each non-zero cost dimension with its unit label. Table cells show all active dimensions; single-dimension models are unchanged. Cache read/write costs are added to the provider catalog row and model detail view, rendered only when non-zero. The "Unknown" guard is already correct.

---

## Scope

### In Scope

- `liteLLMEntry`: add all eight non-token cost fields; populate all non-zero dimensions on `ModelPriceEntry`.
- `ModelPriceEntry`: eight new fields, one per non-token dimension; no collapsed primary/secondary representation.
- `model_pricing` DB schema: add eight `synced_*` columns for the non-token dimensions via migration.
- `PricingCatalogEntry`, `UpsertSynced`, `LoadAll`: updated to carry the new columns.
- `PricingCache` / `ModelPricing`: propagate all dimensions through the in-memory cache.
- `modelObjectPricing`: expose all non-zero cost dimensions as named fields (`omitempty`).
- `vmEffectivePricing`: skip routes where all cost fields are zero; effective price comparisons remain token-based.
- API response: `GET /v1/models` and `GET /v1/catalog/virtual-models` expose all active cost dimensions.
- Frontend: render all non-zero cost dimensions with unit labels in models page, virtual models catalog, provider catalog, and model detail view.
- Frontend: cache read/write cost rows in provider catalog and model detail view (rendered only when non-zero).

### Out of Scope

- Mixed-modality request handling (audio/image content in request messages).
- Per-request attribution of non-token cost dimensions in `inference_usage`.
- Multi-dimensional cost attribution for concurrent token + non-token requests.
- Admin override columns for non-token dimensions.
- Budget enforcement changes.
- Changes to the existing token-dimension `effective_*_cost_per_1m` columns on `inference_usage`.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `liteLLMEntry` parses all eight non-token cost fields from the LiteLLM JSON.
- [ ] All non-zero cost dimensions are stored independently on `ModelPriceEntry` and in `model_pricing`; no collapsing to a primary/secondary pair.
- [ ] `vmEffectivePricing` returns `nil` when no enabled route has token pricing.
- [ ] `GET /v1/models` and `GET /v1/catalog/virtual-models` expose all non-zero cost dimensions as named fields.
- [ ] Frontend renders all active cost dimensions with correct unit labels.
- [ ] Frontend renders "Unknown" (amber icon) for `pricing === undefined` — behaviour unchanged.
- [ ] `modelObjectPricing` exposes `cache_read_cost_per_1m` and `cache_write_cost_per_1m` when non-zero.
- [ ] Provider catalog row shows cache read/write costs where present; omits them entirely when zero.
- [ ] Model detail view shows cache read/write costs where present; omits them entirely when zero.
- [ ] "Effective Price" cells for models with non-token pricing but no token pricing show a visual indicator that alternative pricing exists, directing users to the detail view.

**Should Have (P1):**
- [ ] Virtual models catalog renders all active cost dimensions per route in the detail panel.
- [ ] Multi-dimensional models (concurrent token + non-token) display each dimension as a separate labelled row rather than a single combined value.
- [ ] "Effective Price" column header in all three pricing tables (provider catalog, virtual models catalog, models tab) carries an info-icon tooltip defining effective pricing. Provider/models tooltip: "The per-1M token price for a standard text request, after any platform overrides are applied." Virtual models tooltip: "The lowest per-1M token price across all token-priced enabled routes, after overrides. Routes without token pricing are excluded."

### Non-Functional Requirements

**Correctness:**  
- Models with token pricing must not regress — the four existing token-dimension columns are unchanged.
- The `UpsertSynced` conflict target is idempotent; running the sync worker twice produces identical DB state.

**Backwards Compatibility:**  
The API response change is additive only. The existing `effective_input_cost_per_1m` and `effective_output_cost_per_1m` fields are retained and unchanged for token-priced models. New dimension fields are added alongside them with `omitempty`; clients that do not parse them are unaffected.

---

## Dependencies

- **LiteLLM `model_prices_and_context_window.json`** (existing data source): already carries the non-token fields; no source change needed.
- **`PricingCatalogRepository.UpsertSynced`** and **`LoadAll`**: updated in this feature.
- **`PricingCache` / `ModelPricing`** in `pricing_db_cache.go`: updated to propagate the new fields.
- **`fmtCost` utility** (`@orbit/utils`): already exported, accepts a numeric value; unit label is rendered separately.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| Multi-dimensional models display many cost rows, cluttering table cells | Poor UX | Medium | Show all dimensions in detail view; table cells show token pricing prominently with an indicator for additional dimensions |
| Migration on `model_pricing` (adding 8 columns) breaks the sync worker during rollout | Pricing cache empty until restart | Low | Columns are additive with `DEFAULT 0`; existing rows are not modified |
| `vmEffectivePricing` skips a route with real cost because only non-token dimensions are non-zero | Effective price shows "Unknown" for a routable model | Low | Acceptable for current scope — those models are not routable through Orbit today. Revisit when image/audio endpoints ship. |
