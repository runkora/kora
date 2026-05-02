# 45. Embeddings and Responses API Endpoints

**Status:** Accepted  
**Date:** 2026-05-02  
**Deciders:** Orbit Core Team  
**Technical Story:** Add `POST /v1/embeddings` and `POST /v1/responses` to the inference gateway to enable embedding model usage and OpenAI Codex CLI compatibility.

---

## Context

### Background

The Orbit inference gateway exposes two inference endpoints today:

| Endpoint | Wire format | Status |
|----------|-------------|--------|
| `POST /v1/chat/completions` | OpenAI Chat Completions | Implemented |
| `POST /v1/messages` | Anthropic Messages API | Implemented |
| `POST /v1/embeddings` | OpenAI Embeddings | Capability defined, handler missing |
| `POST /v1/responses` | OpenAI Responses API | Not defined |

The `CapabilityEmbedding` constant and the three-layer taxonomy (capability → dialect → modality) defined in ADR-0032 anticipated embedding support. Models with that capability are already discovered by all three provider connectors but are intentionally filtered out of `GET /v1/models` until a handler ships.

The Responses API (`POST /v1/responses`) is OpenAI's newer, stateless inference API introduced in early 2025. It is the protocol used by OpenAI Codex CLI and is OpenAI's stated strategic direction for agentic workloads. Its wire format differs meaningfully from Chat Completions: the request uses an `input` array rather than `messages`, and the response carries `output` items rather than `choices`. The openai-go SDK v1.12.0 (already in `go.mod`) includes the `responses` package with native Go types for the full Responses API.

### Problem Statement

Two classes of workloads cannot be routed through Orbit today:

1. **Embedding workloads** (RAG pipelines, semantic search, vector stores) — users must call providers directly, bypassing all Orbit policy, usage tracking, and cost attribution.
2. **Codex and Responses API clients** — `OPENAI_BASE_URL` can be set to point Codex at any OpenAI-compatible endpoint, but Codex issues `POST /v1/responses`, which the gateway does not handle.

### Goals

- `POST /v1/embeddings`: accept OpenAI-compatible embedding requests, route via the virtual model catalog, return `CreateEmbeddingResponse`.
- `POST /v1/responses`: accept OpenAI Responses API requests (streaming and non-streaming), route via the virtual model catalog.
- Full Orbit integration for both: auth, policy, usage attribution, retry/failover — identical to existing chat endpoints.
- No new routing primitives, no new middleware, no catalog schema changes.
- Bedrock Mantle support within the existing Bedrock connector — no new provider registration required.
- `DialectResponses` as a first-class API dialect so usage analytics can distinguish Responses API traffic from Chat Completions traffic.

### Non-Goals

- Stateful Responses sessions (`previous_response_id` support)
- Provider-hosted tools (`web_search`, `code_interpreter`, `file_search`)
- Image generation, rerank, audio generation
- Bedrock Mantle as a separate connector — it is an extension of the existing Bedrock connector

---

## Decision Drivers

* **Consistency** — both endpoints should follow the existing seven-step orchestration pattern (principal extract → policy → route → call → retry → usage → respond) without special-casing
* **Zero translation loss** — requests and responses should be native SDK types, passed through with no hand-rolled translation layer
* **Provider-agnostic** — the gateway should not assume which providers support which endpoints; provider failures are handled by the existing retry/failover system
* **Minimal blast radius** — changes should be additive; no existing code path should be modified except to add new interface methods and route entries

---

## Considered Options

### Option 1: Native SDK pass-through (chosen)

Add `EmbeddingsConnector` and `ResponsesConnector` dialect interfaces to `ProviderConnector`. Each provider implements both using its existing `openaiClient` (identical pattern to `openaiChatCompletion`). The service layer adds `Embeddings()`, `Responses()`, and `ResponsesStream()` methods following the existing orchestration pattern. Two new handler files are added.

**Pros:**
- Preserves all Responses API fields (response IDs, output item IDs, built-in tool results) that a translation layer would lose or mangle
- Minimal code: each provider implementation is ~20 lines, identical to `openaiChatCompletion`
- Future-proof: when providers extend their Responses API support, no gateway changes are needed
- Consistent with the "zero translation loss" principle established for chat completions

**Cons:**
- Providers that do not support the Responses API endpoint will return errors; the failover system handles this, but the operator must configure routing accordingly
- No automatic mapping of Responses API calls to Anthropic Messages API for Claude models on Bedrock (those models are unavailable for Responses on Bedrock regardless)

### Option 2: Translate Responses API → Chat Completions

Implement a translation layer at the handler level: map `ResponseNewParams.Input` → `messages`, translate `tools`, call the existing `ChatCompletion` / `ChatCompletionStream` service methods, then translate the response back into the Responses API shape.

**Pros:**
- Works for all providers that support chat completions (including Bedrock + Claude)

**Cons:**
- Lossy: `response.id`, `output[].id`, built-in tool results, and Responses-specific metadata cannot be reconstructed from a Chat Completions response
- High maintenance burden: the Responses and Chat Completions APIs continue to diverge; translation code needs ongoing updates
- Defeats the purpose of supporting the Responses API — Codex and other clients expect the Responses schema

### Option 3: Proxy only to providers that natively support the Responses API

Restrict `POST /v1/responses` routing to Azure and Vertex AI virtual model routes; reject Bedrock targets at the handler level.

**Pros:**
- No surprise failures on Bedrock routes

**Cons:**
- Bakes provider capability assumptions into the gateway layer — violates the provider-agnostic principle
- The virtual model catalog and retry/failover system already handle unavailable providers gracefully; explicit filtering is redundant and fragile

---

## Decision

**Chosen Option: Option 1 — Native SDK pass-through**

Two new dialect interfaces are added to `ProviderConnector`. All three providers implement both using their existing `openaiClient`. The service layer adds three new methods following the existing orchestration pattern. Two new handler files are added. Route entries are added to `Controller.Boot()` and `inferenceRoutes()`.

### Rationale

The native pass-through approach is consistent with every other dialect already implemented in the gateway. The translation approach was considered and rejected for the same reasons the Anthropic → OpenAI translation for the `/chat/completions` endpoint exists only as a compatibility shim inside Bedrock's connector (where a topology constraint forces it), not as a first-class translation layer.

Provider-level failures are an expected operational condition — they are handled gracefully by the retry/failover system without gateway-level special-casing.

---

## Implementation

### New Interfaces (`connector.go`)

```go
type EmbeddingsConnector interface {
    Embeddings(ctx context.Context, req openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error)
}

// ResponsesStreamResult mirrors AnthropicStreamEvent: value type (not pointer)
// for the event, consistent with how the union type is returned from stream.Current().
type ResponsesStreamResult struct {
    Event responses.ResponseStreamEventUnion
    Err   error
}

type ResponsesConnector interface {
    Responses(ctx context.Context, req responses.ResponseNewParams) (*responses.Response, error)
    ResponsesStream(ctx context.Context, req responses.ResponseNewParams) (<-chan ResponsesStreamResult, error)
}
```

Both interfaces are embedded in `ProviderConnector` alongside the existing `OpenAIConnector` and `AnthropicConnector`.

### New Capability Constant (`connector.go`)

No new capability constant is required for the Responses API.

The three-layer taxonomy (capability → dialect → modality) describes **model** properties:
- `CapabilityChat` — the model can do text generation
- `CapabilityEmbedding` — the model produces embedding vectors
- `CapabilityImageGeneration` — the model generates images

"Supports `POST /v1/responses`" is not a model property. It is a **connector/endpoint** property: does the provider's OpenAI-compatible endpoint implement the `/responses` path? No provider exposes this through their model listing APIs, so a `CapabilityResponses` constant would be a dead value that is never populated by discovery. The Responses API is better understood as a new wire format (a third dialect alongside `openai` and `anthropic`) for the existing chat capability.

Any virtual model with `CapabilityChat` routes is a valid target for `POST /v1/responses`. Operators use the same virtual model catalog entries they already have for chat completions. If a provider connector's `Responses()` call fails, the retry/failover system handles it identically to any other provider error.

The `CapabilityEmbedding` comment in `connector.go` is updated to remove the "handler not yet implemented" notice — no other changes to the capability constants are required.

### New Dialect Constant (`models.go`)

A new `DialectResponses` constant is added:

```go
DialectResponses APIDialect = "responses"
```

The Responses API is a genuinely distinct wire format from Chat Completions (`input` vs `messages`, `output` vs `choices`, response IDs, stateful session concept). Recording it as `DialectOpenAI` would permanently obscure the distinction in usage analytics — operators would be unable to query "what fraction of my traffic is Codex/Responses API vs chat completions?"

A one-line DB migration updates the `CHECK` constraint on `inference_usage.api_dialect` from `IN ('openai', 'anthropic')` to `IN ('openai', 'anthropic', 'responses')`. All `POST /v1/responses` usage records are written with `APIDialect = DialectResponses`. Embeddings continue to use `DialectOpenAI`.

### Provider Implementations

Each provider adds two new files following the exact structure of `openai.go`:

**`embeddings.go`** (all 3 providers):
```
Embeddings() → openaiClient.Embeddings.New(ctx, params) → MapOpenAIError on error
```

All three providers support embeddings via their OpenAI-compatible endpoint:
- Bedrock: Amazon Titan Embed, Cohere Embed, etc.
- Vertex AI: `text-embedding-004` and others via the Vertex AI OpenAI-compatible endpoint
- Azure: Azure OpenAI embedding deployments

**`responses.go`** (all 3 providers):
```
Responses()       → openaiClient.Responses.New(ctx, params) → MapOpenAIError on error
ResponsesStream() → openaiClient.Responses.NewStreaming(ctx, params) → goroutine drains ssestream.Stream[ResponseStreamEventUnion]
```

Note on Vertex AI: Whether the Vertex AI OpenAI-compatible endpoint supports `POST /responses` is unconfirmed at time of writing. The same error-handling path applies if it does not.

### Bedrock Mantle Extension

AWS Bedrock exposes the Responses API through a separate service called **Bedrock Mantle** — a distributed inference engine that hosts OpenAI models (e.g. `openai.gpt-oss-120b`) on AWS infrastructure. Bedrock Mantle uses a different endpoint domain (`bedrock-mantle.{region}.api.aws`) but **accepts the same AWS credentials and SigV4 signing** as the existing Bedrock runtime endpoint. No new API keys or IAM credentials are required.

Because Mantle extends Bedrock's model catalog and shares the same AWS account, region, and credentials, it is implemented as an extension of the existing Bedrock connector rather than a separate connector. This avoids exposing two distinct "Bedrock" providers to operators.

**Connector struct additions (`providers/bedrock/connector.go`):**

```go
type Connector struct {
    openaiClient    *openai.Client            // bedrock-runtime endpoint (Nova, Titan, Llama, Mistral)
    mantleClient    *openai.Client            // bedrock-mantle endpoint (OpenAI models)
    anthropicClient *anthropic.Client         // Anthropic Messages API (Claude)
    bedrockClient   *bedrockSDK.Client
    region          string
    logger          logging.Logger

    // mantleModelIDs is populated during ListModels from the Mantle endpoint.
    // Nil on cold start (before first ListModels call); requests for Mantle models
    // during the warmup window fall through to openaiClient, fail, and are handled
    // by the routing engine's failover system — the same cold-start behavior that
    // applies to all provider model caches.
    mantleModelIDs atomic.Pointer[map[string]bool]
}
```

The `mantleClient` is constructed in `New()` using the same `newSigV4Transport(awsCfg)` as `openaiClient`, pointed at the Mantle endpoint:

```go
mantleEndpoint := fmt.Sprintf("https://bedrock-mantle.%s.api.aws/v1", cfg.Region)
mantleClient := openai.NewClient(
    openaiopt.WithBaseURL(mantleEndpoint),
    openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
        httpclient.NewTracingTransport("orbit.bedrock-mantle", newSigV4Transport(awsCfg)),
    )),
)
```

**Model source classification at discovery time (`providers/bedrock/connector.go`):**

`ListModels` calls both endpoints and merges results. Models discovered from the Mantle endpoint are stored in `mantleModelIDs`:

```
listMantleModels(ctx)   → models from mantleClient.Models.ListAutoPaging
listOnDemandFMs(ctx)    → models from bedrockClient (existing)
listSystemProfiles(ctx) → inference profiles (existing)
→ store mantle model ID set in c.mantleModelIDs
→ return merged []ProviderModel
```

**Request dispatch in `ChatCompletion` / `ChatCompletionStream`:**

No prefix matching. Dispatch uses the discovery-time classification:

```go
func (c *Connector) useMantleClient(modelID string) bool {
    set := c.mantleModelIDs.Load()
    return set != nil && (*set)[modelID]
}
```

Dispatch order in `ChatCompletion`:
```
useMantleClient(id)   → mantleClient      (discovered via Mantle at ListModels time)
isAnthropicModel(id)  → anthropicClient   (Claude — still not on Mantle)
else                  → openaiClient      (native Bedrock runtime models)
```

**`Responses` / `ResponsesStream` always use `mantleClient`** — the Responses API is only available on the Mantle endpoint. The `bedrock/responses.go` implementation is distinct from the other providers: it calls `mantleClient.Responses.New()` / `mantleClient.Responses.NewStreaming()` rather than `openaiClient`.

### Service Layer (`service.go`)

Three new methods, each following the existing seven-step orchestration pattern. All three pass `ChatCompletionRequest{Model: string(params.Model)}` to the routing engine — it is model-ID-based and `ChatCompletionRequest` is the shared routing key for all inference endpoints. This is safe: `responses.ResponseNewParams.Model` is `shared.ResponsesModel = string` and `openai.EmbeddingNewParams.Model` is `EmbeddingModel = string` — both are string type aliases with direct conversion.

```
Embeddings(ctx, EmbeddingNewParams) (*CreateEmbeddingResponse, error)
  steps: principal → policy(checkModelPolicy) → route(ChatCompletionRequest{Model}) → retryTargets(Embeddings) → usage(promptTokens only) → return

Responses(ctx, ResponseNewParams) (*Response, error)
  steps: principal → policy(checkModelPolicy) → route(ChatCompletionRequest{Model}) → retryTargets(Responses) → usage(InputTokens/OutputTokens) → return

ResponsesStream(ctx, ResponseNewParams) (<-chan ResponsesStreamResult, error)
  steps: principal → policy(checkModelPolicy) → route(ChatCompletionRequest{Model}) → retryTargets(ResponsesStream) → goroutine(drainResponsesStream+usage) → return channel
```

A new `drainResponsesStream` helper is required in `service.go`, analogous to the existing `drainOpenAIStream` and `drainAnthropicStream`. It:
- Forwards all events from the provider channel to the output channel
- Accumulates token usage by detecting the terminal `"response.completed"` event: `event.AsResponseCompleted().Response.Usage.InputTokens` / `.OutputTokens`
- Treats `"response.failed"` and `"response.incomplete"` events as stream errors, wrapping them as `ErrProviderError`
- Handles `errClientDisconnected` when `ctx.Done()` fires mid-stream
- Returns `(inputTokens, outputTokens int, streamErr error)`

Usage attribution:
- **Embeddings**: `PromptTokens = resp.Usage.PromptTokens`, `CompletionTokens = 0`, `TotalTokens = PromptTokens`, `APIDialect = DialectOpenAI`
- **Responses (non-streaming)**: `PromptTokens = int(resp.Usage.InputTokens)`, `CompletionTokens = int(resp.Usage.OutputTokens)`, `APIDialect = DialectResponses`
- **Responses (streaming)**: input/output tokens from `drainResponsesStream` return values, `APIDialect = DialectResponses`

### Handler Layer

**`handlers_embeddings.go`**:
- `POST /embeddings` — decode `EmbeddingNewParams`, call `svc.Embeddings()`, return `CreateEmbeddingResponse`
- Body limit: 10 MB (embedding inputs are short; no conversation histories)
- No streaming path

**`handlers_responses.go`**:
- `POST /responses` — decode via `responsesEnvelope`, dispatch to streaming or non-streaming path

`responsesEnvelope` is required because `responses.ResponseNewParams` does **not** have a `Stream` field (the SDK's `ResponseService.NewStreaming` injects `stream: true` via a request option rather than via the params struct). The same pattern used by `chatCompletionEnvelope` applies:

```go
type responsesEnvelope struct {
    Params responses.ResponseNewParams
    Stream bool
}

func (e *responsesEnvelope) UnmarshalJSON(b []byte) error {
    if err := e.Params.UnmarshalJSON(b); err != nil {
        return err
    }
    var raw struct {
        Stream bool `json:"stream"`
    }
    _ = json.Unmarshal(b, &raw)
    e.Stream = raw.Stream
    return nil
}
```

- Non-streaming: call `svc.Responses()`, return `Response` as JSON
- Streaming (`stream: true`): call `svc.ResponsesStream()`, write SSE frames (`data: <json>\n\n`), terminate with `data: [DONE]\n\n`
- Body limit: 100 MB (matches chat completions — large tool call results may appear in `input`)

### Route Registration

`controller.go` — `Boot()`:
```go
mux.Handle("POST /embeddings", c.handleEmbeddings)
mux.Handle("POST /responses", c.handleResponses)
```

`main.go` — `inferenceRoutes()`:
```go
{PathPrefix: "/v1/embeddings", Method: "POST", RequiredScope: "inference:invoke"},
{PathPrefix: "/v1/responses", Method: "POST", RequiredScope: "inference:invoke"},
```

No new scope declarations are needed: both endpoints are governed by the existing `inference:invoke` (Layer 1) and `inference:{family}:{alias}:use` (Layer 2, via `checkModelPolicy`) scopes.

### Model Visibility (`GET /v1/models`)

**Correction to original framing:** There is no capability filter in `handleListModels` or `aggregateVMMetadata` that hides embedding models. The comment in `connector.go` describing them as "hidden" is misleading. Embedding models are absent from `GET /v1/models` only because no embedding virtual models have been configured in the catalog — there was no point doing so without a working endpoint.

Once `POST /v1/embeddings` ships, operators can configure embedding virtual models in the catalog. Those models appear in `GET /v1/models` automatically with `capabilities: ["embedding"]`. The only code change in this area is updating the `CapabilityEmbedding` comment in `connector.go` to remove the "not yet implemented" notice.

### Files Modified / Created

| File | Change |
|------|--------|
| `backend/internal/inferencegw/connector.go` | Add `EmbeddingsConnector`, `ResponsesConnector`, `ResponsesStreamResult`; embed new interfaces in `ProviderConnector`; update `CapabilityEmbedding` comment |
| `backend/internal/inferencegw/models.go` | Add `DialectResponses APIDialect = "responses"` |
| `backend/internal/inferencegw/service.go` | Add `Embeddings()`, `Responses()`, `ResponsesStream()`, `drainResponsesStream()` |
| `backend/internal/inferencegw/handlers_embeddings.go` | New: `handleEmbeddings` |
| `backend/internal/inferencegw/handlers_responses.go` | New: `responsesEnvelope`, `handleResponses`, `handleResponsesStream` |
| `backend/internal/inferencegw/controller.go` | Add two routes in `Boot()`; update comment |
| `backend/cmd/inference-gateway/main.go` | Add two entries in `inferenceRoutes()` |
| `backend/internal/inferencegw/providers/bedrock/connector.go` | Add `mantleClient`, `mantleModelIDs`, `useMantleClient()`; update `New()` and `ListModels()` |
| `backend/internal/inferencegw/providers/bedrock/embeddings.go` | New |
| `backend/internal/inferencegw/providers/bedrock/responses.go` | New — uses `mantleClient` (not `openaiClient`) |
| `backend/internal/inferencegw/providers/vertexai/embeddings.go` | New |
| `backend/internal/inferencegw/providers/vertexai/responses.go` | New |
| `backend/internal/inferencegw/providers/azure/embeddings.go` | New |
| `backend/internal/inferencegw/providers/azure/responses.go` | New |
| `backend/migrations/inference-gateway/NNNN_add_responses_dialect.{up,down}.sql` | New: update `api_dialect` CHECK constraint |

One database migration is required: adding `'responses'` to the `api_dialect` CHECK constraint on `inference_usage`.

---

## Consequences

### Positive

- Codex CLI can be pointed at Orbit with `OPENAI_BASE_URL` + `OPENAI_API_KEY` — zero client-side changes required
- Embedding workloads gain all Orbit benefits: policy enforcement, usage attribution, cost tracking, provider failover
- `GET /v1/models` begins returning embedding models once operators configure embedding virtual models in the catalog
- The `ProviderConnector` interface gains `EmbeddingsConnector` and `ResponsesConnector`, completing the two most important OpenAI endpoint families
- Bedrock Mantle support extends Bedrock's model catalog to include OpenAI models at zero additional credential cost — same AWS account, same SigV4 signing
- `DialectResponses` in usage records gives operators clean analytics visibility into Codex/Responses API traffic separately from chat completions

### Negative

- All three providers must implement two new interface methods — this is mechanical but increases interface surface area
- One DB migration required to extend the `api_dialect` CHECK constraint
- Bedrock Mantle model classification depends on `ListModels` having run — during the cold start window before the warmup goroutine completes, Mantle model requests fall through to `openaiClient`, fail, and failover to the next provider. This is the same cold-start behavior as all other provider model caches and is acceptable.

### Neutral

- The `ResponsesConnector` interface adds streaming support; for providers that support the Responses API, streaming will work. For those that don't, it will fail and failover handles it.
- `previous_response_id` is passed through to the upstream provider unchanged; stateful session management is a non-goal for this release.

---

## Validation

### Testing

- Unit tests for new handler files mirroring `handlers_openai_test.go` pattern
- Service-layer tests for `Embeddings()` and `Responses()` mirroring `service_test.go`
- `make check` from repo root must pass

### Manual Validation

```bash
# Embeddings (requires an embedding virtual model configured in catalog)
curl -X POST https://<gateway>/v1/embeddings \
  -H "Authorization: Bearer orb_..." \
  -H "Content-Type: application/json" \
  -d '{"model": "<embedding-vm-id>", "input": "hello world"}'

# Responses API (non-streaming)
curl -X POST https://<gateway>/v1/responses \
  -H "Authorization: Bearer orb_..." \
  -H "Content-Type: application/json" \
  -d '{"model": "<chat-vm-id>", "input": "Say hello"}'

# Responses API (streaming)
curl -X POST https://<gateway>/v1/responses \
  -H "Authorization: Bearer orb_..." \
  -H "Content-Type: application/json" \
  -d '{"model": "<chat-vm-id>", "input": "Say hello", "stream": true}'

# Codex CLI
OPENAI_BASE_URL=https://<gateway>/v1 OPENAI_API_KEY=orb_... codex "list files in current directory"
```

---

## Related Decisions

- [ADR-0023](0023-inference-gateway-architecture.md) — inference gateway core architecture
- [ADR-0032](0032-model-capability-taxonomy.md) — three-layer endpoint taxonomy (capability/dialect/modality)
- [ADR-0039](0039-virtual-model-catalog-and-platform-routing.md) — virtual model catalog and routing engine

---

## References

- [OpenAI Responses API reference](https://platform.openai.com/docs/api-reference/responses)
- [openai-go v1.12.0 responses package](https://pkg.go.dev/github.com/openai/openai-go/responses)
- [OpenAI Codex CLI](https://github.com/openai/codex)
- [AWS Bedrock Mantle — OpenAI-compatible APIs](https://docs.aws.amazon.com/bedrock/latest/userguide/bedrock-mantle.html)
- [PRD: Embeddings and Responses API](../prd/embeddings-and-responses-api.md)

---

**Last Updated:** 2026-05-02
