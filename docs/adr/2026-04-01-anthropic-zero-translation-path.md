# Anthropic Zero-Translation Path

**Status:** Accepted
**Date:** 2026-04-01
**Deciders:** kora platform team
**Technical Story:** The `POST /v1/messages` (Anthropic Messages API) handler originally converted the incoming Anthropic wire format through kora's internal `ChatCompletionRequest`/`ChatCompletionResponse` domain types before forwarding to the provider. This lossy conversion silently dropped tool use, multi-turn system prompts, and other Anthropic-specific fields, meaning Claude Code and other Anthropic-native clients would behave incorrectly when routed through kora.

---

## Context

### Background

The Inference Gateway exposes two inference dialects:

- `POST /v1/chat/completions` — OpenAI Chat Completions wire format.
- `POST /v1/messages` — Anthropic Messages API wire format.

The original `handlers_anthropic.go` implementation decoded the `POST /v1/messages` body into hand-rolled wire structs, then called `anthropicRequestToInternal()` to convert to the shared `ChatCompletionRequest` domain type, forwarded through `svc.ChatCompletion(DialectAnthropic)`, then called `internalResponseToAnthropic()` to convert back to the Anthropic wire format for the response.

This approach had three confirmed bugs:

1. **`stop_reason` double-mapping.** `FromAnthropicMessage` stored the native Anthropic `StopReason` value directly into `ChatCompletionResponse.FinishReason`. Then `internalResponseToAnthropic` passed that value through `mapToAnthropicStopReason()`, which treated it as an OpenAI value — producing garbage output (e.g. `"stop"` → `"end_turn"` → re-mapped to `"end_turn"` accidentally, or `"end_turn"` treated as unknown and discarded).

2. **Tool use silently dropped.** `ChatCompletionRequest` has no tool-use fields. Any `tools`, `tool_choice`, or `tool_result` content blocks in the Anthropic request body were silently discarded during `anthropicRequestToInternal()`.

3. **`system` field wrong type.** The Anthropic Messages API accepts `system` as an array of content blocks (`[]TextBlockParam`). The hand-rolled struct modelled it as a single string, silently truncating multi-block system prompts.

The root cause is structural: the `ChatCompletionRequest`/`ChatCompletionResponse` internal types are designed around the OpenAI schema. The Anthropic schema has a fundamentally different structure (content blocks, tool use, separate system param, streaming event types) and cannot be losslessly represented in the shared domain type.

### Goals

- `POST /v1/messages` must forward requests to Anthropic-compatible provider endpoints with zero information loss.
- `POST /v1/messages` responses must be byte-compatible with the Anthropic wire format — no field remapping, no value translation.
- Tool use, multi-turn system prompts, vision content blocks, and all other Anthropic-specific features must pass through unchanged.
- The internal domain types (`ChatCompletionRequest`/`ChatCompletionResponse`) are preserved for use by the OpenAI dialect path and for usage/metrics recording.

### Non-Goals

- Removing the `ChatCompletion`/`ChatCompletionStream` OpenAI dialect path — it remains the primary path for non-Anthropic models.
- Cross-dialect failover (automatically retrying an Anthropic request via the OpenAI path on a different provider) — deferred; not lossless.
- Operator-visible dialect selection — the dialect is determined by which endpoint the client called.

---

## Decision Drivers

* **Correctness for Claude Code.** Claude Code uses the full Anthropic Messages API surface including tool use, streaming, and complex content blocks. Any lossy translation breaks its behaviour.
* **SDK-native types as the boundary.** The `anthropic-sdk-go` library ships `anthropic.MessageNewParams` and `*anthropic.Message` with generated `UnmarshalJSON`/`MarshalJSON` that are guaranteed to match the Anthropic wire format exactly. Using these types as the handler boundary eliminates the translation problem at its source.
* **Separation of concerns.** Metrics and usage recording operates on token counts, not on the full request/response body. The internal domain type is a sufficient representation for that purpose — it does not need to be the transport type.

---

## Considered Options

### Option A: Fix the internal domain type

Extend `ChatCompletionRequest` and `ChatCompletionResponse` with Anthropic-specific fields (tool use, content block arrays, system block array, etc.) and fix `anthropicRequestToInternal`/`internalResponseToAnthropic` to round-trip them correctly.

**Pros:**
- Single code path through the service layer.

**Cons:**
- The two schemas are structurally incompatible at a deep level. A unified type either becomes a union of both (large, confusing) or a lowest-common-denominator (lossy by design).
- Every new Anthropic API feature (e.g. extended thinking, document blocks) requires a corresponding update to the shared type, creating permanent coupling.
- Does not solve the problem for future providers that speak neither OpenAI nor Anthropic schema.

### Option B: Zero-translation passthrough using SDK types (chosen)

Extend the `AnthropicConnector` interface with two new methods that use native SDK types:

```go
AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan AnthropicStreamEvent, error)
```

The handler decodes directly into `anthropic.MessageNewParams` (using its own `UnmarshalJSON`), calls the service layer's `AnthropicMessages`/`AnthropicMessagesStream` methods, and serializes the response directly from `*anthropic.Message` or from raw `AnthropicStreamEvent` channel values. No intermediate kora domain types are involved in the request/response path.

Usage metrics are recorded from `msg.Usage.InputTokens`/`msg.Usage.OutputTokens` on the response — the token counts the service layer needs are available on the SDK type without requiring a full conversion.

**Pros:**
- Zero information loss: the SDK types are the authoritative Anthropic wire representation.
- No maintenance burden: new Anthropic API fields are handled automatically by the SDK.
- Clean separation: domain types are for OpenAI dialect and internal metrics; SDK types are for Anthropic dialect transport.

**Cons:**
- Anthropic SDK types now appear in the handler and service layers (previously confined to the convert package). Managed by keeping them behind the `AnthropicConnector` interface boundary.
- Cross-dialect failover (retrying a failed Anthropic request via the OpenAI path) is not possible on this path — the domain type conversion would be lossy. Acceptable trade-off; failover across providers on the same dialect is still supported.

### Option C: Direct HTTP proxy

Capture the raw `POST /v1/messages` body bytes and forward them directly to the Bedrock Anthropic endpoint, bypassing the SDK entirely.

**Pros:**
- Absolute zero translation.

**Cons:**
- Breaks policy enforcement, auth injection, usage recording, and routing.
- Not composable with the failover or lowest-cost routing strategies.
- Cannot inject the correct model ID for the target provider.

---

## Decision

**Chosen Option:** Option B — zero-translation passthrough using native Anthropic SDK types.

### Rationale

The lossy translation bugs were not implementation defects that could be patched — they were a consequence of using a schema-mismatched intermediate type. Option A would require maintaining permanent coupling between the kora domain model and the Anthropic API schema. Option C bypasses all the infrastructure kora provides. Option B uses the SDK-generated types as the transport boundary, which is the correct layering: the provider SDK owns the wire format, kora owns the routing and policy.

---

## Consequences

### Positive

- `POST /v1/messages` is now byte-compatible with the Anthropic API: tools, vision, multi-turn system prompts, streaming, and all other features work correctly for Claude Code and other Anthropic-native clients.
- New Anthropic API features (extended thinking, document blocks, etc.) are handled transparently by SDK upgrades with no kora code changes.
- The `stop_reason` double-mapping, tool-use drop, and `system` type bugs are eliminated structurally, not by patching.

### Negative

- `anthropic.MessageNewParams` and `*anthropic.Message` appear at the service and handler layers. This is a deliberate exception to the "SDK types confined to the convert package" guideline, justified by the correctness requirement.
- Cross-dialect failover on the Anthropic path is not supported: if Bedrock rejects a `POST /v1/messages` request, kora cannot transparently retry via the OpenAI path on Azure/Vertex without lossy conversion.

### Neutral

- The old `AnthropicChatCompletion`/`AnthropicChatCompletionStream` domain-type methods are retained on the `AnthropicConnector` interface for the OpenAI-dialect internal path (when `ChatCompletion` self-dispatches Anthropic models to `anthropicChatCompletion`).
- Usage metrics are recorded from `msg.Usage.InputTokens`/`msg.Usage.OutputTokens` — no conversion required.
- The `messagesEnvelope` type (with custom `UnmarshalJSON`) captures both `anthropic.MessageNewParams` and the `stream bool` flag from the same request body bytes, since `MessageNewParams` is an outbound type and does not model the `stream` field.

---

## Implementation

### 1. `AnthropicStreamEvent` (`internal/inferencegw/connector.go`)

```go
// AnthropicStreamEvent wraps a raw Anthropic SDK stream event with an optional error.
// On a mid-stream provider error, Err is non-nil and Event is the zero value.
type AnthropicStreamEvent struct {
    Event anthropic.MessageStreamEventUnion
    Err   error
}
```

Using a wrapper type (rather than `<-chan anthropic.MessageStreamEventUnion`) is required to propagate mid-stream errors through the channel without silently closing it — which was the behaviour of the previous implementation.

### 2. `AnthropicConnector` interface extension (`internal/inferencegw/connector.go`)

```go
type AnthropicConnector interface {
    // Normalised path — used for internal failover and metrics.
    AnthropicChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)
    AnthropicChatCompletionStream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, error)

    // Zero-translation path — used for POST /v1/messages.
    AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
    AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan AnthropicStreamEvent, error)
}
```

All three provider connectors (Bedrock, Azure, Vertex AI) implement both method pairs. Providers that cannot support the Anthropic dialect return `ErrDialectNotSupported`.

### 3. Bedrock implementation (`providers/bedrock/anthropic.go`)

Both `AnthropicMessages` and `AnthropicMessagesStream` use `c.anthropicClient.Messages.NewStreaming` internally (the Anthropic SDK always streams at the transport level; non-streaming is assembled from the full stream). The model ID is injected from `params.Model` which is set by the service layer to the resolved canonical ID.

### 4. Service layer (`internal/inferencegw/service.go`)

```go
func (s *Service) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
func (s *Service) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan AnthropicStreamEvent, error)
```

Both methods:
1. Call `s.router.RouteRequest` to resolve the target provider (using `params.Model` as the model field).
2. Set `params.Model` to the resolved canonical model ID.
3. Dispatch to the connector's `AnthropicMessages`/`AnthropicMessagesStream`.
4. Record usage from `msg.Usage.InputTokens`/`msg.Usage.OutputTokens`.

### 5. Handler (`internal/inferencegw/handlers_anthropic.go`)

```go
// messagesEnvelope decodes both anthropic.MessageNewParams and the stream flag
// from the same request body bytes.
type messagesEnvelope struct {
    Params anthropic.MessageNewParams
    Stream bool
}

func (e *messagesEnvelope) UnmarshalJSON(b []byte) error {
    if err := e.Params.UnmarshalJSON(b); err != nil {
        return err
    }
    var raw struct{ Stream bool `json:"stream"` }
    _ = json.Unmarshal(b, &raw)
    e.Stream = raw.Stream
    return nil
}
```

`handleMessages` decodes via `api.DecodeJSON` into `messagesEnvelope`, dispatches to `handleMessagesStream` if `env.Stream` is true, otherwise calls `svc.AnthropicMessages` and writes the `*anthropic.Message` directly via `api.JSON`.

`handleMessagesStream` writes SSE by ranging over the `<-chan AnthropicStreamEvent` channel, marshalling `ev.Event` directly for each event. Event names are taken from `ev.Event.Type`. Mid-stream errors from `ev.Err` are written as `event: error` SSE frames.

### 6. Bedrock `ChatCompletion` / `ChatCompletionStream` self-dispatch

The Bedrock connector's `ChatCompletion` and `ChatCompletionStream` methods inspect the model ID at call time:

```go
func (c *Connector) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
    if isAnthropicModel(req.Model) {
        return c.anthropicChatCompletion(ctx, req)
    }
    return c.openaiChatCompletion(ctx, req)
}
```

`isAnthropicModel` checks `strings.Contains(id, "anthropic.")`, which matches both bare IDs (`anthropic.claude-*`) and all cross-region inference profile prefixes (`us.anthropic.*`, `apac.anthropic.*`, `global.anthropic.*`). This ensures OpenAI-dialect callers targeting Claude models are routed to the correct AWS transport automatically.

---

## Validation

### Success Metrics

- `POST /v1/messages` with a `tools` array returns correct `tool_use` content blocks in the response.
- `POST /v1/messages` with `stream: true` produces valid SSE with correct `event:` names matching the Anthropic streaming spec.
- `POST /v1/messages` with an array `system` field (multiple `TextBlockParam` entries) does not truncate any blocks.
- `stop_reason` in non-streaming responses is the raw Anthropic value (`end_turn`, `tool_use`, `max_tokens`, `stop_sequence`) with no remapping.
- `make check` passes with zero new lint issues.

---

## Related Decisions

- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference Gateway architecture.
- [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) — Provider model catalog and alias routing; implemented concurrently with this ADR.

---

## References

- [Anthropic Messages API reference](https://docs.anthropic.com/en/api/messages)
- [anthropic-sdk-go `MessageNewParams`](https://pkg.go.dev/github.com/anthropics/anthropic-sdk-go#MessageNewParams)
- [Anthropic streaming event types](https://docs.anthropic.com/en/api/messages-streaming)

---

**Last Updated:** 2025-07-22
