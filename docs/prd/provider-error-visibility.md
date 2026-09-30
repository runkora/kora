# Provider Error Visibility

**Status:** Implemented
**Created:** 2026-06-19
**Owner:** Platform Engineering
**Stakeholders:** Engineers using kora as an API gateway

---

## Executive Summary

When an engineer's code or agent hits a provider API error through the kora inference gateway, they receive a generic `503 provider_error` response with no actionable detail. Debugging requires either reading raw server logs (hard to correlate) or navigating to the kora UI logs tab and clicking into a detail modal. This feature makes provider errors self-explanatory at the API level, prominently visible in the logs table, and properly instrumented in server logs.

---

## Problem Statement

### Current State

The kora inference gateway wraps every upstream provider error into a uniform `ErrProviderError` before returning it to callers. The `ConnectorError` struct carries full detail — HTTP status, provider error code (e.g. `THROTTLED`, `CONTEXT_WINDOW_EXCEEDED`), and the provider's own error message — but this information is:

1. **Dropped at the API boundary.** Callers receive `{"code":"provider_error","message":"upstream provider returned an error"}` regardless of whether the failure was a rate limit, an invalid request, a model not found, or a transient service outage.
2. **Silent in server logs for streaming paths.** Non-streaming paths log the error string via `ErrorResponse`, but all streaming paths (`ChatCompletionStream`, `AnthropicMessagesStream`, `ResponsesStream`) record only an OTel span error — no `logger.Error` call runs when mid-stream provider errors occur.
3. **Buried in the UI.** The logs tab table row shows a generic `provider_error` status badge; the actual error code and provider message are only visible inside the detail modal after clicking a row.

### User Pain Points

- An engineer's agent gets a `503` — they don't know if it's a rate limit, an auth problem, a model not found, or an internal error. They have to either parse server logs or navigate to the kora UI.
- Operators grepping server logs for provider errors see only the Go error string (`"provider bedrock error [THROTTLED]: ..."`) with no structured fields — cannot alert on `error_code = THROTTLED` or filter by provider.
- Engineers scanning the Logs tab to understand a spike in failures see all rows as `provider_error` with no differentiation between throttled vs. auth-failed vs. model-not-found.
- Streaming errors (the majority of production traffic) are completely absent from server logs — an error that occurs mid-stream after 10 seconds of content produces no log line.

### Business Impact

- Longer debugging cycles when provider-side incidents occur — engineers cannot tell the root cause without raw log access.
- Missed alerting opportunities: because streaming errors don't produce log lines, SLO dashboards based on log-derived metrics miss a large class of errors.
- Support burden: engineers open tickets for errors that are self-explanatory (e.g. "I got a 503, is kora down?") when the answer is "Bedrock throttled you".

---

## Goals and Objectives

### Primary Goals

1. Make provider errors **self-debugging**: the API response to the caller includes enough detail to understand and act on the error without consulting logs or the UI.
2. Ensure **structured server logs** are emitted for every provider error regardless of whether the call was streaming or non-streaming.
3. Surface the **error code** directly in the Logs tab table rows so engineers can scan failures at a glance.

### Success Metrics

- An engineer calling the inference gateway gets a `provider_error` response that includes the provider's error code (e.g. `THROTTLED`) and a human-readable message from the provider.
- A search for `error_code=THROTTLED` in the server log pipeline returns all throttled requests, including streaming ones.
- An engineer looking at the Logs tab can see error codes inline in the table without clicking into each row.

### Non-Goals

- Prompt/completion body logging (content is never stored).
- Changing the HTTP status codes returned for different error types (out of scope for this feature; requires separate discussion).
- Adding new filtering or search dimensions to the logs page (only visual improvement to the existing status column).
- Admin cross-tenant log visibility.

---

## Proposed Solution

### 1. Structured provider error logging in the service layer

A shared helper `logProviderError` is added to `Service` in `service.go`. It emits a `logger.Error` log entry with structured fields: `provider`, `model`, `error_code`, `provider_http_status`, and `provider_message` (when available). This helper is called in every error path across all six inference methods (non-streaming and streaming variants of ChatCompletion, AnthropicMessages, Responses, and Embeddings).

Streaming goroutines — which currently record only an OTel span error — gain identical structured log coverage.

### 2. Provider error details in the API response

`inferenceErrorResponse` in `controller.go` is updated: when the underlying error is an `ErrProviderError` that wraps a `ConnectorError`, the API response:
- Uses the `ConnectorError.ProviderMessage` as the response message (already documented as safe to surface — no internal infrastructure detail).
- Includes `error_code` in the `details` map.

This gives callers actionable information immediately. The `ConnectorError.ProviderMessage` field comment already says "Safe to surface to API callers — contains no internal infrastructure detail."

### 3. Error code visible in the Logs tab table

The Status column in `logs-tab.tsx` is updated to render the `error_code` as a subtitle beneath the status badge when present. This makes it possible to scan a page of failures and distinguish `THROTTLED` from `AUTH_FAILED` from `CONTEXT_WINDOW_EXCEEDED` without opening each row's detail modal.

The table search function is also extended to match against `error_code`.

---

## Scope

### In Scope

- `logProviderError` helper + calls in all six service methods.
- `inferenceErrorResponse` improvement for `ErrProviderError`.
- Logs tab status column showing `error_code` as subtitle.
- Logs tab search function extended to match `error_code`.

### Out of Scope

- HTTP status code changes (e.g. returning 429 for THROTTLED instead of 503).
- New log filtering/faceting on the logs page.
- Admin log views.
- Changes to `FailureDetails` schema or DB storage (already complete).

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `logger.Error` is called with structured fields (`provider`, `model`, `error_code`, `provider_http_status`, `provider_message`) for every provider error in all six service methods.
- [ ] Streaming error paths produce a log entry (currently silent).
- [ ] `inferenceErrorResponse` passes `ProviderMessage` and `error_code` through for `ErrProviderError` cases.
- [ ] Logs tab status column shows `error_code` as a subtitle when present.

**Should Have (P1):**
- [ ] Table search matches against `error_code`.
- [ ] The detail modal continues to show the full `failure_details` (already implemented).

### Non-Functional Requirements

**Security:**
- `ConnectorError.ProviderMessage` is already documented as safe to surface. No provider-internal infrastructure details (connection strings, internal hostnames, etc.) are present in this field — the SDK extracts it from the provider's own API error response body.
- `error_code` values are controlled by kora (a fixed enum like `THROTTLED`, `CONTEXT_WINDOW_EXCEEDED`) — no raw provider error codes are forwarded.

**Performance:**
- No new database queries or API calls. Changes are all in the logging and response-shaping layer.

**Observability:**
- Structured log fields enable log-based metrics and alerts on `error_code` without regex parsing.

---

## Dependencies

- `ConnectorError` struct (already carries `ProviderMessage`, `HTTPStatus`, `Code`).
- `extractFailureDetails` and `extractErrorCode` helpers (already exist in `service.go`).
- `FailureDetails` stored in `inference_usage` (already complete).
- `InferenceUsageRecord.error_code` exposed in the API and frontend types (already complete).

---

## Related Documents

- [inference-logs PRD](./inference-logs.md) — original logs feature
- [ADR: provider-error-visibility](../adr/2026-06-19-provider-error-visibility.md)
