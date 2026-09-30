# Inference Log Details — Product Requirements Document

**Status:** Implemented
**Created:** 2026-05-08
**Last Updated:** 2026-05-15
**Stakeholders:** Platform Engineering, Tenant Admins, Individual API Consumers

---

## Executive Summary

The kora Model Gateway records one row per inference request with solid attribution and metrics, but provides no way to drill into a specific request. The logs table surfaces 9 of the 22 available fields, hides all routing and error details, and stores nothing about structured provider failure context. This PRD defines two layered improvements: a **detail modal** that surfaces all existing data for a selected row (Layer 1, frontend-only), and **failure details capture** that stores structured provider error information alongside every non-success usage record (Layer 2, one migration + minor service change).

---

## Problem Statement

### Current State

Every inference request produces one row in `inference_usage` with 22 columns covering identity, model selection, token counts, cost, status, and latency. The Logs tab renders a table with 9 of those columns visible and no row-level interaction beyond filtering.

**Admins reviewing routing behaviour:**
- Can see `routing_strategy` exists as a column but it is never shown in the table UI.
- `retry_count`, `api_dialect`, `principal_type`, `principal_id`, and the full cache breakdown are fetched on every page load but never rendered.

**Developers debugging a failed request:**
- Can see the status (`provider_error`) and a terse `error_code` (e.g. `ThrottlingException`) but not what the provider actually said or what HTTP status it returned.
- Must grep application logs or traces to reconstruct error context the gateway already had at request time.

### Business Impact

- Operators cannot self-serve root-cause analysis for failed requests; they must contact Platform Engineering or dig into distributed traces.
- The logs table's apparent depth (the filter set is rich) creates an expectation of detail that the read-path does not deliver.
- Customers evaluating the platform for enterprise use expect request-level debuggability as a baseline.

---

## Goals and Objectives

### Primary Goals

1. Expose all data already available in a usage record through a per-row detail modal — zero backend changes required.
2. Capture structured provider failure context on every `provider_error` request so that the detail modal tells the full debugging story.
3. Keep both layers additive and non-breaking: no existing column is removed or retyped, no existing API contract changes.

### Success Metrics

- All 22 existing fields on `InferenceUsageRecord` are accessible via the detail modal within one click from the logs table.
- `failure_details` is populated on 100% of `provider_error` requests from feature launch.
- A developer can identify the likely cause of a `provider_error` without leaving the kora UI.

### Non-Goals

- Storing prompt or completion content (message text, system prompts, tool definitions) — this is Layer 3 and requires a separate privacy/retention design.
- Real-time log streaming or push-based updates.
- CSV export or log download.
- Modifying the analytics or aggregation layer.
- Any change to the `inference_usage` indexes beyond the one new JSONB column.

---

## User Stories

### Layer 1 — Detail Modal

**As a** developer,
**I want to** click a row in the logs table and see all available information about that request,
**So that** I can understand routing decisions, cache behaviour, and error context without leaving the page.

**Acceptance Criteria:**
- [ ] Clicking any row in the logs table opens a detail modal.
- [ ] The modal title shows the request ID in monospace.
- [ ] The modal is divided into labelled sections: Identity, Model, Routing, Performance, and (if non-success) Failure.
- [ ] Identity section shows: principal ID, principal type, token ID (if present), PAT name (if resolvable from existing list), API dialect.
- [ ] Model section shows: virtual model, provider model, API dialect.
- [ ] Routing section shows: routing strategy, retry count.
- [ ] Performance section shows: latency, prompt tokens, completion tokens, cache read tokens, cache creation tokens, cache hit rate (computed), estimated cost.
- [ ] Failure section (shown only when status is not `success`) shows: status badge, error code.
- [ ] The modal uses the existing `Modal` component from `@orbit/terra` with `size="lg"` and a constrained scrollable body.
- [ ] The modal is triggered by row click on `DataTable`, following the same pattern as `ModelDetailModal` in the Models tab.
- [ ] The modal can be dismissed via the close button, Escape key, or clicking the backdrop.
- [ ] No new API call is made when opening the modal — all data comes from the row already loaded in the table.

---

**As a** developer,
**I want to** see the cache hit rate for a specific request,
**So that** I can verify prompt caching is working as expected for my integration.

**Acceptance Criteria:**
- [ ] Cache hit rate is shown in the Performance section as a percentage, computed client-side as `cache_read_input_tokens / (prompt_tokens + cache_read_input_tokens)`.
- [ ] If `cache_read_input_tokens` is 0, the field shows "—" rather than "0%".

---

### Layer 2 — Failure Details

**As a** developer,
**I want to** see structured provider error details for a failed request,
**So that** I can understand exactly what the provider returned rather than only seeing a terse error code.

**Acceptance Criteria:**
- [ ] The Failure section of the detail modal shows `failure_details` content when present.
- [ ] Shown fields: provider HTTP status, provider error type, provider error message.
- [ ] For `routing_failed`, `policy_denied`, `timeout`, and `canceled` statuses: no `failure_details` section is shown (NULL for these statuses).
- [ ] `failure_details` is only populated on `provider_error` records.

---

**As a** developer,
**I want** historical requests recorded before this feature shipped to still open in the detail modal,
**So that** the feature is not confusing for pre-existing log data.

**Acceptance Criteria:**
- [ ] When `failure_details` is NULL on a failed row, the Failure section shows only the existing `status` and `error_code` fields.

---

## Proposed Solution

### Layer 1 — Detail Modal (Frontend Only)

A new `InferenceLogDetailModal` component under `frontend/src/app/(dashboard)/model-gateway/_components/`. It receives an `InferenceUsageRecord` (already fully typed) and a PAT name lookup map (already available in the logs tab's loaded state).

The logs table (`LogsTabShell` / `logs-tab.tsx`) adds `onRowClick={setSelectedRecord}` to the `DataTable`, following the identical pattern used by `ModelDetailModal` in `models-tab.tsx`. No new API call is made.

**Modal layout (sections, top to bottom):**

| Section | Fields |
|---|---|
| Identity | Principal ID + type, PAT name + token ID, API dialect |
| Request | Virtual model → provider model |
| Routing | Routing strategy, retry count |
| Performance | Latency, prompt / completion / cache tokens, cache hit rate, estimated cost |
| Failure | Status badge, error code, `failure_details` fields (non-success only) |

The modal uses `size="lg"` and `<ModalBody constrained>` to handle scrolling when all sections are present.

### Layer 2 — Schema and Capture

One new nullable JSONB column is added to `inference_usage` via a Goose migration:

**`failure_details JSONB`** — structured provider failure context. Populated only for `provider_error` and `auth_error` records; NULL for all other statuses.

```json
{
  "provider_http_status": 429,
  "provider_error_type": "ThrottlingException",
  "provider_error_message": "Rate exceeded for model claude-3-5-sonnet"
}
```

`provider_error_message` is truncated to 500 characters before storage.

**Capture location:** `failure_details` is populated from the `ConnectorError` already in scope at the `provider_error` and `auth_error` write-path branches in `service.go`. `ConnectorError` must be extended with an `HTTPStatus int` field to support `provider_http_status` capture (the HTTP status is currently discarded after deriving `Code` and `Retryable`).

**No new indexes** are added to the JSONB column in v1.

### BFF and TypeScript changes

The existing `InferenceUsageRecord` TypeScript interface gains one new optional field:

```typescript
failure_details: FailureDetails | null;
```

No BFF route changes are needed — the inference gateway's `GET /v1/usage` already returns full `InferenceUsageRecord` rows; the two new fields will be included automatically once the Go struct is updated.

---

## Scope

### In Scope

- `InferenceLogDetailModal` component (new file, Layer 1)
- Row-click handler on `DataTable` in `logs-tab.tsx` (Layer 1)
- Goose migration adding `failure_details` JSONB column (Layer 2)
- `ConnectorError` extended with `HTTPStatus int` field (Layer 2)
- `InferenceUsageRecord` Go struct updated with `FailureDetails` field (Layer 2)
- `service.go` capture logic for `failure_details` at `provider_error` and `auth_error` branches (Layer 2)
- `repository.go` INSERT updated to include the new column (Layer 2)
- TypeScript types updated with new optional field (Layer 2)
- Cache hit rate computed client-side in the modal (Layer 1)

### Out of Scope

- Prompt or completion content storage (Layer 3 — separate PRD required)
- Request shape parameter capture (`request_metadata`) — dropped; no unified request struct exists in `service.go` to extract from without per-dialect extraction work
- GIN indexes on the new JSONB column (deferred until filtering is needed)
- `policy_rule` capture for `policy_denied` records — the existing `error_code` field is sufficient; capturing the OPA rule identifier requires extending `policy.CheckResult` and is deferred
- Filtering or searching by `failure_details` fields in the logs table
- Changes to the analytics endpoints or aggregation layer
- Backfilling historical rows

### Future Considerations

- **Layer 3 — Content logging:** opt-in per tenant, separate `inference_content` table, TTL-based retention. Requires a dedicated PRD and privacy review before implementation.
- **`policy_rule` capture:** add rule identifier to `policy.CheckResult`, populate `failure_details` for `policy_denied` records. Deferred until the policy evaluator is extended.
- **GIN index on `failure_details`:** enables filtering logs by `provider_error_type`, `provider_http_status`, etc.
- **Failure details in analytics:** aggregate `provider_error_type` counts to identify the most common provider failure modes.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Detail modal opens on row click with all 22 existing fields rendered in labelled sections
- [ ] Modal matches the `ModelDetailModal` trigger pattern (`onRowClick` on `DataTable`)
- [ ] Modal uses Terra `Modal` component with `size="lg"` and constrained body
- [ ] Cache hit rate computed and shown in Performance section
- [ ] NULL `failure_details` handled gracefully — Failure section's structured fields omitted, not shown empty
- [ ] Goose migration adds `failure_details JSONB` (nullable) to `inference_usage`
- [ ] `ConnectorError` extended with `HTTPStatus int`
- [ ] Go struct and repository INSERT updated for the new column
- [ ] `failure_details` populated on all `provider_error` requests
- [ ] TypeScript interface updated with new optional field

**Should Have (P1):**
- [ ] Provider HTTP status shown alongside error type and message for `provider_error` and `auth_error` records

**Nice to Have (P2):**
- [ ] Keyboard navigation: arrow keys move between rows while modal is open
- [ ] "Copy request ID" button in modal header

### Non-Functional Requirements

**Performance:**
- Opening the modal must not trigger a network request — all data is already in the row object.
- The migration must use `ADD COLUMN ... DEFAULT NULL` to avoid a full table rewrite on a live table.

**Security:**
- `failure_details` must never contain prompt or completion text or any value derived from user-supplied message content.
- `failure_details.provider_error_message` is truncated to 500 characters before storage to bound exposure in cases where a provider echoes request content in its error message.
- The new JSONB column is scoped to the existing `tenant_id` row — no new access control surface is introduced.

**Observability:**
- No new metrics or spans are required for Layer 1.
- Layer 2 capture failures must not cause the usage write to fail — log and omit the JSONB field rather than propagating an error.

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Notes |
|---|---|---|
| `inference_usage` table | Available | Migration adds one new nullable column |
| Terra `Modal` component | Available | `size="lg"` + `constrained` body already supported |
| `ModelDetailModal` pattern | Available | Row-click on `DataTable` pattern to copy from `models-tab.tsx` |
| `ConnectorError` type | Requires extension | `HTTPStatus int` field must be added; currently HTTP status is discarded after deriving `Code` and `Retryable` |

### External Dependencies

None. Both layers use existing infrastructure.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| Migration causes downtime on large `inference_usage` table | Medium | Low | `ADD COLUMN ... DEFAULT NULL` is a metadata-only operation in PostgreSQL; no row rewrite |
| Modal is too tall for short viewports | Low | Medium | `<ModalBody constrained>` limits to `max-h-[60vh]` with internal scroll |
| Pre-existing rows with NULL `failure_details` confuse users | Low | Medium | Failure section's structured fields omitted entirely (not rendered as empty) when NULL |

---

## Alternatives Considered

### Drawer vs. Modal for the detail view

A slide-over drawer would offer more vertical space and keep the table visible alongside the detail. However, the existing pattern in kora uses modals for row detail views (see `ModelDetailModal` in the Models tab). Introducing a drawer would require a new Terra component and establish a second interaction paradigm for the same pattern. For the field volume in Layers 1 and 2, a `size="lg"` modal with a constrained scrollable body is sufficient. This decision should be revisited if Layer 3 (content logging) ships — rendering full message threads in a 512px modal would be cramped.

### Capturing `failure_details` in a separate table

Same argument as above — 1:1 relationship, write-once, JSONB handles schema evolution as new providers with different error shapes are added. A separate table would require a JOIN on every usage record fetch with no benefit at current scale.

---

## Open Questions

1. **Should we show `failure_details` fields for `timeout` status?**
   - **Decision:** NULL. `UsageStatusTimeout` is defined in `models.go` but is not written by any branch in `service.go` — the status is currently unused. If a gateway-level timeout is introduced in a future iteration, the `failure_details` decision for that status can be evaluated then (likely a `timeout_phase` field). No action required for v1.
   - **Status:** Closed

---

## Errata — Inference Logs PRD

The following corrections apply to [PRD: Inference Logs](inference-logs.md). That document is immutable; amendments are recorded here.

**OQ-1 (token_id attribution):** The decision states `claims.Orbit.SessionID` is used as the PAT record UUID. The actual implementation uses `claims.Orbit.PatID`, a distinct field. `SessionID` is a per-issuance revocation key; `PatID` is the stable PAT record UUID. The risk table entry referencing `SessionID` semantics is similarly affected — the correct field is `PatID`.

**OQ-3 (pagination):** The decision states offset-based pagination was chosen. The actual `InferenceUsageRepository.List` implementation uses cursor-based pagination (`CursorTime` + `CursorID` keyset cursors, no `Offset` field). The recommendation to not hardcode offset values in the URL scheme is correct but the rationale is wrong.

**P1 requirement — Key Status column:** The requirement derives revocation status from `PAT.RevokedAt`. The current `GET /identity/tokens` response does not include `revoked_at` — the TypeScript `PAT` interface only exposes `expires_at`. Accurately surfacing revoked vs. active/expired status requires `GET /identity/tokens` to be updated to include `revoked_at` in its response.

**Missing risk — Deleted PAT in `?key=` URL:** A PAT that has been deleted (not merely revoked) will not appear in the `GET /identity/tokens` list. A `?key=<id>` URL containing a deleted PAT's ID will return 0 results with no indication of why. Mitigation: when `?key=<id>` does not match any PAT in the live list, the UI should surface a "Key not found or deleted" message rather than showing an empty filtered table.

---

## Timeline and Milestones

### Phases

**Phase 1: Detail modal — Layer 1** (no backend changes)
- `InferenceLogDetailModal` component
- Row-click wiring in `logs-tab.tsx`
- TypeScript cache hit rate computation
- All 22 existing fields rendered in sectioned layout

**Phase 2: Schema and capture — Layer 2**
- Goose migration: `failure_details` JSONB column
- `ConnectorError` extended with `HTTPStatus int`
- Go struct update (`InferenceUsageRecord`)
- Capture logic in `service.go` at `provider_error` and `auth_error` branches
- Repository INSERT update
- TypeScript interface update
- Modal updated to render Layer 2 fields

### Key Milestones

- [x] PRD Approved
- [x] [inference-log-details](../adr/2026-06-05-inference-log-details.md) Approved
- [ ] Phase 1 Complete — detail modal in production
- [ ] Phase 2 Complete — metadata capture and modal fields live

---

## Appendix

### Related Documents

- [PRD: Inference Analytics](inference-analytics.md) — aggregation layer built on the same `inference_usage` table
- [inference-log-details](../adr/2026-06-05-inference-log-details.md) — architectural decisions for detail modal presentation and JSONB column design

### Glossary

| Term | Definition |
|---|---|
| Layer 1 | Frontend-only detail modal; surfaces existing fields; no backend changes |
| Layer 2 | JSONB column `failure_details` added to `inference_usage` |
| Layer 3 | Content logging (prompt/completion text); out of scope for this PRD |
| `failure_details` | Nullable JSONB field capturing structured provider failure context for `provider_error` records |
| Cache hit rate | `cache_read_input_tokens / (prompt_tokens + cache_read_input_tokens)` — computed client-side |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______

---

**Last Updated:** 2026-05-15
