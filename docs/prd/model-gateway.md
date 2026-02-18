# Model Gateway - Product Requirements Document

**Status:** Approved  
**Created:** 2025-01-26  
**Last Updated:** 2025-01-27  
**Owner:** Orbit Core Team  
**Stakeholders:** Orbit Core Team, Agent Developers, Enterprise Users

---

## Executive Summary

The Model Gateway is Orbit's unified interface layer between agents and AI model providers. It exposes an OpenAI-compatible API that routes requests to provider-specific backends — AWS Bedrock, Google Vertex AI, and OpenAI — using a "Bring Your Own Credentials" (BYOC) model. The MVP delivers production reliability (streaming, retries, failover, connection pooling), intelligent routing (lowest-cost and failover chains), and comprehensive cost and token observability. Auth is minimal in the first release (user and app-level access tokens) with full Orbit identity, policy, and credential broker integration deferred to a subsequent phase. Per-token rate limiting is P1 (Post-MVP).

---

## Problem Statement

### Current State

Orbit is the control plane for AI agents. While the nervous system architecture (identity, policy, credentials, events) is being established, there is currently no standardized interface between agents and AI models. Each agent developer must implement provider-specific integrations from scratch, handle reliability patterns manually, and solve credential distribution in isolation.

### User Pain Points

**For Agent Developers:**
- Must implement provider-specific integrations for each model (OpenAI, Bedrock, Vertex AI, etc.)
- Need to handle retries, timeouts, and failovers manually in every agent
- Cannot switch models or providers without code changes
- No standardized way to track costs or token usage across providers

**For Platform Operators:**
- No visibility into which models agents are calling or at what cost
- Cannot enforce rate limits on a per-user or per-application basis
- Difficult to configure failover or cost-optimized routing centrally
- Credentials distributed to many consumers increases the blast radius of a leak

**For Security Teams:**
- Provider API keys must currently be distributed directly to agent applications
- No centralized audit point for model access
- Risk of credential leakage through agent logs, errors, or environment variables

### Business Impact

- **Development friction:** Every agent developer solves the same integration problem independently
- **Operational risk:** Without a unified reliability layer, provider failures cause agent failures
- **Cost opacity:** No visibility into cross-provider spend makes optimization impossible
- **Security exposure:** Distributed credential management increases attack surface

---

## Goals and Objectives

### Primary Goals

1. **Unified Provider Interface:** A single OpenAI-compatible API endpoint that routes to AWS Bedrock, Google Vertex AI, and OpenAI through a BYOC model — operators supply their own cloud credentials or API keys.
2. **Production Reliability:** Streaming support, automatic retries with exponential backoff, failover chains, and connection pooling built in from day one.
3. **Intelligent Routing:** Lowest-cost routing and explicit failover chains as the two primary routing strategies.
4. **Access Control:** User-level and application-level access token generation. Per-token rate limiting (RPM/TPM) is P1.
5. **Observability:** Accurate cost and token tracking (input, output, total) and structured request/response logging across all providers.

### Success Metrics

**Reliability:**
- 99.9% successful completion rate for model requests (with automatic failover)
- Gateway overhead < 50ms p95 (excluding model response latency)
- Zero request data loss during provider transient failures (retry handles transparently)

**Developer Experience:**
- Single API endpoint serves all supported providers and their models
- Zero provider-specific code required in agent applications
- Drop-in compatibility with OpenAI SDK

**Cost & Token Observability:**
- Per-request cost and token tracking with ≥ 99% accuracy versus provider-reported figures
- Cost aggregation queryable by token, model, provider, and time period

**Access Control:**
- 100% of requests authenticated via gateway-issued token

### Non-Goals (MVP)

- Orbit Policy Engine (OPA) integration — deferred to Post-MVP
- Orbit Event Spine integration — deferred to Post-MVP
- Orbit Credential Broker integration — deferred to Post-MVP
- Advanced load balancing (latency-based, performance-based) — P1
- Circuit breaker pattern — P1
- Response caching (simple or semantic) — P1
- Model group aliases — P1
- Support for 100+ individual provider integrations (Orbit is BYOC, not a provider catalog)
- Custom model fine-tuning or hosting
- Built-in RAG or vector database integration
- Model evaluation or benchmarking frameworks

---

## User Stories

### US-001: Bring Your Own Cloud Credentials

**As an** agent developer or platform operator,  
**I want to** configure the gateway with my own AWS Bedrock credentials, Google Vertex AI service account, or OpenAI API key,  
**So that** I can leverage my existing enterprise agreements, data residency controls, and security posture.

**Acceptance Criteria:**
- [ ] AWS Bedrock: Configure via IAM role/credentials; gateway calls Bedrock APIs on behalf of agents
- [ ] Google Vertex AI: Configure via GCP service account credentials; gateway calls Vertex AI APIs on behalf of agents
- [ ] OpenAI: Configure via API key; gateway calls OpenAI APIs on behalf of agents
- [ ] Credentials are configured at the gateway level and never exposed to agent code
- [ ] All three providers are accessible via the same OpenAI-compatible request format

---

### US-002: OpenAI-Compatible API with Streaming

**As an** agent developer,  
**I want to** use the same OpenAI SDK or HTTP client I already know,  
**So that** I can integrate the gateway without learning new APIs or changing my agent code.

**Acceptance Criteria:**
- [ ] `POST /v1/chat/completions` accepts OpenAI-format request bodies
- [ ] Non-streaming responses return OpenAI-format JSON
- [ ] Streaming responses return Server-Sent Events (SSE) in OpenAI stream format
- [ ] Model name in the request (e.g., `anthropic/claude-sonnet-4.5`) is passed through to the appropriate provider
- [ ] Multimodal requests (vision, etc.) are passed through to providers that support them (Bedrock/Vertex)

---

### US-003: Access Token Generation

**As a** developer or administrator,  
**I want to** generate access tokens for individual users or specific applications,  
**So that** I can manage access independently for different consumers (e.g., a local dev environment vs. a production service).

**Acceptance Criteria:**
- [ ] API to create a user-scoped token (linked to a human user identity)
- [ ] API to create an application-scoped token (linked to a named application/service)
- [ ] Tokens can be revoked individually
- [ ] Token type (user vs. app) is recorded and visible in usage logs
- [ ] All gateway requests require a valid token; requests without a valid token receive 401

---

### US-004: Rate Limiting Per Token/Credential *(P1 — Post-MVP)*

**As a** platform operator,  
**I want to** configure token-per-minute (TPM) and request-per-minute (RPM) limits per access token,  
**So that** I can prevent any single consumer from exhausting provider quotas or running up unexpected costs.

> **Note:** Rate limiting is deferred to P1. The token schema stores `rate_limit_rpm` and `rate_limit_tpm` fields from MVP so limits can be set at creation time, but enforcement is not implemented until P1.

**Acceptance Criteria:**
- [ ] TPM and RPM limits configurable per token at creation time (schema only in MVP)
- [ ] Requests exceeding the limit receive 429 Too Many Requests with a Retry-After header *(P1)*
- [ ] Rate limit counters are tracked accurately across concurrent requests *(P1)*
- [ ] Default rate limits apply when none are explicitly configured *(P1)*

---

### US-005: Routing — Lowest Cost

**As a** platform operator,  
**I want to** configure a routing policy that selects the lowest-cost provider/model combination for a given request,  
**So that** I can minimize model spend without manual model selection in every agent.

**Acceptance Criteria:**
- [ ] Routing engine maintains a cost table (per-token prices) for each configured provider and model
- [ ] When a request's model field maps to a routing group, the engine selects the lowest-cost viable option
- [ ] Cost table is configurable and updatable without gateway restart
- [ ] Routing decision is recorded in request logs

---

### US-006: Routing — Failover Chains

**As a** platform operator,  
**I want to** define ordered failover chains (primary → secondary → tertiary),  
**So that** if the primary provider is unavailable or returns an error, the gateway automatically retries with the next option.

**Acceptance Criteria:**
- [ ] Failover chains are configurable per routing group
- [ ] On provider error (5xx, timeout, rate limit), gateway tries the next provider in the chain
- [ ] Failover attempts are logged with the original error and the failover target
- [ ] If all providers in the chain fail, gateway returns an appropriate error to the caller

---

### US-007: Retries with Exponential Backoff

**As an** agent developer,  
**I want** the gateway to automatically retry transient provider failures,  
**So that** my agent does not fail due to brief provider instability.

**Acceptance Criteria:**
- [ ] Configurable max retry attempts (default: 3)
- [ ] Exponential backoff with jitter between retries
- [ ] Retries only on retriable errors (5xx, timeouts, rate limit 429 from provider)
- [ ] Non-retriable errors (4xx client errors) are not retried
- [ ] Total retry attempts and backoff durations are logged

---

### US-008: Connection Pooling

**As a** platform operator,  
**I want** the gateway to maintain persistent connection pools to each provider,  
**So that** we avoid TLS handshake overhead on every request and reduce tail latency.

**Acceptance Criteria:**
- [ ] One connection pool per configured provider
- [ ] Pool size is configurable per provider
- [ ] Connections are reused across agent requests
- [ ] Pool health is monitored; stale connections are recycled

---

### US-009: Cost and Token Tracking

**As an** operations engineer or finance stakeholder,  
**I want** every model request to be tracked with input tokens, output tokens, and estimated cost,  
**So that** I can understand spend by consumer, model, and provider.

**Acceptance Criteria:**
- [ ] Every request records: token ID, model, provider, input tokens, output tokens, total tokens, estimated cost (USD), latency, timestamp
- [ ] Token counts sourced from provider response where available; estimated via tokenizer where not
- [ ] Cost calculated using a configurable per-model price table ($/1K input tokens, $/1K output tokens)
- [ ] Usage queryable by token, model, provider, and time range
- [ ] Streaming requests track token usage from the final SSE `usage` chunk

---

### US-010: Request and Response Logging

**As a** platform operator,  
**I want** structured logs for every request and response passing through the gateway,  
**So that** I can debug issues, audit access, and understand traffic patterns.

**Acceptance Criteria:**
- [ ] Every request logs: timestamp, token ID, model requested, provider routed to, HTTP status, latency, input/output token count, error (if any)
- [ ] Configurable log levels (debug logs full request/response body; production logs metadata only)
- [ ] Request body logging is opt-in due to potential PII in prompts
- [ ] Logs are structured JSON, compatible with standard log aggregation pipelines

---

## Proposed Solution

### High-Level Architecture

```
Agent / Application
       │
       │  OpenAI-compatible HTTP API
       ▼
┌─────────────────────────────────────────────────┐
│                  Gateway API Server              │
│  - Token authentication                         │
│  - Request validation                           │
│  - Streaming (SSE)                              │
└──────────────────────┬──────────────────────────┘
                       │
            ┌──────────▼──────────┐
            │    Routing Engine   │
            │  - Lowest cost      │
            │  - Failover chains  │
            │  - Retries/backoff  │
            └──────────┬──────────┘
                       │
       ┌───────────────┼───────────────┐
       ▼               ▼               ▼
┌─────────────┐ ┌────────────┐ ┌────────────────┐
│   OpenAI    │ │ AWS Bedrock│ │ Google Vertex  │
│  Connector  │ │ Connector  │ │  AI Connector  │
│  (API key)  │ │ (IAM/BYOC) │ │ (SA/BYOC)      │
└─────────────┘ └────────────┘ └────────────────┘
                       │
            ┌──────────▼──────────┐
            │  Observability Layer│
            │  - Cost tracking    │
            │  - Token tracking   │
            │  - Structured logs  │
            └─────────────────────┘
```

### Key Design Decisions

**BYOC Model (Bring Your Own Credentials):**  
The gateway does not maintain a catalog of provider credentials. Operators configure their own AWS IAM credentials, GCP service accounts, or OpenAI API keys. This design keeps Orbit out of the credential-custody business until the Credential Broker is ready, and aligns with enterprise requirements for data sovereignty.

**OpenAI-Compatible API Surface:**  
The gateway exposes only the OpenAI `chat/completions` endpoint format for MVP. All three providers (OpenAI, Bedrock, Vertex AI) expose chat completion interfaces; the connector layer translates. This means agents need zero provider-specific code.

**Streaming as First-Class:**  
Streaming (SSE) is implemented from day one alongside regular completions. Retry and failover logic is adapted for streaming: failover only triggers before the first token is received (once streaming begins, the connection is owned by the provider). Token counting for cost tracking uses the final `usage` chunk in the stream.

**Minimal Auth (MVP):**  
The MVP uses gateway-issued tokens (user-scoped and application-scoped) validated locally. This is intentionally thin — full Orbit identity (OIDC/SAML) and the Credential Broker are deferred to the post-MVP integration phase, at which point the token layer will be replaced by Orbit's identity system.

**Stateless Gateway:**  
The gateway is stateless. Cost and usage records are persisted to PostgreSQL. This enables horizontal scaling without coordination overhead. Rate limiting (P1) will use in-process state for single-instance deployments and Redis-backed state for horizontal scale.

### Provider Connector Design

Each connector implements a shared interface:

```go
type ProviderConnector interface {
    // ChatCompletion executes a non-streaming completion request.
    ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)

    // ChatCompletionStream executes a streaming completion request,
    // returning a channel of response chunks.
    ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamChunk, error)

    // Name returns the provider identifier (e.g., "openai", "bedrock", "vertexai").
    Name() string

    // ModelCost returns the per-token pricing for a given model.
    ModelCost(model string) (*ModelPricing, error)
}
```

Provider connectors are responsible for:
- Translating OpenAI-format requests to provider-native format
- Translating provider-native responses back to OpenAI format
- Normalizing error types (so the routing engine can determine retryability)
- Multimodal pass-through (vision/audio content blocks forwarded as-is where provider supports)

### Routing Engine

The routing engine receives a request and a routing configuration and returns an ordered list of `(provider, model)` targets. The engine tries each target in order, applying retry logic per target before moving to the next.

**Lowest-Cost Routing:**  
Given a routing group (e.g., "claude-sonnet"), the engine queries the cost table for all configured providers that serve a model in that group and selects the cheapest per-output-token. This is recalculated per request so price table updates take effect immediately.

**Failover Chain Routing:**  
Given an ordered list of `(provider, model)` pairs, the engine attempts each in sequence. A failure (provider error, timeout, rate limit) triggers the next entry in the chain. The chain is exhausted before returning an error to the caller.

### Token Authentication

The MVP token system issues opaque tokens stored in PostgreSQL with metadata:

```
tokens
├── id (uuid)
├── token_hash (sha256 of raw token — raw token only returned at creation)
├── type (user | app)
├── owner_id (user ID or application name)
├── rate_limit_rpm (requests per minute, nullable = no limit)
├── rate_limit_tpm (tokens per minute, nullable = no limit)
├── created_at
├── expires_at (nullable)
└── revoked_at (nullable)
```

Tokens are presented as Bearer tokens in the Authorization header. The gateway validates on every request: existence, non-revocation, and non-expiry. Rate limit enforcement (RPM/TPM headroom checks) is P1.

---

## Scope

### In Scope (MVP)

**API:**
- `POST /v1/chat/completions` — non-streaming and streaming (SSE)
- `POST /v1/tokens` — create user or application token
- `DELETE /v1/tokens/{id}` — revoke token
- `GET /v1/usage` — query cost and token usage records

**Provider Support (BYOC):**
- AWS Bedrock (all models accessible via Bedrock: Claude, Llama, Mistral, Titan, etc.)
- Google Vertex AI (all models accessible via Vertex: Gemini, Claude on Vertex, etc.)
- OpenAI API (GPT-4, GPT-4o, o1, etc.)

**Multimodal:**  
Multimodal content (images, documents, audio where supported) is passed through to providers that natively support it. No normalization or re-encoding is performed in MVP.

**Routing Strategies:**
- Lowest cost (selects cheapest provider/model in a routing group)
- Failover chain (ordered list of provider/model targets, auto-advances on failure)

**Reliability:**
- Retries with exponential backoff and jitter (configurable max attempts, default 3)
- Connection pooling per provider (configurable pool size)
- Streaming-safe failover (failover only before first byte of stream response)

**Access Control:**
- User-scoped token generation and revocation
- Application-scoped token generation and revocation

**Observability:**
- Per-request cost tracking (input tokens × input price + output tokens × output price)
- Token usage tracking (input, output, total per request)
- Structured request/response logging (metadata always, body opt-in)
- Routing decision logging (which provider was selected, why, how many retries)

### Out of Scope (MVP)

| Feature | Disposition |
|---------|-------------|
| Orbit Policy Engine (OPA) integration | Deferred — Post-MVP Phase |
| Orbit Event Spine integration | Deferred — Post-MVP Phase |
| Orbit Credential Broker integration | Deferred — Post-MVP Phase |
| Orbit OIDC/SAML identity integration | Deferred — Post-MVP Phase |
| Response caching (simple or semantic) | P1 |
| Circuit breaker pattern | P1 |
| Latency-based or performance-based routing | P1 |
| Model group aliases | P1 |
| Embedding model endpoints (`/v1/embeddings`) | P1 |
| Image generation endpoints | Future |
| Support for local/self-hosted models (Ollama, vLLM) | Future |
| Custom model fine-tuning or hosting | Never (out of scope for Orbit) |

---

## Requirements

### Functional Requirements

**P0 — Must Have (MVP):**
- [ ] OpenAI-compatible `POST /v1/chat/completions` endpoint
- [ ] Streaming support (SSE) on chat completions
- [ ] AWS Bedrock connector (BYOC — IAM/access key credentials)
- [ ] Google Vertex AI connector (BYOC — service account credentials)
- [ ] OpenAI connector (BYOC — API key)
- [ ] Multimodal pass-through for providers that support it
- [ ] User-scoped and application-scoped access token generation
- [ ] Token revocation
- [ ] Lowest-cost routing strategy
- [ ] Failover chain routing strategy
- [ ] Retries with exponential backoff (configurable, default max 3)
- [ ] Connection pooling per provider
- [ ] Provider pricing cache: poll AWS Price List API and GCP Cloud Billing Catalog API on configurable TTL (default 1h); OpenAI prices manually configured in config
- [ ] Per-request cost tracking (input + output tokens × cached price table)
- [ ] Per-request token usage tracking
- [ ] Structured request/response metadata logging
- [ ] Usage query API (by token, model, provider, time range)

**P1 — Should Have (Post-MVP):**
- [ ] Orbit Policy Engine integration (OPA-based model access control)
- [ ] Orbit Event Spine integration (structured event emission per request)
- [ ] Orbit Credential Broker integration (replace BYOC with brokered credentials)
- [ ] Per-token rate limiting (RPM and TPM)
- [ ] Circuit breaker pattern per provider
- [ ] Response caching (in-memory, then Redis)
- [ ] Latency-based routing strategy
- [ ] Model group aliases (map logical name → provider/model)
- [ ] Embedding model support (`/v1/embeddings`)

**P2 — Nice to Have:**
- [ ] Request body logging with PII redaction pipeline
- [ ] Cost alerts and budget thresholds per token
- [ ] A/B routing (split traffic across providers for comparison)
- [ ] Local model support (Ollama, vLLM)
- [ ] Admin UI for token management and usage dashboards

### Non-Functional Requirements

**Performance:**
- Gateway request overhead ≤ 50ms p95 (excluding provider response time)
- Support ≥ 1,000 concurrent streaming connections per gateway instance
- Connection pool reuse eliminates per-request TLS handshake overhead

**Security:**
- Provider credentials stored as environment variables or config files, never in database
- Access tokens stored as SHA-256 hashes; raw token returned only at creation
- TLS required for all outbound provider connections
- Request body logging disabled by default (opt-in per deployment)
- All 401/403/429 responses include no information beyond the error code

**Reliability:**
- 99.9% successful routing rate with failover across configured providers
- Graceful degradation: if a provider is unavailable, failover chain absorbs the failure
- Stateless design: gateway restart loses no in-flight data (requests are idempotent via retry)
- Streaming connections handle provider disconnects with appropriate error propagation

**Scalability:**
- Horizontal scaling via stateless instances behind a load balancer
- PostgreSQL for usage/cost persistence; read replicas supported for usage queries

**Observability:**
- Structured JSON logs on every request (timestamp, token ID, model, provider, status, latency, tokens, cost, error)
- Prometheus metrics endpoint: request rate, error rate, latency percentiles, token usage, cost per provider
- Health check endpoint (`GET /healthz`) for load balancer integration

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Requirement | MVP Impact |
|------------|--------|-------------|------------|
| PostgreSQL | Available | Token storage, usage records | Required |
| Orbit Identity (OIDC/SAML) | Planned | Full identity integration | **Deferred** — MVP uses gateway-issued tokens |
| Orbit Policy Engine (OPA) | Planned | Model access control | **Deferred** — Post-MVP |
| Orbit Credential Broker | Planned | Brokered provider credentials | **Deferred** — MVP uses BYOC config |
| Orbit Event Spine | Planned | Structured event emission | **Deferred** — MVP uses structured logs |

### External Dependencies

| Dependency | Purpose | Risk |
|------------|---------|------|
| AWS Bedrock API | Bedrock provider connector | API changes; mitigated by connector versioning |
| Google Vertex AI API | Vertex provider connector | API changes; mitigated by connector versioning |
| OpenAI API | OpenAI provider connector | API changes; mitigated by connector versioning |
| Redis (optional) | Distributed rate limiting (P1) | Required when rate limiting is implemented at scale |

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Provider API changes break connectors | High | Medium | Connector versioning; integration test suite against provider APIs; changelog monitoring |
| Cost tracking inaccuracy leads to billing surprises | High | Medium | Use provider-reported token counts as source of truth; flag requests where counts are estimated; reconciliation tooling |
| Streaming complicates retry/failover logic | Medium | Medium | Failover only triggers before first byte; document the constraint clearly; test with slow/failing provider stubs |
| Rate limiting state inconsistency under horizontal scale | Medium | Low | Rate limiting is P1; when implemented, in-process for single instance and Redis-backed for multi-instance |
| Gateway becomes single point of failure | High | Medium | Stateless design enables horizontal scaling; health checks for load balancer; document multi-instance deployment |
| BYOC credentials misconfigured by operators | Medium | High | Clear error messages on startup credential validation; docs with IAM policy templates for Bedrock/Vertex |
| Provider pricing changes make cost table stale | Low | High | Cost table is configurable without restart; document update procedure; consider automated pricing sync as P1 |

---

## Alternatives Considered

### Alternative 1: Use LiteLLM Proxy

**Description:** Deploy LiteLLM as the gateway layer and integrate Orbit components around it.

**Why not chosen:**
- Deep integration with Orbit identity and policy evaluation requires gateway internals access
- Would require maintaining a fork or relying on a plugin system not designed for Orbit's architecture
- LiteLLM's 100+ provider model is broader than Orbit's BYOC requirement
- Does not align with Orbit's nervous system architecture (just a router, not part of the control plane)

### Alternative 2: Use Portkey

**Description:** Use Portkey's open-source gateway as Orbit's model layer.

**Why not chosen:**
- Portkey is a complete commercial product, not a composable component
- Cannot embed deeply with Orbit credential broker or event spine
- License and control concerns for core infrastructure
- Building our own enables the control plane integration story from day one

### Alternative 3: Direct Provider Calls (No Gateway)

**Description:** Provide SDK wrappers for each provider; agents call providers directly.

**Why not chosen:**
- Distributes credential management — security risk and operational overhead
- No centralized policy enforcement point for future governance features
- Every agent implements retries and failover independently
- Cannot centralize cost tracking or enforce rate limits

---

## Open Questions

| # | Question | Status | Resolution |
|---|----------|--------|------------|
| 1 | Streaming support timeline | **Resolved** | Streaming is P0; ships with MVP |
| 2 | Routing strategies for MVP | **Resolved** | Lowest-cost and failover chains are P0 |
| 3 | Provider priority for MVP | **Resolved** | AWS Bedrock, Google Vertex AI, OpenAI |
| 4 | Auth approach for MVP | **Resolved** | Gateway-issued tokens (user + app scoped); Orbit identity integration deferred |
| 5 | Policy evaluation at gateway | **Resolved** | Deferred to Post-MVP; not in MVP scope |
| 6 | Multimodal support | **Resolved** | Supported via provider pass-through; no normalization in MVP |
| 7 | Cost table maintenance | **Resolved** | AWS Bedrock and Google Vertex AI both expose pricing APIs (AWS Price List API and GCP Cloud Billing Catalog API); gateway polls both on a configurable TTL (default 1h) and caches results in-memory. OpenAI has no pricing API — OpenAI prices are manually set in config and reloaded without restart. On pricing API failure, last cached prices are used and a staleness warning is logged. |
| 8 | Rate limiting | **Resolved** | Moved to P1. Not in MVP scope. |

---

## Implementation Phases

### Phase 1: Foundation (Target: Week 1–2)

**Goal:** Working gateway with OpenAI provider, token auth, and streaming.

- Gateway HTTP server with OpenAI-compatible `POST /v1/chat/completions`
- Non-streaming and streaming (SSE) support
- OpenAI provider connector
- Token authentication (user + app token creation, validation, revocation)
- Basic request/response logging (metadata)
- Health check endpoint

**Exit Criteria:** Can make streaming and non-streaming completions via the gateway using an OpenAI API key, authenticated with a gateway token.

---

### Phase 2: Multi-Provider (Target: Week 3–4)

**Goal:** All three BYOC providers live; multimodal pass-through working.

- AWS Bedrock connector (IAM credential support; response normalization)
- Google Vertex AI connector (service account support; response normalization)
- Multimodal content pass-through (images/documents in request body forwarded to providers)
- Connection pooling per provider (configurable pool size)
- Provider error normalization (standard error types for routing engine)

**Exit Criteria:** Can route completions to Bedrock and Vertex AI via the gateway; multimodal requests work with Vertex/Bedrock models that support them.

---

### Phase 3: Routing & Reliability (Target: Week 5–6)

**Goal:** Smart routing, failover, and retries operational.

- Routing configuration schema (routing groups, cost table, failover chains)
- Lowest-cost routing strategy (select cheapest provider/model in group per request)
- Failover chain routing strategy (ordered provider/model list, advance on failure)
- Retries with exponential backoff and jitter (configurable max attempts)
- Routing decision logging (provider selected, strategy, retries attempted)

**Exit Criteria:** Failover chain automatically routes to secondary on primary failure; lowest-cost routing selects cheapest provider.

---

### Phase 4: Observability (Target: Week 7–8)

**Goal:** Complete cost and token tracking; usage query API; Prometheus metrics.

- Per-request cost tracking (input + output tokens × configurable price table)
- Token usage persistence to PostgreSQL
- `GET /v1/usage` API with filtering by token, model, provider, time range
- Prometheus metrics endpoint (request rate, error rate, p50/p95/p99 latency, token usage, cost)
- Configurable request body logging (opt-in, for debugging)
- Cost estimation for streaming responses (from SSE `usage` final chunk)

**Exit Criteria:** Every request has cost and token usage recorded; usage is queryable; Prometheus scrape returns all key metrics.

---

### Post-MVP: Orbit Integration Phase

**Goal:** Replace minimal auth and BYOC with Orbit control plane components.

- Orbit Identity integration (replace gateway tokens with OIDC/SAML-backed identity)
- Orbit Credential Broker integration (replace BYOC config with brokered credentials)
- Orbit Policy Engine integration (OPA-based model access control before each request)
- Orbit Event Spine integration (structured events for every gateway request/response)
- Circuit breaker pattern per provider
- Response caching (in-memory, Redis)

---

## Timeline and Milestones

| Milestone | Target Date | Status |
|-----------|------------|--------|
| PRD Approved | 2025-01-27 | ✅ |
| ADR Created | 2025-01-28 | Pending |
| Phase 1 Complete (Foundation) | 2025-02-07 | Pending |
| Phase 2 Complete (Multi-Provider) | 2025-02-21 | Pending |
| Phase 3 Complete (Routing & Reliability) | 2025-03-07 | Pending |
| Phase 4 Complete (Observability) | 2025-03-21 | Pending |
| MVP Alpha Release | 2025-03-28 | Pending |
| Post-MVP Integration Phase Kickoff | 2025-04-01 | Pending |

---

## Appendix

### Related Documents

- [ADR-TBD](../adr/TBD-model-gateway-architecture.md) — Architecture decision record (to be created)
- [Orbit Architecture](../architecture.md) — Overall system design
- [Competitive Analysis](../competitive-analysis.md) — LiteLLM, Portkey, LangSmith analysis
- [Competitive Insights Summary](../competitive-insights-summary.md) — Strategic positioning

### References

- [OpenAI API Reference](https://platform.openai.com/docs/api-reference) — API compatibility target
- [AWS Bedrock API](https://docs.aws.amazon.com/bedrock/latest/APIReference/) — Bedrock connector reference
- [Google Vertex AI API](https://cloud.google.com/vertex-ai/docs/reference) — Vertex connector reference
- [LiteLLM Documentation](https://docs.litellm.ai/) — Model gateway reference and competitive context
- [Portkey AI Gateway](https://portkey.ai/docs/product/ai-gateway) — Enterprise gateway reference and competitive context

### Glossary

| Term | Definition |
|------|-----------|
| BYOC | Bring Your Own Credentials — operators supply cloud credentials/API keys; Orbit does not custody them |
| Failover Chain | An ordered list of (provider, model) pairs; the gateway advances to the next on failure |
| Lowest-Cost Routing | Routing strategy that selects the provider/model combination with the lowest per-token price |
| Connection Pool | Pre-established HTTP connections to a provider, reused across requests |
| Routing Group | A logical model name (e.g., "claude-sonnet") that maps to one or more provider/model options |
| SSE | Server-Sent Events — the streaming protocol used by OpenAI and supported by the gateway |
| Token (Access) | A gateway-issued authentication credential (not a language model token) |
| Token (LLM) | A unit of text in a language model's vocabulary, used for cost calculation |
| Nervous System | Orbit's architectural metaphor — the gateway is the sensory-motor interface |
| Event Spine | Orbit's event-driven signaling system (integrated Post-MVP) |
| Credential Broker | Orbit component that manages secrets/API keys with zero-trust model (integrated Post-MVP) |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
