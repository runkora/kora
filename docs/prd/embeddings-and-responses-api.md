# Embeddings and Responses API Endpoints — Product Requirements Document

**Status:** Draft  
**Created:** 2026-05-01  
**Last Updated:** 2026-05-01  
**Owner:** Orbit Core Team  
**Stakeholders:** Orbit Core Team, Agent Developers, Enterprise Platform Operators

---

## Executive Summary

The Orbit inference gateway currently exposes two inference endpoints: `POST /v1/chat/completions` (OpenAI dialect) and `POST /v1/messages` (Anthropic dialect). This PRD covers the addition of two new endpoints — `POST /v1/embeddings` and `POST /v1/responses` — to enable embedding model usage and full compatibility with the OpenAI Responses API. The Responses API is the protocol used by OpenAI Codex CLI and is the strategic direction OpenAI is moving toward for agentic workloads.

---

## Problem Statement

### Current State

Orbit's inference gateway routes chat completion and Anthropic Messages API traffic to three cloud providers (AWS Bedrock, Google Vertex AI, Azure AI). The gateway's virtual model catalog already classifies provider models by capability (`chat`, `embedding`, `image`, etc.), and embedding models are present on all three providers, but there is no handler to serve them. The `CapabilityEmbedding` type is defined in code but models with that capability are intentionally hidden from `GET /v1/models` until a handler ships.

Similarly, OpenAI's Responses API (`POST /v1/responses`) is a distinct, newer wire format that replaces the Chat Completions API for many agentic use cases. Tools like OpenAI Codex CLI use the Responses API exclusively and cannot be pointed at an endpoint that only speaks Chat Completions.

### User Pain Points

**For Agent Developers:**
- Cannot use Orbit as a drop-in replacement for the OpenAI API with tools (like Codex) that use the Responses API.
- Cannot generate embeddings for RAG pipelines, semantic search, or other embedding-dependent workloads through Orbit — must bypass the gateway and call providers directly with raw credentials.
- Bypassing the gateway means no usage tracking, no cost attribution, no policy enforcement, and no provider failover for these use cases.

**For Platform Operators:**
- Embedding model calls bypass Orbit entirely, creating a blind spot in cost and usage analytics.
- Credentials must be distributed to applications that need embeddings, increasing attack surface.
- No policy control over which principals can use embedding models or Responses API.

### Business Impact

- **Codex compatibility gap:** Codex CLI is a high-value developer tool. Operators who want to route Codex through Orbit (for auditability, cost control, or model substitution) cannot do so today.
- **Incomplete provider abstraction:** Orbit promises a unified inference interface; the absence of embeddings and the Responses API are visible gaps in that promise.
- **Lost observability:** Usage that bypasses the gateway is invisible to analytics, policy, and cost attribution.

---

## Goals and Objectives

### Primary Goals

1. **Embeddings endpoint:** `POST /v1/embeddings` — fully OpenAI-compatible, routable to any provider that exposes an OpenAI-compatible embeddings endpoint, with usage tracking and policy enforcement.
2. **Responses API endpoint:** `POST /v1/responses` — OpenAI Responses API compatibility, enabling tools like Codex CLI to be pointed at Orbit with zero client-side changes. Streaming and non-streaming supported.
3. **Provider parity:** Both endpoints work across all three configured providers (Bedrock, Vertex AI, Azure). Provider-level failures are handled by the existing retry/failover system — the gateway degrades gracefully when a provider does not support an endpoint.
4. **Full Orbit integration:** Usage tracking, cost attribution, policy enforcement (via OPA), and virtual model routing apply to both new endpoints, identical to existing chat endpoints.

### Success Metrics

- `POST /v1/embeddings` returns a valid `CreateEmbeddingResponse` for any configured embedding model virtual model.
- `POST /v1/responses` returns a valid `Response` for any configured chat model virtual model.
- Codex CLI can be configured with `OPENAI_BASE_URL=<orbit-gateway>` and works end-to-end with zero code changes.
- All requests to both endpoints appear in `GET /v1/usage` with correct token counts and cost attribution.
- `GET /v1/models` returns embedding models (currently hidden) once the embeddings handler ships.
- Gateway overhead for both endpoints is ≤ 50ms p95 (excluding provider latency), matching the existing chat endpoints.

### Non-Goals

- **Stateful Responses sessions:** The `previous_response_id` field enables stateful multi-turn conversations managed server-side. Orbit is a stateless proxy; it will forward `previous_response_id` to the upstream provider unchanged but will not store or validate response IDs itself. Clients using this field against providers that do not support it will receive provider errors.
- **Built-in Responses tools:** The Responses API supports provider-hosted tools (`web_search`, `code_interpreter`, `file_search`). These are OpenAI-specific features; Orbit passes them through to the provider unchanged. Whether they work depends entirely on the upstream provider's support.
- **Image generation (`POST /v1/images/generations`):** Out of scope for this release.
- **Rerank (`POST /v1/rerank`):** Out of scope for this release.
- **Embeddings streaming:** Embeddings are synchronous by nature; no streaming support is required or planned.
- **`CapabilityResponses` provider detection:** The constant will be defined but provider `models.go` files will not be updated to auto-detect it in this release.

---

## User Stories

### Primary User Stories

**As a** developer using Codex CLI,  
**I want to** point Codex at my Orbit gateway by setting `OPENAI_BASE_URL` and `OPENAI_API_KEY`,  
**So that** all Codex inference traffic flows through Orbit with full auditability and policy control.

**Acceptance Criteria:**
- [ ] `POST /v1/responses` accepts the Responses API wire format (`input`, `model`, `tools`, `stream`, etc.)
- [ ] Non-streaming and streaming (`stream: true`) responses both work correctly
- [ ] Standard OpenAI error shapes are returned on failure
- [ ] Usage is recorded in `inference_usage` with correct token attribution

---

**As a** developer building a RAG pipeline,  
**I want to** call `POST /v1/embeddings` through Orbit using my existing OpenAI SDK client,  
**So that** embedding calls are governed by the same policy, cost tracking, and failover as my chat calls.

**Acceptance Criteria:**
- [ ] `POST /v1/embeddings` accepts `EmbeddingNewParams` (model, input, encoding_format, dimensions)
- [ ] Returns a `CreateEmbeddingResponse` with the embedding vectors
- [ ] Embedding models appear in `GET /v1/models` once the handler ships
- [ ] Usage is recorded with prompt tokens populated from the provider response

---

**As a** platform operator,  
**I want to** define virtual models for embedding models in the catalog,  
**So that** I can route embedding requests to the cheapest or most appropriate provider.

**Acceptance Criteria:**
- [ ] Virtual models with `CapabilityEmbedding` routes are served by `POST /v1/embeddings`
- [ ] Virtual models with `CapabilityChat` routes are served by `POST /v1/responses`
- [ ] Policy enforcement applies to both endpoints using the existing `inference:{family}:{alias}:use` scope

---

### Secondary User Stories

**As a** developer,  
**I want to** see my embedding model calls in the usage log and analytics dashboard,  
**So that** I have full cost visibility across all model usage types.

---

## Proposed Solution

### High-Level Approach

Both endpoints follow the existing gateway pattern exactly: request decoding → principal extraction → policy check → virtual model routing → provider call with retry/failover → usage record write → response. No new middleware, no new routing primitives, no new auth flow.

- **Embeddings** is a pure request/response endpoint (no streaming). All three providers expose an OpenAI-compatible embeddings endpoint accessible via the existing `openaiClient` in each connector. Implementation is a thin handler + service method + one new method per provider connector.

- **Responses API** follows the same pattern as `POST /v1/chat/completions`: supports both streaming and non-streaming. The openai-go SDK v1.12.0 already includes the `responses` package with `ResponseNewParams`, `Response`, and `ResponseStreamEventUnion`. All three providers expose the Responses API via their OpenAI-compatible endpoint; providers that do not support it return errors handled by the existing failover system.

### Key Components

- **`POST /v1/embeddings` handler** (`handlers_embeddings.go`): decodes `EmbeddingNewParams`, calls service, returns `CreateEmbeddingResponse`.
- **`POST /v1/responses` handler** (`handlers_responses.go`): decodes `ResponseNewParams`, dispatches to streaming or non-streaming path, formats SSE or JSON response.
- **`EmbeddingsConnector` interface** (`connector.go`): `Embeddings(ctx, EmbeddingNewParams) (*CreateEmbeddingResponse, error)` — added to `ProviderConnector`.
- **`ResponsesConnector` interface** (`connector.go`): `Responses(...)` and `ResponsesStream(...)` — added to `ProviderConnector`.
- **Provider implementations** (3 providers × 2 new files each): thin wrappers around `openaiClient.Embeddings.New()` and `openaiClient.Responses.New()` / `openaiClient.Responses.NewStreaming()`.
- **Service methods** (`service.go`): `Embeddings()` and `Responses()`/`ResponsesStream()` following the existing 7-step orchestration pattern.
- **Route registration** (`controller.go`, `main.go`): two new route entries, both using the existing `inference:invoke` scope.

---

## Scope

### In Scope

- `POST /v1/embeddings` — non-streaming, OpenAI-compatible
- `POST /v1/responses` — streaming and non-streaming, OpenAI Responses API compatible
- Provider implementations for Bedrock, Vertex AI, and Azure
- Usage tracking for both endpoints (token counts, cost estimation, latency)
- Policy enforcement via existing `inference:invoke` + `inference:{family}:{alias}:use` scopes
- Virtual model routing via existing routing engine (no catalog changes required)
- Embedding models visible in `GET /v1/models` once the handler ships

### Out of Scope

- Stateful Responses sessions (`previous_response_id`)
- Provider-hosted tools (`web_search`, `code_interpreter`, `file_search`)
- Image generation (`POST /v1/images/generations`)
- Rerank (`POST /v1/rerank`)
- New `api_dialect` DB value (both endpoints recorded as `openai`)
- Provider-specific embedding format translation (pass-through only)

### Future Considerations

- `GET /v1/responses/{id}` — retrieve a completed response by ID (requires Orbit to store response IDs)
- Per-capability pricing tiers (embedding tokens priced differently from chat tokens)
- `CapabilityResponses` detection in provider `models.go` files so `GET /v1/models` can advertise which models support `POST /v1/responses`
- Image generation and rerank endpoints

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `POST /v1/embeddings` accepts valid `EmbeddingNewParams` and returns `CreateEmbeddingResponse`
- [ ] `POST /v1/responses` accepts valid `ResponseNewParams` and returns `Response` (non-streaming)
- [ ] `POST /v1/responses` with `stream: true` returns SSE-formatted `ResponseStreamEventUnion` events
- [ ] Both endpoints enforce authentication (existing `AuthMiddleware`)
- [ ] Both endpoints write an `InferenceUsageRecord` on every request (success and failure)
- [ ] Both endpoints respect policy (`inference:invoke` scope at Layer 1)
- [ ] Both endpoints route via the virtual model catalog (existing routing engine)
- [ ] Provider errors on one target trigger failover to the next target (existing retry logic)
- [ ] Both endpoints registered in `inferenceRoutes()` for OPA policy-service scope registration

**Should Have (P1):**
- [ ] `GET /v1/models` returns embedding models once embeddings handler is live
- [ ] Prompt token counts populated in usage records from provider response
- [ ] Cost estimation applied to embeddings (embedding pricing uses `InputCostPerMillion` only)
- [ ] OTel tracing spans for both new service methods

**Nice to Have (P2):**
- [ ] Provider-level debug logging consistent with existing `openaiChatCompletion` log pattern

### Non-Functional Requirements

**Performance:**
- Gateway overhead ≤ 50ms p95 (excluding provider latency), matching existing endpoints
- Streaming response first-byte latency equivalent to `POST /v1/chat/completions`

**Security:**
- No provider credentials exposed to callers
- All requests require a valid Orbit platform token (enforced by existing `AuthMiddleware`)
- Policy enforcement via OPA identical to existing chat endpoints

**Reliability:**
- Provider failures on one target trigger retry/failover via existing `retryTargets` scaffolding
- Client disconnects during streaming are detected and recorded as `canceled` in usage log

**Observability:**
- OTel trace spans on `Service.Embeddings()` and `Service.Responses()` / `Service.ResponsesStream()`
- All requests appear in `GET /v1/usage` with `api_dialect = 'openai'`

---

## Dependencies

### Internal Dependencies

- **Virtual model catalog** (complete): embedding virtual models must be defined for `POST /v1/embeddings` to route successfully. The handler will work immediately but return routing errors until embedding virtual models are configured.
- **Provider connectors** (complete): all three providers have `openaiClient` instances already wired that support the required SDK calls.

### External Dependencies

- **openai-go SDK v1.12.0** (already in `go.mod`): includes `responses` package with `ResponseNewParams`, `Response`, `ResponseStreamEventUnion`, and `EmbeddingNewParams`, `CreateEmbeddingResponse`.
- **Provider support for Responses API**: Bedrock does not support the OpenAI Responses API endpoint; calls will fail and the failover system will handle it. Azure and Vertex AI support depends on the specific deployment/endpoint configured.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Provider does not support Responses API endpoint | Medium — requests to that provider fail | Medium (Bedrock certain, Azure/Vertex uncertain) | Existing retry/failover handles it; operators configure routes only to supporting providers |
| `previous_response_id` used by Codex in stateful mode | High — multi-turn sessions break | Low — Codex primarily uses it for context, not required for basic use | Document the limitation; stateless mode works fully |
| Embedding usage costs tracked incorrectly | Low — cost estimates wrong | Low | Use `InputCostPerMillion` only; set completion tokens to 0 |
| openai-go Responses types change between SDK versions | Low | Low — SDK is stable | Pin SDK version in `go.mod` |

---

## Alternatives Considered

### Alternative 1: Translate Responses API → Chat Completions

Map `ResponseNewParams.Input` to `messages`, `tools` to OpenAI function tools, and translate `Response` back to the Responses API wire format at the gateway layer.

**Why not chosen:**
- Responses API has a significantly different shape (`input` vs `messages`, `output` items vs `choices`, built-in tools, etc.) — translation is lossy and error-prone
- The openai-go SDK natively supports the Responses API; native pass-through is simpler and future-proof
- Translation would break Responses-specific fields (response IDs, output item IDs, etc.) that clients may depend on

### Alternative 2: Add a new `api_dialect = 'responses'` to the DB

Track Responses API calls distinctly from Chat Completions calls in usage logs.

**Why not chosen:**
- Requires a DB migration to update the `CHECK` constraint on `inference_usage.api_dialect`
- The distinction can be added in a future iteration when analytics demand justifies the migration overhead
- Both are "OpenAI-compatible" wire formats from an operator perspective

---

## Open Questions

1. **Responses API on Vertex AI:** Does the Vertex AI OpenAI-compatible endpoint support `POST /v1/responses`? To be validated during implementation.
   - **Status:** Open

2. **Embedding virtual model configuration:** Should we ship default virtual model catalog entries for common embedding models (e.g., `text-embedding-3-small` on Azure, `amazon/titan-embed-text-v2` on Bedrock)?
   - **Status:** Open — likely a follow-up, not a blocker for the endpoint itself

3. **Responses API streaming usage:** The `ResponseCompletedEvent` at the end of a Responses stream carries final token counts. Should we accumulate and record these, or record zero tokens for streaming responses in v1?
   - **Status:** Open — recommendation is to accumulate from `ResponseCompletedEvent`, consistent with how chat completions handle `stream_options.include_usage`

---

## Timeline and Milestones

### Phases

**Phase 1: Core Endpoints** (Target: 2026-05-08)
- ADR written and approved
- `POST /v1/embeddings` handler + provider implementations
- `POST /v1/responses` handler + provider implementations (non-streaming)
- Usage tracking for both

**Phase 2: Responses Streaming + Polish** (Target: 2026-05-10)
- `POST /v1/responses` streaming path
- OTel spans
- `GET /v1/models` returns embedding models

### Key Milestones

- [ ] PRD Approved: 2026-05-01
- [ ] ADR Created: 2026-05-01
- [ ] Implementation Started: TBD
- [ ] Testing Complete: TBD
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [Inference Gateway PRD](inference-gateway.md) — parent gateway PRD
- [ADR-0023](../adr/0023-inference-gateway-architecture.md) — inference gateway architecture
- [ADR-0032](../adr/0032-model-capability-taxonomy.md) — three-layer endpoint taxonomy
- [ADR-0039](../adr/0039-virtual-model-catalog-and-platform-routing.md) — virtual model catalog and routing
- [ADR-0045](../adr/0045-embeddings-and-responses-api-endpoints.md) — implementation ADR

### References

- [OpenAI Responses API documentation](https://platform.openai.com/docs/api-reference/responses)
- [OpenAI Embeddings API documentation](https://platform.openai.com/docs/api-reference/embeddings)
- [openai-go SDK v1.12.0 responses package](https://pkg.go.dev/github.com/openai/openai-go/responses)
- [OpenAI Codex CLI](https://github.com/openai/codex)

### Glossary

- **Responses API:** OpenAI's newer inference API (`POST /v1/responses`) with a different wire format from Chat Completions, designed for agentic and tool-use workloads.
- **Embeddings:** Dense vector representations of text/images produced by embedding models, used for semantic search, RAG, and clustering.
- **Virtual model:** An Orbit catalog entry that maps a consumer-facing model ID (`family/name`) to one or more provider-specific model routes with configurable failover and routing strategy.
- **BYOP:** Bring Your Own Provider — operators supply their own cloud credentials; Orbit never holds provider API keys on behalf of customers.
