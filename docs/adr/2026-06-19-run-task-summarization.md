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
2. **An authorized inference path** — the agent already calls the inference gateway continuously from inside the session-manager pod, at `{ANTHROPIC_BASE_URL}/run/{runID}/v1/messages`. That path is the *only* context where all three of the following hold at once: the identity-proxy sidecar injects the agent's platform token, OPA authorizes the call under the agent's grant, and the gateway attributes cost to the run's budget.
3. **The runs table** — has a clean extension point: a nullable `TEXT` column can be added without breaking existing rows.

### Critical constraint (learned during implementation)

A first implementation generated the summary **in the `orbit-agents` control plane**, calling the inference gateway directly. It does not work, and cannot:

- **No inference identity.** `orbit-agents` is not an agent. Its direct call to the gateway carries no agent token, so OPA rejects it — observed as `403 {"code":"policy_denied"}`. There is no policy that authorizes the control plane to perform inference, nor should there be.
- **Wrong / unknown model.** The control plane only knows the agent type's *default* `model_id`, which may be empty or overridden. The summary must use the agent's **effective** model, which is resolved at session-manager start.
- **No budget attribution.** A control-plane call has no `run/{runID}` context, so the cost cannot be attributed or enforced.

These are not incidental — they are the platform working as designed (*the control plane is never trusted to perform an agent's actions*). Summarization must therefore happen where the agent's identity, model, and budget already live: the **session-manager**.

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

* **Authorized inference only**: The summary's LLM call must go through the agent's own identity, grant, and per-run budget — the platform must never grant the control plane a side-channel to perform inference.
* **Effective model**: The summary must be generated with the model the agent actually runs, resolved at session-manager start, not the agent type's default.
* **Non-blocking**: Summarization failures must never affect run lifecycle or API response latency.
* **Reuse the existing callback surface**: The session-manager already POSTs run state and events back to orbit-agents over `/internal/runs/{id}/*`; a summary callback fits the same pattern.
* **Testability**: The summarizer and the callback must be unit-testable without a live gateway.
* **Consistency with existing patterns**: Follow the repository pattern, use `httpclient.Base` for the gateway call, log warnings on failure.

---

## Considered Options

### Option 1: Async goroutine in `StartRun` calling the gateway from orbit-agents ❌ (rejected — attempted, does not work)

**Description:** Immediately after the initial message is sent, `StartRun` spawns a background goroutine in `orbit-agents` that calls the inference gateway directly and persists the result via `SetRunSummary`.

**Why it fails:** `orbit-agents` has no agent identity, so OPA denies the call (`403 policy_denied`); it only knows the agent type's *default* model, not the effective one; and a control-plane call has no per-run budget context. See *Critical constraint* above. This was implemented first and reverted.

### Option 3 (chosen): Generate in the session-manager over the agent's authorized path, report back via callback

**Description:** When the session-manager receives the run's **first user message**, it spawns a detached background goroutine that calls `{ANTHROPIC_BASE_URL}/run/{runID}/v1/messages` — the same authorized path the agent uses — with the session's **effective model**. The identity-proxy injects the agent token, OPA authorizes it, and the gateway attributes the (tiny) cost to the run's budget. The session-manager then reports the summary to orbit-agents via a new best-effort callback, `POST /internal/runs/{runID}/summary`, which persists it set-once.

**Pros:**
- The only design where inference identity, effective model, and budget attribution all hold.
- Reuses the existing session-manager → orbit-agents callback surface (`/internal/runs/{id}/*`).
- Non-blocking: detached goroutine, `StartRun` and the run lifecycle are untouched.
- Summary is durable (DB row), survives reconnects and SSE replay.

**Cons:**
- Touches a second binary (session-manager) and adds one callback endpoint.
- Summary is not in the `StartRun` response; it appears on the next frontend fetch (~1–2 s).

### Option 2: Synchronous call inside `StartRun`, returned in the response

**Description:** Call the LLM synchronously before returning from `StartRun`. Include the summary in the response body.

**Pros:**
- Summary available immediately in the `StartRun` response.

**Cons:**
- Adds 1–3 seconds to `StartRun` latency on every run (the LLM round-trip).
- A slow or unavailable inference gateway blocks run creation.
- Violates the principle that `StartRun` should be fast and responsive.

### Option 3a: Summarize in the session-manager after the first agent *turn* (rejected timing variant)

**Description:** Same location as the chosen option, but trigger after the first assistant turn *completes* rather than on the first user message, to summarize from richer output.

**Cons:**
- First agent turn can take 30–120 seconds; summary visibility is badly delayed.
- The user's initial message already captures intent well enough for a ≤120-char label; waiting buys little.

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

**Chosen Option:** Option 3 — Generate in the session-manager over the agent's authorized inference path, report back via a best-effort callback.

### Rationale

This is the only option that satisfies the platform's own rules. Summarization is an inference action, and *every inference action must run under an agent's identity, grant, and budget*. The session-manager is the one place that context exists:

1. **Identity** — the call originates from the agent's pod, so the identity-proxy injects the agent token and OPA authorizes it under the same grant the agent already holds. No new policy, no control-plane inference side-channel.
2. **Effective model** — the session-manager holds the model the agent actually runs with (`CreateSessionRequest.Model`, falling back to the configured default), so the summary uses the same model.
3. **Budget** — calling `{base}/run/{runID}/v1/messages` attributes the ~80-token cost to the run, exactly like the agent's own calls.

It keeps `StartRun` fast (the call is a detached goroutine in the session-manager), reuses the existing `/internal/runs/{id}/*` callback surface, and the summary remains durable. The null-on-creation gap (summary appears ≈ 1–2 s later) is unchanged and acceptable: the frontend shows it on the next fetch.

---

## Consequences

### Positive

- All run list views become meaningfully scannable.
- The summary call is correctly authorized, attributed, and uses the agent's real model — it cannot bypass policy or budget.
- Reuses the existing session-manager → orbit-agents callback surface; no new external services or network policies.
- Testable in isolation (the summarizer and the `SetSummary` callback are unit-tested with `httptest`).
- Failure is silent to the user (graceful degradation).

### Negative

- Spans two binaries: generation in the session-manager, persistence in orbit-agents via a new `/internal/runs/{id}/summary` callback.
- The `StartRun` response does not include the summary; callers see `null` initially.
- One additional ~80-token LLM call per run with an initial message, attributed to the run's budget.

### Neutral

- A new `summary TEXT` nullable column is added to the `runs` table; existing rows keep `null` with no schema issues.
- `SetRunSummary` is set-once (`WHERE summary IS NULL`) so a resumed session never clobbers the original label.
- No inference-gateway URL config is added to orbit-agents — the session-manager reuses the `ANTHROPIC_BASE_URL` already present in its pod.

---

## Implementation

### Data Layer

A goose migration adds `summary TEXT` (nullable) to the `runs` table:

```sql
ALTER TABLE runs ADD COLUMN summary TEXT;
```

The `Run` struct gains a `Summary *string` field. The `RunRepository` interface gains `SetRunSummary(ctx, runID, summary string) error`, implemented set-once:

```sql
UPDATE runs SET summary=$1, updated_at=$2 WHERE id=$3 AND summary IS NULL
```

Zero rows affected (already set, or run gone) is not an error — the callback is idempotent. `GetRun`/`ListRuns` scan the column; `CreateRun` leaves it null.

### Summarizer (session-manager)

`internal/agents/sessionmanager/summarizer.go` posts an **Anthropic Messages API** request (the dialect the agent uses) to `{inferenceURL}/v1/messages`, where `inferenceURL = {ANTHROPIC_BASE_URL}/run/{runID}`:

- **model**: the session's effective model (`session.model`).
- **system**: "You are a concise task labeler … ≤ 120 characters … output only the sentence."
- **messages**: `[{role: user, content: <initial message>}]`
- **max_tokens**: 60

It embeds `httpclient.Base` (satisfies the no-raw-HTTP-client lint rule) and truncates the result to 120 runes. It sets **no** Authorization header — the identity-proxy sidecar injects the agent token on the way out, exactly as for the agent's own calls.

### Trigger & callback

`handleCreateSession` computes `inferenceURL` from the pod's `ANTHROPIC_BASE_URL` and passes it plus the model into `SessionConfig`. On the **first** user message (`session.summarizeOnce`), `Registry.Send` spawns a detached goroutine (15 s timeout) that calls the summarizer and then `Notifier.SetSummary(ctx, runID, summary)`.

`HTTPNotifier.SetSummary` POSTs `{"summary": …}` to `POST /internal/runs/{runID}/summary` on orbit-agents. The orbit-agents handler (`handleSetRunSummary`, behind the same `authorizeCallback` as the other internal callbacks) calls `RunService.SetRunSummary` → `repo.SetRunSummary`. Both generation and callback are best-effort: failures log at WARN and never affect the run.

### Frontend

1. `frontend/src/shared/api.ts` — add `summary?: string` to the `Run` interface.
2. `frontend/src/app/(dashboard)/agents/_types/index.ts` — add `summary: string | null` to `RunRow`; populate in `toRunRow`.
3. `agent-runs-tab.tsx` — add a "Task" column before "Status" that renders the summary with a text truncation style, or `—` if null.
4. `run-topbar.tsx` — render the summary as a small muted text line beside the run ID badge, truncated.

### Config

No new config. The session-manager reuses the `ANTHROPIC_BASE_URL` already injected into its pod (`internal/agents/k8s.go`). In local dev (no `ANTHROPIC_BASE_URL`, `NoopNotifier`) summarization is simply skipped.

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

**Last Updated:** 2026-06-20
