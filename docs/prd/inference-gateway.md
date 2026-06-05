# Inference Gateway — Product Requirements Document

**Status:** Implemented  
**Created:** 2025-01-26  
**Last Updated:** 2025-07-14  
**Supersedes:** `docs/prd/model-gateway.md` (Model Gateway PRD — retired)  
**Owner:** Orbit Core Team  
**Stakeholders:** Orbit Core Team, Agent Developers, Enterprise Platform Operators

---

## Executive Summary

The Inference Gateway is Orbit's unified interface layer between agents and AI model providers. It exposes an OpenAI-compatible API that routes requests to provider-specific backends — AWS Bedrock, Google Vertex AI, Azure AI, and OpenAI direct — using a "Bring Your Own Provider" (BYOP) model where operators supply their own cloud credentials at deployment time.

Authentication uses Orbit's existing platform token system ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) from day one — there is no separate credential layer. Any principal with a valid Orbit platform token (agent, human, or service) can call the inference API. Cost and token usage are attributed at full principal resolution: per-tenant, per-principal, and per-principal-type.

The Policy Engine integration point is designed into the architecture from launch. Until the Policy Engine (OPA) ships, all models are available to all authenticated principals. Once OPA is operational, model access will be governed by policy without any gateway changes.

MVP delivers production reliability (streaming, retries, failover, connection pooling), intelligent routing (lowest-cost and failover chains), and comprehensive cost and token observability with rich attribution. Per-principal rate limiting is P1 (Post-MVP).

---

## Problem Statement

### Current State

Orbit is the control plane for AI agents. The platform layers (identity, policy, credentials, events) are still being established. There is currently no standardised inference interface. Each agent developer must implement provider-specific integrations from scratch, handle reliability patterns manually, and solve credential distribution in isolation.

### User Pain Points

**For Agent Developers:**
- Must implement provider-specific integrations for each model (OpenAI, Bedrock, Vertex AI, Azure AI, etc.)
- Need to handle retries, timeouts, and failovers manually in every agent
- Cannot switch models or providers without code changes
- No standardised way to track costs or token usage across providers

**For Platform Operators:**
- No visibility into which models agents are calling or at what cost
- Cannot enforce rate limits or model access policy on a per-tenant or per-principal basis
- Difficult to configure failover or cost-optimised routing centrally
- Provider API credentials distributed to many consumers increases blast radius of a leak

**For Security Teams:**
- Provider API keys must currently be distributed directly to agent applications
- No centralised audit point for model access
- Risk of credential leakage through agent logs, errors, or environment variables

### Business Impact

- **Development friction:** Every agent developer solves the same integration problem independently
- **Operational risk:** Without a unified reliability layer, provider failures cause agent failures
- **Cost opacity:** No visibility into cross-provider, cross-tenant spend makes optimisation impossible
- **Security exposure:** Distributed credential management increases attack surface

---

## Goals and Objectives

### Primary Goals

1. **Unified Provider Interface:** A single OpenAI-compatible API endpoint that routes to AWS Bedrock, Google Vertex AI, Azure AI, and OpenAI direct through a BYOP model — operators supply their own cloud credentials or API keys at deployment time.
2. **Platform Identity Integration:** Authentication uses Orbit's platform token system ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) from day one. Any authenticated Orbit principal — agent, human, or service — can call the gateway without a separate credential.
3. **Policy-Ready Architecture:** The Policy Engine integration point is built into the gateway from launch. Model access control will activate automatically when the OPA-based Policy Engine ships — no gateway changes required.
4. **Production Reliability:** Streaming support, automatic retries with exponential backoff, failover chains, and connection pooling built in from day one.
5. **Intelligent Routing:** Lowest-cost routing and explicit failover chains as the two primary routing strategies.
6. **Rich Observability:** Full cost and token tracking attributed at per-tenant, per-principal, and per-principal-type granularity on every request.

### Success Metrics

**Reliability:**
- 99.9% successful completion rate for inference requests (with automatic failover)
- Gateway overhead ≤ 50ms p95 (excluding provider response latency)
- Zero request data loss during provider transient failures (retry handles transparently)

**Developer Experience:**
- Single API endpoint serves all supported providers and their models
- Zero provider-specific code required in agent applications
- Drop-in compatibility with OpenAI SDK — agents use the same client they already know
- Authenticated via the platform token agents already carry (no additional credential)

**Cost and Token Observability:**
- Per-request cost and token tracking with ≥ 99% accuracy versus provider-reported figures
- Cost and usage queryable by tenant, principal, principal type, model, provider, and time period

**Access Control:**
- 100% of requests authenticated via Orbit platform token
- Policy enforcement point built and stubbed from day one — activates when Policy Engine ships

---

## Non-Goals (MVP)

- Policy Engine (OPA) enforcement — integration point is designed and stubbed; enforcement activates when OPA ships
- Orbit Event Spine integration — deferred to Post-MVP
- Provider credential management via Helm chart — deferred to the infrastructure ADR (not yet in this branch)
- Advanced load balancing (latency-based, performance-based) — P1
- Circuit breaker pattern — P1
- Response caching (simple or semantic) — P1
- Model group aliases — P1
- Per-principal rate limiting (RPM/TPM) — P1
- Anthropic direct API as a separate provider — Claude models are available via AWS Bedrock; the Anthropic Messages API wire format (for Claude Code compatibility) is P1
- Custom model fine-tuning or hosting
- Built-in RAG or vector database integration
- Model evaluation or benchmarking frameworks

---

## User Stories

### US-001: Agent Inference via Platform Token

**As an** agent workload with a valid Orbit platform token (attested via SPIFFE/SPIRE),  
**I want to** call the Inference Gateway using the same Bearer token I already carry,  
**So that** I can make model inference calls without any additional credentials or configuration.

**Acceptance Criteria:**
- [ ] Requests authenticated via Orbit platform JWT (`Authorization: Bearer <platform-token>`)
- [ ] Token validated against Orbit's JWKS endpoint (signature, expiry, JTI blocklist via [unified-identity-model](../adr/2026-03-05-unified-identity-model.md))
- [ ] Principal context (`tenant_id`, `principal_id`, `principal_type`) extracted from token claims
- [ ] Requests without a valid platform token receive `401 Unauthorized`
- [ ] Token validation is performed at the gateway layer — application handlers receive pre-validated principal context

---

### US-002: Human Developer Inference Access

**As a** human developer authenticated to the Orbit Control Plane Portal via OIDC,  
**I want to** call the Inference Gateway using my existing Orbit platform token,  
**So that** I can test inference calls during development without managing a separate API credential.

**Acceptance Criteria:**
- [ ] Human principals (OIDC-federated) authenticate the same way as agent principals
- [ ] Usage attributed to `principal_type: HUMAN` with the human's `principal_id` in logs and usage records
- [ ] No separate token or credential flow required beyond the existing Orbit session

---

### US-003: Bring Your Own Provider (Operator Configuration)

**As a** platform operator,  
**I want to** configure the gateway with my own AWS Bedrock credentials, Google Vertex AI service account, Azure AI endpoint, or OpenAI API key at deployment time,  
**So that** I can leverage my existing enterprise agreements, data residency controls, and security posture.

**Acceptance Criteria:**
- [ ] AWS Bedrock: configured via IAM role/access key credentials (environment variables)
- [ ] Google Vertex AI: configured via GCP service account credentials (environment variable / secret)
- [ ] Azure AI: configured via Azure API key and endpoint URL (environment variables)
- [ ] OpenAI: configured via API key (environment variable)
- [ ] Provider credentials are read at startup from environment variables only — never stored in database or YAML config
- [ ] Gateway startup validates provider credential presence and logs clear errors for misconfigured providers
- [ ] Credential provisioning mechanism via Helm chart is deferred to the infrastructure ADR

---

### US-004: OpenAI-Compatible API with Streaming

**As an** agent developer,  
**I want to** use the same OpenAI SDK or HTTP client I already know,  
**So that** I can integrate the gateway without learning new APIs or changing my agent code beyond the base URL.

**Acceptance Criteria:**
- [ ] `POST /v1/chat/completions` accepts OpenAI-format request bodies
- [ ] Non-streaming responses return OpenAI-format JSON
- [ ] Streaming responses return Server-Sent Events (SSE) in OpenAI stream format
- [ ] Model name in the request (e.g. `anthropic.claude-sonnet-4-5`, `gemini-2.0-flash`, `gpt-4o`, `meta-llama-3-70b-instruct`) is routed to the appropriate provider
- [ ] Multimodal requests (vision, documents) are passed through to providers that support them

---

### US-005: Multi-Provider Routing

**As a** platform operator,  
**I want to** configure which provider handles which model requests,  
**So that** I can direct different model families to the cloud provider I have agreements with.

**Acceptance Criteria:**
- [ ] Requests route to AWS Bedrock, Google Vertex AI, Azure AI, or OpenAI direct based on routing configuration
- [ ] All four providers are accessible via the same OpenAI-compatible request format
- [ ] Provider selection is recorded in request logs

---

### US-006: Routing — Lowest Cost

**As a** platform operator,  
**I want to** configure a routing policy that selects the lowest-cost provider/model combination for a given request,  
**So that** I can minimise model spend without manual model selection in every agent.

**Acceptance Criteria:**
- [ ] Routing engine maintains a cost table (per-token prices) for each configured provider and model
- [ ] When a request's model field maps to a routing group, the engine selects the lowest-cost viable option
- [ ] Cost table is configurable and reloadable without gateway restart
- [ ] Routing decision is recorded in request logs (provider selected, strategy used)

---

### US-007: Routing — Failover Chains

**As a** platform operator,  
**I want to** define ordered failover chains (primary → secondary → tertiary),  
**So that** if the primary provider is unavailable or returns an error, the gateway automatically retries with the next option.

**Acceptance Criteria:**
- [ ] Failover chains are configurable per routing group
- [ ] On provider error (5xx, timeout, rate limit), gateway tries the next provider in the chain
- [ ] Failover attempts are logged with the original error and the failover target
- [ ] If all providers in the chain fail, gateway returns an appropriate error to the caller
- [ ] Failover is suppressed once streaming has begun (first byte received from provider)

---

### US-008: Retries with Exponential Backoff

**As an** agent developer,  
**I want** the gateway to automatically retry transient provider failures,  
**So that** my agent does not fail due to brief provider instability.

**Acceptance Criteria:**
- [ ] Configurable max retry attempts (default: 3)
- [ ] Exponential backoff with jitter between retries
- [ ] Retries only on retriable errors (5xx, timeouts, rate limit 429 from provider)
- [ ] Non-retriable errors (4xx client errors) are not retried
- [ ] Total retry attempts and backoff durations are logged per request

---

### US-009: Connection Pooling

**As a** platform operator,  
**I want** the gateway to maintain persistent connection pools to each provider,  
**So that** we avoid TLS handshake overhead on every request and reduce tail latency.

**Acceptance Criteria:**
- [ ] One HTTP connection pool per configured provider
- [ ] Pool size configurable per provider
- [ ] Connections are reused across inference requests
- [ ] Pool health is monitored; stale connections are recycled

---

### US-010: Per-Request Cost and Token Tracking with Full Attribution

**As an** operations engineer or finance stakeholder,  
**I want** every inference request tracked with input tokens, output tokens, estimated cost, and full principal attribution,  
**So that** I can understand spend by tenant, agent, principal type, model, and provider.

**Acceptance Criteria:**
- [ ] Every request records: `tenant_id`, `principal_id`, `principal_type`, model, provider, input tokens, output tokens, total tokens, estimated cost (USD), latency (ms), timestamp
- [ ] Token counts sourced from provider response where available; estimated via tokenizer where not
- [ ] Cost calculated using a configurable per-model price table ($/1K input tokens, $/1K output tokens)
- [ ] Streaming requests capture token usage from the final SSE `usage` chunk
- [ ] Usage queryable by `tenant_id`, `principal_id`, `principal_type`, model, provider, and time range
- [ ] AWS Bedrock and Google Vertex AI pricing polled from their respective pricing APIs on configurable TTL (default: 1h); last cached price used on API failure with staleness warning logged
- [ ] Azure AI and OpenAI prices manually configured in config and reloadable without restart

---

### US-011: Structured Request/Response Logging

**As a** platform operator,  
**I want** structured logs for every request and response passing through the gateway,  
**So that** I can debug issues, audit access, and understand traffic patterns at the principal level.

**Acceptance Criteria:**
- [ ] Every request logs: timestamp, `tenant_id`, `principal_id`, `principal_type`, model requested, provider routed to, routing strategy, HTTP status, latency (ms), input/output token count, estimated cost, retry count, error (if any)
- [ ] Configurable log levels: debug logs full request/response body; production logs metadata only
- [ ] Request body logging is opt-in due to potential PII in prompts
- [ ] Logs are structured JSON, emitted via Orbit's standard `*logging.Logger` — compatible with standard log aggregation pipelines

---

### US-012: Policy Engine Integration Point

**As a** platform operator (future state),  
**I want** model access to be governed by the Orbit Policy Engine,  
**So that** I can restrict which agents or tenants can call which models, enforce cost thresholds, or require approval for expensive models.

**Acceptance Criteria (current — stub):**
- [ ] Gateway calls a `PolicyEvaluator` interface before routing each request
- [ ] The default implementation allows all requests (stub — policy engine not yet shipped)
- [ ] The interface accepts: principal context, requested model, provider, estimated cost per token
- [ ] The interface returns: allow/deny + reason

**Acceptance Criteria (future — when OPA ships):**
- [ ] OPA-backed implementation replaces the stub with no gateway code changes required
- [ ] Policy can restrict by: principal type, tenant, specific principal ID, model family, provider, cost threshold
- [ ] Denied requests return `403 Forbidden` with a structured reason

---

### US-013: Health Check and Metrics

**As a** platform operator,  
**I want** a health check endpoint and Prometheus metrics from the gateway,  
**So that** load balancers and monitoring systems can observe gateway health and performance.

**Acceptance Criteria:**
- [ ] `GET /healthz` returns `200 OK` when the gateway is ready to serve; `503` during startup or degraded state
- [ ] Prometheus metrics endpoint exposes: request rate, error rate, latency percentiles (p50/p95/p99), token usage rate, cost per provider, active streaming connections
- [ ] Metrics labelled by: provider, model, routing strategy, principal type, HTTP status

---

## Proposed Solution

### High-Level Architecture

```
Agent / Human / Service Principal
          │
          │  Orbit Platform Token (Bearer JWT)
          │  OpenAI-compatible HTTP API
          ▼
┌─────────────────────────────────────────────────┐
│              Orbit Gateway (Edge Envoy)          │
│  - Platform token validation (sig, exp, JTI)    │
│  - Layer 1 policy enforcement ([universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md))        │
│  - Principal context injected into headers      │
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│             Inference Gateway Service            │
│                                                  │
│  ┌─────────────────────────────────────────┐    │
│  │           Gateway API Handler            │    │
│  │  - Principal context extraction          │    │
│  │  - Request validation                    │    │
│  │  - Streaming (SSE)                       │    │
│  └────────────────────┬────────────────────┘    │
│                        │                         │
│  ┌─────────────────────▼──────────────────┐     │
│  │          Policy Evaluator              │     │
│  │  - Stub: allow all (until OPA ships)   │     │
│  │  - Interface: ready for OPA plug-in    │     │
│  └─────────────────────┬──────────────────┘     │
│                        │                         │
│  ┌─────────────────────▼──────────────────┐     │
│  │           Routing Engine               │     │
│  │  - Lowest cost strategy                │     │
│  │  - Failover chain strategy             │     │
│  │  - Retries / exponential backoff       │     │
│  └────┬──────────┬──────────┬─────────┬───┘     │
│       │          │          │         │          │
│  ┌────▼──┐  ┌───▼───┐ ┌───▼───┐ ┌───▼──────┐  │
│  │Bedrock│  │Vertex │ │Azure  │ │ OpenAI   │  │
│  │Connec.│  │AI Con.│ │AI Con.│ │ Direct   │  │
│  └────┬──┘  └───┬───┘ └───┬───┘ └───┬──────┘  │
│       │          │          │         │          │
│  ┌────▼──────────▼──────────▼─────────▼──────┐  │
│  │          Observability Layer               │  │
│  │  - Cost tracking (per tenant/principal)    │  │
│  │  - Token usage tracking                    │  │
│  │  - Structured request/response logging     │  │
│  │  - Prometheus metrics                      │  │
│  └────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
                       │
              ┌────────▼────────┐
              │   PostgreSQL    │
              │  (usage records)│
              └─────────────────┘
```

### Key Design Decisions

**Platform Token Authentication:**  
The Inference Gateway accepts Orbit platform JWTs only. No separate token system is built or maintained. Token validation occurs at the Orbit Gateway (Envoy, Layer 1 per [universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md)) before the request reaches the Inference Gateway service. The service extracts principal context from headers injected by Envoy after validation. This keeps the Inference Gateway's authentication concern minimal and consistent with all other Orbit services.

**Principal-Level Attribution:**  
Every inference request is attributed to its full principal context — `tenant_id`, `principal_id`, `principal_type`. For agents, this maps to the attested SPIFFE identity. For humans, to the federated OIDC identity. For service principals, to the platform service identity. This enables genuinely granular cost and usage analysis that was not possible with the previous per-gateway-token attribution model.

**Policy Engine Interface from Day One:**  
The `PolicyEvaluator` interface is defined and called on every request from launch. The default implementation is a stub that allows all requests. When the OPA-backed Policy Engine ships, it is plugged in behind the interface — no gateway changes required. This avoids the pattern of "design the enforcement point later" that leads to architectural debt.

**BYOP Model (Bring Your Own Provider):**  
The gateway does not custody provider credentials. Operators configure AWS IAM credentials, GCP service accounts, Azure API keys, or OpenAI API keys as environment variables at deployment time. The mechanism for supplying these at deploy time (Helm chart configuration, secrets injection) is deferred to the infrastructure ADR.

**OpenAI-Compatible API Surface:**  
The gateway exposes the OpenAI `chat/completions` endpoint format. All four providers expose chat completion interfaces; the connector layer translates request and response formats. Agents need zero provider-specific code.

**Streaming as First-Class:**  
Streaming (SSE) ships alongside regular completions from day one. Failover is suppressed once the first byte of a stream response is received — the connection is owned by the provider at that point. Token counting for cost tracking uses the final `usage` chunk in the stream.

**Stateless Gateway:**  
The gateway is stateless. Usage records are persisted to PostgreSQL. This enables horizontal scaling without coordination overhead. Rate limiting (P1) will use Redis-backed counters when implemented.

### Provider Connector Design

Each connector implements a shared interface:

```go
type ProviderConnector interface {
    // ChatCompletion executes a non-streaming completion request.
    ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)

    // ChatCompletionStream executes a streaming completion request,
    // returning a channel of response chunks.
    ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamChunk, error)

    // Name returns the provider identifier (e.g. "bedrock", "vertexai", "azureai", "openai").
    Name() string

    // ModelCost returns the per-token pricing for a given model.
    ModelCost(model string) (*ModelPricing, error)
}
```

Provider connectors are responsible for:
- Translating OpenAI-format requests to provider-native format
- Translating provider-native responses back to OpenAI format
- Normalising error types (so the routing engine can determine retryability)
- Multimodal pass-through (vision/document content blocks forwarded as-is where provider supports)

**Provider Capability Summary:**

| Provider | Auth Mechanism | Notable Models | Multimodal |
|---|---|---|---|
| AWS Bedrock | IAM role / access key (env var) | Claude (Anthropic), Llama (Meta), Mistral, Titan | ✓ |
| Google Vertex AI | Service account JSON (env var / secret) | Gemini, Claude on Vertex, Llama | ✓ |
| Azure AI | API key + endpoint URL (env var) | GPT-4o, o1 (Azure OpenAI), Llama, Mistral, Phi | ✓ |
| OpenAI direct | API key (env var) | GPT-4o, o1, GPT-4.1 | ✓ |

### Policy Evaluator Interface

```go
// PolicyEvaluator is the interface between the Inference Gateway and the Orbit Policy Engine.
// The default implementation is a stub that permits all requests.
// When the OPA-backed Policy Engine ships, it replaces the stub via dependency injection.
type PolicyEvaluator interface {
    // EvaluateInferenceRequest returns whether the principal is permitted to
    // make the described inference request. A nil error with Allow=true permits
    // the request. A nil error with Allow=false returns 403 with the Reason.
    EvaluateInferenceRequest(ctx context.Context, req *InferencePolicyRequest) (*PolicyDecision, error)
}

type InferencePolicyRequest struct {
    Principal     Principal  // tenant_id, principal_id, principal_type, claims
    Model         string     // requested model identifier
    Provider      string     // target provider name
    EstimatedCost float64    // estimated cost per 1K tokens for this model/provider
}

type PolicyDecision struct {
    Allow  bool
    Reason string // populated on deny; returned to caller in 403 response
}
```

### Routing Engine

The routing engine receives a request and routing configuration and returns an ordered list of `(provider, model)` targets. It tries each target in order, applying retry logic per target before advancing to the next.

**Lowest-Cost Routing:**  
Given a routing group (e.g. `"claude-sonnet"`), the engine queries the cost table for all configured providers that serve a model in that group and selects the cheapest per-output-token. Recalculated per request so price table updates take immediate effect.

**Failover Chain Routing:**  
Given an ordered list of `(provider, model)` pairs, the engine attempts each in sequence. A failure (provider error, timeout, upstream rate limit) triggers the next entry. The chain is exhausted before returning an error to the caller.

### Usage Record Schema

```
inference_usage
├── id             (uuid)
├── tenant_id      (from platform token — non-nullable)
├── principal_id   (from platform token — non-nullable)
├── principal_type (AGENT | HUMAN | SERVICE — from platform token)
├── model          (model identifier as requested)
├── provider       (provider that served the request: bedrock | vertexai | azureai | openai)
├── routing_strategy (lowest_cost | failover_chain | direct)
├── input_tokens   (integer)
├── output_tokens  (integer)
├── total_tokens   (integer)
├── estimated_cost_usd (numeric — input_tokens × input_price + output_tokens × output_price)
├── latency_ms     (integer — time from request receipt to response completion)
├── retry_count    (integer — retries attempted before success or final failure)
├── status         (success | provider_error | policy_denied | auth_error)
└── created_at     (timestamp)
```

---

## Scope

### In Scope (MVP)

**API:**
- `POST /v1/chat/completions` — non-streaming and streaming (SSE)
- `GET /v1/usage` — query usage and cost records with filtering
- `GET /healthz` — liveness/readiness probe
- `GET /metrics` — Prometheus metrics scrape endpoint

**Authentication:**
- Orbit platform token validation (JWT — signature, expiry, JTI blocklist)
- Principal context extraction (`tenant_id`, `principal_id`, `principal_type`)

**Provider Support (BYOP):**
- AWS Bedrock (all models accessible via Bedrock API)
- Google Vertex AI (all models accessible via Vertex AI API)
- Azure AI (Azure OpenAI models + Azure-hosted open models)
- OpenAI direct API

**Routing Strategies:**
- Lowest cost (selects cheapest provider/model in a routing group)
- Failover chain (ordered list of provider/model targets, auto-advances on failure)

**Reliability:**
- Retries with exponential backoff and jitter (configurable max attempts, default 3)
- Connection pooling per provider (configurable pool size)
- Streaming-safe failover (failover only before first byte of stream response)

**Policy Engine Integration:**
- `PolicyEvaluator` interface defined and called on every request
- Default stub implementation: allow all (enforced once OPA ships)

**Observability:**
- Per-request usage records persisted to PostgreSQL with full principal attribution
- Structured request/response metadata logging via Orbit's `*logging.Logger`
- Prometheus metrics endpoint
- Provider pricing: AWS and Vertex polled via pricing APIs (configurable TTL, default 1h); Azure AI and OpenAI prices manually configured

### Out of Scope (MVP)

| Feature | Disposition |
|---|---|
| Policy Engine (OPA) enforcement | Interface stubbed — enforcement activates when OPA ships |
| Orbit Event Spine integration | Deferred — Post-MVP |
| Helm chart / credential provisioning mechanism | Deferred — infrastructure ADR |
| Per-principal rate limiting (RPM/TPM) | P1 — Post-MVP |
| Circuit breaker pattern per provider | P1 |
| Response caching (simple or semantic) | P1 |
| Latency-based or performance-based routing | P1 |
| Model group aliases | P1 |
| Embedding model endpoints (`/v1/embeddings`) | P1 |
| Anthropic direct API as a separate provider | Not in scope — Claude available via AWS Bedrock |
| Anthropic Messages API wire format (`POST /v1/messages`) | P1 — enables Claude Code and Anthropic SDK clients |
| Image generation endpoints | Future |
| Local/self-hosted models (Ollama, vLLM) | Future |
| Custom model fine-tuning or hosting | Never — out of scope for Orbit |

---

## Requirements

### Functional Requirements

**P0 — Must Have (MVP):**
- [ ] Orbit platform token validation on every request (JWT sig, expiry, JTI blocklist)
- [ ] Principal context extraction (`tenant_id`, `principal_id`, `principal_type`) from validated token
- [ ] OpenAI-compatible `POST /v1/chat/completions` endpoint
- [ ] Streaming support (SSE) on chat completions
- [ ] `PolicyEvaluator` interface defined, called on every request, stub returns allow-all
- [ ] AWS Bedrock connector (IAM/access key credentials from environment)
- [ ] Google Vertex AI connector (service account credentials from environment)
- [ ] Azure AI connector (API key + endpoint URL from environment)
- [ ] OpenAI direct connector (API key from environment)
- [ ] Multimodal pass-through for providers that support it
- [ ] Lowest-cost routing strategy
- [ ] Failover chain routing strategy
- [ ] Retries with exponential backoff and jitter (configurable, default max 3)
- [ ] Connection pooling per provider (configurable pool size)
- [ ] Provider pricing: AWS Bedrock and Vertex AI polled from pricing APIs (configurable TTL, default 1h); Azure AI and OpenAI prices manually configured in config
- [ ] Per-request usage records persisted to PostgreSQL with full principal attribution
- [ ] Structured request/response metadata logging (Orbit `*logging.Logger`)
- [ ] `GET /v1/usage` API with filtering by tenant, principal, principal type, model, provider, time range
- [ ] `GET /healthz` health check endpoint
- [ ] `GET /metrics` Prometheus metrics endpoint

**P1 — Post-MVP:**
- [ ] OPA-backed `PolicyEvaluator` implementation (plug-in to existing interface)
- [ ] Orbit Event Spine integration (structured event emission per request)
- [ ] Per-principal rate limiting (RPM and TPM, Redis-backed)
- [ ] Circuit breaker pattern per provider
- [ ] Response caching (in-memory, then Redis)
- [ ] Latency-based routing strategy
- [ ] Model group aliases (map logical name → provider/model)
- [ ] Embedding model support (`/v1/embeddings`)
- [ ] Automated pricing sync as scheduled job
- [ ] **Anthropic Messages API dialect** (`POST /v1/messages`) — enables Claude Code and Anthropic SDK clients; edge Envoy `x-api-key` → `Authorization: Bearer` promotion on the `/v1/messages` path; Anthropic SSE event lifecycle serialiser; internal routing and connectors unchanged

**P2 — Nice to Have:**
- [ ] Request body logging with PII redaction pipeline
- [ ] Cost alerts and budget thresholds per tenant or principal
- [ ] A/B routing (split traffic across providers for comparison)
- [ ] Local model support (Ollama, vLLM)
- [ ] Admin UI for usage dashboards and routing configuration

### Non-Functional Requirements

**Performance:**
- Gateway request overhead ≤ 50ms p95 (excluding provider response time)
- Support ≥ 1,000 concurrent streaming connections per gateway instance
- Connection pool reuse eliminates per-request TLS handshake overhead

**Security:**
- Provider credentials loaded from environment variables only — never stored in database or YAML config
- TLS required for all outbound provider connections
- Request body logging disabled by default (opt-in per deployment)
- All `401`/`403` responses contain no information beyond the error code
- Platform token validation follows the JTI blocklist pattern from [unified-identity-model](../adr/2026-03-05-unified-identity-model.md)

**Reliability:**
- 99.9% successful routing rate with failover across configured providers
- Graceful degradation: if a provider is unavailable, failover chain absorbs the failure
- Stateless design: gateway restart loses no in-flight data (requests are idempotent via retry)
- Streaming connections handle provider disconnects with appropriate error propagation

**Scalability:**
- Horizontal scaling via stateless instances behind Orbit Gateway
- PostgreSQL for usage persistence; read replicas supported for usage queries
- Redis required for rate limiting when P1 is implemented

**Observability:**
- Structured JSON logs on every request: timestamp, `tenant_id`, `principal_id`, `principal_type`, model, provider, routing strategy, status, latency, tokens, cost, retry count, error
- Prometheus metrics: request rate, error rate, latency percentiles (p50/p95/p99), token usage, cost per provider, active streaming connections
- All metrics labelled by provider, model, principal type, and HTTP status

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Requirement | Impact |
|---|---|---|---|
| Orbit Platform Token ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) | **Implemented** | Identity and principal extraction | Required — authentication from day one |
| Orbit Gateway / Envoy ([universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md)) | **Designed** | Layer 1 token validation + principal context injection | Required — gateway enforces before service sees request |
| PostgreSQL | **Implemented** | Usage record persistence | Required |
| Policy Engine (OPA) | Planned | Model access control | Deferred — interface stubbed; enforcement activates when OPA ships |
| Orbit Event Spine | Planned | Structured event emission | Deferred — Post-MVP |
| Helm Chart Infrastructure | Planned | Provider credential provisioning at deploy time | Deferred — separate infrastructure ADR |
| Redis | **Implemented** | Rate limiting (when P1 ships) | Not required for MVP |

### External Dependencies

| Dependency | Purpose | Risk |
|---|---|---|
| AWS Bedrock API | Bedrock provider connector | API changes; mitigated by connector versioning |
| AWS Price List API | Automated Bedrock pricing | API unavailability; mitigated by cache + staleness warning |
| Google Vertex AI API | Vertex provider connector | API changes; mitigated by connector versioning |
| GCP Cloud Billing Catalog API | Automated Vertex pricing | API unavailability; mitigated by cache + staleness warning |
| Azure AI API | Azure AI provider connector | API changes; mitigated by connector versioning |
| OpenAI API | OpenAI direct connector | API changes; mitigated by connector versioning |

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| Provider API changes break connectors | High | Medium | Connector versioning; integration test suite against provider APIs; changelog monitoring |
| Cost tracking inaccuracy leads to billing surprises | High | Medium | Use provider-reported token counts as source of truth; flag requests where counts are estimated; reconciliation tooling |
| Streaming complicates retry/failover logic | Medium | Medium | Failover only triggers before first byte; document the constraint clearly; test with slow/failing provider stubs |
| Policy Engine interface becomes a blocking dependency | Medium | Low | Stub implementation allows full MVP without OPA — interface is a thin abstraction, not a runtime dependency |
| Gateway becomes single point of failure | High | Medium | Stateless design enables horizontal scaling; health checks for load balancer; document multi-instance deployment |
| BYOP credentials misconfigured by operators | Medium | High | Clear startup validation and error messages; docs with IAM policy templates for each provider |
| Provider pricing changes make cost table stale | Low | High | AWS and Vertex prices auto-polled with configurable TTL; staleness logged; Azure and OpenAI manually updated via config reload |
| Principal attribution adds overhead to every request | Low | Low | Claims extracted from already-validated JWT in memory — no additional network calls required |

---

## Alternatives Considered

### Alternative 1: Standalone Token System (Previous PRD Approach)

**Description:** Issue gateway-specific tokens (user-scoped and app-scoped) stored in PostgreSQL, independent of Orbit's identity system.

**Why not chosen:**
- Creates a second identity surface to manage, maintain, and secure — directly contradicting Orbit's unified identity model ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md))
- Agents already carry platform tokens via the SPIFFE/SPIRE + Envoy sidecar flow — requiring a second credential is unnecessary friction
- Attribution is coarser (per-gateway-token, not per-principal) — loses `tenant_id`, `principal_type`, and all the richness of the platform token
- Was an explicitly temporary design in the previous PRD, pending identity integration that is now complete

### Alternative 2: Use LiteLLM Proxy

**Description:** Deploy LiteLLM as the gateway layer and integrate Orbit components around it.

**Why not chosen:**
- Deep integration with Orbit identity and policy evaluation requires gateway internals access
- Would require maintaining a fork or relying on a plugin system not designed for Orbit's architecture
- LiteLLM's 100+ provider model is broader than Orbit's BYOP requirement
- Does not align with Orbit's posture — a standalone router, not part of an integrated control plane

### Alternative 3: Use Portkey

**Description:** Use Portkey's open-source gateway as Orbit's inference layer.

**Why not chosen:**
- Portkey is a complete commercial product, not a composable component
- Cannot embed deeply with Orbit principal context, credential broker, or event spine
- License and control concerns for core infrastructure
- Building our own enables the control plane integration story from day one

### Alternative 4: Direct Provider Calls (No Gateway)

**Description:** Provide SDK wrappers for each provider; agents call providers directly.

**Why not chosen:**
- Distributes credential management — security risk and operational overhead
- No centralised policy enforcement point for future governance features
- Every agent implements retries and failover independently
- Cannot centralise cost tracking or enforce rate limits
- Loses principal-level attribution entirely

---

## Open Questions

| # | Question | Status | Resolution |
|---|---|---|---|
| 1 | Streaming support timeline | **Resolved** | Streaming is P0; ships with MVP |
| 2 | Routing strategies for MVP | **Resolved** | Lowest-cost and failover chains are P0 |
| 3 | Provider set for launch | **Resolved** | AWS Bedrock, Google Vertex AI, Azure AI, OpenAI direct |
| 4 | Auth approach | **Resolved** | Orbit platform tokens ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) from day one; no standalone token system |
| 5 | Policy evaluation at gateway | **Resolved** | Interface designed and stubbed from day one; enforcement activates when OPA ships |
| 6 | Multimodal support | **Resolved** | Supported via provider pass-through; no normalisation in MVP |
| 7 | Cost table maintenance | **Resolved** | AWS Bedrock and Vertex AI polled from pricing APIs (configurable TTL, default 1h). Azure AI and OpenAI manually configured in config. On pricing API failure, last cached prices used with staleness warning logged. |
| 8 | Rate limiting | **Resolved** | Moved to P1 — Post-MVP |
| 9 | Attribution granularity | **Resolved** | Per-tenant, per-principal, per-principal-type on every request |
| 10 | Anthropic direct API | **Resolved** | Not in MVP scope as a separate provider — Claude models covered via AWS Bedrock |
| 12 | Anthropic Messages API wire format (Claude Code) | **Resolved** | P1 — adds `POST /v1/messages` dialect; edge Envoy handles `x-api-key` → `Authorization: Bearer` header promotion; agent sidecars already inject `Authorization: Bearer` transparently via ext_proc ([envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md)); routing/connectors unchanged |
| 11 | Credential provisioning mechanism | **Resolved** | Credentials are environment variables; Helm chart mechanism deferred to infrastructure ADR |

---

## Implementation Phases

### Phase 1: Foundation (Target: Week 1–2)

**Goal:** Working gateway with OpenAI provider, platform token auth, policy evaluator stub, and streaming.

- Gateway HTTP server with OpenAI-compatible `POST /v1/chat/completions`
- Non-streaming and streaming (SSE) support
- Platform token validation (JWT sig, expiry, JTI blocklist) — principal context extraction
- `PolicyEvaluator` interface + stub allow-all implementation
- OpenAI direct provider connector
- Basic request/response metadata logging (Orbit `*logging.Logger`)
- `GET /healthz` health check endpoint
- Prometheus metrics endpoint skeleton

**Exit Criteria:** Can make streaming and non-streaming completions via the gateway using an OpenAI API key, authenticated with an Orbit platform token. Policy evaluator interface is called on every request.

---

### Phase 2: Multi-Provider (Target: Week 3–4)

**Goal:** All four BYOP providers live; multimodal pass-through working.

- AWS Bedrock connector (IAM credential support; response normalisation; error normalisation)
- Google Vertex AI connector (service account support; response normalisation)
- Azure AI connector (API key + endpoint support; response normalisation)
- Multimodal content pass-through (images/documents in request body forwarded to providers that support them)
- Connection pooling per provider (configurable pool size)
- Provider error normalisation (standard error types for routing engine)

**Exit Criteria:** Can route completions to all four providers via the gateway. Multimodal requests work with providers that support them.

---

### Phase 3: Routing and Reliability (Target: Week 5–6)

**Goal:** Smart routing, failover, and retries operational.

- Routing configuration schema (routing groups, cost table, failover chains)
- Lowest-cost routing strategy (select cheapest provider/model in a routing group per request)
- Failover chain routing strategy (ordered provider/model list, advance on failure)
- Retries with exponential backoff and jitter (configurable max attempts, default 3)
- Routing decision logging (provider selected, strategy used, retries attempted)
- Streaming-safe failover enforcement (no failover after first byte received)

**Exit Criteria:** Failover chain automatically routes to secondary on primary failure. Lowest-cost routing selects the cheapest configured provider.

---

### Phase 4: Observability (Target: Week 7–8)

**Goal:** Complete cost and token tracking with full principal attribution; usage query API; Prometheus metrics complete.

- Per-request usage records persisted to PostgreSQL (`tenant_id`, `principal_id`, `principal_type`, model, provider, tokens, cost, latency, retries, status)
- `GET /v1/usage` API with filtering by tenant, principal, principal type, model, provider, time range
- Provider pricing: AWS Bedrock and Vertex AI pricing poll (configurable TTL, default 1h); Azure AI and OpenAI manual config
- Prometheus metrics: request rate, error rate, p50/p95/p99 latency, token usage rate, cost per provider — labelled by provider, model, principal type, HTTP status
- Configurable request body logging (opt-in)
- Cost estimation for streaming responses from final SSE `usage` chunk

**Exit Criteria:** Every request has a usage record with full principal attribution. Usage is queryable. Prometheus scrape returns all key metrics.

---

### Post-MVP: Orbit Integration Phase

**Goal:** Connect the gateway deeply into the Orbit control plane.

- OPA-backed `PolicyEvaluator` implementation (model access control per principal, tenant, cost threshold)
- Orbit Event Spine integration (structured event per request — emits to event spine alongside structured logs)
- Per-principal rate limiting (RPM and TPM; Redis-backed for horizontal scale)
- Circuit breaker pattern per provider
- Response caching (in-memory, then Redis)
- **Anthropic Messages API dialect** (`POST /v1/messages`): Anthropic handler, Anthropic SSE serialiser, edge Envoy `x-api-key` → `Authorization: Bearer` header promotion on the `/v1/messages` path; enables Claude Code and all Anthropic SDK clients with zero-code-change integration

---

## Timeline and Milestones

| Milestone | Target Date | Status |
|---|---|---|
| PRD Updated and Approved | 2025-07-14 | In Review |
| ADR Created | TBD | Pending |
| Phase 1 Complete (Foundation + Auth) | TBD | Pending |
| Phase 2 Complete (Multi-Provider) | TBD | Pending |
| Phase 3 Complete (Routing and Reliability) | TBD | Pending |
| Phase 4 Complete (Observability) | TBD | Pending |
| MVP Alpha Release | TBD | Pending |
| Post-MVP Integration Phase Kickoff | TBD | Pending |

---

## Appendix

### Related Documents

- [unified-identity-model](../adr/2026-03-05-unified-identity-model.md) — Unified Principal model and platform token
- [universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md) — Gateway and sidecar enforcement topology
- [credential-aware-outbound-gateway](../adr/2026-03-05-credential-aware-outbound-gateway.md) — Credential broker (Post-MVP integration)
- [inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md) — Inference Gateway architecture decision record
- [Orbit Architecture](../architecture.md) — Overall system design
- [Platform Overview Diagram](../architecture/platform-overview.excalidraw)

### References

- [OpenAI API Reference](https://platform.openai.com/docs/api-reference) — API compatibility target
- [AWS Bedrock API](https://docs.aws.amazon.com/bedrock/latest/APIReference/) — Bedrock connector reference
- [AWS Price List API](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/price-list-query-api.html) — Bedrock pricing source
- [Google Vertex AI API](https://cloud.google.com/vertex-ai/docs/reference) — Vertex connector reference
- [GCP Cloud Billing Catalog API](https://cloud.google.com/billing/docs/reference/rest) — Vertex pricing source
- [Azure AI Inference API](https://learn.microsoft.com/en-us/azure/ai-studio/reference/reference-model-inference-api) — Azure AI connector reference

### Glossary

| Term | Definition |
|---|---|
| BYOP | Bring Your Own Provider — operators supply cloud credentials/API keys; Orbit does not custody them |
| Platform Token | Orbit-issued JWT carrying a normalised Principal — the single credential accepted by all Orbit services ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) |
| Principal | The unified identity abstraction for any Orbit actor: HUMAN, AGENT, or SERVICE |
| Failover Chain | An ordered list of (provider, model) pairs; the gateway advances to the next on failure |
| Lowest-Cost Routing | Routing strategy that selects the provider/model combination with the lowest per-token price |
| Connection Pool | Pre-established HTTP connections to a provider, reused across requests |
| Routing Group | A logical model name (e.g. `"claude-sonnet"`) that maps to one or more provider/model options |
| SSE | Server-Sent Events — the streaming protocol used by OpenAI and supported by the gateway |
| PolicyEvaluator | The interface between the Inference Gateway and the Orbit Policy Engine; stubbed as allow-all until OPA ships |
| Token (LLM) | A unit of text in a language model's vocabulary, used for cost calculation |
| API Dialect | A client-facing wire format (OpenAI-compatible or Anthropic Messages API) — the gateway supports multiple dialects via separate inbound translation handlers sharing a single internal routing and connector layer |
| Anthropic Messages API | The native wire format used by Claude Code and all Anthropic SDK clients — `POST /v1/messages`, separate system field, content array format, distinct SSE event lifecycle |
| Event Spine | Orbit's event-driven signalling system (integrated Post-MVP) |
| Credential Broker | Orbit component that manages secrets/API keys (integrated Post-MVP) |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
