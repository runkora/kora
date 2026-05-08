# 53. Run & Session Lifecycle

**Status:** Proposed  
**Date:** 2026-05-08  
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

## Considered Options

### Option A: Session = Run (single entity, tied to browser)

**Description:** A session is created when the browser connects and torn down when it disconnects. The CLI subprocess and workspace are tied to the browser connection.

**Pros:**
- Simple — one entity, no split model
- Easy to reason about cleanup

**Cons:**
- Work is lost on every browser disconnect
- Cannot support headless agents (no browser connection = no session = no run)
- Cannot replay history to a reconnecting user (session is gone)

### Option B: Run as durable entity, Session as transient connection

**Description:** A run is a server-side entity tied to a pod and worktree. It persists independently. A session is a browser connection to a run — transient, created on connect, destroyed on disconnect. A run can have zero or one active sessions.

**Pros:**
- Work survives disconnects
- Headless runs are observable without a browser connection
- Full event history replayable to any session connecting to the run
- Clean basis for future resume: run persists as failed/completed, resume creates a new session

**Cons:**
- Two entities to manage (run + session)
- Cleanup requires checking run state, not just connection state

### Option C: Run as durable entity with persistent subprocess

**Description:** Same as Option B, but the CLI subprocess also persists between sessions (no subprocess restart on reconnect).

**Pros:**
- Seamless reconnect — subprocess retains full in-memory state

**Cons:**
- Subprocess persistence across pod rescheduling is not possible without shared state
- Subprocess crash loses all in-memory state regardless
- Resume must handle subprocess restart anyway — in-memory state is not reliable

---

## Decision

**Chosen Option: B — Run as durable entity, Session as transient connection**

### Definitions

**Run** — the unit of work. A server-side entity created when a user initiates an agent task. Tied to a specific pod ordinal and worktree. Persists until completed, failed, or explicitly deleted. A run can have zero active sessions (unobserved) or one (user connected).

**Session** — a browser connection to a run. Created when a user opens the chat or observation UI for a run. Destroyed on disconnect. Does not affect the run.

### Run State Machine

```
pending
  → starting    (pod assigned, worktree provisioned, subprocess starting)
  → active      (CLI subprocess running and responsive)
  → completing  (CLI subprocess has exited cleanly; snapshot in progress)
  → completed   (snapshot done; resources released)
  → failed      (CLI subprocess exited with error; snapshot attempted)
```

**Transitions:**

| From | To | Trigger |
|---|---|---|
| pending | starting | Pod available; `orbit-agents` assigns pod ordinal |
| starting | active | session-manager signals subprocess is ready |
| active | completing | CLI subprocess exits with code 0 |
| active | failed | CLI subprocess exits with non-zero code or signal |
| completing | completed | Workspace snapshot written to object storage |
| completing | failed | Snapshot fails after retry exhaustion |

**There is no timeout transition.** A run in `active` state is never timed out or cleaned up. The only trigger for leaving `active` is the CLI subprocess exiting.

**User disconnect does not change run state.** The session is destroyed; the run continues.

### Session State Machine

```
connecting → active → disconnected
```

Sessions are lightweight records. No timeout on active sessions — they are destroyed on disconnect only.

### Run Cleanup Policy

- **Active run**: never cleaned up. Pod resources held.
- **Completing**: workspace being snapshotted. Pod resources held until `completed`.
- **Completed / Failed**: workspace archived. Pod resources released (pod returned to available pool for next run). `run_events` retained.

### Run Data Model

```
runs
├── id                uuid, primary key
├── tenant_id         text, non-nullable
├── agent_type_id     uuid, references agent_types
├── state             text — enum: pending | starting | active | completing | completed | failed
├── pod_ordinal       int — assigned pod in the StatefulSet
├── created_by        text — principal_id of the initiating user
├── started_at        timestamptz — when state became active
├── completed_at      timestamptz
├── created_at        timestamptz
└── updated_at        timestamptz

sessions
├── id                uuid, primary key
├── run_id            uuid, references runs
├── principal_id      text — the user who connected
├── connected_at      timestamptz
└── disconnected_at   timestamptz — null while connected
```

### Event Log

All events emitted by the transform layer are written to `run_events` as they occur. This is the source of truth for run history and the basis for event replay on reconnect.

```
run_events
├── id                uuid, primary key
├── run_id            uuid, references runs
├── sequence_no       bigint — monotonically increasing per run
├── event_type        text — text_delta | thinking_delta | tool_use | tool_result | message_stop
├── payload           jsonb — event-type-specific fields
└── created_at        timestamptz
```

Writes to `run_events` are durable. An event is not emitted to SSE without first being written to the database.

### Future Resume (Not v1)

The `run_events` table is the source of truth for resume. When a run is in `failed` state and a user wants to continue: `orbit-agents` starts a new run (`state: starting`), loads the `run_events` from the previous run to reconstruct conversation context, passes that context to the transform layer at subprocess startup, and transitions to `active`. No separate conversation storage is required.

---

## Consequences

### Positive

- Work survives browser disconnects — no lost runs due to network issues
- Full history always available — users see complete context on reconnect
- Headless runs observable without browser connection
- Clean basis for resume — `run_events` is all the context needed

### Negative

- Two entities (run + session) are slightly more complex than a single session model
- Active runs occupy pod resources indefinitely — there is no backstop for stuck runs (to be addressed with a future admin override or stall detection feature)

### Neutral

- `run_events` grows over time — retention and archival policy needed for long-running agents

---

## Implementation

### State Transitions

State transitions are written to the `runs` table atomically. `orbit-agents` uses optimistic locking (compare-and-swap on `state`) to prevent concurrent conflicting transitions.

### Session-Manager Signalling

The session-manager signals run state changes to `orbit-agents` via an internal HTTP callback: `POST /internal/runs/{id}/state` with the new state. This is an intra-cluster call over mTLS; no platform JWT required.

On receiving a state signal, `orbit-agents`:
1. Validates and applies the state transition (compare-and-swap on `runs.state`)
2. Writes a `run_state` event to `run_events` — this is the only place `run_state` events originate
3. Issues a Postgres `NOTIFY run:{run_id}` so active SSE connections receive the state change in the browser event feed

`run_state` events are never written by the session-manager directly; all event log writes for state transitions go through `orbit-agents`.

### User Message Storage

User messages sent via `POST /api/v1/runs/{id}/messages` are stored in a separate `run_messages` table — they are NOT written to `run_events`. This separation keeps the event log strictly limited to agent output (what to render in the chat UI), while `run_messages` captures the full input side of the conversation for future resume.

```
run_messages
├── id            uuid, primary key
├── run_id        uuid, references runs
├── sequence_no   bigint — monotonically increasing per run
├── content       text
├── created_at    timestamptz
```

On future resume, both `run_events` and `run_messages` are interleaved by `sequence_no` to reconstruct the full conversation context for the new CLI subprocess invocation.

---

## Validation

### Success Metrics

- Browser disconnect does not change run state
- Full event history replayed correctly on reconnect with no missing events
- Run transitions to `completed` within 30 seconds of CLI subprocess exit
- Workspace snapshot succeeds for 99%+ of completed runs

---

## Related Decisions

- [ADR-0049](0049-agent-cli-framework-and-transform-layer.md) — Transform layer that emits events written to `run_events`
- [ADR-0052](0052-orbit-agents-service-architecture.md) — Service that owns the state machine
- [ADR-0054](0054-agent-event-streaming-protocol.md) — Streaming protocol built on `run_events`
- [ADR-0055](0055-agent-workspace-model.md) — Workspace lifecycle tied to run lifecycle

---

**Last Updated:** 2026-05-08
