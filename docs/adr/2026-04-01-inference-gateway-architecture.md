# Inference Gateway Architecture

**Status:** Accepted
**Date:** 2026-04-01
**Deciders:** kora Core Team  
**Technical Story:** [Inference Gateway PRD](../prd/inference-gateway.md) — defines the requirement for a unified, provider-agnostic inference layer authenticated via kora platform tokens.

---

## Context

### Background

kora is the control plane for AI agents. A central use case of any agent is calling language and multimodal models. Without a platform-level inference layer, every agent developer must:

- Implement provider-specific API integrations independently
- Distribute provider credentials (AWS IAM keys, GCP service account JSON, OpenAI API keys) to agent workloads
- Implement reliability patterns (retries, failover, connection pooling) in every agent
- Produce no centralised audit trail of model access, cost, or token usage across the platform

The Model Gateway PRD (now retired) proposed a solution but specified a standalone authentication model — gateway-issued tokens independent of kora's identity system. Since that PRD was written, the kora identity layer ([unified-identity-model](2026-03-05-unified-identity-model.md)), SPIFFE/SPIRE attestation ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md)), the Envoy enforcement topology ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md), [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md)), and the ext_proc token injection sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) have all been fully designed and partially implemented. The Inference Gateway must integrate with this infrastructure from day one rather than creating a parallel identity surface.

This ADR documents the architectural decisions for the **Inference Gateway** — the kora-native, provider-agnostic inference service that:

- Accepts kora platform tokens as its only authentication mechanism
- Exposes an OpenAI-compatible HTTP API
- Routes requests to AWS Bedrock, Google Vertex AI, Azure AI, and OpenAI direct via a "Bring Your Own Provider" (BYOP) model
- Enforces access control through a `PolicyEvaluator` interface designed to receive the kora Policy Engine (OPA) with no structural changes
- Attributes all cost and token usage at full principal resolution (`tenant_id`, `principal_id`, `principal_type`) on every request

### Problem Statement

We need to define:

1. How the Inference Gateway authenticates callers — specifically whether it uses kora's existing platform token system or a new standalone token system
2. What API surface the gateway exposes to agent developers
3. How provider-specific protocol translation is structured (the connector abstraction)
4. How streaming (SSE) is handled through the gateway layer without buffering or breaking the stream
5. How the routing engine is designed — deciding between inline routing logic and a dedicated strategy abstraction
6. Where policy evaluation occurs, and how the gateway remains policy-engine-agnostic until OPA ships
7. How BYOP credentials are supplied to the gateway at deployment time
8. How the gateway is deployed relative to the existing multi-binary structure ([multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md))

### Goals

- Authenticate all callers via kora platform tokens — no additional credential surface
- OpenAI-compatible API so agents can use any existing OpenAI SDK with no code changes beyond the base URL
- Clean connector abstraction that allows adding new providers without touching routing or handler code
- Streaming that is transparent to callers — SSE responses flow directly from provider to client without buffering
- Policy evaluation on every request, backed by a stub until OPA ships — then swapped in without structural changes
- Full principal-attributed cost and token tracking on every request
- Clean binary and package placement per [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md)

### Non-Goals

- OPA policy enforcement (Policy Engine not yet built — interface stubbed)
- kora Event Spine integration (Post-MVP)
- Helm chart / credential provisioning mechanism (separate infrastructure ADR)
- Per-principal rate limiting (Post-MVP — interface designed to accommodate Redis-backed implementation)
- Anthropic direct API as a separate provider — Claude models are available via AWS Bedrock
- Anthropic Messages API wire format (`POST /v1/messages`) — P1; see Architecture section for the planned design

---

## Decision Drivers

* **Platform identity consistency**: kora's identity model ([unified-identity-model](2026-03-05-unified-identity-model.md)) defines a unified Principal. The Inference Gateway must consume this model, not define a parallel one.
* **Agent transparency**: Agents carry platform tokens issued by the Envoy ext_proc sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)). Adding a second credential would break the transparency principle — agents must not manage infrastructure credentials.
* **OpenAI SDK ubiquity**: The OpenAI SDK is the de facto standard for model inference across the industry. Agents already written against it should require zero code changes to point at kora's gateway.
* **Provider diversity**: Four providers at launch with more anticipated. The architecture must absorb new providers without structural changes.
* **Policy enforcement topology**: [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) establishes that Envoy (Layer 1) handles coarse-grained access control, but model-level and cost-based policy requires application-layer context (the specific model, estimated cost per token) that Envoy cannot reason about. The Policy Engine integration must be at the application layer.
* **Streaming correctness**: SSE streams must not be buffered — cost tracking and failover semantics must be designed around the streaming lifecycle, not against it.
* **Multi-binary deployment ([multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md))**: The Inference Gateway is a new independently deployable binary with its own SPIFFE identity, following the structure established in [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md).
* **Multi-dialect extensibility**: The OpenAI-compatible API is the MVP wire format, but the gateway must be designed so that a second inbound dialect (Anthropic Messages API — for Claude Code compatibility) can be added without touching the routing engine, connectors, policy evaluator, or observability layer. Inbound translation is the only concern that varies between dialects.

---

## Considered Options

---

### Decision 1: Authentication Approach

#### Option A: Standalone Gateway Token System (Previous PRD Approach)

**Description:** Issue gateway-specific opaque tokens (user-scoped and app-scoped), store them as SHA-256 hashes in PostgreSQL. The gateway validates incoming tokens against its own table. kora identity integration is deferred.

**Pros:**
- Gateway is self-contained — no dependency on the kora identity service at runtime
- Simple to implement in isolation

**Cons:**
- Creates a second identity surface: operators now manage both kora platform credentials and Inference Gateway tokens
- Agents must carry two credentials — a platform token for all kora services plus an inference token, breaking the transparency principle from [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)
- Attribution is per-gateway-token, not per-principal — `tenant_id`, `principal_type`, and the richness of the Principal model are lost
- This was explicitly described as a temporary design in the previous PRD, pending kora identity integration that is now complete
- Contradicts the unified identity model ([unified-identity-model](2026-03-05-unified-identity-model.md)) at the point in the platform's development when that model is fully established

#### Option B: kora Platform Token ([unified-identity-model](2026-03-05-unified-identity-model.md))

**Description:** The gateway accepts kora platform JWTs as its only authentication mechanism. Envoy (Layer 1, [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)) validates the JWT (signature, expiry, JTI blocklist) and forwards the request to the Inference Gateway with the `Authorization: Bearer` header intact. The Inference Gateway handler parses the JWT a second time — a cheap local operation using the same RS256 public key — to extract principal context directly from `PlatformClaims.Orbit` (tenant ID, principal type, identity source, subject). No header injection; the token is the source of truth.

**Pros:**
- Zero additional credential surface — agents already carry platform tokens issued by the ext_proc sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)); they simply point their OpenAI client at the Inference Gateway base URL
- Human developers use their existing kora session token; service principals use their service identity token — the same session that accesses all other kora services
- Full Principal model available on every request: `tenant_id`, `principal_id`, `principal_type`, `identity_source`
- Consistent with the enforcement topology already designed — Envoy handles authentication, the application handles business logic
- Attribution is maximally granular at no implementation cost: the token already carries everything we need

**Cons:**
- Runtime dependency on kora Gateway and identity service for JWT validation — if the identity service is unavailable, JTI blocklist checks degrade (mitigated: short-lived JWTs expire naturally; blocklist check is fast Redis lookup per [unified-identity-model](2026-03-05-unified-identity-model.md))
- Requires the kora Gateway (Envoy) to be deployed in the call path — not an issue for production, but local Tier-1 development needs awareness

---

### Decision 2: API Surface Design

#### Option A: Native Multi-Provider API

**Description:** Expose a purpose-built API where the caller specifies the provider explicitly in the request body alongside the model and messages.

**Pros:**
- Exposes full provider capabilities without normalisation trade-offs

**Cons:**
- Every agent must implement provider-specific request shapes — the primary pain point the gateway is supposed to solve
- Breaks portability — switching providers requires code changes in every agent
- No benefit: agents do not need to know which provider serves the request; that is the gateway's concern

#### Option B: OpenAI-Compatible API (Translation Layer)

**Description:** Expose `POST /v1/chat/completions` accepting the OpenAI request format. The gateway translates to provider-native formats internally; responses are translated back to OpenAI format. Provider selection is entirely internal (routing configuration).

**Pros:**
- Agents using the OpenAI SDK need only change the `base_url` — zero other code changes
- Provider portability is transparent: the operator can change the underlying provider without touching agent code
- OpenAI's API is the de facto industry standard; agents are likely already written against it
- OpenAI SDK clients exist in every major language
- The internal `ChatCompletionRequest` type acts as a dialect-neutral canonical form: adding a new inbound dialect (e.g. Anthropic Messages API for P1) is a new handler and serialiser only — no changes to routing, connectors, policy evaluation, or observability

**Cons:**
- Translation layer must be maintained as each provider's API evolves
- Some provider-specific capabilities (provider-native features not in the OpenAI spec) are not exposed without extensions

#### Option C: gRPC / Protobuf API

**Description:** Define a gRPC service for inference requests.

**Pros:**
- Strong typing; efficient binary serialisation
- Bidirectional streaming built in

**Cons:**
- Not compatible with the OpenAI SDK — defeats the zero-code-change goal
- gRPC streaming is fundamentally different from SSE — existing agent code using SSE would require a full rewrite
- Adds a new technology to the platform stack without material benefit over HTTP/SSE for this use case

---

### Decision 3: Provider Connector Abstraction

#### Option A: No Abstraction — Inline Switch Logic

**Description:** Route handlers contain `switch provider` logic and call provider-specific code paths directly in the handler.

**Pros:**
- Less boilerplate for a small number of providers

**Cons:**
- Handler grows proportionally with every new provider — violates single-responsibility
- Testing a handler requires mocking all providers
- Cannot add providers without modifying core handler code
- Routing engine cannot reason about providers as interchangeable units

#### Option B: `ProviderConnector` Interface per Provider

**Description:** Define a `ProviderConnector` interface. Each provider (Bedrock, Vertex AI, Azure AI, OpenAI) implements the interface independently. The routing engine holds a registry of connectors and selects one (or an ordered list) per request. Handlers are provider-agnostic.

```go
type ProviderConnector interface {
    ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)
    ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamChunk, error)
    Name() string
    // Note: pricing is NOT on the connector. PricingCache is the single source of pricing
    // truth, injected into RoutingEngine. This avoids a dual-source inconsistency where
    // connector-held static prices diverge from the background-polled cache.
}
```

**Pros:**
- Handlers and routing engine are provider-agnostic — adding a new provider is purely additive
- Each connector is independently testable with mock providers
- Error normalisation is connector-owned — the routing engine receives typed errors and can decide retryability without knowing provider-specific error shapes
- Pricing responsibility lives in `PricingCache`, not the connector — a single refresh goroutine owns all pricing data; connectors stay focused on wire-format translation

**Cons:**
- More initial structure than Option A for the first two providers
- Interface must be carefully designed to avoid leaking provider-specific concerns (e.g. regional endpoints, auth header shapes) into the interface contract

#### Option C: Use LiteLLM / Portkey as Translation Layer

**Description:** Embed or proxy through LiteLLM or Portkey for provider translation.

**Pros:**
- LiteLLM supports 100+ providers out of the box; no translation code to write

**Cons:**
- Deep integration with kora identity and policy requires gateway internals access — not possible through a third-party proxy without forking
- LiteLLM's broad scope (100+ providers, proxy mode, SDK mode) is architectural noise for kora's BYOP-4-provider requirement
- Introduces a Python runtime dependency into a Go monorepo
- License and maintenance risk for core platform infrastructure
- Cannot embed the `PolicyEvaluator` interface or principal context extraction inside a third-party process

---

### Decision 4: Streaming Architecture

#### Option A: Buffer Full Response, Then Stream to Client

**Description:** The gateway reads the provider's full SSE stream into a buffer, then writes the buffered response to the client.

**Pros:**
- Simplifies token counting (all tokens available before responding)
- Simplifies failover (can retry before the client sees any data)

**Cons:**
- Eliminates the latency benefit of streaming — the client receives no tokens until the model is fully done
- Buffers the entire response in memory for every concurrent streaming request — memory pressure at scale
- Not a real streaming implementation — breaks the user-visible semantics that streaming exists to provide

#### Option B: Direct SSE Pipe — Provider Stream → Client Stream

**Description:** The gateway opens a streaming connection to the provider and pipes SSE chunks directly to the client as they arrive. The final chunk carries usage statistics (`usage` field in OpenAI's streaming format), which the gateway captures for cost tracking before closing the response stream.

**Pros:**
- True streaming — client sees first token as soon as the provider produces it
- No in-memory buffering of response body
- Token counting and cost tracking use the `usage` chunk in the final SSE event — accurate without buffering
- Matches the semantics clients expect when setting `stream: true`

**Cons:**
- Failover is only possible before the first byte — once streaming has begun, the connection is committed to the chosen provider. Clearly documented constraint.
- Token usage arrives at the end of the stream — cost record is written asynchronously after stream completion

The streaming lifecycle with cost tracking:

```
Client                    Gateway                   Provider
  |                          |                          |
  |--POST /v1/chat/--------→|                          |
  |   completions            |---provider stream req--→|
  |                          |←-- chunk 1 -------------|
  |←-- chunk 1 -------------|                          |
  |                          |←-- chunk 2 -------------|
  |←-- chunk 2 -------------|                          |
  |                          |←-- [DONE] + usage ------|
  |←-- [DONE] + usage ------|  ↑ capture tokens        |
  |                          |  write usage record      |
  |                          |  (async, post-stream)    |
```

---

### Decision 5: Routing Engine Design

#### Option A: Inline Routing Logic in Handlers

**Description:** The handler directly selects the provider and model based on the request model field, with routing logic expressed as conditionals in the handler.

**Pros:**
- Simpler initial implementation

**Cons:**
- Handler is responsible for both request handling and routing decisions — violates SRP
- Adding a new routing strategy (e.g. latency-based, P1) requires modifying the handler
- Failover and retry loops nested inside a handler become difficult to test
- The routing configuration schema has nowhere natural to live

#### Option B: Dedicated Routing Engine with Pluggable Strategy

**Description:** A `RoutingEngine` receives the request's model field and the routing configuration, applies a strategy (lowest-cost or failover chain), and returns an ordered list of `(provider, model)` targets. The handler calls the routing engine to get the target list, then delegates execution to the connector layer. The engine applies retry logic per target before advancing.

```go
type RoutingEngine interface {
    // Resolve returns an ordered slice of (connector, model) targets for the request.
    // The caller attempts each in order, advancing on retriable failure.
    Resolve(ctx context.Context, req *ChatCompletionRequest) ([]RouteTarget, error)
}

type RouteTarget struct {
    Connector  ProviderConnector
    Model      string   // provider-native model identifier
    Strategy   RoutingStrategy
}

type RoutingStrategy string

const (
    StrategyLowestCost    RoutingStrategy = "lowest_cost"
    StrategyFailoverChain RoutingStrategy = "failover_chain"
    StrategyDirect        RoutingStrategy = "direct"
)
```

**Pros:**
- Handlers are routing-agnostic — they call `RoutingEngine.Resolve` and iterate the target list
- Each routing strategy is independently testable
- Adding P1 strategies (latency-based, A/B routing) is additive — new `RoutingStrategy` value + new resolver branch
- Retry loop sits in the execution layer, not the handler
- Routing configuration schema naturally maps to the engine's input

**Cons:**
- More initial structure than Option A

---

### Decision 6: Policy Evaluation Point

#### Option A: Envoy ext_authz Only (Layer 1)

**Description:** Rely entirely on the Layer 1 Envoy enforcement from [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) for all policy decisions. No application-layer policy evaluation in the gateway.

**Pros:**
- No application-layer policy code required
- Consistent with the universal enforcement topology

**Cons:**
- Envoy's `ext_authz` can evaluate: "is this principal allowed to call `/v1/chat/completions`?" — but it cannot evaluate: "is this principal allowed to call `gpt-4o`?" or "is this tenant's estimated cost below its budget threshold?"
- Model-level access control, cost-based policies, and per-model-family restrictions require application-layer context (the resolved model, estimated cost per token) that is only available inside the gateway process
- Deferring all policy to Layer 1 means OPA policies can only operate at endpoint granularity — not model granularity

#### Option B: `PolicyEvaluator` Interface at the Application Layer

**Description:** Define a `PolicyEvaluator` interface in the gateway's application layer. It is called on every request after routing resolution (when the model and provider are known) but before the connector executes the call. The default implementation is a stub that returns allow for all requests. When the OPA-backed Policy Engine ships, it replaces the stub behind the interface with no structural changes to the gateway.

```go
type PolicyEvaluator interface {
    EvaluateInferenceRequest(ctx context.Context, req *InferencePolicyRequest) (*PolicyDecision, error)
}

type InferencePolicyRequest struct {
    Principal    Principal
    Model        string
    Provider     string
    // EstimatedCostUSD is the estimated cost for this specific request in USD,
    // computed from ModelPricing using per-million-token rates:
    //   cost = (estimatedInputTokens / 1_000_000 * pricing.InputPerMillionTokens)
    //        + (estimatedOutputTokens / 1_000_000 * pricing.OutputPerMillionTokens)
    // A conservative estimate (max_tokens for output) is acceptable for policy evaluation.
    EstimatedCostUSD float64
}

type PolicyDecision struct {
    Allow  bool
    Reason string
}

// AllowAllPolicyEvaluator is the stub implementation used until OPA ships.
type AllowAllPolicyEvaluator struct{}

func (a *AllowAllPolicyEvaluator) EvaluateInferenceRequest(_ context.Context, _ *InferencePolicyRequest) (*PolicyDecision, error) {
    return &PolicyDecision{Allow: true}, nil
}
```

**Pros:**
- Model-level and cost-based policies are possible from day one once OPA ships — no gateway refactoring required
- The stub makes the integration point explicit and testable (mock the interface in handler tests)
- Complements Layer 1 (not replaces it): Layer 1 answers "can this principal call the inference API?"; Layer 2 answers "can this principal call *this model* at *this cost*?"
- Keeps the gateway policy-engine-agnostic — the OPA client is an implementation detail of the `PolicyEvaluator` implementation, not the gateway

**Cons:**
- Application-layer evaluation adds a small amount of latency per request (local interface call + stub logic)
- Until OPA ships, the evaluator is a no-op — the interface carries conceptual weight before it carries functional weight

---

## Decision

| # | Decision | Chosen Option |
|---|---|---|
| 1 | Authentication approach | **Option B** — kora platform tokens ([unified-identity-model](2026-03-05-unified-identity-model.md)) |
| 2 | API surface | **Option B** — OpenAI-compatible API with translation layer |
| 3 | Provider abstraction | **Option B** — `ProviderConnector` interface per provider |
| 4 | Streaming architecture | **Option B** — Direct SSE pipe, usage capture from final chunk |
| 5 | Routing engine | **Option B** — Dedicated `RoutingEngine` with pluggable strategies |
| 6 | Policy evaluation | **Option B** — `PolicyEvaluator` interface at application layer, stub default |

### Rationale

Every decision in this ADR resolves to the option that integrates cleanly with existing kora architecture rather than re-inventing infrastructure already designed and partly built. The common thread:

**Authentication**: Platform tokens are already issued to every agent via the ext_proc sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)). Requiring a second token would break agent transparency and create a parallel identity surface immediately after completing the work to unify identity. The Inference Gateway is a kora service — it uses kora credentials.

**API surface**: The OpenAI-compatible API surface is the only choice that delivers zero-code-change integration for agent developers. The translation complexity lives in the connector layer, not in every agent ever written.

**Connector interface**: The `ProviderConnector` interface is the direct application of Go's interface idiom to the multi-provider problem. Handlers and the routing engine work against the interface; connector implementations are interchangeable. Error normalisation at the connector boundary means the routing engine can make retry/failover decisions without understanding provider-specific error codes.

**Streaming**: Buffering the full response before streaming destroys streaming's purpose. The direct SSE pipe approach with deferred cost record writing is the only approach that delivers real streaming semantics while still capturing accurate token counts from the stream's final `usage` chunk.

**Routing engine**: The dedicated `RoutingEngine` with a strategy pattern is the correct structural choice. It cleanly separates the concerns of "which provider and model should serve this request" from "how do I call that provider". Retry and failover logic belongs in the execution loop, not in the HTTP handler.

**Policy evaluator**: Envoy's Layer 1 enforcement answers endpoint-level access control. Model-level policy (which model a tenant can use, cost thresholds, model family restrictions) requires the resolved model and estimated cost — context only available inside the gateway process at request time. The interface-plus-stub pattern is the established kora approach to designing integration points before the dependent service ships.

---

## Architecture

### Service Placement ([multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md))

The Inference Gateway is a new binary under `backend/cmd/inference-gateway/`, following the multi-binary model from [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md).

```
backend/
├── cmd/
│   ├── identity/                     # existing
│   ├── identity-proxy/               # existing
│   ├── inference-gateway/            # NEW
│   │   ├── main.go                   # wires internal/inferencegw only
│   │   ├── config/
│   │   │   ├── base.yaml
│   │   │   ├── local.yaml
│   │   │   ├── development.yaml
│   │   │   └── production.yaml
│   │   └── migrations/               # SQL migrations for inference_usage table
│   │       └── 20250714000000_create_inference_usage.sql
│   └── example/                      # existing
│
├── internal/
│   ├── inferencegw/                  # NEW — consumed only by cmd/inference-gateway
│   │   ├── models.go                 # ChatCompletionRequest/Response, Principal, StreamChunk, etc.
│   │   ├── connector.go              # ProviderConnector interface + ModelPricing
│   │   ├── router.go                 # RoutingEngine interface + RouteTarget + strategies
│   │   ├── policy.go                 # PolicyEvaluator interface + AllowAllPolicyEvaluator stub
│   │   ├── service.go                # inference orchestration (route → evaluate → execute → record)
│   │   ├── handlers_openai.go        # POST /v1/chat/completions (OpenAI dialect); named _openai from the start so Phase 5 adds handlers_anthropic.go without a rename
│   │   ├── usage_repository.go       # InferenceUsageRepository interface + postgres implementation
│   │   ├── pricing.go                # PricingCache — polls AWS/Vertex pricing APIs; static for Azure/OpenAI
│   │   └── providers/
│   │       ├── bedrock/
│   │       │   └── connector.go      # BedrockConnector implements ProviderConnector
│   │       ├── vertexai/
│   │       │   └── connector.go      # VertexAIConnector implements ProviderConnector
│   │       ├── azureai/
│   │       │   └── connector.go      # AzureAIConnector implements ProviderConnector
│   │       └── openai/
│   │           └── connector.go      # OpenAIConnector implements ProviderConnector
│   └── platform/                     # unchanged
│
└── k8s/
    └── inference-gateway.yaml        # Deployment + ServiceAccount
```

**SPIFFE Identity:**  
The inference-gateway pod carries label `spiffe.io/component: inference-gateway` in the `orbit-system` namespace. The generic `orbit-system-component` `ClusterSPIFFEID` template ([multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md)) automatically issues SPIFFE ID `spiffe://orbit.test/system/inference-gateway`. No per-service `ClusterSPIFFEID` resource is required.

### Request Lifecycle

```
1. Client sends:  POST /v1/chat/completions
                  Authorization: Bearer <platform-token>

2. Orbit Gateway (Envoy — Layer 1, [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)):
   - Validates JWT signature, expiry, JTI blocklist (per [unified-identity-model](2026-03-05-unified-identity-model.md))
   - On allow: forwards request to inference-gateway service unchanged
     (Authorization: Bearer header is passed through intact)

3. Inference Gateway — Gateway API Handler:
   - Parses JWT from Authorization header → PlatformClaims
     (RS256, same signing key as identity service)
   - Extracts principal context from the token's OrbitClaims:
       claims.Orbit.TenantID        → tenant context
       claims.Orbit.PrincipalType   → AGENT | HUMAN | SERVICE | SYSTEM
       claims.Orbit.IdentitySource  → ATTESTED | FEDERATED | PLATFORM
       claims.Subject               → principal ID (JWT subject field)
   - Validates request body (model field, messages array)
   - Calls RoutingEngine.Resolve(req) → []RouteTarget

   **Implementation prerequisite:** `PlatformClaims` and `OrbitClaims` currently live in
   `internal/identity/models.go`. Feature packages must not import each other ([repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md)).
   Before implementing the inference gateway handler, these types must be moved to
   `internal/platform/token/` so both `internal/identity` and `internal/inferencegw` can
   import them without a cross-feature dependency. The identity service `parseToken` /
   `signToken` functions should also be extracted to `internal/platform/token/jwt.go` so
   JWT logic has a single home.

4. Inference Gateway — PolicyEvaluator (application layer):
   - Calls EvaluateInferenceRequest(principal, model, provider, estimatedCost)
   - Stub returns allow; OPA implementation enforces model/cost policies when shipped
   - On deny: returns 403 with PolicyDecision.Reason

5. Inference Gateway — Execution Loop (retry + failover):

   The retry contract is **per-target with a global attempt cap**:

   For each RouteTarget t in the ordered list:
     a. Attempt t up to cfg.Retry.MaxAttemptsPerTarget times (default: 2).
        Between attempts: exponential backoff with full jitter, capped at MaxBackoff.
     b. Retriable errors for the same target: 5xx, timeout, 429.
        Non-retriable errors (4xx excluding 429): advance to next target immediately.
     c. After MaxAttemptsPerTarget failures on t: advance to next RouteTarget.
     d. The global attempt ceiling cfg.Retry.MaxTotalAttempts (default: 4) caps
        the combined attempts across all targets regardless of list length.
     e. If all targets exhausted or MaxTotalAttempts reached: return 503 to client.

   Behaviour by strategy:
   - StrategyDirect:        Single target; MaxAttemptsPerTarget retries on that target only.
   - StrategyLowestCost:    Targets ordered by ascending price; advance on failure.
   - StrategyFailoverChain: Targets in explicit config order; advance on any failure
                            (MaxAttemptsPerTarget = 1 for failover chains by default —
                            the intent is deterministic fallback, not per-provider retry).

6. Streaming path only:

   Happy path:
   - First byte received from provider → pipe SSE chunks directly to client (200 + SSE headers committed)
   - Final SSE chunk contains usage statistics → captured before stream close
   - Cost record written asynchronously after stream close

   Error paths (after 200 + SSE headers are committed — HTTP status cannot change):

   a. Provider drops connection mid-stream (TCP reset / timeout):
      - Emit: `data: {"error":{"type":"provider_error","message":"provider connection lost"}}`
      - Emit: `data: [DONE]`
      - Close the response body.
      - Write a usage record with status='provider_error', tokens from any captured chunks.

   b. Provider returns an error event within the stream (e.g. Bedrock error frame):
      - Translate to: `data: {"error":{"type":"provider_error","message":"<normalised message>"}}`
      - Emit: `data: [DONE]`
      - Close the response body.
      - Write usage record with status='provider_error'.

   c. Client disconnects (request context cancelled):
      - Detect via `ctx.Done()` in the pipe loop.
      - Stop piping; do not write further SSE events (client is gone).
      - Write a usage record with status='success' and tokens captured up to disconnect.
        Rationale: tokens were consumed by the provider; cost must be attributed regardless
        of whether the client received the full response.

   d. Usage record write fails after stream closes:
      - Log the error at ERROR level with full principal + model context.
      - Do not propagate the error to the client (stream is already closed).
      - This is an acceptable data-loss window for MVP; a write-ahead event log is the P1 mitigation.

   Failover is **not available** after the first byte is piped to the client.
   The commitment point is the first `w.Write()` call — after that, the connection
   is bound to the chosen provider for the lifetime of the stream.

7. Inference Gateway — Observability:
   - Write inference_usage record to PostgreSQL (non-blocking for streaming)
   - Emit structured log: tenant_id, principal_id, principal_type, model, provider,
     strategy, status, latency_ms, input_tokens, output_tokens, estimated_cost_usd,
     retry_count
   - Increment OTel metrics counters and histograms ([opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md); using
     go.opentelemetry.io/otel/metric — already an indirect dep; no new
     dependency required)
```

---

### Core Interface Contracts

#### `ProviderConnector`

```go
// ProviderConnector is the interface all provider implementations must satisfy.
// Each implementation is responsible for translating OpenAI-format requests to
// provider-native format and normalising responses and errors back to the
// platform-standard types.
//
// Pricing is NOT part of this interface. PricingCache is the single source of
// pricing truth and is injected directly into RoutingEngine. This prevents the
// dual-source inconsistency that would arise if connectors held their own static
// prices while PricingCache runs background polling for Bedrock and Vertex AI.
type ProviderConnector interface {
    // ChatCompletion executes a non-streaming inference request.
    ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)

    // ChatCompletionStream executes a streaming inference request.
    // The returned channel emits StreamChunk values until the stream is complete.
    // The final chunk carries usage statistics (input_tokens, output_tokens).
    // The channel is closed by the connector when the provider stream ends.
    ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamChunk, error)

    // Name returns the canonical provider identifier.
    // Used in routing configuration, logs, and usage records.
    // Values: "bedrock", "vertexai", "azureai"
    Name() string
}

type ModelPricing struct {
    InputPerMillionTokens  float64 // USD per 1M input tokens
    OutputPerMillionTokens float64 // USD per 1M output tokens
}

// ConnectorError is the normalised error type returned by all connectors.
// The routing engine uses Retriable to decide whether to retry or advance.
type ConnectorError struct {
    Provider   string
    StatusCode int
    Message    string
    Retriable  bool
}
```

#### `RoutingEngine`

```go
// RoutingEngine resolves an ordered list of route targets for a request.
// The concrete implementation (e.g. DefaultRoutingEngine) receives a PricingCache
// dependency at construction time and uses it to rank targets for StrategyLowestCost.
//
// Dependency injection wiring (cmd/inference-gateway/main.go):
//
//   pricingCache := pricing.NewPricingCache(cfg.Pricing, logger)
//   routingEngine := routing.NewDefaultRoutingEngine(connectorRegistry, pricingCache, cfg.Routing)
//   policyEvaluator := policy.NewAllowAllPolicyEvaluator()
//   usageRepo := usage.NewPostgresRepository(db)
//   svc := inferencegw.NewInferenceService(routingEngine, policyEvaluator, usageRepo, pricingCache, logger)
//
// PricingCache is also injected into InferenceService so it can compute
// EstimatedCostUSD for the PolicyEvaluator call before routing begins.
type RoutingEngine interface {
    // Resolve returns an ordered slice of RouteTargets for the request.
    // The caller attempts each target in sequence, advancing on retriable failure.
    // For StrategyLowestCost, targets are ordered by ascending PricingCache.Price().
    Resolve(ctx context.Context, req *ChatCompletionRequest) ([]RouteTarget, error)
}

type RouteTarget struct {
    Connector ProviderConnector
    Model     string          // provider-native model identifier (e.g. "anthropic.claude-sonnet-4-5-v1:0")
    Strategy  RoutingStrategy
}

type RoutingStrategy string

const (
    StrategyLowestCost    RoutingStrategy = "lowest_cost"
    StrategyFailoverChain RoutingStrategy = "failover_chain"
    StrategyDirect        RoutingStrategy = "direct"
)
```

#### `PolicyEvaluator`

```go
type PolicyEvaluator interface {
    EvaluateInferenceRequest(ctx context.Context, req *InferencePolicyRequest) (*PolicyDecision, error)
}

type InferencePolicyRequest struct {
    Principal    Principal
    Model        string
    Provider     string
    // EstimatedCostUSD is the estimated cost for this specific request in USD,
    // computed from PricingCache using per-million-token rates:
    //   cost = (estimatedInputTokens / 1_000_000 * pricing.InputPerMillionTokens)
    //        + (estimatedOutputTokens / 1_000_000 * pricing.OutputPerMillionTokens)
    // Use max_tokens as a conservative output estimate. Units are consistent with
    // ModelPricing.InputPerMillionTokens / OutputPerMillionTokens and
    // inference_usage.estimated_cost_usd.
    EstimatedCostUSD float64
}

type PolicyDecision struct {
    Allow  bool
    Reason string // populated on deny; included in 403 response body
}

// AllowAllPolicyEvaluator is the default stub. All requests are permitted.
// Replaced by an OPA-backed implementation when the Policy Engine ships.
type AllowAllPolicyEvaluator struct{}

func (a *AllowAllPolicyEvaluator) EvaluateInferenceRequest(_ context.Context, _ *InferencePolicyRequest) (*PolicyDecision, error) {
    return &PolicyDecision{Allow: true}, nil
}
```

#### `InferenceUsageRepository`

```go
type InferenceUsageRepository interface {
    Create(ctx context.Context, record *InferenceUsageRecord) error
    Query(ctx context.Context, filter InferenceUsageFilter) ([]*InferenceUsageRecord, error)
}

// APIDialect identifies the inbound wire format used by the caller.
// openai: POST /v1/chat/completions (MVP)
// anthropic: POST /v1/messages (P1 — Anthropic Messages API for Claude Code compatibility)
type APIDialect string

const (
    DialectOpenAI    APIDialect = "openai"
    DialectAnthropic APIDialect = "anthropic"
)

type InferenceUsageRecord struct {
    ID               uuid.UUID
    TenantID         string
    PrincipalID      string
    PrincipalType    PrincipalType  // AGENT | HUMAN | SERVICE | SYSTEM ([unified-identity-model](2026-03-05-unified-identity-model.md))
    Model            string
    Provider         string
    RoutingStrategy  RoutingStrategy
    APIDialect       APIDialect     // openai | anthropic — which inbound wire format was used
    InputTokens      int
    OutputTokens     int
    TotalTokens      int
    EstimatedCostUSD float64
    LatencyMS        int
    RetryCount       int
    Status           UsageStatus    // success | provider_error | policy_denied | auth_error
    CreatedAt        time.Time
}

type InferenceUsageFilter struct {
    TenantID      string
    PrincipalID   string
    PrincipalType *PrincipalType
    Provider      string
    Model         string
    APIDialect    *APIDialect // nil = all dialects; set to filter by openai or anthropic
    From          time.Time
    To            time.Time
    Limit         int
    Offset        int
}
```

### Database Schema

```sql
-- Migration: 20250714000000_create_inference_usage.sql

CREATE TABLE inference_usage (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT        NOT NULL,
    principal_id        TEXT        NOT NULL,
    -- principal_type values must match [unified-identity-model](2026-03-05-unified-identity-model.md) PrincipalType constants.
    -- SYSTEM principals are valid callers (e.g. automated pipelines); excluding them
    -- here would silently drop usage records for SYSTEM-initiated inference.
    principal_type      TEXT        NOT NULL CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM')),
    model               TEXT        NOT NULL,
    -- provider and routing_strategy are nullable: they are unknown before routing runs.
    -- NULL values indicate a pre-routing failure (policy_denied, auth_error).
    -- The application layer enforces the valid provider set; no DB CHECK constraint
    -- is used here so that adding a new connector requires zero schema migrations.
    provider            TEXT,
    routing_strategy    TEXT        CHECK (routing_strategy IN ('lowest_cost', 'failover_chain', 'direct')),
    input_tokens        INTEGER     NOT NULL DEFAULT 0,
    output_tokens       INTEGER     NOT NULL DEFAULT 0,
    total_tokens        INTEGER     NOT NULL DEFAULT 0,
    estimated_cost_usd  NUMERIC(12, 8) NOT NULL DEFAULT 0,
    latency_ms          INTEGER     NOT NULL,
    retry_count         INTEGER     NOT NULL DEFAULT 0,
    api_dialect         TEXT        NOT NULL DEFAULT 'openai' CHECK (api_dialect IN ('openai', 'anthropic')),
    status              TEXT        NOT NULL CHECK (status IN ('success', 'provider_error', 'policy_denied', 'auth_error')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Note: api_dialect defaults to 'openai' so the MVP migration is forwards-compatible.
-- When the P1 Anthropic Messages API dialect ships, the column already exists and
-- AnthropicHandler simply writes 'anthropic' — no schema migration required at P1.

-- Indexes for the primary usage query patterns (GET /v1/usage filters)
CREATE INDEX idx_inference_usage_tenant_id        ON inference_usage (tenant_id, created_at DESC);
CREATE INDEX idx_inference_usage_principal_id     ON inference_usage (principal_id, created_at DESC);
CREATE INDEX idx_inference_usage_principal_type   ON inference_usage (principal_type, created_at DESC);
CREATE INDEX idx_inference_usage_provider_model   ON inference_usage (provider, model, created_at DESC);
CREATE INDEX idx_inference_usage_created_at       ON inference_usage (created_at DESC);
```

### P1: Anthropic Messages API Dialect

> **Status: Post-MVP (P1).** Designed here to confirm architectural fit; implementation is deferred. The MVP ships with the OpenAI-compatible API only.

Claude Code and all tools built on the Anthropic Python/TypeScript SDKs use the Anthropic Messages API — a distinct wire format on a different endpoint, with a different streaming event lifecycle and a different authentication header convention (`x-api-key` rather than `Authorization: Bearer`). Supporting these clients without changes to the caller is a P1 requirement.

**Header Normalisation (Edge Envoy — not gateway service):**

Anthropic SDK clients send credentials as `x-api-key`. kora platform tokens are JWTs. For human callers (e.g. Claude Code running locally), the developer sets `ANTHROPIC_API_KEY` to their kora platform token. The edge Envoy (inbound north-south gateway, [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)) is configured with a Lua or `header_to_metadata` filter that promotes `x-api-key` → `Authorization: Bearer` on the `/v1/messages` path before JWT validation runs. This is a filter-chain configuration on the edge Envoy listener — not a pod sidecar concern:

```
Claude Code (local)
  │  ANTHROPIC_API_KEY = <orbit-platform-token>
  │  x-api-key: <orbit-platform-token>
  │  POST /v1/messages
  ▼
Edge Envoy (north-south inbound gateway)
  │  Lua filter: x-api-key → Authorization: Bearer  (scoped to path: /v1/messages)
  │  JWT validation runs as normal ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md))
  │  Request forwarded with Authorization: Bearer intact
  ▼
Inference Gateway — Anthropic handler
  │  Parses JWT → PlatformClaims (same path as OpenAI handler)
  │  No knowledge of x-api-key convention required
```

For agent workloads using the Anthropic SDK inside a pod, the ext_proc identity-proxy sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) already intercepts all outbound requests and injects `Authorization: Bearer <platform-token>`, overwriting any `x-api-key` the SDK sets before the request leaves the pod. This path requires zero changes.

**Dual-Handler Architecture:**

The gateway's internal `ChatCompletionRequest` and `ChatCompletionResponse` types serve as a dialect-neutral canonical form. Dialect translation is entirely an inbound and outbound concern of each handler — the shared execution core is untouched:

```
POST /v1/messages         →  AnthropicHandler
                               │  translate: AnthropicRequest → ChatCompletionRequest
                               │  translate: ChatCompletionResponse → AnthropicResponse
                               │  translate: stream chunks → Anthropic SSE event lifecycle
                               ▼
POST /v1/chat/completions →  OpenAIHandler
                               │  translate: OpenAIRequest → ChatCompletionRequest
                               │  translate: ChatCompletionResponse → OpenAIResponse
                               ▼
                          InferenceService.Execute()   ← shared, dialect-unaware
                               │
                          RoutingEngine                ← unchanged
                          PolicyEvaluator              ← unchanged
                          ProviderConnector(s)         ← unchanged
                          InferenceUsageRepository     ← unchanged (+ api_dialect column)
```

**Anthropic SSE Lifecycle:**

The Anthropic streaming protocol uses a different sequence of named events than OpenAI's `data: {...}` chunks:

```
event: message_start       — message metadata, estimated input tokens
event: content_block_start — block index and type
event: content_block_delta — incremental text delta
  ... N deltas ...
event: content_block_stop
event: message_delta       — stop reason, output token count
event: message_stop
```

The `AnthropicHandler` serialises the gateway's internal `<-chan StreamChunk` into this event sequence. The direct SSE pipe decision (Decision 4) applies equally — chunks are written to the client as they arrive; usage statistics are captured from the `message_delta` event before the stream closes.

**Package additions (P1 only — not MVP):**

```
internal/inferencegw/
├── handlers_openai.go       # MVP — OpenAI-compatible handler and SSE serialiser
├── handlers_anthropic.go    # P1  — Anthropic Messages API handler and SSE serialiser
└── ...
```

**`api_dialect` attribution:**

All usage records carry an `api_dialect` field to distinguish requests by inbound wire format. See the updated `InferenceUsageRecord` type and database schema below. This column defaults to `'openai'` so the MVP migration is forwards-compatible with the P1 addition.

---

### BYOP Credential Model

Provider credentials are loaded from environment variables at gateway startup. The gateway validates that credentials are present for all configured providers and logs a clear error and refuses to start if a configured provider's credentials are missing.

| Provider | Environment Variables |
|---|---|
| AWS Bedrock | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (or IAM role via `AWS_ROLE_ARN` + IRSA) |
| Google Vertex AI | `GOOGLE_APPLICATION_CREDENTIALS` (path to service account JSON), `VERTEX_PROJECT_ID`, `VERTEX_LOCATION` |
| Azure AI | `AZURE_AI_API_KEY`, `AZURE_AI_ENDPOINT` |
| OpenAI | `OPENAI_API_KEY` |

Credentials are **never**:
- Written to the database
- Logged at any level
- Included in YAML configuration (YAML fields for secrets use `yaml:"-"`)
- Included in error responses

The mechanism for supplying these environment variables at deploy time (Helm values, sealed secrets, external secrets operator) is out of scope for this ADR and addressed in the infrastructure ADR.

### Pricing Architecture

```go
// PricingCache maintains per-model token prices for all configured providers.
// AWS Bedrock and Vertex AI prices are polled from their respective pricing APIs
// on a configurable TTL (default: 1 hour).
// Azure AI and OpenAI prices are manually configured and reloaded without restart.
type PricingCache interface {
    // Price returns the ModelPricing for the given provider and model.
    // Returns last cached price with a staleness warning if the pricing API is unavailable.
    Price(provider, model string) (*ModelPricing, error)

    // Refresh forces an immediate cache refresh for the given provider.
    // Called by the background polling goroutine.
    Refresh(ctx context.Context, provider string) error

    // Start launches the background polling goroutine and blocks until ctx is cancelled.
    // Must be called as a goroutine: go pricingCache.Start(ctx).
    // The goroutine stops cleanly when ctx is cancelled (e.g. on SIGTERM).
    // cmd/inference-gateway/main.go wires this into the binary's shutdown context:
    //
    //   ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    //   defer cancel()
    //   var wg sync.WaitGroup
    //   wg.Add(1)
    //   go func() { defer wg.Done(); pricingCache.Start(ctx) }()
    //   // ... serve requests ...
    //   cancel()   // triggered by SIGTERM
    //   wg.Wait()  // wait for polling goroutine to drain before process exits
    Start(ctx context.Context)
}
```

On pricing API failure, the gateway:
1. Returns the last successfully cached price for the model
2. Logs a warning at `WARN` level with the provider name, last refresh time, and error
3. Does not fail or degrade the inference request — stale pricing is preferable to unavailability

### Configuration Schema

```yaml
# cmd/inference-gateway/config/base.yaml
inference_gateway:
  providers:
    bedrock:
      enabled: true
      region: ""             # overridden by AWS_REGION env var; included here for documentation
      # credentials: loaded from AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_ROLE_ARN
    vertexai:
      enabled: true
      # credentials: loaded from GOOGLE_APPLICATION_CREDENTIALS / VERTEX_PROJECT_ID / VERTEX_LOCATION
    azureai:
      enabled: true
      # credentials: loaded from AZURE_AI_API_KEY / AZURE_AI_ENDPOINT
      pricing:                # Azure AI prices — manually configured; no pricing API available
        gpt-4o:
          input_per_million_tokens: 2.50
          output_per_million_tokens: 10.00
    openai:
      enabled: true
      # credentials: loaded from OPENAI_API_KEY
      pricing:                # OpenAI prices — manually configured; reloaded without restart
        gpt-4o:
          input_per_million_tokens: 2.50
          output_per_million_tokens: 10.00

  pricing:
    refresh_interval: 1h     # TTL for AWS Bedrock and Vertex AI pricing API polls
    # Config reload mechanism (GAP-5 / US-006 "reloadable without restart"):
    # Send SIGHUP to the inference-gateway process to trigger a config re-read.
    # The binary re-calls config.Load[T] on SIGHUP and atomically swaps the
    # PricingCache in-memory map. No pod restart required. Kubernetes rolling
    # restarts remain an alternative for operators who prefer not to use SIGHUP.
    # SIGHUP handling is wired in cmd/inference-gateway/main.go alongside the
    # existing SIGTERM/SIGINT shutdown handler.

  routing:
    groups:                  # routing groups map logical model names to provider/model targets
      - name: claude-sonnet
        strategy: lowest_cost
        targets:
          - provider: bedrock
            model: anthropic.claude-sonnet-4-5-v1:0
          - provider: vertexai
            model: claude-sonnet-4-5@20250514
      - name: gpt-4o
        strategy: failover_chain
        targets:
          - provider: openai
            model: gpt-4o
          - provider: azureai
            model: gpt-4o

  server:
    port: 8080
    read_timeout: 60s
    write_timeout: 120s      # applies to non-streaming responses only.
                             # Streaming handlers must call http.ResponseController.SetWriteDeadline
                             # to extend or clear the per-connection deadline before the first SSE
                             # write — the global server WriteTimeout fires for the entire response
                             # duration, which would prematurely close long streams.
    idle_timeout: 120s

  connection_pool:
    max_idle_conns_per_provider: 100
    max_conns_per_provider: 300
    idle_conn_timeout: 90s

  retry:
    max_attempts: 3
    initial_backoff: 200ms
    backoff_multiplier: 2.0
    max_backoff: 5s
    jitter: true
```

---

## Consequences

### Positive

- **Zero additional credential burden on agents:** Platform tokens already carried via the SPIFFE/SPIRE + Envoy ext_proc flow ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) are the only credential required. The inference API is just another kora service from the agent's perspective.
- **OpenAI SDK compatibility:** Agents written against the OpenAI SDK need only change `base_url`. The translation layer isolates provider churn from agent code permanently.
- **Full principal attribution from day one:** Every usage record carries `tenant_id`, `principal_id`, and `principal_type` from the platform token — no schema migration will ever be needed to add these columns.
- **Provider-agnostic gateway internals:** Handlers and the routing engine operate against interfaces. Adding a fifth provider (e.g. Mistral direct API) is a new `providers/mistral/connector.go` implementation — zero changes to routing, handlers, or policy code.
- **Policy hook without the Policy Engine:** The `PolicyEvaluator` interface is defined, called, and tested from day one. The OPA team can plug in their implementation when ready. No gateway refactoring or deployment coordination needed.
- **True streaming:** Direct SSE pipe means agents see the first token as soon as the model generates it. Cost tracking is accurate because the final stream chunk carries the provider's token counts.
- **Clean [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) compliance:** New binary `cmd/inference-gateway/`, feature package `internal/inferencegw/`, own migrations, own SPIFFE identity via pod label — follows the established pattern exactly.

### Negative

- **Envoy dependency in production call path:** In production, Envoy (Layer 1) performs JWT signature/expiry/JTI validation before the request reaches the gateway. In local development (Tier 1 — no cluster, no Envoy), the gateway receives requests directly; JTI blocklist validation does not run. Developers obtain a real platform token via `make auth-device` against the local identity service and send it in the `Authorization: Bearer` header — no mock or bypass needed. The gateway's own JWT parsing extracts principal context identically in both environments.
- **Provider translation maintenance:** Four connector implementations must be maintained and updated as provider APIs evolve. Error normalisation must be comprehensive — a connector that leaks provider-specific error shapes into the routing engine's error handling breaks the abstraction.
- **Anthropic Messages API SSE serialiser (P1 cost):** The Anthropic streaming event lifecycle is distinct from OpenAI's — `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`. The `AnthropicHandler` must serialise the gateway's internal `StreamChunk` channel into this exact sequence. The core streaming pipe (Decision 4) is unchanged; only the serialisation layer differs.
- **Streaming cost record is asynchronous:** For streaming requests, the usage record is written after the stream closes. This means in-flight streaming requests are not immediately visible in usage queries. Acceptable for MVP; a streaming-aware usage ledger is a P1 consideration.
- **Pricing table staleness for Azure AI and OpenAI:** Manual price configuration means prices drift if not updated. Automated pricing for Azure AI and OpenAI is a P1 item — until then, operators must update YAML config and trigger a config reload.
- **Failover not available mid-stream:** Once the first byte of a streaming response is received, the gateway cannot failover. This is an inherent constraint of streaming and is clearly documented — callers that require failover guarantees should use non-streaming completions.

### Neutral

- The `cmd/inference-gateway/` binary is a new independently deployable unit following the exact same operational model as `cmd/identity/`. No new operational patterns are introduced.
- Provider connectors live under `internal/inferencegw/providers/` — a sub-package pattern not yet used elsewhere but consistent with Go conventions and consistent with the feature-based isolation principle in [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md).
- The `PolicyEvaluator` stub is a permanent part of the codebase, not scaffolding to be deleted. When OPA ships, the stub is retained as the development-mode and test-mode default.
- The `api_dialect` column is included in the MVP `inference_usage` migration (`DEFAULT 'openai'`) specifically to avoid a schema migration when the Anthropic dialect ships at P1. This is a deliberate forwards-compatible design choice, not premature optimisation — the cost is one extra column with a static default value.

---

## Implementation

### Phase 1: Foundation (Weeks 1–2)

> **Usage record coverage note:** The `InferenceUsageRepository` PostgreSQL implementation is built in Phase 4. Phases 1–3 produce inference completions but no usage records are persisted. This is intentional. The "100% usage records" validation metric applies only from Phase 4 onward — Phase 1–3 exit criteria are scoped to handler correctness and provider connectivity, not usage attribution.

- `internal/inferencegw/models.go` — all core types
- `internal/inferencegw/connector.go` — `ProviderConnector` interface
- `internal/inferencegw/policy.go` — `PolicyEvaluator` interface + `AllowAllPolicyEvaluator`
- `internal/inferencegw/router.go` — `RoutingEngine` interface + `StrategyDirect` implementation
- `internal/inferencegw/providers/openai/connector.go` — OpenAI direct connector
- `internal/inferencegw/handlers_openai.go` — `POST /v1/chat/completions` (non-streaming + streaming); named `handlers_openai.go` from the start so Phase 5 adds `handlers_anthropic.go` with no rename
- `GET /v1/models` stub endpoint — returns the configured routing group names as model IDs; prevents OpenAI SDK clients (including Claude Code) from failing on their startup `GET /v1/models` probe with a 404
- `internal/inferencegw/service.go` — orchestration: route → evaluate → execute → record
- `cmd/inference-gateway/main.go` — binary entrypoint, dependency wiring
- `cmd/inference-gateway/config/` — YAML config for all environments
- `cmd/inference-gateway/migrations/` — `inference_usage` table migration
- `GET /healthz` endpoint
- `GET /readyz` endpoint (ready once provider connectors initialise)
- Note: `GET /metrics` (OTel export) lands in Phase 4 alongside the metrics instrumentation

### Phase 2: Multi-Provider (Weeks 3–4)

- `internal/inferencegw/providers/bedrock/connector.go`
- `internal/inferencegw/providers/vertexai/connector.go`
- `internal/inferencegw/providers/azureai/connector.go`
- Multimodal content pass-through (image/document content blocks)
- Connection pooling per provider (`internal/inferencegw/pool.go`)
- Comprehensive error normalisation across all four connectors

> **Bedrock streaming API (MINOR-8):** The Bedrock connector uses `ConverseStream`
> (not the older `InvokeModelWithResponseStream`). `ConverseStream` provides a
> unified multi-model interface with structured Go event types
> (`ContentBlockDeltaEvent`, `MessageStopEvent`) that carry token counts on the
> final event and map cleanly to the internal `StreamChunk` model.
> `InvokeModelWithResponseStream` requires per-model JSON parsing and does not
> standardise usage reporting across Bedrock model families. This choice is locked
> at Phase 2 — the connector must not mix the two APIs.

### Phase 3: Routing and Reliability (Weeks 5–6)

- `internal/inferencegw/pricing.go` — `PricingCache` with AWS/Vertex polling + Azure AI/OpenAI static config
- `StrategyLowestCost` routing implementation
- `StrategyFailoverChain` routing implementation
- Retry loop with exponential backoff and jitter
- Streaming-safe failover guard (no failover after first byte)
- Routing decision logging

### Phase 4: Observability (Weeks 7–8)

- `internal/inferencegw/usage_repository.go` — PostgreSQL implementation
- `GET /v1/usage` handler with full filter support
- Cost estimation for streaming requests (from final SSE `usage` chunk)
- OTel metrics ([opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md)): request rate, error rate, latency percentiles, token usage, cost per provider — exported via the OTel metric API (`go.opentelemetry.io/otel/metric`); no `github.com/prometheus/client_golang` dependency required
- Structured request/response metadata logging (metadata only; prompt body opt-in)
- Configurable request body logging with clear PII warning in docs

### Phase 5: Anthropic Messages API Dialect (P1 — Post-MVP)

> Dependent on Phase 1–4 completion. Designed in this ADR to confirm architectural fit; implemented as a follow-on sprint.

- `internal/inferencegw/handlers_anthropic.go` — `AnthropicHandler`: translates `POST /v1/messages` request/response to/from internal `ChatCompletionRequest`/`ChatCompletionResponse`; implements the Anthropic SSE event lifecycle serialiser (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`)
- Edge Envoy Lua filter configuration: `x-api-key` → `Authorization: Bearer` header promotion scoped to the `/v1/messages` path; added to the kora Gateway Envoy listener filter chain (infrastructure change, separate PR)
- `InferenceUsageRepository`: `api_dialect` column is already present in the MVP migration with `DEFAULT 'openai'` — no schema migration required at P1; `AnthropicHandler` writes `'anthropic'` to the field
- Handler registration: `POST /v1/messages` route registered alongside the existing `POST /v1/chat/completions` route in `cmd/inference-gateway/main.go`
- `InferenceUsageFilter.APIDialect` field added for filtering usage queries by dialect
- Integration tests: Claude Code connection test (`ANTHROPIC_BASE_URL` pointing at gateway, `ANTHROPIC_API_KEY` set to a real platform token obtained from the local identity service)

### SYSTEM Principal Handling

SYSTEM principals ([unified-identity-model](2026-03-05-unified-identity-model.md)) represent automated platform pipelines (e.g. scheduled
evaluation jobs, internal benchmarking runners) that carry service-account-style platform
tokens. The Inference Gateway handler treats them identically to AGENT principals:
the JWT is parsed, `claims.Orbit.PrincipalType` is read, policy is evaluated,
and usage is attributed with `principal_type = 'SYSTEM'`.

No special routing or short-circuit logic applies to SYSTEM callers. If the Policy Engine
later needs to differentiate them (e.g. exempt SYSTEM principals from cost-threshold
policies), that logic belongs in the `PolicyEvaluator` implementation — not in the handler.

The `inference_usage.principal_type` CHECK constraint includes `'SYSTEM'`. The
`InferenceUsageRecord.PrincipalType` comment documents all four valid values.

### Local Development

For Tier 1 local development (no cluster, no Envoy), no bypass or stub is required. The identity service runs locally via `make dev-up` + `make dev`. A real platform token is obtained with `make auth-device` and sent as `Authorization: Bearer <token>` directly to the inference gateway. The gateway parses the JWT normally — the same code path as production. No mock, no bypass, no header simulation.

### Migration Path

There is no existing Inference Gateway service to migrate from. Phase 1 is a greenfield implementation. The retired Model Gateway PRD (`docs/prd/model-gateway.md`) was not implemented — no existing code, database schema, or deployed artifact needs migration.

---

## Validation

### Success Metrics

- Gateway request overhead ≤ 50ms p95 (excludes provider response time) — verified via OTel histogram `inference_gateway_handler_duration_ms`
- Streaming first-byte latency equivalent to direct provider call ± 5ms overhead — no buffering introduced
- All four providers pass integration tests (mock provider stubs in unit tests; real provider smoke tests in staging)
- Usage records contain `tenant_id`, `principal_id`, `principal_type` on 100% of requests (Phase 4+ only — Phases 1–3 produce no usage records by design; see Phase 1 note) — verified by usage query integration tests
- `PolicyEvaluator` interface is called on 100% of requests — verified by handler unit tests using a mock evaluator

### Monitoring

- OTel metrics ([opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md)): `inference_gateway_request_total` (by provider, model, principal_type, api_dialect, status), `inference_gateway_handler_duration_ms` (histogram), `inference_gateway_stream_connections_active` (gauge), `inference_gateway_tokens_total` (by provider, model, direction), `inference_gateway_cost_usd_total` (by provider, model)
- Structured logs: every request produces a log line with full attribution — queryable in any standard log aggregation platform
- Alerting (post-MVP): provider error rate > threshold, pricing API staleness > 6h, cost spike per tenant

---

## Related Decisions

- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — Feature package structure; repository pattern
- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md) — YAML config system; secrets as env vars
- [database-infrastructure-layer](2026-03-03-database-infrastructure-layer.md) — PostgreSQL via `pgx/v5`; no ORMs
- [unified-identity-model](2026-03-05-unified-identity-model.md) — Unified Principal model; platform token schema; JTI blocklist
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE attestation; SVID lifecycle
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Envoy enforcement topology; Layer 1 / Layer 2 model; JWT validation at the edge
- [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — Istio + Cilium network enforcement
- [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) — Multi-binary deployment; `cmd/` structure; SPIFFE identity via pod labels
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — ext_proc sidecar; transparent platform token injection into agent outbound calls
- [opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md) — OTel observability discipline; span status rules
- ADR-TBD — Infrastructure / Helm chart credential provisioning (pending)

---

## References

- [Inference Gateway PRD](../prd/inference-gateway.md)
- [OpenAI API Reference](https://platform.openai.com/docs/api-reference) — API compatibility target
- [AWS Bedrock Converse API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_Converse.html)
- [Google Vertex AI generateContent API](https://cloud.google.com/vertex-ai/generative-ai/docs/reference/rest)
- [Azure AI Inference REST API](https://learn.microsoft.com/en-us/azure/ai-studio/reference/reference-model-inference-api)
- [Server-Sent Events specification](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [Anthropic Messages API Reference](https://docs.anthropic.com/en/api/messages) — P1 dialect target

---

## Notes

The Inference Gateway is the "motor cortex" of kora's nervous system metaphor — it translates agent intent (an inference request) into coordinated action (a model call) through the platform's unified identity, policy, and observability infrastructure. Every architectural decision in this ADR is made in service of that metaphor: the agent should be aware only of what it wants to do, not of how the infrastructure does it.

**Last Updated:** 2025-07-15
