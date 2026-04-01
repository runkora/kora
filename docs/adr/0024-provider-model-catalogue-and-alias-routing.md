# 0024. Provider Model Catalogue and Alias Routing

**Status:** Accepted (Amended 2025-07-22)
**Date:** 2025-07-17
**Deciders:** Orbit platform team
**Technical Story:** Bedrock `ListModels` returns ~400+ raw AWS model IDs — many are inactive, duplicated under two IDs, or carry internal region-routing prefixes (e.g. `us.`, `eu.`) that clients must not need to understand. This ADR defines a richer `ProviderModel` type, per-provider filtering rules, and alias resolution in the routing engine.

---

## Context

### Background

The Inference Gateway's `GET /v1/models` endpoint aggregates model lists from all enabled provider connectors via `ProviderConnector.ListModels`, which currently returns `[]string` — bare model IDs. The routing engine stores these verbatim; the HTTP handler renders them as `<provider>/<model-id>`.

AWS Bedrock exposes two separate APIs for model discovery:

- **`ListFoundationModels`** — returns every model AWS has ever onboarded, including `LEGACY` status models and models that do not support on-demand (pay-per-token) invocation. Calling one of these returns a 400 or 403. Note: the `ModelLifecycleStatus` field is **not populated in all regions** — in `ap-southeast-2` it returns an empty string, so a strict `ACTIVE` filter would silently drop all models in that region.
- **`ListInferenceProfiles`** — returns system-defined cross-region routing profiles with region-prefixed IDs (e.g. `us.anthropic.claude-sonnet-4-6`). Newer Anthropic and some other models require a profile ID, not the bare foundation model ID, to receive on-demand throughput.

The two APIs overlap: the same underlying model (e.g. Claude Sonnet 4.6) appears as both `anthropic.claude-sonnet-4-6` (foundation model, on-demand not supported directly) and `us.anthropic.claude-sonnet-4-6` (inference profile, works). Both IDs are currently returned — one is broken, one works, and clients cannot tell them apart.

### Problem Statement

1. **Unusable models are listed.** `ListFoundationModels` returns `LEGACY` models and models with no `ON_DEMAND` throughput. Calling them fails.
2. **Duplicate IDs for the same model.** A model that requires an inference profile appears twice: once as a bare FM ID (broken) and once as a profile ID (working).
3. **Implementation detail leaks to clients.** The `us.` / `eu.` / `ap.` prefix is an AWS internal region-routing concept. Clients should not need to know or remember it.
4. **No display names.** Every model ID is an opaque slug. The `/v1/models` response has no `display_name` field.
5. **No alias routing.** A client that requests `bedrock/claude-sonnet-4-6` (the clean alias) gets a routing failure because the router does an exact-match lookup and cannot resolve it to `us.anthropic.claude-sonnet-4-6`.

### Goals

- Return only models that can actually be called via on-demand (pay-per-token) invocation.
- Each model appears exactly once in the listing with the canonical working ID.
- Provide a human-readable `display_name` for all known models; auto-generate one for unknown models.
- Expose stable, clean `aliases` (e.g. `bedrock/claude-sonnet-4-6`) that clients can use in requests.
- The routing engine resolves aliases to canonical IDs before dispatching to a connector.
- Establish the extension point through which operators can add custom aliases in a future ADR.

### Non-Goals

- Operator-managed alias curation (deferred — this ADR establishes the data model and resolution path).
- Filtering models by capability beyond on-demand throughput support (e.g. vision, function calling).
- Caching the model catalogue between requests (handled separately by a future cache layer).
- Changing the Vertex AI or Azure `ListModels` implementations beyond the interface signature update.

---

## Decision Drivers

* **Correctness first** — clients must not receive IDs that return errors when used.
* **Clean API surface** — the `us.` prefix is an AWS internal detail; the public API should hide it.
* **Zero breaking change to the request wire format** — existing clients using full canonical IDs (e.g. `bedrock/us.anthropic.claude-sonnet-4-6`) must continue to work unchanged.
* **Extensibility** — the `Aliases` field on `ProviderModel` is the hook for future operator-managed aliasing without any further interface changes.
* **Minimal blast radius** — only the Bedrock connector, the shared `ProviderModel` type, and the routing engine change. Service layer, handlers, and other connectors are barely touched.

---

## Considered Options

### Option 1: Static allowlist

Hardcode the list of Bedrock model IDs that are known to work. No API calls — just return the fixed list.

**Pros:**
- No dependency on AWS discovery APIs at list-time.
- Completely predictable output.

**Cons:**
- List goes stale immediately; any new model requires a code deploy.
- Operators deploying Bedrock in private regions or with provisioned throughput see nothing useful.
- Does not scale to Vertex AI or Azure where the model set varies by deployment.

### Option 2: Dynamic filter only (no aliases)

Keep `ListModels` returning `[]string`. Apply filtering in the Bedrock connector (ACTIVE + ON_DEMAND + deduplicate with inference profile). Do not add display names or aliases.

**Pros:**
- Minimal interface change — `[]string` stays.
- Solves the correctness problem (broken IDs removed).

**Cons:**
- Does not solve the `us.` prefix leaking to clients.
- No display names — UX remains poor.
- Alias routing not possible — clients must know and use exact AWS IDs.
- Extension point for operator curation has nowhere to attach.

### Option 3: Enriched `ProviderModel` + filtering + alias routing (chosen)

Change `ProviderConnector.ListModels` to return `[]ProviderModel` (was `[]string`). Enrich `ProviderModel` with `DisplayName string` and `Aliases []string`. Implement Bedrock-specific filtering and deduplication. Build an alias lookup table in the routing engine that resolves clean aliases to canonical IDs before dispatch.

**Pros:**
- Solves correctness: only on-demand-capable, active models are returned.
- Solves duplication: one entry per model using the canonical working ID.
- Hides AWS implementation detail: `us.` prefix stays as the canonical ID internally but clean aliases are advertised and routable.
- Provides display names sourced directly from the AWS API; gracefully auto-generates for models where the API returns empty.
- Operator aliasing can be layered on top later with no interface change.
- Zero breaking change: exact canonical IDs still route correctly.

**Cons:**
- Interface change (`[]string` → `[]ProviderModel`) requires updating all three connectors and the router.
- Alias table is rebuilt on every `ListModels` call (acceptable for MVP; caching is a follow-on).

---

## Decision

**Chosen Option:** Option 3 — Enriched `ProviderModel` + filtering + alias routing.

### Rationale

Option 1 is operationally untenable — the AWS model catalogue changes frequently enough that a static list would become a maintenance burden within weeks. Option 2 solves correctness but leaves the UX problems (opaque IDs, no routing from clean aliases) unaddressed. Option 3 has the highest upfront change surface but produces the correct end-state: clients see clean IDs, can use friendly aliases, and receive display names. The interface change to `[]ProviderModel` is contained to four call sites (three connectors + the router) and the extension point it creates (the `Aliases` field) eliminates the need for a further interface revision when operator curation is added.

---

## Consequences

### Positive

- `GET /v1/models` returns only models that clients can actually use.
- Each model appears once with a predictable, clean ID.
- Clients can address models with human-friendly aliases (`bedrock/claude-sonnet-4-6`) instead of `bedrock/us.anthropic.claude-sonnet-4-6`.
- The `display_name` field in the API response makes UIs significantly more usable without additional mapping on the client side.
- Operator-level alias curation has a clear, already-wired extension point.

### Negative

- All three connector `ListModels` implementations must be updated to return `[]ProviderModel` (trivial for Azure/Vertex, substantive for Bedrock).
- Display names for Azure and Vertex AI are auto-generated from model ID slugs; curated names for those providers are a follow-on.
- Alias table is rebuilt from a live API call on every `ListModels` invocation — no in-process cache. Acceptable for MVP but should be addressed in a follow-on.
- `estimated_cost_usd` in usage records is `$0.00` until a live pricing source is integrated (static pricing data has been removed; see §7 below).

### Neutral

- The canonical Bedrock model ID in usage records and pricing lookups remains the full profile ID (e.g. `us.anthropic.claude-sonnet-4-6`). Aliases are resolved at routing time and never stored.
- Vertex AI and Azure connectors return `[]ProviderModel` with no aliases initially — their display names are auto-generated from the model ID. Curated names for those providers are a follow-on.

---

## Implementation

### 1. Enrich `ProviderModel` (`internal/inferencegw/connector.go`)

```go
// ProviderModel is a model returned by a live provider listing.
type ProviderModel struct {
    Provider    ProviderName
    ModelID     string   // canonical working ID, no provider/ prefix
    DisplayName string   // human-readable name, e.g. "Claude Sonnet 4.6"
    Aliases     []string // clean aliases, no provider/ prefix, e.g. ["claude-sonnet-4-6"]
}
```

Update `ProviderConnector.ListModels` signature:

```go
ListModels(ctx context.Context) ([]ProviderModel, error)
```

### 2. Bedrock connector (`providers/bedrock/`)

**`providers/bedrock/models.go`** — contains:
- `knownRegionPrefixes []string` — the set of cross-region inference profile prefixes sourced from the live API (`apac.`, `au.`, `global.`, `us.`, `eu.`, `ap.`).
- `knownNamespacePrefixes []string` — provider namespace segments embedded in Bedrock model IDs (`anthropic.`, `amazon.`, `meta.`, `mistral.`, etc.).
- `stripRegionPrefix(id string) string` — removes a known region prefix to recover the base FM ID.
- `aliases(canonicalID string) []string` — generates clean stable aliases by stripping region prefix, provider namespace, and trailing `:N` version suffix in sequence.
- `fallbackDisplayName(modelID string) string` — derives a human-readable name from the model ID slug. Used **only** when the API returns an empty `ModelName`.
- `buildProviderModel(canonicalID, name string) ProviderModel` — constructs the full `ProviderModel` from the canonical ID and the API-sourced display name.

> **Implementation note:** the `knownDisplayNames` static map originally proposed in this ADR was **not implemented**. `ListFoundationModels` returns `ModelName` directly (e.g. `"Claude 3 Sonnet"`) and `ListInferenceProfiles` returns `InferenceProfileName`. Live API names are the primary source; `fallbackDisplayName` is the fallback only for models where the API returns an empty string.

**`providers/bedrock/connector.go` `ListModels`** — replace current implementation with:

```
1. Call ListFoundationModels with no filters (AWS SDK does not support combined filters).
2. Filter results — keep only models where:
   - InferenceTypesSupported contains ON_DEMAND.
   - ModelLifecycle.Status != LEGACY (nil is treated as active; the field is unpopulated
     in some regions such as ap-southeast-2 so a strict ACTIVE check silently drops
     all models there).
   Track excluded (LEGACY) FM IDs in a separate excludedFMs set.
3. Build activeFMs map (fmID → fmEntry{name}) and excludedFMs set.
4. Call ListInferenceProfiles (paginated, SystemDefined type only).
5. Build profilesByBase map: base FM ID → profileEntry{id, name}
   e.g. "anthropic.claude-sonnet-4-6" → profileEntry{
     id:   "us.anthropic.claude-sonnet-4-6",
     name: "US Anthropic Claude Sonnet 4.6",
   }
   When a base FM has multiple regional profiles, keep only the first returned.
6. For each active FM:
   - If a profile exists for it, use the profile ID as the canonical ModelID.
   - Prefer the FM's ModelName over the profile InferenceProfileName (the FM name
     is cleaner: "Claude 3 Haiku" vs "APAC Anthropic Claude 3 Haiku").
   - Each model appears exactly once.
7. For profiles whose base FM is NOT in activeFMs (profile-only models):
   - Skip if the base FM is in excludedFMs — LEGACY FM suppresses its profiles too.
   - Include the remaining profiles as new entries using InferenceProfileName as display name.
8. DisplayName is sourced directly from ModelName / InferenceProfileName API fields.
   fallbackDisplayName() is called only when the API returns an empty string.
```

The implementation is decomposed into three private helpers to stay within the cyclomatic complexity limit:
- `listOnDemandFMs(ctx) (activeFMs, excludedFMs, err)` — steps 1–3
- `listSystemProfiles(ctx) profilesByBase` — steps 4–5
- `mergeModels(activeFMs, excludedFMs, profilesByBase) []ProviderModel` — steps 6–8

### 3. Azure and Vertex AI connectors

Update `ListModels` return type to `[]ProviderModel`. Wrap each string ID from the existing iterator into a `ProviderModel` with `DisplayName: displayNameFromID(id)` and empty `Aliases`. No filtering logic change.

### 4. Routing engine alias resolution (`internal/inferencegw/router.go`)

Add a private helper `resolveAlias(model string) string` that translates a `"provider/alias"` string to `"provider/canonicalModelID"`. Returns the input unchanged if the alias is not in the table.

The alias map is rebuilt during every successful `ListModels` call and stored on `RoutingEngine` as an `atomic.Pointer[map[string]string]`. Swapping the pointer is lock-free — `RouteRequest` readers are never blocked by a concurrent `ListModels` write.

A second `atomic.Pointer[[]ProviderModel]` (`modelCatalog`) is updated alongside the alias table. The `lowestCostTargets` and `failoverChain` routing strategies drive off this catalog rather than a static pricing list, so they work correctly even with an empty pricing cache.

Map keying: `"<provider>/<alias>"` → `"<provider>/<canonicalModelID>"`.

```
RouteRequest("bedrock/claude-sonnet-4-6")
  → resolveAlias("bedrock/claude-sonnet-4-6")
  → "bedrock/us.anthropic.claude-sonnet-4-6"
  → RouteTarget{Provider: bedrock, ModelID: "us.anthropic.claude-sonnet-4-6"}
```

### 5. HTTP handler (`handlers_openai.go`)

Extend `modelObject` with `DisplayName` and `Aliases` fields. Both use `omitempty` — providers without aliases (Azure, Vertex AI) omit the field rather than emitting `null`:

```go
type modelObject struct {
    ID          string   `json:"id"`                      // "bedrock/us.anthropic.claude-sonnet-4-6"
    Object      string   `json:"object"`                  // "model"
    Created     int64    `json:"created"`
    OwnedBy     string   `json:"owned_by"`                // "bedrock"
    DisplayName string   `json:"display_name,omitempty"`  // "Claude Sonnet 4.6"
    Aliases     []string `json:"aliases,omitempty"`       // ["bedrock/claude-sonnet-4-6"]
}
```

Aliases are emitted as provider-prefixed strings (e.g. `"bedrock/claude-sonnet-4-6"`) so clients can use them verbatim as `model` values in inference requests.

### 6. Startup model warm-up (`cmd/inference-gateway/main.go`)

`router.ListModels(ctx)` is called once at service startup, immediately after `NewRoutingEngine` and before `NewService`. This pre-populates the alias table and model catalog so that:

- Alias routing works from the first inference request, not just after the first `GET /v1/models` call.
- `failoverChain` and `lowestCostTargets` have a populated catalog immediately.

A warm-up failure is logged as a warning but does not prevent startup — the service falls back to an empty alias table until the first successful `ListModels` call.

### 7. Pricing (stub)

The static `DefaultPricing()` function and its hardcoded model entries have been removed. `NewNopPricingCache()` is wired in at startup. All inference usage records will show `estimated_cost_usd = 0.00` until a live pricing source is integrated (a separate ADR will cover this). The `PricingCache` interface and `ModelPricing`/`EstimatedCostUSD` types are retained so the usage-tracking code compiles unchanged.

### 8. Anthropic zero-translation path

A parallel inference path was added alongside the catalogue work: `POST /v1/messages` now decodes directly into `anthropic.MessageNewParams` (native Anthropic SDK type) and encodes the response as `*anthropic.Message`, with no conversion through Orbit domain types. This eliminates all lossy translation on the Anthropic dialect path. See **[ADR-0025](0025-anthropic-zero-translation-path.md)** for the full design and rationale.

### Migration Path

No breaking changes. Clients using the full canonical ID (`bedrock/us.anthropic.claude-sonnet-4-6`) continue to work unchanged. The alias `bedrock/claude-sonnet-4-6` becomes a new, additionally-supported form. The `/v1/models` response gains two new fields (`display_name`, `aliases`) which are additive.

### Timeline

Implemented immediately following ADR acceptance, within the same development cycle as the initial Bedrock connector work.

---

## Validation

### Success Metrics

- `GET /v1/models` returns zero IDs that result in a 400/403 when used as a model in `POST /v1/chat/completions`.
- Each model ID appears at most once in the response per provider.
- `POST /v1/chat/completions` with `"model": "bedrock/claude-sonnet-4-6"` succeeds and routes to `us.anthropic.claude-sonnet-4-6` on Bedrock.
- All known Anthropic models (Claude Haiku 4.5, Sonnet 4.6, Opus 4.5) have non-empty `display_name` values sourced directly from the AWS API.
- `make check` passes with zero new lint issues.

### Monitoring

- Routing failures with `error_code: ROUTING_FAILED` should decrease after deployment as broken IDs are removed from the catalogue.
- Track `inference_usage` records where `model_id` contains a `us.` / `eu.` / `ap.` prefix to confirm alias usage is working in practice (the stored ID should be canonical, not the alias).

---

## Related Decisions

- [ADR-0023](0023-inference-gateway-architecture.md) — Inference Gateway architecture; this ADR refines the `ProviderConnector` interface defined there.
- [ADR-0025](0025-anthropic-zero-translation-path.md) — Anthropic zero-translation path; adds `AnthropicMessages`/`AnthropicMessagesStream` to `AnthropicConnector` and rewrites `handlers_anthropic.go` to pass SDK types end-to-end.

---

## References

- [AWS Bedrock ListFoundationModels API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListFoundationModels.html)
- [AWS Bedrock ListInferenceProfiles API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListInferenceProfiles.html)
- [AWS Bedrock Cross-Region Inference Profiles](https://docs.aws.amazon.com/bedrock/latest/userguide/inference-profiles.html)

---

## Notes

The operator-level alias curation feature (allowing tenants to define `"my-haiku"` → `bedrock/claude-haiku-4-5`) is the natural next step once this foundation is in place. The `Aliases []string` field on `ProviderModel` and the alias lookup table in `RoutingEngine` are intentionally designed to accept operator-supplied entries alongside the auto-generated ones. That feature will be covered in a separate ADR.

**Last Updated:** 2025-07-22
