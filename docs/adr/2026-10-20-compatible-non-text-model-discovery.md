# Non-Text Model Discovery on Compatible Providers

**Status:** Accepted
**Date:** 2026-10-20
**Deciders:** justinwilkin
**Technical Story:** [#480](https://github.com/runkora/kora/issues/480)

---

## Context

A `compatible` InferenceProvider declaring `embeddings` lists models with `GET <baseURL>/models`. OpenRouter limits that list to text-output models, so its embedding and rerank models never reach the catalog. Capability comes from an `embed` check on the model id, which misclassifies embedding models with other names.

This touches **How does it connect?**: it changes which upstream models a provider exposes. Credentials and policy checks are unchanged.

### Goals

- List embedding and rerank models of a provider that declares `embeddings`.
- Classify models from `architecture.output_modalities` when the endpoint publishes it.
- Leave endpoints that ignore the query unchanged.

### Non-Goals

- Serving image, speech, video or transcription.
- The rerank route.

---

## Considered Options

### Option 1: One extra `?output_modalities=all` request

**Description:** After the default listing, request `?output_modalities=all`. Upsert entries whose metadata declares `embeddings` or `rerank`. Skip entries without metadata.

**Pros:**
- One extra request per refresh.
- Same helper as decision-model discovery.
- An endpoint that ignores the query adds nothing.

**Cons:**
- Relies on OpenRouter's `all` value.

### Option 2: One request per declared modality

**Description:** Request `?output_modalities=embeddings` and `?output_modalities=rerank` separately.

**Pros:**
- Smaller responses.

**Cons:**
- More requests, and a new request for each modality added later.

### Option 3: Per-provider model list override

**Description:** Operators name embedding models in the CRD, like `decisionModels`.

**Pros:**
- Works without endpoint metadata.

**Cons:**
- Manual upkeep, and a CRD change.

---

## Decision

**Chosen Option:** Option 1 - one `?output_modalities=all` request.

### Rationale

It finds every model with the fewest requests and needs no CRD change. A failed request keeps the default listing. Providers with no metadata keep the `embed` name check. A per-provider override stays open as a later addition.

---

## Consequences

### Positive

- OpenRouter embedding models appear with capability `embedding` and their listed price.
- Rerank models are classified as `rerank`.

### Negative

- Every refresh of an embeddings provider makes one more request.
- Rerank models are listed before any route can serve them.

### Neutral

- Image, speech, video and transcription models are not added.

---

## Related Decisions

- [2026-10-01-crd-inference-providers.md](2026-10-01-crd-inference-providers.md) - Compatible providers
- [2026-10-10-systemone-decision-models.md](2026-10-10-systemone-decision-models.md) - Decision model discovery this extends

**Last Updated:** 2026-10-21
