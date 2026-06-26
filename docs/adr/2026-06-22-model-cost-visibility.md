# Model Cost Visibility: Pricing on the Models Page and Virtual Models Catalog

**Status:** Accepted  
**Date:** 2026-06-22  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD: Model Cost Visibility](../prd/model-cost-visibility.md)

---

## Context

### Background

The inference gateway has a fully operational pricing pipeline (see [LLM Cost Tracking ADR](2026-06-05-llm-cost-tracking.md)): a `model_pricing` table with synced and override columns, a `PricingCatalogRepository`, and a `PricingCache` used for request-time cost attribution. The provider catalog page already surfaces this data in the admin UI.

However, two consumer-facing views are missing cost data:

1. **`GET /v1/models`** — the endpoint every user's client calls to list available virtual models. The `modelObject` response struct has no pricing field.
2. **`GET /v1/catalog/virtual-models`** — the admin catalog endpoint. The `virtualModelJSON` struct has route metadata (priority, weight) but no pricing summary.

### Problem Statement

Effective per-token pricing is invisible at the point of model selection (models page) and at the point of routing configuration (virtual models catalog). Users and admins must navigate to the provider catalog tab and manually correlate model IDs to find costs.

### Goals

- Expose effective input and output cost per 1M tokens on both endpoints.
- Reuse the existing pricing infrastructure without introducing new DB round-trips.
- Keep the wire format minimal — only effective prices are needed for display; the full pricing entry is already accessible via the provider catalog for admins who need it.

### Non-Goals

- Per-tenant or per-user pricing customization.
- Real-time price fetch on the hot path.
- Changes to the pricing catalog or PricingCatalogRepository contract.

---

## Decision Drivers

* **No new infrastructure** — the pricing catalog is already loaded per request in `handleListProviderModels`; we apply the same `buildPricingMap` pattern to the new handlers.
* **Wire format stability** — the `modelObject` and `virtualModelJSON` structs are consumed by external clients; additions must be additive (new optional fields only).
* **Consistency with existing UI patterns** — the provider catalog's `EffectivePriceCell` component defines the correct visual treatment; reuse its props shape.
* **Cost definition for virtual models** — a virtual model has multiple routes; a single representative cost must be chosen. The lowest-cost enabled route is the most useful: it represents the floor a `lowest_cost` strategy would select, and for `priority` and `weighted` strategies it provides a conservative lower bound.

---

## Considered Options

### Option 1: Add pricing inline to the existing model/virtual-model response objects

Extend `modelObject` and `virtualModelJSON` with an optional `pricing` field containing `effective_input_cost_per_1m` and `effective_output_cost_per_1m`. Populate by loading the pricing catalog once per request (same bulk-load pattern used by the provider catalog handler).

**Pros:**
- Single API call per page load; no new endpoint.
- Clients get pricing in the same payload they already parse.
- Consistent with how provider models embed pricing today.

**Cons:**
- Slightly larger payload for clients that don't use pricing (acceptable — fields are optional/omitempty).

### Option 2: Add a separate `/v1/models/pricing` endpoint

Return a map of model ID → pricing in a separate call that the frontend joins client-side.

**Pros:**
- Existing `/v1/models` contract completely unchanged.

**Cons:**
- Two API calls needed for a single page render (waterfall).
- Frontend must join two async results, complicating loading states.
- No precedent in the codebase for this join pattern.

### Option 3: Expose pricing only via the existing `/v1/catalog/pricing` endpoint

Require the frontend to call the pricing endpoint separately and join.

**Pros:**
- Zero backend changes.

**Cons:**
- Requires admin-scoped access token to call `/v1/catalog`; regular users cannot call it. This makes the models page impossible to enrich for non-admin users.
- Same two-call waterfall problem as Option 2.

---

## Decision

**Chosen Option:** Option 1 — Add pricing inline to existing response objects.

### Rationale

The pricing catalog is already loaded in a single bulk query in `handleListProviderModels`. The same `buildPricingMap` helper can be called from `handleListModels` and `handleListVirtualModels` with negligible added cost (one DB read per page load, already happening in the provider catalog handler). Inline embedding avoids a second network round-trip and is consistent with the provider model pattern already shipping in production.

For virtual models, the representative price is the minimum `effective_input_cost_per_1m` across enabled routes that have a matching pricing catalog entry. This is the natural floor price and aligns with the `lowest_cost` strategy semantics already in the codebase.

---

## Implementation

### Backend Changes

**`handlers_openai.go`** — `modelObject` and `handleListModels`:

```go
type modelObjectPricing struct {
    EffectiveInputCostPer1M  float64 `json:"effective_input_cost_per_1m"`
    EffectiveOutputCostPer1M float64 `json:"effective_output_cost_per_1m"`
}

type modelObject struct {
    // ... existing fields ...
    Pricing *modelObjectPricing `json:"pricing,omitempty"`
}
```

In `handleListModels`: load the pricing catalog via `c.svc.pricingCatalog.LoadAll(ctx)`, build a `map[string]*PricingCatalogEntry`, then for each virtual model iterate its enabled routes and select the entry with the lowest combined cost. Attach as `Pricing` on the `modelObject`.

**`handlers_catalog.go`** — `virtualModelJSON` and `handleListVirtualModels`:

```go
type virtualModelPricingJSON struct {
    EffectiveInputCostPer1M  float64 `json:"effective_input_cost_per_1m"`
    EffectiveOutputCostPer1M float64 `json:"effective_output_cost_per_1m"`
}

type virtualModelJSON struct {
    // ... existing fields ...
    Pricing *virtualModelPricingJSON `json:"pricing,omitempty"`
}
```

The catalog controller already has access to `c.pricingCatalog`. Build the pricing map once and annotate each virtual model in the list.

**`routeJSON`** — Already carries `input_cost_per_1m` / `output_cost_per_1m` from `VirtualModelRoute`, but these are route-level routing hints. The new top-level `pricing` field is sourced from the pricing catalog (canonical effective price), not the route columns.

### Frontend Changes

**`src/shared/api.ts`** — extend `ModelObject`:

```ts
export interface ModelObjectPricing {
  effective_input_cost_per_1m: number;
  effective_output_cost_per_1m: number;
}

export interface ModelObject {
  // ... existing fields ...
  pricing?: ModelObjectPricing;
}
```

**`_types/index.ts`** — extend `ModelRow` and `toModelRow`:

```ts
export interface ModelRow {
  // ... existing fields ...
  pricing?: { effective_input_cost_per_1m: number; effective_output_cost_per_1m: number };
}
```

**`models-tab.tsx`** — add "Effective Price" column (sortable by input cost, reuse `EffectivePriceCell` styling pattern).

**`catalog-server.ts`** — extend `VirtualModel` wire type with optional `pricing` field.

**`catalog-api.ts`** — propagate `pricing` in `toVirtualModel`.

**`catalog/_types/catalog.ts`** — add optional `pricing` to `VirtualModel`.

**`virtual-models-section.tsx`** — add "Effective Price" column (sortable, same pattern as models-tab).

---

## Consequences

### Positive

- Users can evaluate cost at the point of model selection without additional navigation.
- Platform admins can audit cost configuration directly in the virtual models list.
- Consistent with the visual pattern established by the provider catalog table.

### Negative

- One additional bulk DB read per `GET /v1/models` request (pricing catalog load). Acceptable: the catalog is small (tens to low hundreds of rows) and the provider catalog handler already pays this cost per request.
- `modelObject` wire type grows by an optional field; existing clients that don't parse `pricing` are unaffected.

### Neutral

- The `routeJSON` `input_cost_per_1m` / `output_cost_per_1m` fields (routing hints used by the editor panel) are unchanged.

---

## Related Decisions

- [2026-06-05-llm-cost-tracking](2026-06-05-llm-cost-tracking.md) — Established the pricing pipeline and catalog this feature extends.
- [2026-06-10-budget-model-redesign](2026-06-10-budget-model-redesign.md) — Budget enforcement layer that relies on accurate cost attribution.

---

**Last Updated:** 2026-06-22
