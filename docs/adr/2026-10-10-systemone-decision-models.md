# Decision Models and the System One API

**Status:** Accepted  
**Date:** 2026-10-10  
**Deciders:** kora Core Team  
**Technical Story:** [#473](https://github.com/clear-route/orbit/issues/473): add a `decision` capability and `POST /v1/systemone` to the inference gateway. PRD: [systemone-decision-models](../prd/systemone-decision-models.md).

---

## Context

### Background

A decision model takes a `state` and typed questions and returns probabilities. TypeSafe's System One API is the de facto wire format ([reference](https://docs.typesafe.ai/api)). It has no formal specification or standards body.

*   **Request:** `POST /v1/systemone` with `model`, `state` (a string, JSON object or array) and `questions`, a map from caller-chosen ids to questions.
*   **Question types:** `noul` (yes or no), `choice` (up to 255 options) and `score` (2 to 10 ordered levels).
*   **Response:** `model`, `answers` keyed by the question ids, and `usage` with `input_tokens` and `output_tokens`.
*   **Errors:** 401, 422, 429 and 529.

Hosted endpoints that serve the format include TypeSafe, OpenRouter (`/api/v1/systemone`), Liquid d1 and Upstage Solar Decide. Self-hosted servers include `laya-serve` and `kev.serve`. Implementations differ in their limits: Laya caps choices at 100 options, and some implementations accept images. OpenAI's `/v1/decisions` and OpenRouter's `/api/alpha/decisions` are different APIs with different schemas.

The gateway has no place for decision models today:

*   **`compatible` APIs:** `chatCompletions`, `embeddings`, `responses` and `messages` (`internal/platform/inferenceproviders/v1alpha1/types.go` and the `apis` enum in `charts/orbit-crds/templates/crd-inferenceprovider.yaml`).
*   **Capabilities:** chat, embedding, image, video, rerank and audio (`internal/inferencegw/connector.go`).
*   **Routes:** `/chat/completions`, `/messages`, `/embeddings`, `/responses` and `/models` (`internal/inferencegw/controller.go`).

### Problem Statement

Callers that want probabilities for typed questions call a provider directly with a raw key. Those calls bypass policy, usage logs, tenant attribution and budgets. The classifier primitive on the roadmap needs a governed decision call to build on.

### Goals

*   A `compatible` provider can declare the System One API.
*   A client using the TypeSafe SDK works against kora by changing only the base URL.
*   Decision calls use the same identity, scope, policy, budget, retry and usage path as other inference.
*   Decision models are a capability in the virtual model catalog.

### Non-Goals

*   The classifier primitive: tenant-defined classifiers, thresholds, UI, and attachments to triggers or workflows. A separate PRD follows.
*   Translating OpenAI's `/v1/decisions`, whose schema is not confirmed.
*   Shipping or hosting a decision model.
*   Egress or tool-policy use of classifiers.

### The three questions

*   **How does it connect?** The change touches this question. A decision endpoint is one more upstream whose key the gateway holds in a labelled Secret and injects. The caller never sees the key, and the gateway never forwards the caller's token.
*   **What can it access?** No new access mechanism is added. Route scope, the per-model `inference:<family>:<name>:use` scope, policy and the tenant budget apply as on other inference routes.
*   **Who is this agent?** The existing usage path attributes each call to the authenticated principal and tenant, including delegation.

---

## Decision Drivers

*   The format is unspecified and implementations differ. The gateway should interpret as little of the body as it can.
*   A decision answer must come from a decision model. The gateway must not fabricate probabilities.
*   The change should add no new state, no database migration and no usage dialect.

---

## Considered Options

### Option 1: Pass the System One wire format through as a new `compatible` API (chosen)

Add `systemone` to the `compatible` APIs and `POST /v1/systemone` to the gateway, following the embeddings route. New fields and per-implementation limits need no gateway change. Request errors come from the upstream.

### Option 2: Define a kora decision schema and translate to each provider (rejected)

Accept a kora-owned request shape and translate it to System One, and later to OpenAI's `/v1/decisions`. This gives one client contract across providers. A TypeSafe SDK client would no longer work against kora unchanged. The design also translates to one format with no specification and to another whose schema is not confirmed.

### Option 3: Serve decisions over chat completions (rejected)

Bridge a System One request onto a chat model by prompting for JSON. Every chat provider could serve it. The answers would be generated text parsed into numbers, and callers rely on calibrated probabilities. The call would also be priced as generated text.

### Option 4: Call the API and route "decisions" (rejected)

Use `apis: [decisions]` and `POST /v1/decisions`. These names collide with OpenAI's `/v1/decisions` and OpenRouter's `/api/alpha/decisions`.

### Option 5: A second InferenceProvider per mixed endpoint (rejected)

Register an endpoint that serves chat and decision models as two InferenceProviders on the same `baseURL`, one of them with `apis: [systemone]`. Discovery would need no change. It breaks the one-registration-per-endpoint goal of [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md) with two resources, two Secret references and two pricing namespaces for one endpoint. Endpoints that publish model metadata, such as OpenRouter, let discovery tell the models apart without it.

---

## Decision

**Chosen Option:** Option 1, pass the System One wire format through as a new `compatible` API.

### 1. Naming

*   The capability is `decision` (`CapabilityDecision`).
*   The `compatible` API entry is `systemone` (`APISystemOne`).
*   The route is `POST /v1/systemone`.

### 2. Pass-through

*   The gateway replaces `model` with the target's model id, forwards every other field without interpreting it, and returns the upstream body unchanged. It reads only `usage.input_tokens` and `usage.output_tokens` from the response for the usage row.
*   The route is non-streaming, matching a format that has no streaming mode.
*   `handleSystemOne` is an exception to the AGENTS.md handler conventions. It reads the body with `http.MaxBytesReader` and `io.ReadAll` and writes the upstream body with `w.Write` to keep `state`, `questions` and the response byte-for-byte. `api.DecodeJSON` and `api.JSON` would reorder or reformat them.
*   The request body cap is 10 MB like the embeddings route. The cap holds an image where an implementation accepts one.
*   A decision call times out after 15 seconds per attempt, and the gateway reads at most 1 MiB of the upstream response.
*   The request goes to `<baseURL>/systemone` with `Authorization: Bearer <key>`, or with no auth header when the provider has no key.

### 3. The `systemone` API and the `decision` capability

*   A `compatible` provider declares `systemone` in `apis`, alone or with other APIs. Its `baseURL` is the prefix of `/systemone` (e.g., `https://api.typesafe.ai/v1`), and no base URL CEL rule is added.
*   A self-hosted server connects as a `compatible` provider with its `baseURL`, no key, and `spec.models` if it has no model list.
*   A provider whose `apis` is exactly `[systemone]` describes every model it lists as `decision`, with text input and no dialects. It lists through `GET <baseURL>/models` in either shape in use: `{"data":[{"id":…}]}` (OpenAI, OpenRouter) or `{"models":[{"name":…}]}` (TypeSafe). Like other compatible providers, `spec.models` limits the offered models and is the catalog when the endpoint has no model list.
*   A decision model is not offered on the chat endpoints.
*   A mixed endpoint declares `systemone` with other APIs (e.g., OpenRouter) and serves its chat and decision models from **one** InferenceProvider. Discovery identifies its decision models from endpoint metadata and from an explicit list. It does not guess by model name.
*   Discovery for providers that do not declare `systemone` is unchanged.
*   Callers filter on `decision` in the `capabilities` that `GET /v1/models` already returns. No server-side filter parameter is added.
*   A virtual model with the `decision` capability routes `/v1/systemone` calls through its routes by its strategy.

**Endpoint metadata:** OpenRouter's `GET /api/v1/models` (checked 2026-10-10) gives every model an `architecture.output_modalities`, and decision models have `["decisions"]`. The default listing returns only text-output models (442 of 659). `?output_modalities=decisions` returns the 19 decision models (e.g., `typesafe/jev-1.13`, `cloudflare/clef`, `liquid/d1`), each with `pricing.prompt` per token and a `pricing.completion` of `"0"`.

Discovery for a mixed endpoint keeps its existing listing and adds one request, `GET <baseURL>/models?output_modalities=decisions`:

*   A returned model is marked `decision` only when its entry declares `decisions` in `architecture.output_modalities`. An endpoint that ignores the parameter returns entries without that metadata, and none is marked.
*   A decision model gets only the `decision` capability, text input and no dialects. Its description replaces a chat description with the same id.
*   The existing price parser reads the entry's `pricing` block.
*   If the request fails, discovery logs a warning and keeps the chat models.
*   The default listing also misses OpenRouter's embeddings and rerank models. That is a separate follow-up.

**Explicit list:** The optional `decisionModels` field on the `compatible` spec covers endpoints that publish no metadata (e.g., a self-hosted Ollama serving chat models and Clef). Listed ids are described as `decision` and added to the catalog even when no listing returns them, and `spec.models` filters them like any other model. An operator also uses the list to correct a model whose published metadata is wrong. A CEL rule requires `'systemone' in self.apis` whenever `decisionModels` is set. On a decision-only provider the list changes nothing.

### 4. Refusal and no bridging

*   A `decision` virtual model routes only to providers that declare `systemone`. There is no bridge over `chatCompletions`, `messages` or `responses`.
*   The gateway refuses a target whose discovered capability does not match the route, before it calls the connector: a chat model on `/v1/systemone`, and a decision model on a chat route. The refusal is retryable `MODEL_NOT_SUPPORTED`, and failover moves to the next route.
*   A connector that does not declare `systemone` (Bedrock, Vertex AI, Azure, or a `compatible` provider without it) refuses the call the same way. This covers models that discovery has not described.
*   When no route serves a decision call, the caller gets 400 `invalid_request` with `provider <name> does not serve the systemone API for model <id>`. A decision model called on a chat route gets 400 `model_not_supported` with `model <id> on provider <name> is a decision model; call POST /v1/systemone`.

### 5. Scope

*   The route requires `inference:invoke`.
*   No new scope is added. The virtual model scope `inference:<family>:<name>:use` already enforces model-level access. A second route scope would duplicate it, would need adding to every role and PAT that invokes inference, and separates no distinct risk. A dedicated scope can be added later without breaking callers.

### 6. Body validation

*   The gateway validates only the envelope: the body is a JSON object within the size cap, and `model` is a non-empty string.
*   The gateway does **not** validate `questions` or `state`, including question types, option counts and level counts. These limits differ between implementations and change. Enforcing TypeSafe's limits would reject a valid Laya or Kev request, or a newer TypeSafe one.
*   An upstream 400 or 422 returns to the caller with the same status and the upstream's message in the gateway's error envelope (`code`, `message`).
*   An upstream 401 or 403 means the gateway's credential for that provider is wrong. It surfaces as 503 `provider_error` like on other routes.

### 7. 429 and 529

*   Upstream 429 (`THROTTLED`) and 529 (`PROVIDER_ERROR`, a 5xx) are retryable under the existing policy: up to `MaxAttemptsPerTarget` (2) per target with a 500 ms backoff, then the next route, up to `MaxTotalAttempts` (4). The OpenAI SDK also retries inside each attempt. The classifier gets no new case.
*   A final 429 returns as HTTP 429 `rate_limited`. SDKs back off on 429, and the caller's remedy is to slow down.
*   A final 529 maps to 503 `provider_error`, the gateway's standard status for an unavailable upstream.
*   Both final failures write failed usage rows with the connector's error code.
*   The gateway does not forward the upstream `Retry-After`.
*   A budget refusal stays 403 `BUDGET_EXCEEDED`.

### 8. Pricing

*   The LiteLLM registry lists decision models with `mode: evaluation` (checked 2026-10-10), among them `typesafe/jev-*`, `cloudflare/clef*`, `laya/*` and `azure_ai/Microsoft-Decision-1`. Input prices are per token with `output_cost_per_token: 0`, and Jev is $0.042 per million input tokens. Self-hosted entries are priced at zero.
*   `pricing_source.go` maps only `bedrock*`, `vertex_ai*`, `azure` and `azure_ai` to kora providers. Only `azure_ai/Microsoft-Decision-1` is synced today, under the Azure provider type.
*   The sync is not extended to `compatible` providers. They are priced per provider name by design ([CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md), decision 6), and two endpoints with the same model name can price differently.

A decision model on a `compatible` provider is priced in this order:

1.  Discovery syncs the price that the endpoint's `/models` publishes. OpenRouter publishes prices, and TypeSafe's `/v1/models` publishes none.
2.  The operator sets the input and output price through the override API. The override creates the row when no synced price exists, and the LiteLLM file is the reference for the number.
3.  Without a price, the usage row carries `price_unknown` and a cost of zero, and the call does not count against the budget.

Each call is priced through the `resolvePricing` and `estimatedCostUSD` path embeddings use: input tokens at the input price and output tokens at the output price. For Jev the output price is zero to match TypeSafe's billing ("Output tokens are free"). The usage row records the reported `output_tokens` as completion tokens (the TypeSafe examples show 65 for a 392-token input) and total tokens as the sum.

### 9. Usage and budgets

*   Each call writes one `inference_usage` row on success and on failure through the embeddings path: model policy check, `checkBudget`, route, retry, then the row and the spend counter. An exhausted budget refuses the call before it reaches the upstream.
*   The row's `api_dialect` is `openai`. A `systemone` dialect would need a migration to widen the `api_dialect` CHECK and changes to the usage filters and the dashboard. The row's model and its `decision` capability already carry the distinction.

### Rationale

None at this time.

---

## Consequences

### Positive

*   Any System One implementation, hosted or self-hosted, connects as a `compatible` provider with no gateway change.

### Negative

*   Every `ProviderConnector` gains a method. Bedrock, Vertex AI and Azure implement it as a refusal.
*   Request validation belongs to the upstream. A bad request costs a round trip.
*   The error body differs in shape from TypeSafe's. Only the status codes match.
*   Decision models on `compatible` providers are unpriced until the endpoint publishes a price or an operator sets one. Operators must price them before relying on budgets.
*   A mixed endpoint costs one extra `/models` request per discovery. Its decision models are only as accurate as the metadata it publishes, and an endpoint with no metadata needs `decisionModels`.
*   A principal granted `inference:invoke` can call the decision route and the chat routes. Only model scopes separate them.
*   Usage logs cannot filter decision calls by dialect, only by model.

### Neutral

*   Virtual models, routes, grants and the pricing sync are unchanged.
*   The classifier primitive is not started.

---

## Implementation

*   `backend/internal/platform/inferenceproviders/v1alpha1/types.go` and `charts/orbit-crds/templates/crd-inferenceprovider.yaml`: `systemone`, the optional `decisionModels` list and its CEL rule.
*   `backend/internal/inferencegw/connector.go`: `CapabilityDecision`, the `SystemOneConnector` interface, and the request and result types.
*   `backend/internal/inferencegw/providers/compatible/`: the connector method, discovery for `systemone`-only providers, and decision-model detection on mixed endpoints.
*   `backend/internal/inferencegw/providers/{bedrock,vertexai,azure}/`: the refusal.
*   `backend/internal/inferencegw/service.go`, `handlers_systemone.go` and `controller.go`: the service method, handler and route.
*   `backend/cmd/inference-gateway/main.go`: the route table entry.
*   `docs/guides/register-inference-provider.md` and `docs/adr/2026-04-01-model-capability-taxonomy.md`: provider configuration and the capability taxonomy.

---

## Validation

Unit tests cover:

*   The connector: pass-through of `noul`, `choice` and `score` answers, the model rewrite, the refusal when `systemone` is not declared, and discovery for both `/models` shapes.
*   The route: success, a usage row priced from the catalog, the refusal that cites `systemone`, the 422, 429 and 529 mapping, and the budget refusal.
*   The capability: `systemone`-only providers describe models as `decision`.
*   Mixed endpoints, against an `httptest` server shaped like OpenRouter: only models that declare `decisions` are marked, an endpoint that ignores the parameter marks nothing, a failed decision listing keeps the chat models, `decisionModels` adds unlisted ids, and `spec.models` filtering still applies.
*   The CRD: `systemone` is accepted and an unknown API is rejected. `decisionModels` is accepted with `systemone` and rejected without it.

On a cluster, an InferenceProvider with `apis: [systemone]` becomes Ready, and a TypeSafe SDK pointed at the gateway returns answers. It is not yet verified that OpenRouter's `/api/v1/systemone` accepts each of the 19 listed decision models. That check needs an OpenRouter key.

---

## Related Decisions

*   [Embeddings and Responses API Endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md): the pattern followed.
*   [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): the `compatible` type and per-provider pricing.
*   [Model Capability Taxonomy](2026-04-01-model-capability-taxonomy.md): the capability layer.
*   [LLM Cost Tracking](2026-06-05-llm-cost-tracking.md): pricing and budget enforcement.
*   [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md): the route table.

---

## References

*   [TypeSafe API reference](https://docs.typesafe.ai/api)
*   [TypeSafe models](https://docs.typesafe.ai/models)
*   [LiteLLM model prices](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)

---

**Last Updated:** 2026-10-10
