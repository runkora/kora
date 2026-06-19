# Run Task Summarization

<!-- Filename: 2026-06-19-run-task-summarization.md -->

**Status:** Accepted  
**Date:** 2026-06-19  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD: Run Task Summarization](../prd/run-task-summarization.md)

---

## Context

### Background

Every agent run in Orbit is currently identified only by a UUID. The runs table on the agent detail page and the run topbar in the chat view both display this opaque identifier, giving operators and developers no immediate signal about *what a run was asked to do*.

Orbit already has the building blocks to solve this:

1. **The initial message** — `StartRunRequest.InitialMessage` is the first user message sent at run creation time. It captures the user's intent.
2. **The inference gateway** — `orbit-agents` already knows the agent type's configured `model_id` and the inference gateway is reachable from all orbit-agents pods (`http://orbit-inference-gateway.orbit-system.svc.cluster.local`).
3. **The runs table** — has a clean extension point: a nullable `TEXT` column can be added without breaking existing rows.

### Problem Statement

How should Orbit generate and store a short, human-readable summary of each run's task intent, and how should it be surfaced in the UI?

### Goals

- Generate a ≤ 120-character, single-sentence summary of every run that starts with an initial message.
- Persist the summary durably on the run row.
- Display it in all run list and run detail views.
- Not block the `StartRun` API response or the run lifecycle.

### Non-Goals

- Back-filling historical runs.
- User-editable summaries.
- Summary regeneration on follow-up messages.

---

## Decision Drivers

* **Zero new dependencies**: The inference gateway is already in the cluster; the agent's `model_id` is already stored on the agent type.
* **Non-blocking**: Summarization failures must never affect run lifecycle or API response latency.
* **Minimal blast radius**: The change must not touch the session-manager or any other binary.
* **Testability**: The summarizer must be injectable (interface-based) so unit tests can replace the LLM call.
* **Consistency with existing patterns**: Follow the repository pattern, use `httpclient.Base` for the HTTP call, log warnings on failure.

---

## Considered Options

### Option 1: Async goroutine in `StartRun`, persisted via `SetRunSummary`

**Description:** Immediately after the initial message is sent to the session-manager, `StartRun` spawns a background goroutine. The goroutine calls the inference gateway with a compact prompt and persists the result by calling a new `SetRunSummary(ctx, runID, summary)` repository method. The goroutine uses a context detached from the request context (`context.WithoutCancel`) with a 5-second timeout, mirroring the `maybeWarmSandbox` pattern already in the codebase.

**Pros:**
- Zero latency impact on `StartRun` API response.
- No new IPC or callback surface.
- Mirrors the existing `maybeWarmSandbox` goroutine pattern exactly.
- Summary is durable (DB-persisted) before any live subscriber polls.

**Cons:**
- Summary is not in the `StartRun` API response body (caller sees null initially).
- Requires a slight poll/refresh on the frontend to show the summary immediately after run creation.

### Option 2: Synchronous call inside `StartRun`, returned in the response

**Description:** Call the LLM synchronously before returning from `StartRun`. Include the summary in the response body.

**Pros:**
- Summary available immediately in the `StartRun` response.

**Cons:**
- Adds 1–3 seconds to `StartRun` latency on every run (the LLM round-trip).
- A slow or unavailable inference gateway blocks run creation.
- Violates the principle that `StartRun` should be fast and responsive.

### Option 3: Summarize from the session-manager after the first agent turn

**Description:** Let the session-manager detect when the first assistant turn ends, then call back to orbit-agents with a summary request.

**Pros:**
- Summary based on actual agent output (richer signal).

**Cons:**
- First agent turn can take 30–120 seconds; summary visibility is badly delayed.
- Requires a new callback contract in the session-manager binary.
- Much higher implementation complexity.

### Option 4: Client-side summary via the frontend BFF

**Description:** The Next.js BFF generates the summary in a Route Handler after the run is created.

**Pros:**
- No changes to Go backend.

**Cons:**
- Summary is ephemeral unless stored separately.
- Requires the user's browser session to be active.
- Exposes LLM credentials / gateway to the BFF layer.

---

## Decision

**Chosen Option:** Option 1 — Async goroutine in `StartRun`, persisted via `SetRunSummary`

### Rationale

Option 1 is the right choice because:

1. It mirrors an identical pattern already in the codebase (`maybeWarmSandbox`), so there is no new architectural surface to validate.
2. It keeps `StartRun` fast — the single most important invariant for run creation UX.
3. The inference gateway is already a hard dependency of every agent run (agents call it continuously); adding one extra call at start time introduces no new failure mode that is not already present.
4. The summary is durable (DB row) and survives reconnects, page refreshes, and SSE replay.

The null-on-creation gap (summary appears ≈ 1–2 seconds after the run list loads) is acceptable: the frontend refreshes runs on a polling interval, and the summary will appear on the next poll without any special handling.

---

## Consequences

### Positive

- All run list views become meaningfully scannable.
- No new binaries, no new external services, no new network policies required.
- Testable in isolation (the `Summarizer` interface is injected into `RunService`).
- Failure is silent to the user (graceful degradation).

### Negative

- The `StartRun` response does not include the summary; callers see `null` initially.
- One additional LLM call is made per run that has an initial message (billed against the tenant's inference budget — but the cost is negligible: ~50 input tokens + ~30 output tokens).

### Neutral

- A new `summary TEXT` nullable column is added to the `runs` table; existing rows keep `null` with no schema issues.
- The inference gateway URL is a new config field on orbit-agents; defaults to the standard in-cluster address.

---

## Implementation

### Data Layer

A goose migration adds `summary TEXT` (nullable) to the `runs` table:

```sql
ALTER TABLE runs ADD COLUMN summary TEXT;
```

The `Run` struct gains a `Summary *string` field. The `RunRepository` interface gains:

```go
SetRunSummary(ctx context.Context, runID, summary string) error
```

`GetRun` and `ListRuns` select and scan the new column. `CreateRun` does not set it (null on creation).

### Summarizer Interface

```go
// Summarizer generates a short description of a run's task intent.
type Summarizer interface {
    Summarize(ctx context.Context, modelID, initialMessage string) (string, error)
}
```

`InferenceGatewaySummarizer` implements this interface. It posts a compact chat completion request to the inference gateway:

- **System prompt**: "You are a concise task labeler. Given a user's initial request, respond with a single sentence of ≤ 120 characters that captures the task intent. Output only the sentence — no quotes, no preamble."
- **User message**: the initial message verbatim.
- **Max tokens**: 60
- **Temperature**: 0

### Wiring

`RunService` gains an optional `Summarizer` field set via `SetSummarizer(s Summarizer)`. When non-nil and `req.InitialMessage != ""`, a goroutine is spawned after `emitHumanMessage`:

```go
go s.summarizeRun(context.WithoutCancel(ctx), run.ID, at.ModelID, req.InitialMessage)
```

`summarizeRun` calls the summarizer, truncates to 120 chars, calls `s.repo.SetRunSummary`, and logs a WARN on any failure.

### HTTP Client

`InferenceGatewaySummarizer` uses a typed client that embeds `httpclient.Base` (as required by the no-raw-HTTP-client lint rule). The inference gateway URL is read from a new `InferenceGatewayURL` field on `agentsConfig.Agents`.

### Frontend

1. `frontend/src/shared/api.ts` — add `summary?: string` to the `Run` interface.
2. `frontend/src/app/(dashboard)/agents/_types/index.ts` — add `summary: string | null` to `RunRow`; populate in `toRunRow`.
3. `agent-runs-tab.tsx` — add a "Task" column before "Status" that renders the summary with a text truncation style, or `—` if null.
4. `run-topbar.tsx` — render the summary as a small muted text line beside the run ID badge, truncated.

### Config

`cmd/orbit-agents/config/config.go`:
```go
InferenceGatewayURL string `yaml:"inferenceGatewayURL"`
```

`base.yaml`:
```yaml
inferenceGatewayURL: "http://orbit-inference-gateway.orbit-system.svc.cluster.local"
```

`local.yaml`:
```yaml
inferenceGatewayURL: "http://localhost:8081"
```

### Migration Path

The `summary` column is nullable. Existing runs are unaffected. No back-fill is performed.

---

## Validation

### Success Metrics

- Runs started with an initial message have `summary` populated within 5 seconds.
- Runs without an initial message have `summary = null`; the UI renders `—`.
- `StartRun` P95 latency is unchanged (summarization is fully async).
- `make check` passes with zero issues.

### Monitoring

- Inference gateway call latency is already tracked by the gateway's own metrics.
- Warn-level log lines with `run_id` and `error` surface summarization failures in existing log pipelines.

---

## Related Decisions

- [2026-05-11-run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — run state machine and repository pattern context.
- [2026-06-10-hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md) — inference costs and run context.
- [2026-06-04-agents-bff-integration-and-run-chat-wiring](2026-06-04-agents-bff-integration-and-run-chat-wiring.md) — frontend run wiring context.

---

## References

- [PRD: Run Task Summarization](../prd/run-task-summarization.md)

---

**Last Updated:** 2026-06-19
