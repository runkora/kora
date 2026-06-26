# Model Cost Visibility - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-22  
**Last Updated:** 2026-06-22  
**Owner:** Platform Engineering  
**Stakeholders:** Platform Admins, Tenant Users

---

## Executive Summary

Orbit has cost attribution for every inference request and a pricing catalog for provider models, but the effective per-token pricing is invisible to users on the two places they browse available models: the `/model-gateway/models` page (what every user sees) and the virtual models list on `/model-gateway/catalog` (what platform admins configure). This feature surfaces effective pricing in both locations, making costs discoverable at the point of model selection.

---

## Problem Statement

### Current State

- The provider catalog table at `/model-gateway/catalog` (Provider Catalog tab) already shows effective pricing per-model with synced and override columns.
- The virtual models table (Virtual Models tab on the same page) shows name, strategy, routes, and status — but no pricing.
- The models page at `/model-gateway/models` (the user-facing view) shows availability, capabilities, and dialects — but no pricing.

### User Pain Points

- Users browsing the models page cannot see at-a-glance which models are expensive vs cheap before making a selection.
- Platform admins managing virtual models in the catalog cannot quickly compare cost across virtual models without opening each one.
- The only way to find pricing today is to navigate to the Provider Catalog tab, search for the right provider model, and read its cost — a multi-step flow that breaks context.

### Business Impact

- Without visible pricing, users may inadvertently select expensive models for routine tasks, driving unnecessary spend.
- Platform admins cannot make cost-aware routing decisions (e.g. verifying that `lowest_cost` strategy is configured correctly) without leaving the virtual model list.
- Cost transparency is a prerequisite for meaningful budget governance.

---

## Goals and Objectives

### Primary Goals

1. Surface effective input/output cost per 1M tokens on the models page so any user can evaluate cost before selecting a model.
2. Surface effective pricing on the virtual models list so platform admins have at-a-glance cost context when managing routing.

### Success Metrics

- Effective pricing visible in the models table without any additional navigation.
- Effective pricing visible in the virtual models table without opening the editor panel.
- No regression in the provider catalog table's existing pricing display.

### Non-Goals

- Per-user or per-tenant pricing overrides (already deferred in the LLM cost tracking ADR).
- Historical cost trend columns on either table.
- Pricing on the model detail modal (may follow in a future iteration).

---

## User Stories

### Primary User Stories

**As a** tenant user,  
**I want to** see the effective cost per 1M tokens for each model on the models page,  
**So that** I can choose a cost-appropriate model without navigating away.

**Acceptance Criteria:**
- [ ] Models table shows an "Effective Price" column with input cost per 1M tokens.
- [ ] Output cost is visible on hover/sub-text in the same cell.
- [ ] Models with no pricing data show a clear "Unknown" indicator.

---

**As a** platform admin,  
**I want to** see effective pricing on the virtual models table in the catalog,  
**So that** I can verify cost configuration and make routing decisions quickly.

**Acceptance Criteria:**
- [ ] Virtual models table shows an "Effective Price" column.
- [ ] The cost shown reflects the cheapest enabled route for priority/weighted strategies, matching the model that would be selected under `lowest_cost`.
- [ ] Virtual models with no costed routes show "Unknown".

---

## Proposed Solution

### High-Level Approach

**Backend:** Extend the `GET /v1/models` and `GET /v1/catalog/virtual-models` responses to include pricing data, sourced from the existing pricing catalog.

- For `/v1/models`: each virtual model's response object gains an optional `pricing` field containing `effective_input_cost_per_1m` and `effective_output_cost_per_1m`. These are computed from the lowest-cost enabled route's pricing catalog entry (consistent with `lowest_cost` strategy logic — the cheapest available price).
- For `/v1/catalog/virtual-models`: each virtual model JSON gains the same `pricing` field, allowing the admin catalog UI to render it without a separate API call.

**Frontend:** Add an "Effective Price" column to the models table and the virtual models table, reusing the `EffectivePriceCell` component pattern already present in the provider catalog section.

### Key Components

- **Backend `modelObject`**: Add `pricing *modelObjectPricing` field with `effective_input_cost_per_1m` and `effective_output_cost_per_1m`.
- **Backend `virtualModelJSON`**: Add `pricing *virtualModelPricingJSON` field sourced from route cost data joined with the pricing catalog.
- **Frontend `ModelObject` type** (`shared/api.ts`): Add optional `pricing` field.
- **Frontend `ModelRow` type**: Add optional `pricing` field; `toModelRow` propagates it.
- **Frontend models-tab.tsx**: Add sortable "Effective Price" column.
- **Frontend virtual-models-section.tsx**: Add sortable "Effective Price" column.

---

## Scope

### In Scope

- Backend: populate pricing on `/v1/models` response.
- Backend: populate pricing on `/v1/catalog/virtual-models` response.
- Frontend: "Effective Price" column on models page.
- Frontend: "Effective Price" column on virtual models table.
- Pricing for virtual models uses the cheapest enabled route (min of input+output across routes that have pricing data).

### Out of Scope

- Pricing on model detail modal / editor panel.
- Per-tenant pricing or overrides.
- Historical or blended cost metrics.
- Changes to the provider catalog table (already correct).

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `GET /v1/models` response includes `pricing.effective_input_cost_per_1m` and `pricing.effective_output_cost_per_1m` for each virtual model with at least one costed route.
- [ ] `GET /v1/catalog/virtual-models` response includes the same pricing fields.
- [ ] Models page table shows "Effective Price" column (input/output, sortable).
- [ ] Virtual models table shows "Effective Price" column (input/output, sortable).
- [ ] Unknown/missing pricing shows an appropriate indicator (not $0.00).

**Should Have (P1):**
- [ ] Column is sortable by input cost.
- [ ] Column renders consistently with the existing provider catalog's `EffectivePriceCell` style.

### Non-Functional Requirements

**Performance:**
- Backend pricing lookup must not introduce a synchronous DB read per model; use the already-loaded pricing cache or a single bulk load.

**Security:**
- No new policy scope changes required; pricing data is non-sensitive metadata.

---

## Dependencies

### Internal Dependencies

- **Pricing catalog** (`PricingCatalogRepository.LoadAll`): already implemented, used by the provider catalog handler.
- **`fmtCost` utility** (`@orbit/utils`): already exported, used by provider catalog section.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Pricing cache cold on startup | Low (shows "Unknown") | Low | Existing graceful fallback pattern used in provider catalog |
| Route has cost data in DB but not in pricing catalog | Unknown column shows | Medium | Document that pricing catalog is canonical; route-level costs are routing hints only |

---

## Timeline and Milestones

- [x] PRD Created: 2026-06-22
- [ ] ADR Created: 2026-06-22
- [ ] Implementation: 2026-06-22
- [ ] PR Opened: 2026-06-22
