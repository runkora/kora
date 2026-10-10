# Decision Models and the System One API

**Status:** Proposed  
**Date:** 2026-10-10  
**Deciders:** kora Core Team  
**Technical Story:** [#473](https://github.com/clear-route/orbit/issues/473): add a `decision` capability and `POST /v1/systemone` to the inference gateway. PRD: [systemone-decision-models](../prd/systemone-decision-models.md).

---

## Context

### Background

A decision model takes a `state` and typed questions and returns probabilities, not text. TypeSafe's System One API is the de facto wire format ([reference](https://docs.typesafe.ai/api)). There is no formal specification or standards body.

- **Request:** `POST /v1/systemone` with `state` (a string, JSON object or array), `model`, and `questions`, a map from caller-chosen ids to questions.
- **Question types:** `noul` (yes or no), `choice` (up to 255 options) and `score` (2 to 10 ordered levels).
- **Response:** `model`, `answers` keyed by the question ids, and `usage` with `input_tokens` and `output_tokens`.
- **Errors:** 401, 422, 429 and 529.

Endpoints known to serve the format are hosted (TypeSafe, OpenRouter at `/api/v1/systemone`, Liquid d1, Upstage Solar Decide) and self-hosted (`laya-serve`, `kev.serve`). Implementations differ in their limits even where the format matches: Laya caps choices at 100 options, and some implementations accept images.

The gateway has no place for them today:

| Surface | Today |
|---------|-------|
| `compatible` APIs | `chatCompletions`, `embeddings`, `responses`, `messages` (`internal/platform/inferenceproviders/v1alpha1/types.go`, the `apis` enum in `charts/orbit-crds/templates/crd-inferenceprovider.yaml`) |
| Capabilities | chat, embedding, image, video, rerank, audio (`internal/inferencegw/connector.go`) |
| Routes | `/chat/completions`, `/messages`, `/embeddings`, `/responses`, `/models` (`internal/inferencegw/controller.go`) |

The names are a trap. OpenAI's `/v1/decisions` and OpenRouter's `/api/alpha/decisions` are different APIs with different schemas.

### Problem Statement

Callers that want probabilities for typed questions call a provider directly with a raw key. Those calls bypass policy, usage logs, tenant attribution and budgets. The classifier primitive on the roadmap needs a governed decision call to build on.

### Goals

- A `compatible` provider can declare the System One API.
- A client using the TypeSafe SDK works against kora by changing only the base URL.
- Decision calls use the same identity, scope, policy, budget, retry and usage path as other inference.
- Decision models are a capability in the virtual model catalog.

### Non-Goals

- The classifier primitive: tenant-defined classifiers, thresholds, UI, and attachments to triggers or workflows. A separate PRD follows.
- Translating OpenAI's `/v1/decisions`. Its schema is not confirmed.
- Shipping or hosting a decision model. Self-hosted servers connect as `compatible` providers.
- Egress or tool-policy use of classifiers.

### The three questions

- **How does it connect?** This is the question the change touches. A decision endpoint is one more upstream. The gateway holds its key (a labelled Secret named by the InferenceProvider) and injects it; the caller never sees it and its own token is never forwarded.
- **What can it access?** Touched, not changed. The route is scope-enforced like the other inference routes, the virtual model's `inference:<family>:<name>:use` scope and policy apply per model, and the tenant budget applies per call. No new access mechanism is added.
- **Who is this agent?** Preserved. The call is attributed to the authenticated principal and tenant, including delegation, through the same usage path.

---

## Decision Drivers

* A TypeSafe SDK client must work by changing only the base URL.
* The format is unspecified and implementations differ, so the gateway should interpret as little of the body as it can.
* Reuse the existing scope, policy, budget, retry and usage machinery; add no new state.
* Do not fabricate probabilities: a decision answer must come from a decision model.
* Keep the change small: no new database migration, no new usage dialect.

---

## Considered Options

### Option 1: Pass the System One wire format through, as a new `compatible` API (chosen)

**Description:** Add `systemone` to the `compatible` APIs and `POST /v1/systemone` to the gateway. The gateway reads `model` to route, rewrites it to the target's model id, and forwards the rest of the body untouched. It returns the upstream body untouched.

**Pros:**
- SDK compatibility by construction; new fields and per-implementation limits need no gateway change.
- Follows the embeddings pattern exactly.

**Cons:**
- Request errors come from the upstream, not the gateway.

### Option 2: Define a kora decision schema and translate to each provider

**Description:** Accept a kora-owned request shape and translate to System One, and later to OpenAI's `/v1/decisions`.

**Pros:**
- One stable client contract across providers.

**Cons:**
- Breaks the base-URL-only SDK requirement.
- Translates to a format that has no specification, and to one (OpenAI's) whose schema is not confirmed.

### Option 3: Serve decisions over chat completions

**Description:** Bridge a System One request onto a chat model by prompting for JSON.

**Pros:**
- Every chat provider would serve it.

**Cons:**
- Returns generated text parsed into numbers, not calibrated probabilities. The contract callers rely on would be false.
- Prices the call as generated text, which is not what a decision model does.

### Option 4: Name the API and route "decisions"

**Description:** `apis: [decisions]` and `POST /v1/decisions`.

**Pros:**
- Reads naturally.

**Cons:**
- Collides with OpenAI's `/v1/decisions` and OpenRouter's `/api/alpha/decisions`, which have different schemas.

### Option 5: A second InferenceProvider per mixed endpoint

**Description:** An endpoint that serves chat and decision models gets two InferenceProviders on the same `baseURL`: one for chat and one with `apis: [systemone]`.

**Pros:**
- No discovery change: a `systemone`-only provider already describes every model as `decision`.

**Cons:**
- Breaks the one-registration-per-multi-API-endpoint goal of [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): two resources, two Secrets or Secret references, and two pricing namespaces for one endpoint.
- Unnecessary where the endpoint publishes the metadata. OpenRouter does, so discovery can tell the models apart.

---

## Decision

**Option 1, with Options 2, 3, 4 and 5 rejected.**

### 1. Naming

- The capability is `decision`: `CapabilityDecision` in `internal/inferencegw/connector.go`.
- The `compatible` API entry is `systemone`: `APISystemOne` in `internal/platform/inferenceproviders/v1alpha1/types.go` and the `apis` enum in `charts/orbit-crds/templates/crd-inferenceprovider.yaml`.
- The route is `POST /v1/systemone`.
- Nothing is named "decisions", for the collision reason above.

### 2. Pass-through

- The route passes TypeSafe's wire format through to providers that declare `systemone`. The gateway reads `model` and replaces it with the target's model id. Every other field, including `state` and `questions`, is forwarded unchanged and never interpreted. The upstream response body is returned unchanged.
- The gateway reads `usage.input_tokens` and `usage.output_tokens` from the response for the usage row and does nothing else with it.
- The route is non-streaming; the format has no streaming mode.
- The request body is capped at 10 MB, as for embeddings. The cap holds an image where an implementation accepts one.
- Auth to the upstream is `Authorization: Bearer <key>`, or none when the provider has no key, as for the other OpenAI-style APIs. The request goes to `<baseURL>/systemone`.

### 3. The `systemone` API and the `decision` capability

- A `compatible` provider declares `systemone` in `apis`, alone or with others. A `compatible` resource's `baseURL` is the prefix of `/systemone`, e.g. `https://api.typesafe.ai/v1`. No CEL rule is added: the existing rule that `messages` needs a `/v1` base URL does not apply.
- Hosting is unchanged: orbit ships and hosts no decision model. A self-hosted server connects as a `compatible` provider with its `baseURL`, no key, and `spec.models` if it has no model list.
- A provider whose `apis` is exactly `[systemone]` describes every model it lists as `decision`: capability `decision`, text input, no dialects. A decision model is not offered on the chat endpoints.
- A provider that declares `systemone` together with other APIs (a mixed endpoint, such as OpenRouter) serves its chat and decision models from **one** InferenceProvider. Discovery does not guess by name. It identifies decision models from two sources:
  1. **Endpoint metadata.** Verified against the live OpenRouter API on 2026-10-10:
     - Every model in `GET /api/v1/models` carries `architecture.output_modalities`. Decision models have `["decisions"]` and an `architecture.modality` such as `"text->decisions"` or `"text+image->decisions"`.
     - The default listing returns only text-output models (442 of 659), so decision models never appear in it.
     - `?output_modalities=decisions` returns the 19 decision models, among them `typesafe/jev-1.13`, `~typesafe/jev-latest`, `microsoft/microsoft-decision-1`, `cloudflare/clef`, `cloudflare/clef-flash`, `cloudflare/clef-omni`, `liquid/d1`, `upstage/solar-decide`, `inception/mercury-decide` and `jaredpalmer/kev-4b`. `?output_modalities=all` returns all 659. The valid values are text, image, embeddings, audio, video, rerank, decisions, speech, transcription and all.
     - Each entry publishes `pricing.prompt` per token and `pricing.completion` of `"0"` (Jev: `0.000000042`).

     So for a `compatible` provider whose `apis` includes `systemone` alongside other APIs, discovery keeps its existing listing unchanged and makes one extra request, `GET <baseURL>/models?output_modalities=decisions`. A model from that response is marked `decision` only when its entry declares `decisions` in `architecture.output_modalities`. An endpoint that ignores the query parameter returns entries without that metadata (typically its chat models), and none of them is marked. A decision model gets only the `decision` capability, text input and no dialects, as on a decision-only provider. When the same id also came back from the chat listing, the decision description wins. It is priced from the entry's `pricing` block with the existing price parser. If the extra request fails, discovery logs a warning and keeps the chat models; the provider does not fail.
  2. **An explicit `decisionModels` list** on the `compatible` spec, for endpoints that publish no metadata, such as a self-hosted Ollama serving both chat models and Clef. Listed ids are described as `decision` and added to the catalog even when no `/models` listing returns them. `spec.models` filters them like any other model. A CEL rule requires `'systemone' in self.apis` whenever `decisionModels` is set.
  - Trade-off: one extra request per discovery on a mixed endpoint, and trust in what the endpoint publishes, against two resources for one endpoint, a name heuristic (as `embed` is for embeddings) that would mislabel models, or labelling every model on the endpoint `decision`. A model whose metadata is wrong is corrected through `decisionModels`, and an endpoint without metadata needs the list.
- A decision-only provider also accepts `decisionModels`, which changes nothing: every model it lists is already `decision`.
- Discovery for providers that do not declare `systemone` is unchanged. The default listing also misses OpenRouter's embeddings and rerank models; that is a separate follow-up.
- `GET /v1/models` already returns each virtual model's `capabilities`. Callers filter on `decision` there. No server-side filter parameter is added.
- A virtual model with the `decision` capability routes `/v1/systemone` calls through its routes, like every capability: the router picks targets by the virtual model's strategy, and the connector of each target either serves `systemone` or refuses.

### 4. Refusal and no bridging

- **A `decision` virtual model routes only to providers declaring `systemone`. There is no bridge** over `chatCompletions`, `messages` or `responses`. A connector that does not declare `systemone` (Bedrock, Vertex AI, Azure, or a `compatible` provider without it) refuses the call as `MODEL_NOT_SUPPORTED`, retryable, so failover moves to the next route that serves it, as undeclared `messages` and `embeddings` do today.
- When no route serves it, the caller gets HTTP 400 `invalid_request` with the message `provider <name> does not serve the systemone API`. The message names the API. The shared `connectorClientErrorResponse` message ("use POST /v1/chat/completions for non-Anthropic models") is chat-specific and does not name the API, so this route builds its error from the connector's message instead of changing the shared mapping.
- The converse also holds: a chat model called on `/v1/systemone` is refused the same way, and a decision-only provider called on a chat route is refused by its connector.
- Rationale: a bridge returns text parsed into numbers, not probabilities. The gateway does not fabricate a decision.

### 5. Scope

- The route is registered in the route table beside `/v1/embeddings` and `/v1/responses`, in `backend/cmd/inference-gateway/main.go`, with **`inference:invoke`**.
- Why not a new scope: model-level access is already enforced by the virtual model scope `inference:<family>:<name>:use`, which a decision model has like any other model. A second route scope would only duplicate it, would add a scope to every role and PAT that already invokes inference, and would register in the policy service without a distinct risk to separate.
- Trade-off: a principal granted `inference:invoke` can call a decision route as well as a chat route; the grant cannot separate them by route. Operators separate them by granting model scopes, not by route scope. A dedicated scope can be added later without breaking callers if a need appears.

### 6. Body validation

- The gateway validates the envelope only: the body is a JSON object within the size cap, and `model` is a non-empty string, which routing needs.
- It does **not** validate `questions`: not the question types, not the option counts (up to 255 for `choice`, 100 for Laya), not the level counts (2 to 10 for `score`), not the shape of `state`. These limits differ between implementations and change, and a gateway that enforces TypeSafe's limits would wrongly reject a valid Laya or Kev request, or a newer TypeSafe one.
- Request errors come from the upstream. An upstream 422 returns to the caller as HTTP 422 with the upstream's message, so the SDK sees the status it expects. Trade-off: the error body is the gateway's envelope (`code`, `message`), not byte-identical to TypeSafe's; the status codes match.
- An upstream 400 returns as 400. An upstream 401 means the gateway's credential for that provider is wrong, not the caller's, and is not returned as 401: it surfaces as 503 `provider_error`, as it does on other routes.

### 7. 429 and 529

- Upstream **429** and **529** are both retryable. They go through the existing retry policy: per target up to `MaxAttemptsPerTarget` (2) with a 500 ms backoff, then the next route in the virtual model, up to `MaxTotalAttempts` (4). The OpenAI SDK also retries inside each attempt. This is unchanged behaviour shared with every route.
- 429 is already classified `THROTTLED`. 529 is not a standard status and is classified `PROVIDER_ERROR`, retryable because it is a 5xx. The classifier gets no new case.
- When attempts are exhausted: a final **429** is returned to the caller as HTTP **429** `rate_limited`, because SDKs back off on 429 and the caller's remedy is to slow down. A final **529** is mapped to **503** `provider_error`, the gateway's standard status for an unavailable upstream; a client never sees a 529. Both are written as failed usage rows with the connector's error code.
- `Retry-After` from the upstream is not forwarded. SDKs fall back to their own backoff.
- A budget refusal stays 403 `BUDGET_EXCEEDED` as on every route.

### 8. Pricing

The question: does the LiteLLM registry the catalog syncs from include decision models such as Jev?

- **Yes, as a source file, with `mode: evaluation`**, checked against the file on 2026-10-10: `typesafe/jev-latest`, `typesafe/jev-1.13.0`, `typesafe/jev-preview`, `cloudflare/clef`, `cloudflare/clef-flash`, `laya/*`, `azure_ai/Microsoft-Decision-1` and others. Input prices are per token with `output_cost_per_token: 0`; Jev is $0.042 per million input tokens. Self-hosted entries are priced at zero with a note that infrastructure is paid separately.
- **But the sync does not reach them for `compatible` providers.** `pricing_source.go` maps only the LiteLLM providers `bedrock*`, `vertex_ai*`, `azure` and `azure_ai` to kora providers. `typesafe`, `cloudflare` and `laya` are skipped. Of the decision models, only `azure_ai/Microsoft-Decision-1` is synced today, and only under the Azure provider type. The sync is not extended to `compatible` providers: they are priced per provider name by design ([CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md), decision 6), because two endpoints with the same model name can price differently.
- **How decision models are priced for `compatible` providers:**
  1. When the endpoint's `/models` publishes `pricing`, as OpenRouter's does, discovery syncs it as for other compatible models. TypeSafe's `/v1/models` publishes none.
  2. Otherwise through the override API, which creates the row when no synced price exists. The operator sets the input price (for Jev, $0.042 per million) and the output price (for Jev, zero). The LiteLLM file is the reference for the number.
  3. With no price, the usage row is written with `price_unknown` and cost zero, as for any unpriced model. It does not count against the budget, so operators must price decision models before relying on budgets.
- **Each call is priced from the catalog like other inference:** input tokens at the input price and output tokens at the output price, through the same `resolvePricing` and `estimatedCostUSD` path embeddings use. Nothing is hard-coded to input-only. For Jev the output price is $0, so the cost is `input_tokens × input price`, which matches what TypeSafe charges ("Output tokens are free"). A decision provider that charges for output is billed correctly once its output price is set.
- The upstream reports `output_tokens` (the TypeSafe examples show 65 for a 392-token input). The usage row records the reported count as completion tokens, and total tokens as the sum. The issue's statement that output tokens are zero for these models was wrong: the response carries a count, and for Jev it costs nothing. Operators of a provider that charges for output set an output price through the override API.

### 9. Usage and budgets

- Each call writes one `inference_usage` row, on success and on failure, through the path embeddings uses: model policy check, `checkBudget`, route, retry, then the row and the spend counter. The row is attributed to the principal and tenant and counts against the tenant's budget like other inference. An exhausted budget refuses the call before it reaches the upstream.
- The row's `api_dialect` is `openai`, as embeddings use. No `systemone` dialect is added: it would need a migration to widen the `api_dialect` CHECK and changes in the usage filters and the dashboard, for a distinction the row's model and its `decision` capability already carry. Trade-off: the logs cannot filter decision calls by dialect, only by model.

### 10. Discovery for a `systemone`-only provider

- The compatible connector lists models through the OpenAI or Anthropic SDK. A provider with only `systemone` has neither, so it lists through `GET <baseURL>/models` and accepts both shapes seen in the wild: OpenAI and OpenRouter's `{"data":[{"id":…}]}` and TypeSafe's `{"models":[{"name":…}]}`.
- `spec.models` still limits the offered models and is the catalog when the endpoint has no model list, as for other compatible providers.

### Rationale

Passing the body through is the only design that satisfies the SDK requirement and survives an unspecified format. It costs nothing at the gateway when implementations disagree on limits. Everything that is shared (identity, scope, policy, budget, retry, usage) is reused rather than rebuilt, which keeps the change to one connector method, one handler, one service method and a few constants.

---

## Consequences

### Positive

- Decision calls are governed: policy, budget, attribution and usage logs apply.
- Provider keys stay in the gateway.
- Any System One implementation, hosted or self-hosted, connects as a `compatible` provider.
- No migration and no new scope.

### Negative

- Every `ProviderConnector` gains a method; Bedrock, Vertex AI and Azure implement it as a refusal.
- Request validation is the upstream's. A bad request costs a round trip.
- The error body differs from TypeSafe's in shape; only the status codes match.
- Decision models are unpriced until an operator prices them, for `compatible` providers.
- A mixed endpoint costs one extra `/models` request per discovery, and its decision models are only as accurate as the metadata it publishes. An endpoint with no metadata needs `decisionModels`.
- Decision calls are not separable from chat calls by route scope or by usage dialect.

### Neutral

- Virtual models, routes, grants and the pricing sync are unchanged.
- The classifier primitive is not started.

---

## Implementation

- `backend/internal/platform/inferenceproviders/v1alpha1/types.go` and `charts/orbit-crds/templates/crd-inferenceprovider.yaml`: `systemone`, the optional `decisionModels` list and its CEL rule.
- `backend/internal/inferencegw/connector.go`: `CapabilityDecision`, the `SystemOneConnector` interface, request and result types.
- `backend/internal/inferencegw/providers/compatible/`: the connector method, discovery for `systemone`-only providers, decision-model detection on mixed endpoints (the `output_modalities=decisions` request and `decisionModels`), capability.
- `backend/internal/inferencegw/providers/{bedrock,vertexai,azure}/`: refusal.
- `backend/internal/inferencegw/service.go`, `handlers_systemone.go`, `controller.go`: the service method, handler and route.
- `backend/cmd/inference-gateway/main.go`: route table entry.
- `docs/guides/model-providers.md` and `docs/adr/2026-04-01-model-capability-taxonomy.md`: provider configuration and the capability taxonomy.

---

## Validation

Unit tests cover:

- The connector: pass-through of `noul`, `choice` and `score` answers unchanged; model rewrite; refusal when `systemone` is not declared; discovery for both `/models` shapes.
- The route: success, usage row priced from the catalog, refusal naming `systemone` when no route declares it, 422 / 429 / 529 mapping, budget refusal.
- The capability: `systemone`-only providers describe models as `decision`.
- Mixed endpoints, against an `httptest` server shaped like OpenRouter's response: only models that declare `decisions` in `architecture.output_modalities` are marked; an endpoint that ignores the parameter marks nothing; a failed decision listing keeps the chat models; `decisionModels` adds unlisted ids as `decision`; `spec.models` filtering still applies.
- The CRD: `systemone` passes validation and an unknown API is rejected; `decisionModels` is accepted with `systemone` and rejected without it.

On a cluster: an InferenceProvider with `apis: [systemone]` becomes Ready, and a TypeSafe SDK pointed at the gateway returns answers. Not yet verified: that OpenRouter's `/api/v1/systemone` accepts each of the 19 decision models its listing returns. That needs an OpenRouter key.

---

## Related Decisions

- [Embeddings and Responses API Endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md): the pattern followed.
- [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): the `compatible` type and per-provider pricing.
- [Model Capability Taxonomy](2026-04-01-model-capability-taxonomy.md): the capability layer.
- [LLM Cost Tracking](2026-06-05-llm-cost-tracking.md): pricing and budget enforcement.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md): the route table.

---

## References

- [TypeSafe API reference](https://docs.typesafe.ai/api)
- [TypeSafe models](https://docs.typesafe.ai/models)
- [LiteLLM model prices](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)

---

**Last Updated:** 2026-10-10

