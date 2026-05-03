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

A secondary product concern: AWS Bedrock is the largest provider catalogue in the platform, but only **Bedrock Mantle** (a separate AWS endpoint hosting OpenAI models) exposes the Responses API natively. Without a compatibility layer, Codex users on Bedrock would be limited to Mantle models — Claude (the most-used family on the platform) and the rest of the Bedrock catalogue (Nova, Llama, Mistral, Titan, …) would be unreachable from a Codex client.

### Goals

- `POST /v1/embeddings`: accept OpenAI-compatible embedding requests, route via the virtual model catalog, return `CreateEmbeddingResponse`.
- `POST /v1/responses`: accept OpenAI Responses API requests (streaming and non-streaming), route via the virtual model catalog.
- Full Orbit integration for both: auth, policy, usage attribution, retry/failover — identical to existing chat endpoints.
- No new routing primitives, no new middleware, no catalog schema changes.
- Bedrock Mantle support within the existing Bedrock connector — no new provider registration required.
- **Codex reach across the Bedrock catalogue** — Claude and other Bedrock models are usable from Codex clients without operator intervention, even though they do not natively speak the Responses API.
- `DialectResponses` as a first-class API dialect so usage analytics can distinguish Responses API traffic from Chat Completions traffic.

### Non-Goals

- Stateful Responses sessions (`previous_response_id` support)
- Provider-hosted tools (`web_search`, `code_interpreter`, `file_search`)
- Image generation, rerank, audio generation
- Bedrock Mantle as a separate connector — it is an extension of the existing Bedrock connector

---

## Decision Drivers

* **Native where possible** — endpoints that genuinely speak the Responses API should pass through with zero translation, preserving response IDs, output item IDs, built-in tool results, and any future Responses-only fields.
* **Bridge where it expands reach** — for providers / endpoints that do not natively speak the Responses API but do speak a compatible dialect (Anthropic Messages, OpenAI Chat Completions), a translation bridge is preferable to forcing operators to provision a different upstream.
* **Provider-agnostic at the service layer** — the gateway routing engine and service layer never inspect the model name to pick a transport. Connectors are responsible for choosing the right wire path internally.
* **Minimal blast radius** — changes are additive; no existing code path is modified except to add new interface methods and route entries.
* **Honest contract** — bridge limitations are documented and detectable. Where translation cannot faithfully represent a request, the bridge returns a structured error so the routing engine can fail over to a native target.

---

## Considered Options

### Option A: Native pass-through only

Add `EmbeddingsConnector` and `ResponsesConnector` dialect interfaces to `ProviderConnector`. Each provider implements both using its existing `openaiClient`. Where a provider does not natively support an endpoint, the SDK call returns an error and the routing engine fails over to the next target.

**Pros:**
- Zero translation loss for endpoints that are natively supported.
- Minimal code surface.

**Cons:**
- On Bedrock, the only models reachable from a Codex client would be Mantle models (`openai.gpt-oss-*`). Claude and Nova/Llama/Mistral routes would all return errors and exhaust failover.
- Operators wanting Codex coverage for Claude would have to stand up Vertex AI or Azure routes for those models — a significant deployment burden.

### Option B: Translate Responses → Chat Completions (everywhere)

Implement translation at the handler level: map Responses API request shapes to Chat Completions request shapes, call the existing chat completion path, translate the response back.

**Pros:**
- Maximum reach: every Chat-Completions-capable model serves Responses traffic.

**Cons:**
- Lossy for providers that *do* natively speak Responses (Mantle, Vertex, Azure): response IDs, output item IDs, built-in tool results, and Responses-specific metadata are reconstructed instead of preserved.
- High maintenance burden: the Responses and Chat Completions APIs continue to diverge.
- Defeats the purpose of native Responses support where it exists.

### Option C: Hybrid — native where supported, bridge where it expands reach (chosen)

Native pass-through for any provider/endpoint that natively speaks the Responses API. For Bedrock specifically, where the standard runtime endpoint speaks Chat Completions / Messages but not Responses, two compatibility bridges live inside the Bedrock connector:

- **`AnthropicResponsesBridge`** translates Responses API → Anthropic Messages API for Claude models.
- **`ChatCompletionsResponsesBridge`** translates Responses API → OpenAI Chat Completions for the rest of the Bedrock catalogue (Nova, Llama, Mistral, …).

The bridges are *only* used inside the Bedrock connector. Vertex AI and Azure use native pass-through unconditionally.

**Pros:**
- Codex reaches the full Bedrock catalogue through a single virtual model entry per family.
- Native paths (Mantle, Vertex, Azure) preserve all Responses-specific fields with zero translation.
- Provider-agnostic at the service layer: the bridge complexity is encapsulated inside the Bedrock connector.
- Routing engine and policy layer treat all targets identically.

**Cons:**
- Bridge paths are lossy in well-defined ways (see "Bridge Limitations" below).
- Two extra translation surfaces to maintain (`responses_anthropic.go`, `responses_chatcompletions.go`).
- Streaming bridges are inherently approximate: SSE event sequences are reconstructed, not forwarded.

---

## Decision

**Chosen Option: Option C — hybrid native + Bedrock compatibility bridges.**

Two new dialect interfaces (`EmbeddingsConnector`, `ResponsesConnector`) are added to `ProviderConnector`. Vertex AI and Azure implement both interfaces by delegating to their existing `openaiClient`. Bedrock implements them with three-way dispatch: Mantle endpoint (native), Anthropic bridge (Claude), Chat Completions bridge (everything else).

The service layer adds three new methods (`Embeddings`, `Responses`, `ResponsesStream`) following the existing seven-step orchestration pattern. Two new handler files are added. Route entries are added to `Controller.Boot()` and `inferenceRoutes()`.

### Dispatch matrix

| Provider / endpoint | `Responses` / `ResponsesStream` |
|----|----|
| Bedrock Mantle (`openai.*`) | `mantleClient.Responses` (native) |
| Bedrock Anthropic (`anthropic.*`, `claude-*`) | `AnthropicResponsesBridge` → Messages API |
| Bedrock other (`amazon.nova-*`, `meta.llama-*`, `mistral.*`, …) | `ChatCompletionsResponsesBridge` → Chat Completions |
| Vertex AI | `openaiClient.Responses` (native) |
| Azure | `openaiClient.Responses` (native) |

### Bridge Limitations (the lossy-by-design contract)

These limitations apply **only** to Bedrock-bridged paths. Native paths (Mantle, Vertex, Azure) preserve everything the upstream provider returns.

| Limitation | Anthropic bridge | Chat Completions bridge |
|----|----|----|
| `previous_response_id` | rejected with `INVALID_REQUEST` (translation error) | rejected with `INVALID_REQUEST` |
| Built-in tools (`web_search`, `file_search`, `code_interpreter`, `mcp`, …) | dropped silently (only `function` tools forwarded) | dropped silently (only `function` tools forwarded) |
| Custom function tools (`OfFunction`) | translated to Anthropic `tools[]` | translated to Chat Completions `tools[]` |
| `function_call` / `function_call_output` input items | translated to Anthropic `tool_use` / `tool_result` blocks so multi-turn agentic flows work | translated to Chat Completions assistant tool-call / tool-role messages |
| `system` / `developer` role input items | hoisted into Anthropic `params.System` (not appended as user turns) | hoisted into a `system` Chat Completions message |
| Streaming tool-call deltas | tool_use blocks are not streamed; they appear in the terminal `response.completed` event with the same shape as the non-streaming path | identical: tool calls appear in the terminal `response.completed` |
| Response IDs | the upstream Anthropic `msg_…` ID is used as the Response `id` (not a `resp_…` ID) | the upstream Chat Completion `chatcmpl-…` ID is used as the Response `id` |
| Output item IDs | per-item IDs are derived from the upstream message ID; not stable Responses-API `item_…` IDs | same |
| `MaxOutputTokens` / `max_tokens` when omitted | falls back to the model's documented maximum output via `AnthropicModelMaxOutputTokens` (e.g. 64K for Claude Sonnet 4.x, 32K for Claude Opus 4.x); unknown models use a 64K ceiling so the upstream surfaces a clear `INVALID_REQUEST` rather than silent truncation | unset (Chat Completions has no required field) |

The bridge intentionally does **not** impose an arbitrary fallback like 4K — coding agents routinely emit long structured outputs and a silent low cap is a debugging trap. Operators must add new Claude families to the `anthropicMaxOutputTokens` table in `convert/anthropic_max_tokens.go` so the bridge can honour "no max_tokens" requests for them.

`previous_response_id` is the most consequential omission: stateful multi-turn Codex flows that depend on it will not work on bridged paths. Inline conversation history in `input` works as expected.

### Failure modes & failover

- A bridge that cannot honour the request (e.g. `previous_response_id` set) returns a `ConnectorError{Code: "INVALID_REQUEST", Retryable: false}`. The routing engine treats this as a non-retryable error: failover does not advance to the next target, and the client receives a 400.
- A bridge whose underlying upstream (Anthropic / Chat Completions on Bedrock) returns a retryable error (`THROTTLED`, `PROVIDER_UNAVAILABLE`) propagates that error code through `MapAnthropicError` / `MapOpenAIError` so the routing engine can fail over normally.
- A native target whose upstream does not speak Responses (e.g. a Vertex deployment that has not been provisioned for it) returns an SDK error which is mapped to `PROVIDER_ERROR` and causes failover.

### New Interfaces (`connector.go`)

```go
type EmbeddingsConnector interface {
    Embeddings(ctx context.Context, req openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error)
}

// ResponsesStreamResult mirrors AnthropicStreamEvent.
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

The three-layer taxonomy (capability → dialect → modality) describes **model** properties. "Supports `POST /v1/responses`" is not a model property — it is a connector/endpoint property. Any virtual model with `CapabilityChat` routes is a valid target for `POST /v1/responses`. Operators use the same virtual model catalog entries they already have for chat completions.

The `CapabilityEmbedding` comment in `connector.go` is updated to remove the "handler not yet implemented" notice.

### New Dialect Constant (`models.go`)

```go
DialectResponses APIDialect = "responses"
```

The Responses API is a genuinely distinct wire format from Chat Completions. Recording it as `DialectOpenAI` would permanently obscure the distinction in usage analytics. A one-line DB migration updates the `CHECK` constraint on `inference_usage.api_dialect` from `IN ('openai', 'anthropic')` to `IN ('openai', 'anthropic', 'responses')`. All `POST /v1/responses` usage records are written with `APIDialect = DialectResponses`. Embeddings continue to use `DialectOpenAI`.

### Provider Implementations

#### Bedrock (`providers/bedrock/`)

- `connector.go` adds `mantleClient *openai.Client`, `anthropicBridge *bridge.AnthropicResponsesBridge`, `openAIBridge *bridge.ChatCompletionsResponsesBridge`, and `modelRegistry atomic.Pointer[map[string]backendID]`.
- `backend.go` introduces a `backendID` enum (`backendOpenAICompat`, `backendAnthropic`, `backendMantle`, `backendTitanEmbed`, `backendCohereEmbed`) and a `dispatchBackend(modelID)` method that consults the registry populated by `ListModels` and falls back to prefix heuristics on cold start.
- `responses.go` dispatches to Mantle (native), Anthropic bridge, or Chat Completions bridge.
- `embeddings.go` dispatches to native InvokeModel for Titan / Cohere embedding models, OpenAI-compat for the rest.

#### Vertex AI / Azure (`providers/vertexai/`, `providers/azure/`)

- `embeddings.go` and `responses.go` are thin wrappers around `openaiClient.Embeddings.New` / `openaiClient.Responses.New` / `openaiClient.Responses.NewStreaming`.

#### Bridges (`providers/bridge/`)

- `anthropic_responses.go` — `AnthropicResponsesBridge.Execute` and `ExecuteStream`. Holds an `*anthropic.Client`; provider-agnostic so it could be reused on any future connector that holds an Anthropic client.
- `chat_completions_responses.go` — `ChatCompletionsResponsesBridge.Execute` and `ExecuteStream`. Holds an `*openai.Client`.

#### Conversion helpers (`providers/convert/`)

- `responses_anthropic.go` — `ResponsesParamsToAnthropicParams`, `AnthropicMessageToResponsesResponse`, `AnthropicEventToResponsesEvents` plus stream state.
- `responses_chatcompletions.go` — `ResponsesParamsToChatCompletionParams`, `ChatCompletionToResponsesResponse`, `ChatCompletionChunkToResponsesEvents` plus stream state.

### Service Layer (`service.go`)

Three new methods, each following the existing seven-step orchestration pattern. All three pass `ChatCompletionRequest{Model: string(params.Model)}` to the routing engine — it is model-ID-based and `ChatCompletionRequest` is the shared routing key for all inference endpoints.

```
Embeddings(ctx, EmbeddingNewParams) (*CreateEmbeddingResponse, error)
  steps: principal → policy(checkModelPolicy) → route → retryTargets(Embeddings) → usage(promptTokens only) → return

Responses(ctx, ResponseNewParams) (*Response, error)
  steps: principal → policy(checkModelPolicy) → route → retryTargets(Responses) → usage(InputTokens/OutputTokens) → return

ResponsesStream(ctx, ResponseNewParams) (<-chan ResponsesStreamResult, error)
  steps: principal → policy(checkModelPolicy) → route → retryTargets(ResponsesStream) → goroutine(drainResponsesStream+usage) → return channel
```

`drainResponsesStream` accumulates token usage from the terminal `response.completed` event, treats `response.failed` and `response.incomplete` as stream errors, and handles `errClientDisconnected` on `ctx.Done()`.

Usage attribution:
- **Embeddings**: `PromptTokens = resp.Usage.PromptTokens`, `CompletionTokens = 0`, `TotalTokens = PromptTokens`, `APIDialect = DialectOpenAI`.
- **Responses (non-streaming)**: `PromptTokens = int(resp.Usage.InputTokens)`, `CompletionTokens = int(resp.Usage.OutputTokens)`, `APIDialect = DialectResponses`.
- **Responses (streaming)**: input/output tokens from `drainResponsesStream`, `APIDialect = DialectResponses`.

### Handler Layer

**`handlers_embeddings.go`**:
- `POST /embeddings` — decode `EmbeddingNewParams`, call `svc.Embeddings()`, return `CreateEmbeddingResponse`.
- Body limit: 10 MB.

**`handlers_responses.go`**:
- `POST /responses` — decode via `responsesEnvelope` (the SDK's `ResponseNewParams` has no `Stream` field; streaming is selected via request options, so we capture `stream` from a parallel raw decode).
- Non-streaming: `svc.Responses()`, return `Response` as JSON.
- Streaming: `svc.ResponsesStream()`, write SSE frames (`data: <json>\n\n`), terminate with `data: [DONE]\n\n`.
- Body limit: 100 MB.

### Route Registration

`controller.go` — `Boot()`:
```go
mux.Handle("POST /embeddings", c.handleEmbeddings)
mux.Handle("POST /responses", c.handleResponses)
```

`main.go` — `inferenceRoutes()`:
```go
{PathPrefix: "/v1/embeddings", Method: "POST", RequiredScope: "inference:invoke"},
{PathPrefix: "/v1/responses",  Method: "POST", RequiredScope: "inference:invoke"},
```

No new scope declarations are needed: both endpoints are governed by the existing `inference:invoke` (Layer 1) and `inference:{family}:{alias}:use` (Layer 2, via `checkModelPolicy`) scopes.

### Files Modified / Created

| File | Change |
|------|--------|
| `backend/internal/inferencegw/connector.go` | Add `EmbeddingsConnector`, `ResponsesConnector`, `ResponsesStreamResult`; embed in `ProviderConnector`; update `CapabilityEmbedding` comment |
| `backend/internal/inferencegw/models.go` | Add `DialectResponses APIDialect = "responses"` |
| `backend/internal/inferencegw/service.go` | Add `Embeddings()`, `Responses()`, `ResponsesStream()`, `drainResponsesStream()` |
| `backend/internal/inferencegw/handlers_embeddings.go` | New: `handleEmbeddings` |
| `backend/internal/inferencegw/handlers_responses.go` | New: `responsesEnvelope`, `handleResponses`, `handleResponsesStream` |
| `backend/internal/inferencegw/controller.go` | Add two routes in `Boot()`; update comment |
| `backend/cmd/inference-gateway/main.go` | Add two entries in `inferenceRoutes()` |
| `backend/internal/inferencegw/providers/bedrock/connector.go` | Add `mantleClient`, `anthropicBridge`, `openAIBridge`, `modelRegistry`; update `New()` and `ListModels()` |
| `backend/internal/inferencegw/providers/bedrock/backend.go` | New: `backendID` enum, `dispatchBackend`, `inferBackendID` |
| `backend/internal/inferencegw/providers/bedrock/embeddings.go` | New — Titan / Cohere native InvokeModel + OpenAI-compat fallback |
| `backend/internal/inferencegw/providers/bedrock/responses.go` | New — Mantle / Anthropic-bridge / Chat-Completions-bridge dispatch |
| `backend/internal/inferencegw/providers/bridge/anthropic_responses.go` | New — Responses↔Anthropic Messages bridge |
| `backend/internal/inferencegw/providers/bridge/chat_completions_responses.go` | New — Responses↔Chat Completions bridge |
| `backend/internal/inferencegw/providers/convert/responses_anthropic.go` | New — Responses↔Anthropic translation helpers |
| `backend/internal/inferencegw/providers/convert/responses_chatcompletions.go` | New — Responses↔Chat Completions translation helpers |
| `backend/internal/inferencegw/providers/convert/openai.go` | Add `TranslationError`, `WrapTranslationError` |
| `backend/internal/inferencegw/providers/vertexai/embeddings.go` | New |
| `backend/internal/inferencegw/providers/vertexai/responses.go` | New |
| `backend/internal/inferencegw/providers/azure/embeddings.go` | New |
| `backend/internal/inferencegw/providers/azure/responses.go` | New |
| `backend/cmd/inference-gateway/migrations/NNNN_add_responses_dialect.sql` | New: update `api_dialect` CHECK constraint |

---

## Consequences

### Positive

- Codex CLI can be pointed at Orbit with `OPENAI_BASE_URL` + `OPENAI_API_KEY` and reach the entire Bedrock model catalogue (Mantle, Claude, Nova, Llama, Mistral) plus any virtual models routed to Vertex / Azure.
- Embedding workloads gain all Orbit benefits: policy enforcement, usage attribution, cost tracking, provider failover.
- Native paths (Mantle, Vertex, Azure) preserve all Responses-specific fields with zero translation loss.
- Bedrock Mantle support extends Bedrock's model catalogue to OpenAI models at zero additional credential cost — same AWS account, same SigV4 signing.
- `DialectResponses` in usage records gives operators clean analytics visibility into Codex/Responses API traffic separately from chat completions.

### Negative

- Two translation bridges must be maintained as the OpenAI Responses API and Anthropic Messages / OpenAI Chat Completions APIs continue to evolve.
- Bridge paths are lossy in defined ways (see "Bridge Limitations"). Clients depending on `previous_response_id` will not work on bridged paths.
- Streaming bridges reconstruct SSE event sequences from upstream events; the event ordering is correct but exact byte-for-byte parity with a native upstream is not guaranteed.
- One DB migration required to extend the `api_dialect` CHECK constraint.
- Bedrock model classification depends on `ListModels` having run — during the cold-start window the dispatch falls back to prefix heuristics (`inferBackendID`). Mantle models cannot be inferred from the model ID prefix alone, so cold-start Mantle requests fall through to `openaiClient` (the standard Bedrock OpenAI-compat endpoint), fail, and the routing engine fails over.

### Neutral

- Bridges live in a separate `providers/bridge/` package so non-Bedrock connectors do not import them.
- `previous_response_id` is rejected as `INVALID_REQUEST` on bridged paths (to fail fast) and forwarded unchanged on native paths.
- Function-call passthrough on bridged paths is best-effort: if the upstream returns malformed JSON arguments they are forwarded as-is to the client.

---

## Validation

### Testing

- Unit tests for `responses_anthropic.go` and `responses_chatcompletions.go` covering:
  - System / developer role hoisting into `params.System` (Anthropic) / `system` message (Chat Completions).
  - Function-call / function-call-output round-trip through both bridges.
  - Tool definition translation.
  - `previous_response_id` rejection.
  - Streaming → terminal `response.completed` event includes function_call output items (parity with non-streaming).
- Bridge tests asserting `INVALID_REQUEST` is wrapped as a non-retryable `ConnectorError`.
- Embeddings tests for Titan / Cohere multi-input handling.
- `make check` from repo root must pass.

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

# Codex CLI — should reach Mantle, Claude, and Nova/Llama/Mistral routes
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
