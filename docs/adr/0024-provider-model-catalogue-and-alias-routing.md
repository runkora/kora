# 0024. Provider Model Catalogue and Alias Routing

**Status:** Accepted
**Date:** 2025-07-17
**Deciders:** Orbit platform team
**Technical Story:** Bedrock `ListModels` returns ~400+ raw AWS model IDs — many are inactive, duplicated under two IDs, or carry internal region-routing prefixes (e.g. `us.`, `eu.`) that clients must not need to understand. This ADR defines a richer `ProviderModel` type, per-provider filtering rules, and alias resolution in the routing engine.

---

## Context

### Background

The Inference Gateway's `GET /v1/models` endpoint aggregates model lists from all enabled provider connectors via `ProviderConnector.ListModels`, which currently returns `[]string` — bare model IDs. The routing engine stores these verbatim; the HTTP handler renders them as `<provider>/<model-id>`.

AWS Bedrock exposes two separate APIs for model discovery:

- **`ListFoundationModels`** — returns every model AWS has ever onboarded, including `LEGACY` status models and models that do not support on-demand (pay-per-token) invocation. Calling one of these returns a 400 or 403.
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
- Provides display names for well-known models; gracefully auto-generates for others.
- Operator aliasing can be layered on top later with no interface change.
- Zero breaking change: exact canonical IDs still route correctly.

**Cons:**
- Interface change (`[]string` → `[]ProviderModel`) requires updating all three connectors and the router.
- Alias table is rebuilt on every `ListModels` call (acceptable for MVP; caching is a follow-on).
- Static display name map in the Bedrock connector will need maintenance as new models launch.

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
- The Bedrock display name map is hand-maintained; it will lag behind new model launches until a follow-on task is done to auto-sync it.
- Alias table is rebuilt from a live API call on every `ListModels` invocation — no in-process cache. Acceptable for MVP but should be addressed in a follow-on.

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

**`providers/bedrock/models.go`** — new file containing:
- `knownDisplayNames map[string]string` — static map of base model ID fragment → display name for well-known Anthropic, Meta, Mistral, Amazon Nova, and Cohere models.
- `displayName(modelID string) string` — returns the curated name or auto-generates one by title-casing the ID slug.
- `aliases(canonicalID string) []string` — strips the region prefix (`us.`, `eu.`, `ap.`) and the provider namespace (`anthropic.`, `meta.`, `mistral.`, etc.) to produce one or more clean aliases.

**`providers/bedrock/connector.go` `ListModels`** — replace current implementation with:

```
1. Call ListFoundationModels with no filters (AWS SDK does not support combined filters).
2. Filter results:
   - ModelLifecycleStatus == ACTIVE
   - InferenceTypesSupported contains ON_DEMAND
3. Build a set of active FM IDs.
4. Call ListInferenceProfiles (paginated, SystemDefined type only).
5. Build a map: base model ID → profile ID
   (e.g. "anthropic.claude-sonnet-4-6" → "us.anthropic.claude-sonnet-4-6")
6. For each active FM:
   - If a profile exists for it, use the profile ID as ModelID.
   - Otherwise use the bare FM ID.
   - Each model appears exactly once.
7. Populate DisplayName and Aliases for each result.
```

### 3. Azure and Vertex AI connectors

Update `ListModels` return type to `[]ProviderModel`. Wrap each string ID from the existing iterator into a `ProviderModel` with `DisplayName: displayNameFromID(id)` and empty `Aliases`. No filtering logic change.

### 4. Routing engine alias resolution (`internal/inferencegw/router.go`)

Add a private helper `resolveAlias(provider ProviderName, alias string) (string, bool)` that is populated from the most-recent `ListModels` result. In `RouteRequest`, before constructing a `RouteTarget`, pass the requested model ID through alias resolution.

To avoid coupling the router to the live API on every request, the alias map is rebuilt during `ListModels` calls and stored on the `RoutingEngine`. The map is keyed by `"<provider>/<alias>"` → canonical model ID.

```
RouteRequest("bedrock/claude-sonnet-4-6")
  → resolveAlias("bedrock", "claude-sonnet-4-6")
  → "us.anthropic.claude-sonnet-4-6"
  → RouteTarget{Provider: bedrock, ModelID: "us.anthropic.claude-sonnet-4-6"}
```

### 5. HTTP handler (`handlers_openai.go`)

Extend `modelObject` with `DisplayName` and `Aliases` fields:

```go
type modelObject struct {
    ID          string   `json:"id"`           // "bedrock/us.anthropic.claude-sonnet-4-6"
    Object      string   `json:"object"`       // "model"
    Created     int64    `json:"created"`
    OwnedBy     string   `json:"owned_by"`     // "bedrock"
    DisplayName string   `json:"display_name"` // "Claude Sonnet 4.6"
    Aliases     []string `json:"aliases"`      // ["bedrock/claude-sonnet-4-6"]
}
```

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
- All known Anthropic models (Claude Haiku 4.5, Sonnet 4.6, Opus 4.5) have non-empty curated `display_name` values.
- `make check` passes with zero new lint issues.

### Monitoring

- Routing failures with `error_code: ROUTING_FAILED` should decrease after deployment as broken IDs are removed from the catalogue.
- Track `inference_usage` records where `model_id` contains a `us.` / `eu.` / `ap.` prefix to confirm alias usage is working in practice (the stored ID should be canonical, not the alias).

---

## Related Decisions

- [ADR-0023](0023-inference-gateway-architecture.md) — Inference Gateway architecture; this ADR refines the `ProviderConnector` interface defined there.

---

## References

- [AWS Bedrock ListFoundationModels API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListFoundationModels.html)
- [AWS Bedrock ListInferenceProfiles API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListInferenceProfiles.html)
- [AWS Bedrock Cross-Region Inference Profiles](https://docs.aws.amazon.com/bedrock/latest/userguide/inference-profiles.html)

---

## Notes

The operator-level alias curation feature (allowing tenants to define `"my-haiku"` → `bedrock/claude-haiku-4-5`) is the natural next step once this foundation is in place. The `Aliases []string` field on `ProviderModel` and the alias lookup table in `RoutingEngine` are intentionally designed to accept operator-supplied entries alongside the auto-generated ones. That feature will be covered in a separate ADR.

**Last Updated:** 2025-07-17
