# ADR: Provider Error Visibility in the Inference Gateway

**Date:** 2026-06-19
**Status:** Accepted
**Deciders:** Platform Engineering

---

## Context

The kora inference gateway wraps upstream provider errors into a generic `503 provider_error` response. The `ConnectorError` struct carries full structured detail (HTTP status, error code, provider message), but this information is lost at the API boundary and absent from server logs on streaming paths.

Three concrete problems:

1. **Silent streaming errors.** All streaming service methods (`ChatCompletionStream`, `AnthropicMessagesStream`, `ResponsesStream`) record a mid-stream provider error only as an OTel span attribute — no `logger.Error` call runs. Non-streaming paths do log via the `api.ErrorResponse` helper, but that log is triggered by the handler, not the service.

2. **Generic API response.** `inferenceErrorResponse` maps `ErrProviderError` to `api.ServiceUnavailableError("provider_error", "upstream provider returned an error", nil)` — all provider errors look identical to callers regardless of root cause.

3. **No inline error code in the UI table.** The Logs tab shows a status badge (`provider_error`) but the error code (`THROTTLED`, `CONTEXT_WINDOW_EXCEEDED`, etc.) is only visible inside the detail modal.

The goal is minimum-viable, low-risk changes that close all three gaps without restructuring the error-handling architecture.

---

## Decision

### 1. Add `logProviderError` helper to `Service`

A single private method handles structured provider error logging consistently across all service paths:

```go
func (s *Service) logProviderError(ctx context.Context, model string, err error) {
    fields := []logging.Field{
        {Key: "model", Value: model},
    }
    var connErr *ConnectorError
    if errors.As(err, &connErr) {
        fields = append(fields,
            logging.Field{Key: "provider", Value: string(connErr.Provider)},
            logging.Field{Key: "error_code", Value: connErr.Code},
        )
        if connErr.HTTPStatus != 0 {
            fields = append(fields, logging.Field{Key: "provider_http_status", Value: connErr.HTTPStatus})
        }
        if connErr.ProviderMessage != "" {
            fields = append(fields, logging.Field{Key: "provider_message", Value: connErr.ProviderMessage})
        }
    }
    s.logger.Error(ctx, "provider error", fields...)
}
```

This is called:
- In non-streaming service methods immediately after `callWithFailover` / `retryTargets` returns a non-nil error.
- In streaming goroutines immediately after `drainXxxStream` returns a non-nil error that is not `errClientDisconnected` (client disconnects are not provider errors).
- In `Embeddings` after `retryTargets` returns an error.

**Why a helper rather than inline?** All six affected methods have identical field requirements. Inlining would require six near-identical blocks with no shared path to evolve. A helper keeps the callers readable.

**Why `logger.Error` and not `logger.Warn`?** Provider errors are failures that affect the caller's request. They warrant error-level visibility in log aggregation pipelines. Client disconnects and budget blocks are not provider errors and are not logged here.

### 2. Improve `inferenceErrorResponse` for `ErrProviderError`

`inferenceErrorResponse` in `controller.go` currently returns a generic 503 for all `ErrProviderError` cases. The change adds a specific `errors.As` branch before the `errors.Is(err, ErrProviderError)` switch case:

```go
// ConnectorErrors that are already classified as client errors are handled first
// (MODEL_NOT_SUPPORTED, INVALID_REQUEST — already present). Then:

case errors.Is(err, ErrProviderError):
    var connErr *ConnectorError
    if errors.As(err, &connErr) && connErr.ProviderMessage != "" {
        details := map[string]any{"error_code": connErr.Code}
        return api.ServiceUnavailableError("provider_error", connErr.ProviderMessage, details)
    }
    return api.ServiceUnavailableError("provider_error", "upstream provider returned an error", nil)
```

**Why surface `ProviderMessage`?** The `ConnectorError` definition already documents: *"Safe to surface to API callers — contains no internal infrastructure detail."* Providers (Anthropic, Bedrock, Azure, Vertex AI) return structured JSON error bodies; `MapAnthropicError` and `MapOpenAIError` extract only the `message` field from those bodies. This is equivalent to what a caller would receive if they called the provider directly.

**Why not surface `ProviderMessage` for `INVALID_REQUEST`?** `INVALID_REQUEST` already has its own branch that passes `connErr.ProviderMessage` through. No change needed there.

**Why keep the 503 status?** Changing error HTTP status codes (e.g. returning 429 for THROTTLED) affects client retry behaviour and is a breaking change for integrations that rely on the current status. That decision is deferred to a separate ADR.

**`error_code` in details.** The `details` map carries `error_code` using kora's stable code values (THROTTLED, CONTEXT_WINDOW_EXCEEDED, AUTH_FAILED, etc.) — not raw provider strings. Callers can branch on this for targeted handling.

### 3. Show `error_code` in Logs tab table rows

The `status` column in `logs-tab.tsx` currently renders only a `<Badge>`. The cell renderer is updated to include `error_code` as a monospaced caption below the badge when `row.error_code` is non-null:

```tsx
cell: (row) => (
  <Stack gap="tight">
    <Badge variant={getInferenceStatusVariant(row.status)} size="sm">
      {fmtInferenceStatus(row.status)}
    </Badge>
    {row.error_code && (
      <Text size="caption" color="muted" mono>
        {row.error_code}
      </Text>
    )}
  </Stack>
),
```

The search function is extended to match against `row.error_code`:

```tsx
searchFn={(row, q) =>
  row.id.toLowerCase().includes(q) ||
  row.model_id.toLowerCase().includes(q) ||
  (row.virtual_model_id?.toLowerCase().includes(q) ?? false) ||
  (row.error_code?.toLowerCase().includes(q) ?? false)
}
```

**Why inline rather than a new column?** Adding a column for error code would require all users to unhide it (it would be hidden by default) or it would take up permanent table width even for rows where it's null. Inlining beneath the status badge is consistent with the existing model/provider two-line cell pattern used elsewhere in the table.

---

## Alternatives Considered

### Alt 1: Return different HTTP status codes per error code

Map THROTTLED → 429, AUTH_FAILED → 401, CONTEXT_WINDOW_EXCEEDED → 400, etc.

**Rejected for now.** This is a breaking change. Agent runtimes (Claude Code, Codex) have built-in retry logic keyed to specific status codes. Returning 429 for THROTTLED is correct semantics but would cause agents that currently handle 503 to stop retrying. This should be a deliberate opt-in change, not bundled with an observability improvement.

### Alt 2: Add a dedicated `/v1/logs` admin endpoint that returns full error details

Expose a tenant-admin API endpoint that returns all inference logs with full error detail, scoped to the tenant.

**Rejected for this scope.** The logs data is already accessible via `GET /v1/usage` (scoped to the authenticated principal). Admin cross-user visibility is already flagged as a future consideration in the inference-logs PRD. The observability improvements in this ADR don't require a new endpoint.

### Alt 3: Log provider errors only at the handler level (not the service)

Add structured logging inside `inferenceErrorResponse` or in each handler's error branch.

**Rejected.** The handler doesn't have model identity at the point where it calls `inferenceErrorResponse` — it would need to be threaded through. More importantly, streaming errors are communicated through channels, not return values from the service method — the handler level has no natural place to intercept them. The service layer is the right layer because it has full context for all paths.

### Alt 4: Use a separate `error_details` column in the API error response

Return a top-level `error_details` object in the API error JSON with all provider fields.

**Rejected.** The existing `details` map in `APIError` is sufficient. A separate top-level field would require a contract change with all callers. Nesting under `details` is backwards-compatible.

---

## Consequences

### Positive

- Engineers can self-debug provider errors from the API response alone — no UI navigation required.
- Server logs are complete for all request patterns including streaming; log-based alerting and metrics on `error_code` are now possible.
- The Logs tab gives engineers an at-a-glance summary of what kind of errors are occurring without clicking into every row.

### Negative / Trade-offs

- `ProviderMessage` is forwarded verbatim from the provider's error body. If a provider includes unexpected content in their error message (e.g. partial request echoing), it reaches the caller. This is mitigated by the 500-character truncation already applied at the `extractFailureDetails` level before DB storage; however, the API response path truncation is applied at the source (`ProviderMessage` is extracted from the provider's JSON body via `MapOpenAIError` / `MapAnthropicError` — it is always the `message` field of a structured error object, never raw HTTP body content).
- Slightly larger API error response payloads (message field plus a small `details` object). Not a concern at expected volumes.

### Risks

- **False positive provider messages:** A provider error message could reference provider-internal infrastructure (e.g. hostnames or internal error references). Review of the current provider SDKs shows the `message` field in Anthropic and OpenAI error bodies is always user-facing — this is the same message that would reach a caller accessing the provider directly.

---

## Implementation Notes

All changes are confined to:
- `backend/internal/inferencegw/service.go` — `logProviderError` helper + calls
- `backend/internal/inferencegw/controller.go` — `inferenceErrorResponse` improvement
- `frontend/src/app/(dashboard)/model-gateway/_components/logs-tab.tsx` — status cell + search

No database schema changes, no new endpoints, no migration required.
