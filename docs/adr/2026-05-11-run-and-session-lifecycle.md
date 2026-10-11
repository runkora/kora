# Run & Session Lifecycle

**Status:** Accepted
**Date:** 2026-05-11
**Updated:** 2026-05-28
**Deciders:** Platform Engineering
**Technical Story:** Agent interactions need a durable execution model that survives browser disconnects, enables context replay on reconnect, and provides a clean basis for future resume and orchestration features.

---

## Context

### Background

Users interact with managed agents through a browser UI. However, agent work — file editing, LLM calls, tool execution — may take minutes or hours. A model that ties agent lifetime to the browser connection loses work on every disconnect and cannot support headless observation or resumable sessions.

### Problem Statement

Without a durable run model, every browser refresh or network blip loses the agent session. There is also no way to observe a headless agent that is already running. The lifecycle model must decouple browser connections from agent execution.

### Goals

- Runs survive browser disconnects
- Users can reconnect and see full history
- Headless runs can be observed without starting them
- Cleanup does not destroy active work
- The model is extensible to future resume and orchestration features

### Non-Goals

- Run resume from snapshot (designed for, not implemented in v1)
- Multi-agent orchestration
- Scheduled or trigger-based run initiation (deferred)

---

## Decision Drivers

* A run represents a unit of work — it must outlive the browser connection that started it
* Event history must be fully replayable — the user must always see complete context, not just what happened while they were connected
* Cleanup must be safe — no active work should be destroyed
* The model must be simple enough to implement correctly in v1 without over-engineering

---

## Decision

**Run as durable entity, Session as transient in-memory connection**

**Run** — the unit of work. A server-side entity created when a user initiates an agent task. Tied to a specific pod ordinal and worktree. Persists in the DB until explicitly deleted. A run can have zero active sessions (unobserved) or one (user connected via SSE).

**Session** — a live subprocess connection managed by the session-manager pod. Created when a run is assigned to a pod. Destroyed when the subprocess exits or the idle timer fires. Does not have its own DB row — run state captures everything.

---

## Run State Machine

```
pending
  ↓  (pod assigned)
starting
  ↓  (subprocess ready, first message sent)
active  ←─────────────────────┐
  ↓  (message_stop received)   │
idle  ──────────────────────────┘  (new user message arrives)
  ↓  (idle timer fires OR explicit terminate)
completed

active ──→ failed   (subprocess crashes, inference unrecoverable)
starting ──→ failed  (reaper: stuck at boot too long)
active ──→ failed   (reaper: stuck mid-turn too long)
```

### States

| State | Meaning |
|---|---|
| `pending` | Run created in DB; waiting for orbit-agents to assign a pod ordinal |
| `starting` | Pod assigned; session-manager creating the subprocess |
| `active` | Agent is actively processing — between receiving a message and emitting `message_stop` |
| `idle` | Agent completed its turn and is waiting for the next user message; idle timer is running |
| `completing` | Reserved for future snapshot support; not currently reached in normal flow |
| `completed` | Session ended cleanly — idle timer expired with no new message, or user terminated the run |
| `failed` | Unrecoverable error — subprocess crashed, failed to start, or inference gateway returned a fatal error |

### Transition Table

| From | To | Who | Condition |
|---|---|---|---|
| `pending` | `starting` | orbit-agents | Pod ordinal selected; `AssignPod` called |
| `starting` | `active` | session-manager | Subprocess started; initial message delivered to Claude |
| `active` | `idle` | session-manager | `message_stop` event received — agent turn complete, and no subagent or background task still running |
| `idle` | `active` | session-manager | First event from Claude runner on new turn (agent responding) |
| `idle` | `completed` | session-manager | Idle timer fires (`agents.idleTimeout` in the chart: 20 min for a run a person started, 5 min for a trigger run until a person messages it) → `CompleteRun(from="idle", failed=false)` |
| `active` | `completed` | session-manager | Subprocess exited cleanly (`intentionalClose=true`) → `CompleteRun(from="active", failed=false)` |
| `active` | `failed` | session-manager | Subprocess exited unexpectedly (`intentionalClose=false`) → `CompleteRun(from="active", failed=true)` |
| `idle` | `failed` | session-manager | Subprocess exited unexpectedly while idle → `CompleteRun(from="idle", failed=true)` |
| `starting` | `failed` | **reaper** | Stuck in `starting` for longer than `StaleRunTimeout` (default 30 min) |
| `active` | `failed` | **reaper** | Stuck in `active` for longer than `StaleRunTimeout` — agent turn never completed |

### Why `idle` is not `failed`

A run transitions to `idle` when the agent has **successfully** completed a turn and is parked waiting for the user's next message. This is normal, healthy behaviour. Marking it `failed` would be wrong — there is no error. The run will eventually transition to `completed` when the idle timer fires.

`failed` is reserved exclusively for unrecoverable errors: subprocess crashes, startup failures, and inference gateway errors that the agent cannot recover from.

### Why the reaper does not touch `idle`

The reaper exists to clean up runs where the session-manager pod died mid-flight (crash, OOM, node eviction) before it could call `CompleteRun`. In that scenario, the run stays in `starting` or `active` indefinitely with no process backing it.

`idle` runs are **not** stuck — they have a backing process and an active idle timer. They will transition to `completed` on their own. Reaping them would destroy healthy conversations.

### Why the reaper targets `starting` and `active` but not `completing`

`completing` is reserved for a future snapshot flow and is not currently reached. Once snapshot support is added, `completing` runs will also need reaper coverage with an appropriate timeout.

---

## Session-Manager Signalling

All run state transitions flow through `orbit-agents` via an internal HTTP callback:

```
POST /internal/runs/{id}/state
Body: {"from": "<current_state>", "to": "<next_state>"}
```

This is an intra-cluster call over mTLS. No platform JWT required.

On receiving a state signal, orbit-agents:
1. Validates and applies the transition (compare-and-swap on `runs.state` — rejects if `from` doesn't match)
2. Broadcasts the state change to any active SSE subscribers via PubSub

The session-manager uses three lifecycle methods on the `Notifier` interface:

| Method | Transition |
|---|---|
| `IdleRun(runID)` | `active → idle` — called when `message_stop` fires |
| `ActivateRun(runID)` | `idle → active` — called when the agent starts responding to a new message |
| `CompleteRun(runID, fromState, failed)` | `active/idle → completed/failed` — called when the subprocess exits |

`IdleRun` and `ActivateRun` use the `lifecycleClient` (15s timeout) since they are state writes that must not be silently dropped.

---

## Stale Run Reaper

`Service.StartReaper` runs as a background goroutine in orbit-agents. It ticks every 5 minutes and calls `ReapStaleRuns(staleDuration)`, which issues:

```sql
UPDATE runs
SET state='failed', completed_at=$1, updated_at=$1
WHERE state IN ('active', 'starting')
  AND updated_at < $2
RETURNING id
```

The default `staleDuration` is 30 minutes (configurable via `ServiceConfig.StaleRunTimeout`). After reaping, orbit-agents publishes a `run_failed` PubSub event for each reaped run ID so connected SSE clients update immediately.

The reaper is the **only** backstop for runs whose session-manager pod died without calling `CompleteRun`. In normal operation all transitions are handled by the session-manager itself.

---

## Run Data Model

```
runs
├── id                uuid, primary key
├── tenant_id         text, non-nullable
├── agent_type_id     uuid, references agent_types
├── state             text — pending | starting | active | idle | completing | completed | failed
├── pod_ordinal       int — assigned pod in the StatefulSet (-1 until assigned)
├── created_by        text — principal_id of the initiating user
├── started_at        timestamptz — when state became active
├── completed_at      timestamptz — when state became completed or failed
├── created_at        timestamptz
└── updated_at        timestamptz — updated on every state change; used by the reaper
```

---

## Event Log

All events emitted by the transform layer are written to `run_events` as they occur. This is the source of truth for run history and the basis for event replay on reconnect.

```
run_events
├── sequence_no       bigint — monotonically increasing per run (primary key)
├── run_id            uuid, references runs
├── event_type        text — text_delta | text_block | thinking_delta | tool_use | tool_result | message_complete | message_stop | run_error
├── payload           jsonb — event-type-specific fields
└── created_at        timestamptz
```

Durable events (`message_complete`, `text_block`, `tool_use`, `tool_result`, `run_error`) are written to `run_events` before being pushed to SSE. Ephemeral events (`text_delta`, `thinking_delta`, `message_stop`) are push-only and not persisted.

### User Messages

User messages sent via `POST /api/v1/runs/{id}/messages` are stored in `run_messages`, not `run_events`. This keeps the event log strictly limited to agent output.

```
run_messages
├── id            uuid, primary key
├── run_id        uuid, references runs
├── content       text
└── created_at    timestamptz
```

On future resume, both `run_events` and `run_messages` are interleaved by timestamp to reconstruct the full conversation context for the new subprocess.

---

## Consequences

### Positive

- Work survives browser disconnects — no lost runs due to network issues
- Full history always available — users see complete context on reconnect
- `idle` correctly distinguishes "healthy, waiting" from "broken" — users see "Waiting" not "Failed" for normal between-turn pauses
- Reaper provides a safety net for pod crashes without falsely failing healthy conversations

### Negative

- State machine has more states than the simplest model — implementors must understand the `active`/`idle` distinction
- The reaper fires every 5 minutes — this is a periodic DB write even when nothing is stale

### Neutral

- `run_events` grows over time — retention and archival policy needed for long-running agents

---

## Related Decisions

- [agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — Transform layer that emits events written to `run_events`
- [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md) — Service that owns the state machine
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — Streaming protocol built on `run_events`
- [agent-workspace-model](2026-05-11-agent-workspace-model.md) — Workspace lifecycle tied to run lifecycle

---

**Last Updated:** 2026-05-28
