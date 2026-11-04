# Agent Portal & Chat UI

**Status:** Accepted
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** Users need a UI surface to define agents, start sessions, interact via chat, and observe headless runs. This ADR establishes the frontend architecture for the agent portal and chat interface.

---

## Context

### Background

The agent control plane ([agent-cli-framework-and-transform-layer](2026-05-11-agent-cli-framework-and-transform-layer.md) through [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md)) defines the backend infrastructure for managed and BYOC agents. The frontend is the user-facing surface: a portal for managing agent types and runs, a chat UI for interactive sessions, and an observation view for headless agents.

### Problem Statement

Without a frontend, the agent control plane is only accessible via API. The target users — tenant users who want to interact with agents — need a self-service UI that abstracts away all infrastructure concepts and presents a clean, intuitive interface.

### Goals

- Unified portal for managed and BYOC agents
- Chat UI for interactive agent sessions with real-time streaming
- Observation view for headless agents (same UI, chat input disabled)
- Full run history accessible regardless of when the user connects
- Follows Hades design system ([frontend-component-consolidation](2026-05-04-frontend-component-consolidation.md), [terra-design-system-hardening](2026-05-04-terra-design-system-hardening.md))

### Non-Goals

- Agent definition via k8s CRDs or YAML (not the frontend's concern)
- Real-time push notifications for run state (polled via REST in v1)
- Multi-observer: only one browser session per run in v1

---

## Decision Drivers

* Hades design system enforced — no raw HTML, all components from Hades or app-level Tier 2
* SSE via the browser's native `EventSource` API is the simplest streaming mechanism for the chat view
* The portal must handle both managed and BYOC agents without separate views
* Next.js server components handle data fetching; client components handle SSE and interactivity

---

## Considered Options

### Option A: WebSocket for chat

**Description:** Browser opens a WebSocket to `kora-agents`. Messages and events flow bidirectionally over a single persistent connection.

**Pros:**
- Bidirectional — user messages and agent output on one connection
- Natural for real-time interactive use

**Cons:**
- WebSocket requires more complex client and server handling than SSE + POST
- SSE + POST achieves the same result with simpler primitives
- WebSocket is not needed until true bidirectional features (e.g. interrupt mid-stream) are required

### Option B: SSE for output + POST for input

**Description:** Browser opens an `EventSource` connection to the events stream. User messages are sent via separate POST requests.

**Pros:**
- `EventSource` is a browser native — automatic reconnect, no custom retry logic
- POST for messages is a standard HTTP request — simple, cacheable, observable
- Server implementation is simpler than WebSocket

**Cons:**
- Two connections — one persistent SSE, one per-message POST
- Not bidirectional — cannot interrupt mid-stream without a separate POST endpoint

### Option C: Long-polling

**Description:** Browser polls for new events every N seconds.

**Pros:**
- Simplest server implementation

**Cons:**
- Unacceptable latency for token streaming — polling intervals cannot match real-time output

---

## Decision

**Chosen Option: B — SSE (`EventSource`) for output + POST for input**

### Portal Structure

The agent portal lives at `/agents` and has two sections:

**Agent Types** (managed agents): list, create, edit, delete. Each type shows: name, framework, status, active runs, total runs.

**All Agents** (managed + BYOC, unified): list of all current and recent agents. BYOC agents have a "BYOC" badge and no management controls.

### Page Routes

| Route | Description |
|---|---|
| `/agents` | Agent portal — unified list of managed and BYOC agents |
| `/agents/new` | Create managed agent type |
| `/agents/{id}` | Agent type detail — runs list, usage stats |
| `/agents/{id}/runs/{run_id}` | Run view — chat UI or headless observation |

### Chat / Observation View (`/agents/{id}/runs/{run_id}`)

The run view is a single component that renders differently based on run type:

**Interactive (managed agent, user initiated):**
- Full event stream rendered in a scrolling feed: text streaming in real time, tool call cards, thinking blocks (collapsible), message boundaries
- Chat input at the bottom — sends `POST /api/v1/runs/{run_id}/messages`
- Connection status indicator (connected / reconnecting)

**Headless observation (managed agent, headless run):**
- Same event stream feed — full history from start
- Chat input disabled with "Headless run — observation only" label

**BYOC observation:**
- Usage and status only — no event stream (BYOC agents don't use the session-manager event log)

### SSE Connection Lifecycle

```
Component mounts
  → new EventSource('/api/v1/runs/{run_id}/events')
  → browser connects, receives event log replay (full history)
  → receives live events as they occur
  → renders each event according to type

Browser tab closed / component unmounts
  → EventSource.close()
  → kora-agents releases Postgres LISTEN connection

Network disconnect
  → EventSource automatically reconnects (browser-native)
  → sends Last-Event-ID header with last received sequence_no
  → kora-agents resumes from that sequence_no
```

### Event Rendering

| Event Type | UI Rendering |
|---|---|
| `text_delta` | Appended to current assistant message bubble (streaming) |
| `thinking_delta` | Appended to a collapsible "Thinking..." block |
| `tool_use` | Tool call card: tool name + input (collapsible JSON) |
| `tool_result` | Appended to the preceding tool call card: result + is_error badge |
| `message_stop` | Finalises the current assistant message bubble |
| `run_state` | Updates the status indicator (pill badge in header) |

### BFF Route for SSE

The Next.js BFF proxies the SSE stream from `kora-agents`:

```
GET /api/v1/runs/{run_id}/events (Next.js route handler)
  → validates session cookie, extracts user's platform JWT
  → opens SSE connection to kora-agents: GET /api/v1/runs/{run_id}/events
      Authorization: Bearer {user-platform-jwt}
  → pipes stream to browser response
```

The BFF forwards the user's platform JWT (obtained from the session cookie at login and stored server-side) as a `Bearer` token to `kora-agents`. `kora-agents` validates this JWT against the identity-service — the same mechanism used by all other platform services. The browser never holds the platform JWT directly and never calls `kora-agents` directly.

All non-streaming BFF-to-kora-agents calls (POST to start a run, GET to list agent types, etc.) follow the same pattern: the BFF extracts the user's platform JWT from the session and forwards it as `Authorization: Bearer`.

### Agent Type Creation Flow

```
User navigates to /agents/new
  → fills: name, description, framework (select), system prompt (textarea)
  → submits → POST /api/v1/agent-types
  → redirect to /agents/{id} with state: provisioning
  → page polls GET /api/v1/agent-types/{id} until state: active
  → shows success state when active
```

### Hades Component Mapping

| UI Element | Hades Component |
|---|---|
| Layout | `Stack`, `Grid` |
| Agent list | `Table`, `TableRow`, `TableCell` |
| Status badges | `Badge` (variant: success/warning/error/neutral) |
| Run event feed | `Stack` with custom `RunEventItem` (Tier 2) |
| Chat input | `TextArea` + `Button` |
| Tool call card | `Card` + `Stack` (Tier 2 compound) |
| Thinking block | `Collapsible` + `Text` (Tier 2) |
| Framework select | `Select` |
| Scope multi-select | `MultiSelect` |

---

## Consequences

### Positive

- `EventSource` handles reconnection automatically — no custom retry logic in the frontend
- Unified portal reduces cognitive overhead — one place for all agent activity
- SSE + POST is simple to implement and test
- Observation view reuses the same chat component — no separate implementation

### Negative

- BFF proxy adds a hop to the SSE path — acceptable within-cluster latency
- No interruption of mid-stream agent output in v1 (requires a separate cancel endpoint, deferred)
- BYOC agents in the unified view show limited data — may appear incomplete next to managed agents

### Neutral

- Run view polling for state (not push) — may show stale state briefly before the next poll

---

## Implementation

### Component Placement

Following [embeddings-and-responses-api-endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md) (Frontend Component Consolidation):

- `RunEventFeed`, `RunEventItem`, `ToolCallCard`, `ThinkingBlock` — Tier 2 (app-level shared, `frontend/src/components/agents/`)
- `AgentStatusBadge`, `FrameworkBadge` — Tier 2
- All layout and primitive components — Tier 1 (Hades)

### API Client

The agent portal API calls are made via typed clients following the existing platform pattern (generated from OpenAPI or hand-typed per [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) conventions).

---

## Validation

### Success Metrics

- First SSE event rendered in browser within 5 seconds of run start
- Reconnect replays full history without visible gap within 2 seconds
- Agent type creation UI completes and shows active state within 90 seconds end-to-end
- BYOC agents appear in portal within 30 seconds of pod start

---

## Related Decisions

- [terra-design-system-hardening](2026-05-04-terra-design-system-hardening.md) — Hades component enforcement
- [semantic-html-consolidation](2026-05-04-semantic-html-consolidation.md) — Semantic HTML requirements
- [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md) — API surface the UI calls
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run/session model the UI presents
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — SSE event format the UI renders
- [byoc-agent-discovery-and-observability](2026-05-11-byoc-agent-discovery-and-observability.md) — BYOC agents surfaced in the unified portal

---

**Last Updated:** 2026-05-08
