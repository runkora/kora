# Run Task Summarization - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-19  
**Last Updated:** 2026-06-19  
**Owner:** Engineering  
**Stakeholders:** Platform team, Orbit users

---

## Executive Summary

Currently every agent run in Orbit is identified only by an opaque UUID. This PRD specifies a feature that automatically generates a short, human-readable one-sentence summary of each run's intent — derived from the run's initial message — and displays it across all run views in the application. The summary is generated at run creation time using the same LLM and connectivity already configured for the agent, keeping the implementation zero-dependency on external services.

---

## Problem Statement

### Current State

When a user starts a run, Orbit assigns it a UUID (e.g. `a3f7b1c0`). Runs are listed in:
- The **Agent Runs Tab** on the agent detail page (table view showing Run ID, Status, Started, Duration, Tokens, Tools)
- The **Run Topbar** inside the chat view (badge showing the last 8 characters of the run ID)

Neither view shows *what the run is doing* or *what it was asked to do*.

### User Pain Points

- **No scannability**: Users cannot tell at a glance what a list of runs was doing without clicking into each one.
- **No accountability**: When auditing past runs or looking at a colleague's work, there is no quick way to understand run intent without scrolling through the full event stream.
- **Poor navigation UX**: Returning to a run after days or hours requires remembering which UUID corresponds to which task.

### Business Impact

- Increased friction for teams running multiple concurrent or sequential runs on the same agent type.
- Reduces the usefulness of the runs table as an audit surface for platform operators and security reviewers.

---

## Goals and Objectives

### Primary Goals

1. Give every run a short, human-readable description of its task intent, generated automatically at start time.
2. Display this description in the runs list table, the run topbar, and any other run list views.
3. Use the agent's already-configured LLM (same model ID, same inference gateway) so no new external dependencies are introduced.

### Success Metrics

- 100% of runs started with a non-empty initial message have a non-null `summary` field within 2 seconds of run creation.
- The summary column is visible in all run list views.
- The run topbar shows the summary alongside the run ID badge.
- Runs without an initial message display a graceful fallback ("No task description").

### Non-Goals

- Summaries for existing historical runs (back-fill is out of scope for this iteration).
- User-editable summaries.
- Summaries generated from the full conversation (only the initial message is used).
- Summaries for runs started with no initial message (null summary is acceptable).

---

## User Stories

### Primary User Stories

**As a** platform operator,  
**I want to** see a plain-English description of each run in the runs table,  
**So that** I can quickly understand what each run is doing without clicking into the full chat.

**Acceptance Criteria:**
- [ ] The runs table shows a "Task" column with the auto-generated summary.
- [ ] The summary is ≤ 120 characters, a single sentence.
- [ ] Runs with no initial message show `—` or a placeholder.

---

**As a** developer using Orbit,  
**I want to** see the task description in the run topbar while working in the chat view,  
**So that** I always have context about the current run's purpose without scrolling back to the top of the conversation.

**Acceptance Criteria:**
- [ ] The run topbar shows the summary text, truncated with an ellipsis if it exceeds available width.
- [ ] The summary is visible immediately on run load (no loading spinner required).

---

### Secondary User Stories

**As a** security reviewer,  
**I want to** see run intent in the audit view,  
**So that** I can correlate run activity with authorized tasks without full event replay.

---

## Proposed Solution

### High-Level Approach

When a run starts with an initial message, the **session-manager** (the agent's own pod) makes a best-effort LLM call on the agent's authorized inference path — `{ANTHROPIC_BASE_URL}/run/{runID}/v1/messages`, using the agent's **effective** model — to produce a single short sentence describing the task. It reports the result to orbit-agents via a callback, which persists it in a new `summary` column on the `runs` table. The frontend displays this field wherever runs are listed or shown.

Summarization must run where the agent's inference identity, effective model, and per-run budget exist — the session-manager — **not** in the orbit-agents control plane, which has no inference grant (a direct call is denied by policy) and does not know the effective model. See the ADR for the full rationale.

Generation is strictly best-effort: if the LLM call fails or times out, the run proceeds normally with a null summary. It runs in a detached goroutine and never blocks the run or the `StartRun` response.

### User Experience

#### User Flow

1. User starts a run with the initial message "Audit the GitHub repository for exposed secrets and open a report PR".
2. Orbit creates the run, sends the initial message to the agent, and asynchronously calls the LLM to produce: `"Audit GitHub repository for exposed secrets and file a report PR"`.
3. The summary is stored on the run row.
4. When the user visits the agent detail page, the runs table shows the summary in a "Task" column.
5. Inside the chat view, the run topbar shows the summary below or beside the run ID badge.

### Key Components

- **Backend — `runs.summary` column**: A nullable `TEXT` column on the `runs` table, populated asynchronously after run start (set-once).
- **Backend — session-manager summarizer**: Calls the inference gateway's Anthropic `/v1/messages` endpoint on the agent's per-run authorized path with a fixed prompt, then reports the result to orbit-agents via the `POST /internal/runs/{id}/summary` callback, which persists it via a new `SetRunSummary` repository method.
- **API**: The `Run` struct and all run-response payloads include the `summary` field (null-safe).
- **Frontend — `Run` interface**: `summary?: string` added to the shared API type.
- **Frontend — `RunRow`**: `summary: string | null` added to the display-layer type and transformer.
- **Frontend — Agent Runs Tab**: New "Task" column before "Status".
- **Frontend — Run Topbar**: Summary text displayed beside the run ID badge.

---

## Scope

### In Scope

- New `summary` column on `runs` table (goose migration).
- Summarizer in `internal/agents/sessionmanager/` calling the agent's authorized inference path.
- `SetRunSummary` repository method (interface + postgres impl, set-once) and `POST /internal/runs/{id}/summary` callback.
- `summary` field on the `Run` model and all API responses.
- Frontend: `Run` interface, `RunRow`, agent-runs-tab "Task" column, run-topbar summary display.

### Out of Scope

- Back-filling summaries for existing runs.
- Summary editing by users.
- Summaries derived from the full conversation history.
- Dedicated summarization model routing (uses the agent's configured model).

### Future Considerations

- Allow users to override or edit the auto-generated summary.
- Back-fill summaries from first `human_message` run event for historical runs.
- Show summaries in global run search / observability dashboards.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `runs` table has a nullable `summary TEXT` column.
- [ ] `RunSummarizer` generates a ≤ 120-character summary from the initial message, best-effort, non-blocking.
- [ ] `Run` API response includes `summary` (string or null).
- [ ] Agent Runs Tab shows a "Task" column.
- [ ] Run Topbar shows the summary.

**Should Have (P1):**
- [ ] Summary generation completes within 2 seconds (5-second hard timeout).
- [ ] A null summary is gracefully rendered as `—` in the table and omitted in the topbar.

**Nice to Have (P2):**
- [ ] Structured logging when summarization fails (with run ID and error).

### Non-Functional Requirements

**Performance:**
- Summarization must not block the `StartRun` API response. It runs in a detached goroutine in the session-manager, off the run's critical path.
- The inference call has a 15-second timeout; failure is silent to the caller.

**Security:**
- The summary call goes out over the agent's own authorized inference path: the identity-proxy injects the agent token, OPA authorizes it under the agent's existing grant, and the gateway attributes cost to the run's budget. The control plane is **not** granted any inference capability — no new credential surface, no policy exception.
- The initial message is sent to the inference gateway, an internal cluster endpoint.

**Reliability:**
- Failures in summarization do not affect run lifecycle. The run proceeds regardless.

**Observability:**
- Summarization failures are logged at WARN level with `run_id` and `error` fields.

---

## Dependencies

### Internal Dependencies

- **Inference Gateway** (`internal/inferencegw`): The session-manager calls the gateway's Anthropic `/v1/messages` endpoint on the agent's per-run path (already reachable — the agent subprocess calls it for every LLM turn).
- **session-manager → orbit-agents callback**: A new `POST /internal/runs/{id}/summary` internal endpoint persists the reported summary. No new config — the session-manager reuses the `ANTHROPIC_BASE_URL` already in its pod.

### External Dependencies

- None — the inference gateway is an internal cluster endpoint.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Inference gateway unavailable at run start | Medium | Low | Best-effort: summarization failure does not block run. |
| LLM produces a summary > 120 chars | Low | Medium | Truncate to 120 chars before persisting. |
| High latency in summarization affecting UX | Low | Low | Async goroutine; 5-second timeout; UI shows null gracefully. |
| Sensitive data in initial message sent to LLM | Medium | Low | Already the case: the same message is sent to the agent. No new data path. |

---

## Alternatives Considered

### Alternative 1: Derive summary from the first N characters of the initial message

**Description:** Truncate the initial message to 120 characters and use that as the summary.

**Why not chosen:**
- Produces truncated, often mid-sentence descriptions.
- Long preambles (e.g. system context) in the initial message produce misleading summaries.
- Misses the compression value of LLM summarization.

### Alternative 2: Generate the summary in the orbit-agents control plane

**Description:** Have orbit-agents call the inference gateway directly after sending the initial message (the original implementation).

**Why not chosen:**
- The control plane has no agent identity, so OPA denies the call (`403 policy_denied`) — and granting the control plane inference would violate the platform's trust model.
- orbit-agents only knows the agent type's default model, not the run's effective model.
- No per-run budget attribution.
- The chosen design instead generates in the session-manager (where identity, model, and budget exist) and reports back via a callback. See the ADR for the full analysis.

### Alternative 2a: Generate in the session-manager after the first agent *turn*

**Description:** Same location as chosen, but wait until the agent's first turn completes to summarize from richer output.

**Why not chosen:**
- Introduces significant latency (the first agent turn can take 30+ seconds); the initial message already captures intent well enough for a ≤120-char label.

### Alternative 3: Client-side summary generation (frontend)

**Description:** Call an LLM from the browser to summarize the task.

**Why not chosen:**
- Exposes LLM credentials to the browser.
- Inconsistent availability (user's browser must be open for summary to generate).
- Does not produce a durable, server-side record.

---

## Open Questions

1. **Should the summary be regenerated if the initial message is later edited?** (Not applicable — initial messages are immutable in the current model.)
   - **Status:** Resolved — no regeneration needed.

2. **What model should be used for summarization?** The agent type's configured `model_id`, or a fixed lightweight model?
   - **Status:** Resolved — use the agent type's configured `model_id`. This avoids routing complexity and keeps inference costs consistent with the agent's budget policy.

---

## Timeline and Milestones

### Phases

**Phase 1: Core feature** (Target: 2026-06-19)
- DB migration, Go backend (model + repo + summarizer + wiring)
- Frontend type updates + UI display

### Key Milestones

- [x] PRD Approved: 2026-06-19
- [x] ADR Created: 2026-06-19
- [x] Implementation Started: 2026-06-19

---

## Appendix

### Related Documents

- [ADR: 2026-06-19-run-task-summarization](../adr/2026-06-19-run-task-summarization.md)
- [ADR: 2026-05-11-run-and-session-lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md)
- [ADR: 2026-06-10-hierarchical-budget-enforcement](../adr/2026-06-10-hierarchical-budget-enforcement.md)

### Glossary

- **Run**: A durable unit of work; one execution of an agent type.
- **Initial message**: The first user message sent at run start, captured in `StartRunRequest.InitialMessage`.
- **Summary**: A ≤ 120-character, LLM-generated sentence describing the run's task intent.
- **Inference gateway**: The Orbit-internal OpenAI/Anthropic-compatible LLM proxy.
