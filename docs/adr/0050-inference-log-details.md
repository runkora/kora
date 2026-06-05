# 0050. Inference Log Detail Modal and Failure Details Capture

**Status:** Accepted
**Date:** 2026-05-08
**Deciders:** Platform Engineering
**Technical Story:** [Inference Log Details PRD](../prd/inference-log-details.md) — surfacing per-request debugging information through a detail modal and structured request metadata capture.

---

## Context

The `inference_usage` table records 22 fields per request covering identity, model selection, token counts, cost, status, and latency. The Logs tab in the Model Gateway renders a table with 9 of those columns and no row-level interaction. Two gaps limit debuggability:

1. **No detail view.** Fields like `routing_strategy`, `retry_count`, `api_dialect`, `principal_type`, and the full cache token breakdown are fetched on every page load but never rendered. A developer who sees a `provider_error` row cannot determine the routing path taken, the retry count, or the specific error without digging into distributed traces or application logs.

2. **No request shape capture.** The request parameters that drove the call — `max_tokens`, `temperature`, `message_count`, whether tools were present, whether streaming was requested — are not stored. Neither is any structured failure information from the provider beyond a terse `error_code` string. There is no way to answer "did this fail because `max_tokens` was set too low?" or "what did the provider actually say?"

This ADR records the architectural decisions made for Layers 1 and 2 of the inference log detail feature.

### Background

The Orbit Model Gateway (`cmd/inference-gateway`) processes requests across three API dialects (OpenAI, Anthropic, Responses) and translates them to a unified internal representation before dispatching to a provider connector. The unified request struct is fully materialised in `service.go` before any provider call is made, making it a natural capture point for request metadata.

The existing `inference_usage` write path has five branches in `service.go`: success, policy denied, routing failed, provider error, and client canceled. Each branch constructs an `InferenceUsageRecord` and calls `repository.Create`. Both new fields can be populated at each branch without restructuring the write path.

The frontend table is a `DataTable` component from `@orbit/terra`. The Models tab in the same section of the application (`model-gateway/`) already implements a row-click detail modal via `ModelDetailModal`, triggered by `onRowClick={setSelectedModel}` on `DataTable`. The Terra `Modal` component (`@radix-ui/react-dialog` under the hood) supports a `constrained` body variant that limits height to `max-h-[60vh]` with internal scroll.

### Goals

- Expose all existing `InferenceUsageRecord` fields through an in-page detail view.
- Capture non-PII request shape parameters and structured provider failure context on every new request.
- Keep both changes additive: no existing column is removed or retyped, no existing API contract changes.

### Non-Goals

- Storing prompt or completion content (message text, system prompt body, tool definitions) — a separate Layer 3 PRD is required.
- Filtering the logs table by `request_metadata` fields (deferred to a future iteration).
- Backfilling historical rows.

---

## Decision Drivers

* **Pattern consistency**: Orbit already uses row-click modals for detail views; diverging requires introducing a new component and a second interaction paradigm.
* **No unnecessary joins**: the detail view accesses one row; the data model should not require a join to render it.
* **Schema evolution**: provider-specific error shapes and new model parameters will expand over time; the schema must accommodate this without migrations per addition.
* **Privacy by design**: request metadata must capture only non-PII scalars; no user-generated text may be stored under either new column.
* **Zero downtime migration**: the `inference_usage` table is append-heavy and live in production; the migration must not lock the table.

---

## Considered Options

### Decision 1: Detail view presentation — Modal vs. Drawer

#### Option A: Modal (Terra `Modal` component, `size="lg"`)
Uses the existing Terra `Modal` component with `size="lg"` (512px) and `<ModalBody constrained>` for internal scrolling. Triggered by `onRowClick` on `DataTable`, matching the `ModelDetailModal` pattern in the Models tab.

**Pros:**
- No new UI component required; no new interaction pattern introduced.
- The `constrained` body prop handles the ~20-field volume with scroll.
- Consistent with every other detail view in the application.
- Dismissible via Escape, overlay click, and close button — accessibility handled by Radix Dialog.

**Cons:**
- Fixed 512px width is narrower than a drawer for side-by-side comparison.
- If Layer 3 (content logging, full message threads) ships, 512px will be cramped for rendering conversation turns.

#### Option B: Sheet / Slide-over Drawer
A panel that slides in from the right, leaving the table partially visible. Wider surface, better for content-heavy views.

**Pros:**
- More vertical space; better suited for future content logging.
- Keeps the table partially visible for comparison.

**Cons:**
- No existing Sheet component in Terra; introduces a new component and interaction paradigm.
- Inconsistent with the established row-detail pattern.
- Premature for the field volume in Layers 1 and 2.

---

### Decision 2: Request metadata storage — JSONB column vs. normalised table

**Removed from scope.** Capturing `request_metadata` requires per-dialect extraction across the three SDK-native parameter types (OpenAI, Anthropic, Responses). No unified internal request struct exists in `service.go` at the point of capture; the gateway dispatches dialect-native types directly to connectors. The implementation cost is disproportionate to the debugging value for v1. `request_metadata` is dropped entirely. If reintroduced, a shared extraction function called from each dialect branch is the recommended approach.

---

### Decision 2 (renumbered): Failure details storage — JSONB column vs. normalised table

Same trade-off as Decision 2. Additionally: provider error shapes differ across connectors (Bedrock, VertexAI, Azure OpenAI each have distinct error envelopes). Normalising to individual columns would require either a lowest-common-denominator schema or a connector-specific table per provider, both of which are premature.

#### Option A: `failure_details JSONB` column on `inference_usage`
Nullable; populated only on `provider_error` records. Stores provider HTTP status, error type, and error message.

#### Option B: Separate `inference_failure_details` table
Same cons as Option B above, compounded by the provider-specific shape problem.

---

## Decision

### Decision 1: Modal

**Chosen Option: A — Terra `Modal` with `size="lg"` and constrained body.**

The field volume for Layers 1 and 2 fits within a scrollable `size="lg"` modal. The pattern matches the existing `ModelDetailModal` implementation exactly. No new component is needed.

**Caveat:** If Layer 3 (content logging) ships, the detail surface should be re-evaluated. Rendering full message threads in a 512px modal is not appropriate; a Sheet component would be worth introducing at that point.

### Decision 2: `failure_details JSONB` column

**Chosen Option: A — JSONB column on `inference_usage`.**

Same rationale as the dropped `request_metadata` option: 1:1 relationship, write-once, read-on-detail access pattern. Provider-specific error shape variation is an additional argument against normalised columns.

Populated only for `provider_error` and `auth_error` statuses. NULL for `success`, `canceled`, `timeout`, `routing_failed`, and `policy_denied` (no provider was reached and `policy_rule` is out of scope for v1 — the existing `error_code` field is sufficient for policy denial attribution).

| Field | Type | Populated for |
|---|---|---|
| `provider_http_status` | integer or null | `provider_error`, `auth_error` |
| `provider_error_type` | string or null | `provider_error`, `auth_error` |
| `provider_error_message` | string or null (max 500 chars) | `provider_error`, `auth_error` |

`provider_error_message` is truncated to 500 characters before storage. Some providers echo partial request content in error messages; a hard cap limits storage growth and reduces exposure if that occurs.

**Note on `ConnectorError`:** `provider_http_status` requires a new `HTTPStatus int` field on `ConnectorError`. Currently the HTTP status is used to derive `Code` and `Retryable` and then discarded. This is an unacknowledged implementation requirement — `ConnectorError` must be extended as part of Layer 2.

---

## Consequences

### Positive

- Developers can diagnose failed requests without leaving the Orbit UI or consulting distributed traces.
- All 22 existing `InferenceUsageRecord` fields become accessible via one click; no data is wasted.
- `failure_details` is captured in the existing write path with minimal structural change.
- The JSONB column can evolve (new fields added to the JSON object) without a migration.
- The modal pattern and Terra component are reused; no new UI infrastructure is introduced.

### Negative

- Pre-migration rows have NULL `failure_details`; the modal must handle this gracefully (omit the Failure section's structured fields when NULL rather than rendering empty states).
- JSONB fields are not individually queryable without a GIN index; filtering by provider error type requires a future migration.
- The 512px modal width is a constraint if the detail view ever needs to render wider content.
- `ConnectorError` must be extended with `HTTPStatus int` to populate `provider_http_status` — this is additional implementation work not reflected in the original write-path description.

### Neutral

- `failure_details` is not populated for `timeout` status. If timeout diagnostics become important, a future iteration can add a `timeout_phase` field (e.g. `connection`, `first_byte`, `streaming`) to `failure_details`.
- `policy_rule` is not captured in v1. The existing `error_code` field is sufficient for policy denial attribution at current scale.

---

## Implementation

### Migration

```sql
-- +goose Up
ALTER TABLE inference_usage
    ADD COLUMN failure_details JSONB;

-- +goose Down
ALTER TABLE inference_usage
    DROP COLUMN IF EXISTS failure_details;
```

`ADD COLUMN ... DEFAULT NULL` is a metadata-only operation in PostgreSQL 11+; no table rewrite or lock escalation beyond `ACCESS EXCLUSIVE` for the duration of the catalog update (milliseconds on a live table).

### Backend

`InferenceUsageRecord` in `internal/inferencegw/models.go` gains one new field:

```go
FailureDetails *FailureDetails `json:"failure_details"`
```

Corresponding Go struct:

```go
type FailureDetails struct {
    ProviderHTTPStatus   *int    `json:"provider_http_status"`
    ProviderErrorType    *string `json:"provider_error_type"`
    ProviderErrorMessage *string `json:"provider_error_message"` // truncated to 500 chars before storage
}
```

**Prerequisite:** `ConnectorError` must gain an `HTTPStatus int` field. Currently the HTTP status is consumed to derive `Code` and `Retryable` and then discarded at the connector level. `FailureDetails` construction depends on this value being preserved.

The `repository.go` INSERT gains one new parameter for `failure_details`, passed as a `pgtype.JSONB` value (NULL when the pointer is nil).

In `service.go`, `FailureDetails` is constructed from `ConnectorError` fields (for `provider_error` and `auth_error`) at their respective write-path branches. It is nil (stored as NULL) for all other statuses.

Capture failures must not propagate to the caller — log the error and omit the field (pass NULL) rather than failing the usage write.

### Frontend

`InferenceUsageRecord` in `frontend/src/app/(dashboard)/model-gateway/_types/index.ts` gains:

```typescript
failure_details: FailureDetails | null;
```

A new `InferenceLogDetailModal` component is added under `frontend/src/app/(dashboard)/model-gateway/_components/`. The `logs-tab.tsx` `DataTable` gains `onRowClick={setSelectedRecord}` following the `models-tab.tsx` pattern exactly.

### Timeline

- **Layer 1** — `InferenceLogDetailModal` (frontend only): no backend dependency.
- **Layer 2** — Migration, `ConnectorError` extension, Go struct update, capture logic, TypeScript types, modal extended with `failure_details` fields.

---

## Validation

### Success Metrics

- 100% of new `inference_usage` rows have a non-null `failure_details` for `provider_error` and `auth_error` statuses from feature launch.
- Zero network requests triggered by opening the detail modal.
- Pre-existing NULL rows render without errors in the modal.

### Monitoring

- Log (at WARN level) any failure to serialise `FailureDetails`; surface as a metric counter `inference_gateway_metadata_capture_failures_total`.

---

## Related Decisions

- [ADR-0023](0023-inference-gateway-architecture.md) — Inference Gateway architecture; establishes the service.go write-path structure this ADR extends.
- [ADR-0043](0043-inference-analytics.md) — Inference Analytics; the aggregation layer built on the same `inference_usage` table.
- Future: Layer 3 content logging ADR — will re-evaluate the modal vs. drawer decision.

---

## References

- [PostgreSQL `ADD COLUMN` documentation](https://www.postgresql.org/docs/current/sql-altertable.html) — confirms metadata-only operation for nullable column addition.
- [Radix UI Dialog](https://www.radix-ui.com/primitives/docs/components/dialog) — the primitive underlying Terra `Modal`.

---

**Last Updated:** 2026-05-08
