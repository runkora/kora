# Model Capability Taxonomy

**Status:** Accepted
**Date:** 2026-04-01
**Deciders:** Orbit platform team
**Technical Story:** `GET /v1/models` currently returns embedding models, image-generation models, and video-generation models alongside chat models. Clients (e.g. Claude Code) see these IDs and attempt to send chat completion requests to them, receiving confusing provider errors. The `dialects` field on `ProviderModel` was being used as an implicit proxy for "the gateway has a handler for this model", but that is an accidental coupling that breaks down as soon as a second non-chat capability (e.g. embeddings) is added.

---

## Context

### Background

The Inference Gateway `GET /v1/models` aggregates models from all enabled provider connectors. The `ProviderModel` type, introduced in [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md), carries:

- `InputModalities []Modality` — physical input types the model accepts (text, image, audio, …)
- `OutputModalities []Modality` — physical output types the model produces (text, image, embedding, video, …)
- `Dialects []APIDialect` — which chat wire formats the model supports (`openai`, `anthropic`)

The `Dialects` field was designed for the chat endpoint family only. Non-chat models (embeddings, image generation, video generation) have no meaningful dialect assignment and are given an empty `Dialects` slice. After the modality implementation landed, the handler began relying on `len(Dialects) > 0` as the filter condition for "should this model appear in the list". This creates an accidental coupling: the filter condition is correct today, but it breaks down the moment a second non-chat capability (e.g. `POST /v1/embeddings`) is implemented, because embedding models would still have no dialects and would never surface even though the gateway now handles them.

A separate concern surfaced at the same time: embedding, image generation, and video generation are not different wire formats for the same capability — they are entirely different endpoint families with different request schemas, response schemas, and semantics:

| Family | Endpoint | Request schema | Response schema |
|---|---|---|---|
| Chat | `POST /v1/chat/completions` or `POST /v1/messages` | `messages[]` conversation | `choices[]` message |
| Embedding | `POST /v1/embeddings` | `input: string[]` | `data[].embedding: float[]` |
| Image generation | `POST /v1/images/generations` | `prompt: string` | `data[].url` or `data[].b64_json` |
| Video generation | Provider-specific async/poll | N/A (no standard yet) | N/A |
| Rerank | `POST /v1/rerank` (Cohere-style) | `query + documents[]` | `results[].index + score` |

Treating these as dialects would mean the `Dialects` field would need to carry values like `"embeddings"` or `"image"` — values that have nothing in common with `"openai"` and `"anthropic"` other than the field name. That conflation makes the type misleading and makes handler dispatch logic impossible to reason about.

### Problem Statement

1. **Unsupported models appear in `GET /v1/models`.** Embedding, image-generation, and video-generation models are returned, but the gateway has no handler for them. Clients that attempt to use them receive confusing provider errors rather than a clean `400 model_not_supported`.

2. **`Dialects` is doing double duty.** It carries both the wire-format selection signal (for chat) and an implicit capability presence signal (for everything). These are different concerns and should be separated.

3. **No clean extension path for future capabilities.** When `POST /v1/embeddings` is implemented, there is no field on `ProviderModel` to express that a given model supports it. Adding `"embeddings"` to `Dialects` would be semantically wrong. The data model needs a first-class capability field.

### Goals

- Define a `ModelCapability` type that expresses what endpoint family a model belongs to, orthogonal to `Modality` and `APIDialect`.
- Filter `GET /v1/models` to only return models for which the gateway has an active handler, keyed on `ModelCapability`.
- Establish a clean extension point so that when `POST /v1/embeddings`, `POST /v1/images/generations`, or other endpoint families are implemented, the relevant models automatically surface without handler-layer changes.
- Keep `Dialects []APIDialect` scoped exclusively to the chat capability, where it has clear meaning.

### Non-Goals

- Implementing `POST /v1/embeddings`, `POST /v1/images/generations`, or any other non-chat endpoint — this ADR only defines the data model and filter rule.
- Operator-managed capability overrides (e.g. "hide embedding models from this tenant") — deferred.
- Cross-capability failover (retrying a chat request as an embedding request) — not meaningful.
- Changing the Anthropic zero-translation path ([anthropic-zero-translation-path](2026-04-01-anthropic-zero-translation-path.md)) or the alias routing ([provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md)).

---

## Decision Drivers

* **Correctness for chat clients.** Claude Code, OpenAI-compatible clients, and similar tools should only see models they can actually call via chat endpoints.
* **Clean type semantics.** `Capability` answers "what endpoint family?", `Dialect` answers "which wire format within that family?", `Modality` answers "what physical I/O types?". These three questions are orthogonal and should not be conflated.
* **Additive extensibility.** Adding `POST /v1/embeddings` in the future should require no changes to the `GET /v1/models` filter logic — only registering the new capability in the handler's supported-capabilities set.
* **No breaking changes.** Clients already using the `GET /v1/models` response should see fewer (not different) models — only the currently-unsupported non-chat models are removed.

---

## Considered Options

### Option 1: Filter on `len(Dialects) > 0` (status quo, no new type)

Keep the current accidental filter: models with no dialects are hidden, models with at least one dialect are shown.

**Pros:**
- Zero code change required.
- Works correctly today.

**Cons:**
- Breaks the moment embedding support is added — embedding models have no dialects but the gateway would handle them.
- `Dialects` semantics become muddied the moment anyone tries to extend the filter.
- No explicit data model for capability — future developers must infer intent from the filter code.

### Option 2: Add `Capabilities []ModelCapability` to `ProviderModel`; filter on `chat` membership

Introduce a `ModelCapability` string type with constants (`CapabilityChat`, `CapabilityEmbedding`, `CapabilityImageGeneration`, `CapabilityVideoGeneration`, `CapabilityRerank`). Each provider's `ListModels` implementation populates `Capabilities` based on modality inference or live API metadata. The `GET /v1/models` handler maintains a set of capabilities for which a handler exists and filters accordingly.

**Pros:**
- Explicit, self-documenting data model.
- `Dialects` retains its clean "wire format selector for chat" semantics.
- Filter is driven by a set of active handler capabilities — adding `POST /v1/embeddings` means adding `CapabilityEmbedding` to the supported set; embedding models immediately surface with no other change.
- `GET /v1/models?capability=chat` query-parameter filtering is a natural future extension.

**Cons:**
- One additional field on `ProviderModel` and in the `GET /v1/models` JSON response.
- Provider connectors must populate the new field; pattern-inference logic needs updating.

### Option 3: Derive capability entirely from modalities at query time (no stored field)

Instead of storing `Capabilities` on the model, derive it on-the-fly in the handler: models with `output_modalities == ["text"]` are chat, `output_modalities == ["embedding"]` are embedding, etc.

**Pros:**
- No new stored field.
- Single source of truth: `OutputModalities`.

**Cons:**
- Fragile — multimodal models (text+image output) could be either chat or image-gen; the output modality alone is ambiguous.
- `output_modalities: ["text"]` is shared by chat models and text-completion models — not sufficient to determine endpoint family.
- Ties handler filter logic to the modality inference logic; any change to modality values has unintended capability side-effects.

---

## Decision

**Chosen Option:** Option 2 — `Capabilities []ModelCapability` on `ProviderModel`, filter `GET /v1/models` on active handler capabilities.

### Three-Layer Taxonomy

```
ModelCapability  — what endpoint family the model belongs to
     ↓
APIDialect       — which wire format to use within that family (chat only)
     ↓
Modality         — physical I/O types (text, image, embedding, audio, video)
```

**`ModelCapability` constants:**

| Constant | Value | Endpoint |
|---|---|---|
| `CapabilityChat` | `"chat"` | `POST /v1/chat/completions` or `POST /v1/messages` |
| `CapabilityEmbedding` | `"embedding"` | `POST /v1/embeddings` (not yet implemented) |
| `CapabilityImageGeneration` | `"image"` | `POST /v1/images/generations` (not yet implemented) |
| `CapabilityVideoGeneration` | `"video"` | Provider-specific (not yet implemented) |
| `CapabilityRerank` | `"rerank"` | `POST /v1/rerank` (not yet implemented) |

**`ProviderModel` struct (updated):**

```go
type ProviderModel struct {
    Provider        ProviderName
    ModelID         string
    DisplayName     string
    Aliases         []string
    InputModalities  []Modality
    OutputModalities []Modality
    Capabilities    []ModelCapability  // NEW — replaces implicit dialects filter
    Dialects        []APIDialect       // scoped to CapabilityChat models only
}
```

**`GET /v1/models` filter rule:**

The handler maintains a package-level set of currently-active capabilities:

```go
var activeCapabilities = map[ModelCapability]bool{
    CapabilityChat: true,
    // CapabilityEmbedding: true  ← uncomment when POST /v1/embeddings ships
    // CapabilityImageGeneration: true  ← uncomment when POST /v1/images/generations ships
}
```

A model is included in the response if and only if any of its `Capabilities` appear in `activeCapabilities`. Adding a new endpoint family requires: (1) implementing the handler, (2) adding the capability to `activeCapabilities`.

**Capability derivation per provider:**

| Provider | Source |
|---|---|
| Bedrock | `OutputModalities` from `FoundationModelSummary` (live API). `EMBEDDING` output → `CapabilityEmbedding`; `IMAGE` output → `CapabilityImageGeneration`; `VIDEO` output → `CapabilityVideoGeneration`; `TEXT` output → `CapabilityChat`. |
| Vertex AI | Pattern inference on model ID. `embed` → `CapabilityEmbedding`; `imagen`/`imagegeneration` → `CapabilityImageGeneration`; all others → `CapabilityChat`. |
| Azure | Pattern inference on model ID. `text-embedding-*` → `CapabilityEmbedding`; `dall-e`/`dalle` → `CapabilityImageGeneration`; all others → `CapabilityChat`. |

### Rationale

The three-way separation (capability / dialect / modality) keeps each field answering exactly one question. A developer reading `ProviderModel` immediately understands:
- *Can the gateway handle this model?* → check `Capabilities` against `activeCapabilities`
- *Which endpoint to call?* → use `Capabilities` to select the handler group
- *Which wire format within that endpoint?* → use `Dialects` (only relevant for chat)
- *What data types flow in and out?* → use `InputModalities` / `OutputModalities`

The `activeCapabilities` map acts as the authoritative registry of what the gateway can do. It is the single place to update when a new endpoint family ships. No handler logic, no modality inference, no dialect logic needs to change.

---

## Consequences

### Positive

- `GET /v1/models` immediately stops returning embedding, image-generation, and video-generation models that the gateway cannot handle, eliminating client confusion.
- Adding `POST /v1/embeddings` in future requires only: implement the handler, add `CapabilityEmbedding` to `activeCapabilities`, and populate `Capabilities` in provider `ListModels`. No filter logic changes.
- `Dialects []APIDialect` retains clean semantics — it is always scoped to the chat family and always means "which wire format?".
- The `ModelCapability` field is surfaced in `GET /v1/models` JSON, enabling clients to programmatically discover what they can do with a model.

### Negative

- Provider `ListModels` implementations must be updated to populate `Capabilities`. This is a small incremental change since capability can be derived from the already-computed `OutputModalities` in all three connectors.
- One additional field in the `GET /v1/models` response. Existing clients that only read `id`, `dialects`, and modalities are unaffected.

### Neutral

- The `Modality` type and fields are unchanged.
- `APIDialect` type and the `Dialects` field are unchanged in semantics; they become a narrower concept (chat-only) rather than the implicit catch-all they were acting as.
- The `activeCapabilities` map starts with only `CapabilityChat: true` — functionally identical to the `len(Dialects) > 0` filter for the current model set, but explicit and extensible.

---

## Implementation

### Phase 1 — Data model and filter (current)

1. Add `ModelCapability` type and constants to `backend/internal/inferencegw/connector.go`.
2. Add `Capabilities []ModelCapability` field to `ProviderModel`.
3. Populate `Capabilities` in each provider's `ListModels` — derive from `OutputModalities` (already computed) for all three connectors.
4. Add `activeCapabilities` map to `handlers_openai.go`; replace the implicit `len(dialects) > 0` filter with `capabilities ∩ activeCapabilities ≠ ∅`.
5. Surface `capabilities` field in the `modelObject` JSON response.

### Phase 2 — Embedding endpoint (future)

1. Implement `POST /v1/embeddings` handler.
2. Add embedding connector interface method (`CreateEmbedding`) to each provider.
3. Add `CapabilityEmbedding: true` to `activeCapabilities`.
4. Embedding models surface in `GET /v1/models` automatically.

### Phase 3 — Image generation endpoint (future)

1. Implement `POST /v1/images/generations` handler.
2. Add image-gen connector interface method to each provider.
3. Add `CapabilityImageGeneration: true` to `activeCapabilities`.

### Migration Path

The change is additive and non-breaking:
- Models that were previously visible (chat models with `len(Dialects) > 0`) remain visible.
- Models that were previously visible but unsupported (embedding, image-gen, video-gen with `len(Dialects) == 0`) are removed from the response. This is a correct behaviour change, not a breaking API change, since those models could never be successfully called.
- No existing client that uses a model ID from `GET /v1/models` is affected — the IDs, aliases, display names, modalities, and dialects of chat models are unchanged.

---

## Validation

### Success Metrics

- `GET /v1/models` returns only models with `CapabilityChat` in their `Capabilities` slice.
- Embedding and image-generation model IDs (e.g. `bedrock/amazon.titan-embed-text-v2:0`, `bedrock/amazon.nova-canvas-v1:0`) do not appear in the response until the corresponding endpoint is implemented.
- When `POST /v1/embeddings` is implemented, adding `CapabilityEmbedding: true` to `activeCapabilities` causes embedding models to appear in `GET /v1/models` with no other code change.
- The `capabilities` field in `GET /v1/models` response is non-empty for all returned models.

### Monitoring

- Log a warning at startup if `activeCapabilities` is empty (would hide all models).
- Track model counts per capability in the `GET /v1/models` response metrics.

---

## Related Decisions

- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference Gateway architecture; defines the provider connector interface and handler structure.
- [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) — Provider Model Catalog; introduced `ProviderModel`, `InputModalities`, `OutputModalities`, `Dialects`.
- [anthropic-zero-translation-path](2026-04-01-anthropic-zero-translation-path.md) — Anthropic zero-translation path; defines the two chat dialect wire formats.

---

## References

- [OpenAI API Reference — Embeddings](https://platform.openai.com/docs/api-reference/embeddings)
- [OpenAI API Reference — Images](https://platform.openai.com/docs/api-reference/images)
- [AWS Bedrock Foundation Models — Modalities](https://docs.aws.amazon.com/bedrock/latest/userguide/foundation-models.html)

---

## Notes

The `activeCapabilities` map is intentionally a compile-time constant rather than a configuration value. Capability support is a code-level commitment — it requires a handler, connector interface methods, and integration tests to be correct. Making it runtime-configurable would allow operators to advertise capabilities the gateway cannot actually fulfil, which is worse than not advertising them at all.

**Last Updated:** 2025-07-24
