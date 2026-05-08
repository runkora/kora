# Agent Control Plane — Design Session Notes

> Decisions and reasoning from the initial design session. Outstanding questions at the bottom.
> For platform architecture context see existing ADRs and docs/architecture.md.

---

## What We Are Building

A full agentic control plane. Two agent categories:

**Managed agents** — defined via Orbit UI/API, hosted on the platform. Platform deploys infrastructure, issues identity, provisions LLM access. Users interact via chat UI or observe headless activity.

**BYOC agents** — tenant-built containers in tenant namespaces. Platform already provides identity (SPIFFE) and LLM access (inference-gateway) via existing mechanisms. Platform auto-discovers them and surfaces usage/status in the portal. No chat from Orbit UI.

Both appear in the same agent portal view.

---

## Agent Definition (Managed)

What a user configures when creating a managed agent type:

| Field | Notes |
|---|---|
| Name | |
| Description | |
| Framework | `claude-code` \| `codex` \| `opencode` — which CLI to wrap |
| System prompt / instructions | Passed to the framework at session start |
| Tool scopes | Creates grants in policy-service; governs the platform JWT the agent receives |

**Explicitly excluded from v1:** image selection (platform-managed), trigger definitions (scheduled/webhook), per-agent resource limit overrides.

The agent definition is DB-backed in `orbit-agents`. Not a k8s CRD.

---

## Execution Topology

### Decision: StatefulSet per agent type per tenant. Session-manager inside each pod handles multiple concurrent sessions.

**Rejected: spin-up-on-demand (pod per session)**
Every pod carries three containers: session-manager + identity-proxy + Envoy sidecar. Creating and tearing down that stack per session is too expensive — SPIRE SVID acquisition, Envoy startup, and SPIRE entry registration overhead make cold starts prohibitive at session granularity.

**Chosen: StatefulSet + multi-session session-manager**
- One StatefulSet per agent type per tenant
- Pods get stable DNS: `{agent}-{n}.{agent}.{namespace}.svc.cluster.local`
- Each pod's session-manager handles **multiple concurrent sessions**
- Each session gets its own isolated filesystem mount / worktree and its own CLI subprocess
- Pod scaling is resource-driven — HPA on CPU/memory, not session count
- Sidecar cost (identity-proxy + Envoy) amortised across all sessions on the pod

---

## Pod Composition

```
Agent Pod (StatefulSet)
├── session-manager    (Orbit-built, main container)
├── identity-proxy     (existing sidecar — SVID → platform JWT, injected on outbound)
└── envoy              (Istio — mTLS, ext_proc for outbound token injection)
```

**session-manager** is the Orbit-built process that:
- Manages multiple concurrent sessions within the pod
- For each session: creates an isolated worktree and spawns a framework CLI subprocess (Claude Code, Codex, etc.)
- Reads each subprocess stdout via a framework-specific adapter
- Translates output to the Orbit standard event format
- Streams events per-session to `orbit-agents` via SSE
- Pipes user messages to the correct subprocess stdin
- On session end: snapshots worktree, wipes directory, cleans up subprocess

---

## Streaming Architecture

### Why we cannot intercept the inference-gateway stream

Claude Code, Codex, etc. make their own internal calls to the inference-gateway. That stream flows **into** the CLI — it's the LLM sending tokens to the agent process. We cannot relay this to the browser because:
- Wrong direction (into the CLI, not out of it)
- Multiple internal LLM calls per user message (tool use chains)
- Raw stream includes internal scaffolding (tool call markup, system prompts, conversation history replay) the user should not see

**We read the CLI's stdout instead.** This is the processed, composed output — what the user should actually see.

### Framework adapters

Each supported framework emits stdout differently. The session-manager has a framework-specific adapter per CLI that translates its output into the Orbit standard event format. The frontend only speaks the Orbit format. Adding a new framework = adding a new adapter in the session-manager.

### Orbit standard event format

```json
{"type": "text_delta",     "delta": "I'll write that function..."}
{"type": "thinking_delta", "delta": "Let me consider the approach..."}
{"type": "tool_use",       "id": "t1", "name": "write_file", "input": {"path": "src/..."}}
{"type": "tool_result",    "id": "t1", "content": "success"}
{"type": "message_stop"}
```

### Data flow

**Output (streaming to browser):**
```
CLI subprocess stdout
  → session-manager adapter (parse + normalise)
  → SSE to orbit-agents  (intra-cluster, mTLS via Istio, stable StatefulSet DNS)
  → SSE relay to browser
```

**Input (user messages):**
```
browser → POST /sessions/{id}/messages
  → orbit-agents
  → POST to session-manager
  → piped to subprocess stdin
```

SSE connection stays open for the life of the session. User messages are short-lived POSTs on a separate path.

---

## Session Routing

**New session routing:** `orbit-agents` routes new sessions to any pod — the session-manager accepts them and manages capacity internally. `orbit-agents` can distribute across pods via the StatefulSet headless service or round-robin across known ordinals.

**Existing session routing:** once a session is running on a specific pod, all subsequent messages for that session must go to the same pod (subprocess and worktree are local to it). `orbit-agents` stores `session_id → pod_ordinal` in Postgres and routes accordingly via stable StatefulSet DNS.

**Scaling:** HPA on CPU/memory. Pods scale when resource pressure builds from concurrent sessions, not from session count directly. Scale-down only when pods are idle.

---

## orbit-agents Service

New backend service (`backend/cmd/orbit-agents`). Follows existing platform patterns.

Owns:
- Agent type registry (CRUD, DB-backed)
- Session and run state machine (DB-backed)
- Session routing (label queries + StatefulSet DNS)
- SSE relay between browser and session-manager
- Grant creation/deletion in policy-service on agent type changes
- k8s resource management: creates StatefulSet, headless Service, RBAC per agent type
- SPIRE entry registration (calls SPIRE registration gRPC API — one entry per agent type, keyed to pod selector)
- BYOC agent discovery (watches k8s pod events in tenant namespaces)

### Decision: no CRDs in v1

**Rejected: extend the Orbit Operator with OrbitAgent CRDs**
The operator is designed for platform infrastructure (SPIRE, Istio, charts). Agent management is a runtime control plane concern — frequent state changes, API-driven, DB-backed state machines. Declarative reconciliation is the wrong model for run-level lifecycle. Bloats the operator's mission.

**Rejected: new service + lightweight OrbitAgent CRD for k8s reconciliation**
Adds CRD machinery before we know the data model is stable. GitOps management of agent types is not a v1 requirement.

**Chosen: orbit-agents service, fully imperative**
Manages k8s resources directly via the k8s API. Includes a lightweight watch-based drift detector to catch externally deleted resources. CRDs promoted in a future version when GitOps management of agent types becomes a real requirement.

---

## Workspace Model

- Each session gets an isolated worktree: `/workspaces/{session-id}/` on the pod's volume
- On session end: snapshotted to platform storage
- On session resume: snapshot restored into a fresh worktree
- On pod recycle: worktree directory wiped

---

## BYOC Agent Visibility

- `orbit-agents` watches k8s pod events across all `orbit-tenant-*` namespaces
- Pods with Orbit SPIFFE identity surfaced in the agent portal
- Usage already attributed via inference-gateway JWT (`principal_type: AGENT`)
- Portal shows: status, usage stats, run history inferred from usage logs
- No chat interaction; no lifecycle management by platform
- Same portal view as managed agents, different capability flags surfaced

---

## Topology

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  orbit-system                                                                │
│                                                                              │
│  ┌───────────┐    ┌────────────────────────────────────────────────────┐    │
│  │  frontend │◄──►│  orbit-agents                                       │    │
│  └───────────┘    │  registry · sessions · routing · SSE relay          │    │
│                   └──────┬────────┬──────────┬─────────┬───────────────┘    │
│                          │        │          │         │                     │
│                       k8s API  postgres   SPIRE    policy-svc               │
│                                                                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │ inference-gateway│  │  policy-service   │  │ identity-service │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
└───────────────────────────────────────────────────────────────────────────-─┘
                          │ mTLS · StatefulSet DNS
┌─────────────────────────▼───────────────────────────────────────────────────┐
│  orbit-tenant-{id}                                                           │
│                                                                              │
│  coding-agent StatefulSet                                                    │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │  Pod 0  [session-state: active]                                      │   │
│  │  coding-agent-0.coding-agent.orbit-tenant-example.svc...             │   │
│  │  ┌─────────────────────────────┐  ┌───────────────┐  ┌───────────┐  │   │
│  │  │  session-manager            │  │identity-proxy │  │  envoy    │  │   │
│  │  │  ┌──────────┐  ┌─────────┐  │  │ SVID→JWT      │  │ mTLS      │  │   │
│  │  │  │ Claude   │  │worktree │  │  │ injected on   │  │ ext_proc  │  │   │
│  │  │  │ Code     │  │/sess-0/ │  │  │ outbound LLM  │  │ (outbound)│  │   │
│  │  │  │ (proc)   │  │         │  │  │ calls         │  │           │  │   │
│  │  │  └──────────┘  └─────────┘  │  └───────────────┘  └───────────┘  │   │
│  │  └─────────────────────────────┘                                     │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │  Pod 1  [session-state: available]                                   │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  BYOC pods (auto-discovered, portal visibility only)                 │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Run vs Session

Two distinct entities:

**Run** — unit of work. Server-side, persists independently of browser connections. Tied to a pod + worktree. A run can have zero or one active session at any time.

**Session** — a browser connection to a run. Transient. Destroyed on disconnect; run continues.

### Run lifecycle
```
pending → starting → active → completed
                             → failed
```
- No timeout while the CLI subprocess is running
- Cleanup is snapshot-gated: CLI exits → snapshot workspace → mark completed/failed → release resources
- User disconnect does not affect the run

### Context retention on reconnect
Events are persisted to a `run_events` table (Postgres) as emitted by the transform layer. On reconnect: `orbit-agents` replays the full event log to the new SSE connection, then attaches to the live stream via Postgres LISTEN/NOTIFY. User always sees full history regardless of when they connect.

### Resume (not v1 — design for it)
Fresh CLI subprocess + reconstruct conversation context from `run_events`. No separate conversation store needed — `run_events` is the source of truth.

---

## Transform Layer

A per-framework wrapper presenting a standard interface to the session-manager regardless of the underlying CLI. The session-manager always speaks the Orbit event format upward. Each transform layer handles:

- Starting the CLI subprocess (flags, env, working directory)
- Sending user messages (stdin pipe, API, or file — per framework)
- Reading and parsing stdout (framework-specific format)
- Translating to the Orbit standard event format
- Signalling subprocess exit/crash

Adding a new framework = implementing a new transform layer. Everything above is framework-agnostic.

---

## Outstanding Questions

### 1. Transform layer — per-framework spike needed
Does Claude Code support interactive stdin mode as a long-running subprocess? Does Codex? What does each emit during tool execution? Needs a spike per framework before the transform layer interface can be finalised.

### 2. Chat protocol — reconnection detail
Replay-on-reconnect is resolved via `run_events`. Still to design: what the client does if the SSE connection drops mid-stream, backpressure handling, joining a headless run already in progress.

### 3. Workspace snapshot storage
S3, cloud-agnostic. **Deferred.**

### 4. MCP tool access for agents
**Deferred.**

### 5. Delegation / act-on-behalf
**Deferred.**

### 6. Run error paths
CLI crash and pod eviction mid-run state transitions not fully defined. Resume design covers the recovery concept but platform-side failure handling needs detail.
