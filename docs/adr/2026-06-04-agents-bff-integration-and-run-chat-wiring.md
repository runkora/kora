# Agents BFF Integration and Run Chat Wiring

**Status:** Accepted  
**Date:** 2026-06-04
**Deciders:** Platform Engineering  
**Technical Story:** The agents portal UI ([agent-portal-and-chat-ui](2026-05-11-agent-portal-and-chat-ui.md)) is fully scaffolded against static mock data. The `orbit-agents` backend ([orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md)) has a complete REST + SSE API surface. This ADR closes the gap by wiring every BFF route to the real backend and replacing the static run chat with a live SSE-backed implementation.

---

## Context

### Background

[agent-portal-and-chat-ui](2026-05-11-agent-portal-and-chat-ui.md) established the agents portal architecture: Next.js RSC pages for initial data, client-side BFF routes for mutations, and an `EventSource` SSE connection for the run chat. [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) defines the kora standard event format emitted by the session-manager through `orbit-agents`. [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) established the BFF session pattern — the browser never holds a platform JWT; the BFF extracts it from the session cookie and forwards it to backend services.

The portal implementation went through a UI-first build phase where all BFF routes (`/api/agents/**`) return static mock data. The run chat hook (`useRun`) is backed entirely by hardcoded turns. Nothing calls the real `orbit-agents` service.

### Problem Statement

The agents portal is non-functional in any environment where the backend is reachable. Every meaningful user action — listing agents, starting runs, sending messages — silently operates against mock fixtures. There is no path from the browser to `orbit-agents`.

### Goals

- Wire all ten BFF route handlers to the real `orbit-agents` REST API
- Implement an SSE proxy BFF route that pipes the `orbit-agents` event stream to the browser
- Replace the static `useRun` hook with a live SSE-backed implementation
- Wire user message sending to `POST /api/v1/runs/{id}/messages`
- Ensure agent settings save (name, description, framework, system prompt, model, concurrency) persists to the backend
- Add `model_id` to the `AgentType` schema (backend + migration)
- Add the missing `PATCH /api/v1/agent-types/{id}` handler to `orbit-agents`

### Non-Goals

- **Approval flows**: Claude Code runs headless and currently does not surface `tool_use` approval gates to external callers. The `agent.approval` event type and the approve/reject interaction are deferred until the session-manager exposes a stable approval protocol.
- **Tool permission UI to policy wiring**: The allow/approve/block toggles in the settings tab remain UI-only placeholders; policy integration is a separate story.
- **Canvas views**: The `CanvasPanel` and `CanvasView` types are defined. They will light up automatically once `tool_result` events carry canvas payloads — no frontend changes required.
- **Multi-observer**: Only one active SSE subscriber per run, per [agent-portal-and-chat-ui](2026-05-11-agent-portal-and-chat-ui.md).
- **Horizontal scaling of the SSE path**: Sticky routing for `orbit-agents` is deferred per [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md).
- **Webhooks**: Outbound webhook callbacks on run completion or failure are not part of this integration. Deferred to a future story.

---

## Decision Drivers

- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) mandates the BFF pattern — the browser never calls `orbit-agents` directly
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md)'s event format is already implemented in `orbit-agents` and the session-manager; the frontend must map those event types to `Turn` state, not invent a new protocol
- `EventSource` reconnect with `Last-Event-ID` is handled natively by the browser; the BFF proxy must forward this header to preserve replay semantics
- The `orbitClient` / `agentsClient` pattern already used by other BFF routes (models, identity) keeps backend call logic consistent

---

## Considered Options

### Option A: Direct browser → orbit-agents calls

**Description:** Remove the BFF proxy. The browser holds a platform JWT and calls `orbit-agents` directly for CRUD and SSE.

**Pros:**
- One fewer network hop for SSE events (lower latency)
- Simpler frontend code — no BFF intermediary

**Cons:**
- Violates [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md): platform JWTs must never be exposed to the browser
- CORS policy and cluster-internal `orbit-agents` URL would need to be opened to the public internet
- Inconsistent with every other feature in the portal

### Option B: BFF proxy for all routes (chosen)

**Description:** All browser requests go through the Next.js BFF. The BFF validates the session cookie, extracts the platform JWT, and forwards it to `orbit-agents` as `Authorization: Bearer`. The SSE stream is proxied without buffering via the Next.js Route Handler streaming API.

**Pros:**
- Consistent with [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) and all other portal features
- `orbit-agents` remains cluster-internal — never exposed to the public internet
- Session validation at the BFF layer means `orbit-agents` can trust the JWT without additional OIDC round-trips

**Cons:**
- BFF adds one network hop on the SSE path — acceptable given in-cluster latency

---

## Decision

**Chosen Option: B — BFF proxy for all routes.**

### Backend changes required

**`model_id` field**

The `AgentType` model and its settings form include a model selector. The current backend schema does not persist `model_id`. A new migration adds `model_id TEXT` (nullable) to `agent_types`. The Go `AgentType` struct gains `ModelID *string`. `CreateAgentType`, `UpdateAgentType`, and all SELECTs are updated accordingly.

**PATCH handler for agent type updates**

`orbit-agents` currently exposes `POST` (create) and `DELETE` for agent types but no `PATCH`. The settings tab calls `PATCH /api/agents/types/{id}` on save. Three additions are required:

1. A new `UpdateAgentTypeRequest` struct in `models.go` (all fields optional): `name`, `description`, `framework`, `system_prompt`, `max_concurrent_runs_per_pod`, `model_id`.
2. An `UpdateAgentType(ctx, req)` method on the `Service` that applies partial updates via the existing `repo.UpdateAgentType`.
3. A `handleUpdateAgentType` handler in `server.go` wired to `PATCH /api/v1/agent-types/{id}` in `Boot()` and registered in `agentRoutes()` under the `agents:types:write` scope. The handler returns the updated `AgentType`.

### BFF config and client

A new environment variable `KORA_AGENTS_URL` is added to `frontend/src/server/config.ts` alongside the existing `KORA_API_URL` and `INFERENCE_GATEWAY_URL`. A typed `agentsClient(accessToken)` is created in `frontend/src/server/agents-client.ts` following the existing `orbitClient` pattern (10-second timeout, `no-store` cache, `Authorization: Bearer` header).

### BFF route wiring

All ten existing BFF route handlers under `frontend/src/app/api/agents/` replace their mock returns with real backend calls via `agentsClient`. The `withAuth` wrapper already validates the session; handlers extract `session.accessToken` and `session.tenantId` for each call. `tenantId` is passed as a query parameter on all list endpoints and in the request body for `POST /api/v1/runs` — `orbit-agents` has no JWT-based tenant extraction and requires it to be supplied explicitly.

Note: the PATCH handler for `types/[typeId]/route.ts` does not yet exist (the file currently only exports `GET` and `DELETE`). It must be added as a new export alongside the existing handlers.

| BFF route | HTTP method | Backend call |
|---|---|---|
| `/api/agents/types` | GET | `GET /api/v1/agent-types?tenant_id={tenantId}` |
| `/api/agents/types` | POST | `POST /api/v1/agent-types` (body forwarded; inject `tenant_id`) |
| `/api/agents/types/[typeId]` | GET | `GET /api/v1/agent-types/{typeId}` |
| `/api/agents/types/[typeId]` | PATCH | `PATCH /api/v1/agent-types/{typeId}` (new export — handler does not exist yet) |
| `/api/agents/types/[typeId]` | DELETE | `DELETE /api/v1/agent-types/{typeId}` |
| `/api/agents/runs` | GET | `GET /api/v1/runs?tenant_id={tenantId}` |
| `/api/agents/types/[typeId]/runs` | GET | `GET /api/v1/runs?tenant_id={tenantId}&agent_type_id={typeId}` |
| `/api/agents/types/[typeId]/runs` | POST | `POST /api/v1/runs` (body includes `tenant_id`, `agent_type_id`) |
| `/api/agents/runs/[runId]` | GET | `GET /api/v1/runs/{runId}` |
| `/api/agents/runs/[runId]` | DELETE | `DELETE /api/v1/runs/{runId}` |
| `/api/agents/runs/[runId]/messages` | POST | `POST /api/v1/runs/{runId}/messages` (backend returns 202 Accepted; BFF mirrors 202) |

Two RSC initial-data loaders are updated to call the backend directly via `agentsClient(accessToken)` rather than returning mock data:
- `agents-server.ts` — calls `GET /api/v1/agent-types` and `GET /api/v1/runs` with `tenant_id`
- `agent-detail-server.ts` — calls `GET /api/v1/agent-types/{slug}` (currently also ignores `accessToken` and returns a mock)

Both files currently have `void accessToken` placeholders that will be replaced with real client calls.

### SSE proxy BFF route

A new route handler `frontend/src/app/api/agents/runs/[runId]/events/route.ts` is added:

1. Validates session via `withAuth`
2. Extracts `Last-Event-ID` from the incoming request headers
3. Opens a `fetch()` to `${KORA_AGENTS_URL}/api/v1/runs/{runId}/events` with `Authorization: Bearer {accessToken}` and the forwarded `Last-Event-ID`
4. Constructs a streaming `NextResponse` that pipes the backend response body directly — no buffering
5. Sets response headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`

The proxy does not parse or re-encode events — it is a transparent byte pipe. This preserves SSE `id:`, `event:`, and `data:` framing exactly as `orbit-agents` emits it, including sequence numbers for browser-native `Last-Event-ID` replay.

### Frontend run chat hook

`useRun` is rewritten against the live event stream:

**Initialisation**: On mount, `GET /api/agents/runs/{runId}` fetches the run's current state, agent name, and model. This provides the initial `runState` and header metadata without waiting for SSE.

**SSE subscription**: A `useRunEvents` hook opens an `EventSource` to `/api/agents/runs/{runId}/events`. Events are reduced into `turns` and `views` state according to [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md)'s event format:

| Event type ([agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md)) | Frontend action |
|---|---|
| `text_delta` | Append to the current in-progress `AgentTurn`'s text element |
| `thinking_delta` | Append to the in-progress turn's `ThinkingBlock` |
| `tool_use` | Append a `ToolCall` with `status: 'running'` to the in-progress turn |
| `tool_result` | Update the matching `ToolCall` (by `tool_use_id`) to `done` or `error`; attach canvas view if present |
| `message_stop` | No-op for live rendering; turn accumulation continues until `message_complete` |
| `message_complete` | Flush the in-progress `AgentTurn` to `turns[]` and clear the accumulator |
| `run_state` | Update `runState` |

On reconnect (browser-native `EventSource` retry with `Last-Event-ID`), token-level events (`text_delta`, `thinking_delta`) that occurred before the client connected are not available — this is by design per [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md). The client will receive `message_complete` for any turns that completed while disconnected, rendering them as complete turn bubbles.

**Message sending**: `addHumanMessage(content)` optimistically appends a `HumanTurn` to local state, then `POST /api/agents/runs/{runId}/messages` with `{ content }`. No response body is expected (202 Accepted).

**Reconnect strategy**: The `useRunEvents` hook monitors `EventSource.onerror`. On error, it closes and reopens with exponential backoff (1 s → 2 s → 4 s, capped at 30 s). The browser's built-in `Last-Event-ID` replay ensures no durable events are missed.

### Run termination

The "Stop run" action in the run topbar calls `DELETE /api/agents/runs/{runId}` (BFF → backend). On 204 response, `runState` is set to `'failed'` locally and the chat input is disabled.

---

## Consequences

### Positive

- The agents portal becomes functional end-to-end in any environment with `orbit-agents` reachable
- SSE proxy preserves all [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) semantics (sequence numbers, replay, backpressure) without frontend complexity
- `EventSource` reconnect is browser-native — the hook handles only the error-case backoff, not the reconnect protocol itself
- No approval UI is shipped until the session-manager protocol is stable — no incomplete UI surfaces

### Negative

- Two backend changes (`model_id` migration, PATCH handler) are required before the settings tab is fully functional
- Token-level events missed during a disconnect are not recoverable — users reconnecting mid-turn see only completed turns (by design per [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md))
- BFF SSE proxy adds one in-cluster hop; acceptable given deployment topology

### Neutral

- Mock data in `_mock/data.ts`, `agents-server.ts`, and `agent-detail-server.ts` is removed; local development requires `orbit-agents` to be running or a local stub
- `KORA_AGENTS_URL` is a new required environment variable in all deployment environments

---

## Implementation

### Ordered steps

```
Step 1  Backend: model_id migration + AgentType struct + query updates
Step 2  Backend: UpdateAgentTypeRequest struct + Service.UpdateAgentType method
                 + PATCH /api/v1/agent-types/{id} handler registered in server.go and agentRoutes()
        (Steps 1–2 can land in one PR; settings save unblocked after merge)

Step 3  Frontend: KORA_AGENTS_URL config + agentsClient (agents-client.ts)
Step 4  Frontend: Add model_id to UpdateAgentTypeRequest in frontend/src/shared/api.ts
Step 5  Frontend: Replace all ten BFF mock route handlers with real calls
                  (includes adding the missing PATCH export to types/[typeId]/route.ts)
                  Update agents-server.ts and agent-detail-server.ts to use agentsClient
Step 6  Frontend: Add SSE proxy BFF route (runs/[runId]/events/route.ts)
        (Steps 3–6 can land together; CRUD + SSE proxy unblocked after merge)

Step 7  Frontend: Rewrite useRun — fetch initial state + useRunEvents SSE hook
Step 8  Frontend: Wire addHumanMessage to POST messages + run termination
        (Steps 7–8 complete the run chat)
```

Steps 1–2 are backend-only and can be worked in parallel with Steps 3–6 (frontend plumbing). Step 7 depends on Step 6 (SSE proxy must exist) and benefits from Step 2 (PATCH) being deployed.

### Deferred: approval flows

Claude Code currently runs headless. The session-manager does not expose a stable protocol for surfacing tool approval requests to external callers or routing approve/reject responses back to the subprocess. The `ApprovalPrompt` component and its local `resolution` state remain in place as UI scaffolding. Wiring approval responses to the backend is deferred until the session-manager approval protocol is defined and stabilised.

---

## Validation

### Success Metrics

- Agent list page renders real agents (not mocks) within 3 seconds of navigation
- Agent settings save persists name, description, system prompt, and model to `orbit-agents` without error
- First `text_delta` event renders in the run chat within 2 seconds of the agent emitting it
- Browser reconnect (simulated by dropping and restoring network) replays durable event history without a page reload
- Run termination (DELETE) transitions run to failed state and disables chat input

---

## Related Decisions

- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — BFF session pattern; platform JWTs never exposed to browser
- [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md) — `orbit-agents` REST API surface this ADR wires to
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run and session lifecycle model
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — kora standard event format; event table ownership; replay semantics
- [agent-portal-and-chat-ui](2026-05-11-agent-portal-and-chat-ui.md) — Portal architecture this ADR implements

---

**Last Updated:** 2026-05-25
