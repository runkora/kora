# Agent Control Plane — Product Requirements Document

**Status:** Implemented  
**Created:** 2026-05-08  
**Last Updated:** 2026-05-08  
**Owner:** Orbit Core Team  
**Stakeholders:** Orbit Core Team, Enterprise Platform Operators, Agent Developers, End Users

---

## Executive Summary

The Agent Control Plane is Orbit's execution layer for AI agents. It allows platform operators and tenant users to define, deploy, and interact with AI agents that run inside the cluster with full platform identity, governed LLM access, and isolated workspaces. Managed agents are built on real agentic CLI frameworks (Claude Code, Codex, OpenCode) and are accessible via a chat UI or observable in headless mode. Tenant-deployed (BYOC) agents are auto-discovered and surfaced in the same portal with usage visibility. The control plane is the realisation of Orbit's agent-first architecture — making agents first-class citizens of the platform rather than external callers of it.

---

## Problem Statement

### Current State

Orbit's identity, policy, and inference infrastructure already supports agents as first-class principals. The `AGENT` principal type exists, SPIFFE/SPIRE attestation works for workloads in tenant namespaces, and the inference-gateway attributes usage to agents. However, there is no control plane for agent lifecycle. There is no way to define an agent, deploy it, start a session, interact with it, or observe what it is doing. Agents that exist today are externally managed and invisible to the platform beyond token usage.

### User Pain Points

**For end users (tenants):**
- No way to start and interact with an AI agent through the Orbit UI
- No visibility into what agents are doing — no activity feed, no run history, no context
- Cannot resume an agent session after closing the browser
- Cannot observe a headless agent run in progress

**For platform operators:**
- No self-service surface for defining agent types — requires manual k8s and SPIRE configuration
- No unified portal for managed and BYOC agents — agents from different tenants are invisible
- No governance of agent workloads beyond the existing token/policy layer
- No way to see which agents are active, what they are running, or how much they cost

**For agent developers (BYOC):**
- Must configure SPIRE entries and k8s resources manually — no self-service
- No platform surface to understand their agent's identity, usage, or health

### Business Impact

- **Missing product surface:** The platform cannot fulfil its agent-first value proposition without a way to actually run and interact with agents
- **Operator friction:** Every new agent type requires manual platform configuration — not scalable
- **Invisible workloads:** Agent activity, cost, and health are not surfaced, making governance impossible
- **Competitive gap:** Platforms that offer built-in agentic execution are becoming the default expectation

---

## Goals and Objectives

### Primary Goals

1. **Agent definition:** Allow users to define managed agent types via the Orbit UI and API — specifying the framework, instructions, and tool access
2. **Managed execution:** Platform deploys, scales, and manages agent infrastructure — SPIRE identity, LLM access, workspace — transparently
3. **Interactive sessions:** Users can start a chat session with any managed agent and interact with it in real time with streaming output
4. **Headless observation:** Users can observe a headless agent run in progress — seeing tool calls, thinking, and output — without interacting
5. **Context retention:** Sessions can be resumed; full run history is available regardless of when the user connects
6. **BYOC visibility:** Tenant-deployed agents are auto-discovered and surfaced in the portal alongside managed agents
7. **Policy-governed access:** Agent LLM access is governed by the existing grant and policy system — no new auth surface

### Success Metrics

- Agent type created via UI deploys a running StatefulSet within 60 seconds
- Time-to-first-token for a new agent chat session under 5 seconds
- 100% of agent LLM calls attributed to correct principal in usage logs
- Run history and event log fully available on reconnect with no data loss
- BYOC agents discovered within 30 seconds of pod reaching Running state
- Zero SPIRE or grant configuration required by tenant users for managed agents

### Non-Goals (v1)

- MCP tool server access for agents (deferred)
- RFC 8693 delegation / act-on-behalf flows (deferred)
- Workspace resume (design for it, do not implement in v1)
- Trigger definitions — scheduled runs, webhook-triggered runs (deferred)
- BYOC agent chat interaction or lifecycle management from Orbit UI
- Per-agent-type resource limit configuration by users (platform defaults apply)
- Multi-agent orchestration / agent-to-agent communication
- Agent marketplace or template sharing

---

## User Stories

### US-001: Define a Managed Agent Type

**As a** tenant user,  
**I want to** create a managed agent type via the Orbit UI,  
**So that** I have a running agent I can start sessions with.

**Acceptance Criteria:**
- [ ] User can create an agent type with: name, description, framework selection, system prompt, tool scope selection
- [ ] Available frameworks: `claude-code`, `codex`, `opencode`
- [ ] Tool scopes selection creates corresponding grants in the policy service — the agent's platform JWT is governed by those grants
- [ ] On creation, platform provisions a StatefulSet in the tenant namespace with the correct configuration
- [ ] SPIRE entry is registered automatically for the agent type (pod selector based)
- [ ] Agent type appears in the agent portal within 60 seconds of creation
- [ ] User does not configure container images, SPIRE entries, k8s resources, or grants manually

---

### US-002: Start a Chat Session with a Managed Agent

**As a** tenant user,  
**I want to** open a chat session with a managed agent from the Orbit UI,  
**So that** I can interact with it in real time.

**Acceptance Criteria:**
- [ ] User selects an agent type from the portal and initiates a new run
- [ ] An available agent pod is assigned and a workspace is provisioned for the run
- [ ] Chat UI opens and the SSE stream begins — user sees agent output streaming in real time
- [ ] User can send messages and see the agent respond
- [ ] Tool calls and tool results are visible inline in the chat stream
- [ ] Thinking/reasoning output is visible where the framework emits it
- [ ] First token appears within 5 seconds of session start

---

### US-003: Observe a Headless Agent Run

**As a** tenant user,  
**I want to** observe a headless agent run already in progress,  
**So that** I can see what the agent is doing without interacting.

**Acceptance Criteria:**
- [ ] Headless runs appear in the agent portal with their current state
- [ ] User can open any active run and see the full event stream from the beginning — tool calls, output, thinking — replayed then live
- [ ] Chat input is disabled for headless runs
- [ ] User can observe without affecting the run

---

### US-004: Reconnect to an Ongoing Run

**As a** tenant user,  
**I want to** close my browser and return to an ongoing agent run later,  
**So that** I do not lose context and can continue interacting.

**Acceptance Criteria:**
- [ ] Closing the browser does not stop the agent run
- [ ] On return, the run appears in the portal with its current state
- [ ] Opening the run replays the full event history from the beginning, then attaches to the live stream
- [ ] User can continue sending messages if the run is still active
- [ ] No data is lost between disconnection and reconnection

---

### US-005: View Run History

**As a** tenant user,  
**I want to** see a history of my agent runs,  
**So that** I can review past activity and outcomes.

**Acceptance Criteria:**
- [ ] Agent portal shows all runs for a given agent type: state, started at, completed at, duration
- [ ] Completed run event logs are accessible and fully replayable
- [ ] Runs are scoped to the authenticated user's tenant

---

### US-006: BYOC Agent Discovery and Visibility

**As a** platform operator or tenant user,  
**I want** tenant-deployed agents to appear automatically in the agent portal,  
**So that** I have a single view of all agent activity across managed and BYOC agents.

**Acceptance Criteria:**
- [ ] BYOC pods in tenant namespaces with a platform SPIFFE identity are discovered automatically within 30 seconds of reaching Running state
- [ ] BYOC agents appear in the agent portal alongside managed agents
- [ ] Portal shows: agent name (from pod labels), status, LLM usage, cost attribution
- [ ] Chat and lifecycle controls are absent for BYOC agents — observation only
- [ ] BYOC agents disappear from the portal when their pods are terminated

---

### US-007: Agent LLM Access via Platform Identity

**As a** managed agent running in the cluster,  
**I want** my LLM calls to be authenticated and governed by the platform automatically,  
**So that** I do not need to manage credentials or implement auth logic.

**Acceptance Criteria:**
- [ ] Agent pods receive a SPIFFE SVID automatically via the Workload API
- [ ] identity-proxy sidecar exchanges SVID for a platform JWT
- [ ] Outbound LLM calls have the platform JWT injected transparently by Envoy
- [ ] inference-gateway enforces the agent's grants — only scopes granted at agent type creation are available
- [ ] All LLM usage is attributed to the agent principal in the usage logs
- [ ] Agent code requires zero credential management or auth configuration

---

### US-008: Deactivate or Delete an Agent Type

**As a** tenant user,  
**I want to** delete an agent type I no longer need,  
**So that** its resources are cleaned up and it stops appearing in the portal.

**Acceptance Criteria:**
- [ ] User can delete an agent type via the UI or API
- [ ] Active sessions are drained before teardown (no mid-session termination)
- [ ] On deletion: StatefulSet, SPIRE entry, grants, and associated k8s resources are removed
- [ ] Completed run history is retained after deletion
- [ ] Agent type no longer appears in the portal

---

## Proposed Solution

### High-Level Approach

The Agent Control Plane consists of a new platform service (`orbit-agents`) that owns agent type registration, run lifecycle, session routing, and event streaming. Agent workloads run as StatefulSets in tenant namespaces — one StatefulSet per agent type per tenant — with a platform-built session-manager process as the main container. The session-manager wraps the agentic CLI (Claude Code, Codex, OpenCode) via a per-framework transform layer that normalises all output to a standard Orbit event format. Events are persisted to a run event log, enabling full context replay on reconnect. The platform issues agent identity, governs LLM access, and manages workspaces transparently — agent code has no awareness of these concerns.

### Agent Definition Model

A managed agent type is defined by:

| Field | Description |
|---|---|
| Name | Human-readable identifier for the agent type |
| Description | What this agent does |
| Framework | Which CLI to wrap: `claude-code`, `codex`, `opencode` |
| System prompt | Instructions passed to the framework at session start |
| Tool scopes | Scopes granted to this agent — creates grants in policy-service |

The definition is stored in the `orbit-agents` database. It drives the creation of the StatefulSet, SPIRE entry, and policy grants. Users do not configure infrastructure directly.

### Execution Model

Each agent type runs as a Kubernetes StatefulSet in the tenant namespace. Pods have stable, predictable DNS names that allow the `orbit-agents` service to route to specific pods. Each pod contains three containers:

- **session-manager** — Orbit-built process; manages multiple concurrent sessions, owns worktrees and subprocess lifecycle
- **identity-proxy** — existing sidecar; exchanges SPIFFE SVID for platform JWT, injects it on outbound LLM calls
- **Envoy** — Istio-injected sidecar; enforces mTLS, runs ext_proc for transparent token injection

The session-manager handles multiple concurrent sessions within a single pod. Each session gets an isolated worktree and a dedicated CLI subprocess. Pods scale horizontally via HPA on CPU and memory — scaling is resource-driven, not session-count driven.

### Transform Layer

The session-manager does not speak any framework-specific protocol. Each supported CLI framework has a transform layer that wraps the subprocess and presents a standard interface to the session-manager. The transform layer handles: starting the CLI with the correct flags and environment, sending user messages to it, reading and parsing its output, and translating that output into the Orbit standard event format. Adding support for a new framework requires implementing a new transform layer — no other component changes.

The Orbit standard event format is a structured stream of typed events:

| Event Type | Description |
|---|---|
| `text_delta` | A chunk of assistant text output |
| `thinking_delta` | A chunk of assistant reasoning/thinking |
| `tool_use` | The agent is invoking a tool |
| `tool_result` | The result of a tool invocation |
| `message_stop` | The agent has finished its current response |

This format is what the frontend renders. It is framework-agnostic.

### Run and Session Model

**Run** — the unit of work. Server-side entity tied to a specific agent pod and worktree. Persists independently of browser connections. A run is active as long as the CLI subprocess is running. Runs survive user disconnects.

**Session** — a browser connection to a run. Transient. Created when a user opens the chat UI for a run; destroyed on disconnect. A run can have zero active sessions (unobserved) or one (user watching or interacting).

Run lifecycle:
```
pending → starting → active → completed
                             → failed
```

There is no timeout on an active run. Cleanup is triggered by the CLI subprocess exiting — on exit, the workspace is snapshotted and pod resources are released. User disconnection does not trigger cleanup.

### Event Log and Context Retention

All events emitted by the transform layer are persisted to a `run_events` table in PostgreSQL as they occur. On reconnect, `orbit-agents` replays the full event log for the run to the new SSE connection, then attaches to the live stream. The user always sees the full run history regardless of when they connect or how many times they have disconnected.

The `run_events` table is also the source of truth for run resume (not v1) — a fresh CLI subprocess can reconstruct conversation context from the persisted event log without any separate conversation storage.

### Session Routing

New sessions are routed to any available pod for the agent type. Existing sessions are pinned to the pod where they were created — the subprocess and worktree are local to that pod. The `orbit-agents` service stores `run_id → pod_ordinal` in its database and routes subsequent requests via the pod's stable StatefulSet DNS name. Routing is cross-namespace (orbit-system → orbit-tenant-{id}), secured by Istio mTLS.

### BYOC Agent Visibility

`orbit-agents` watches Kubernetes pod events across all `orbit-tenant-*` namespaces. Pods with an Orbit SPIFFE identity (identified by annotations set during SPIRE attestation) are surfaced in the agent portal. LLM usage is already attributed to BYOC agents via the inference-gateway JWT — no additional instrumentation is required. BYOC agents appear in the same portal view as managed agents but without chat, lifecycle, or workspace capabilities.

### Streaming Architecture

```
User (browser)
   │  SSE (EventSource) ← streaming output
   │  POST per message  → user input
   ▼
orbit-agents service
   │  Postgres LISTEN/NOTIFY (live events)
   │  Postgres SELECT (event replay on connect)
   │  POST (session claim + message relay)
   ▼
session-manager (in agent pod, via StatefulSet DNS)
   │  reads stdout
   ▼
transform layer (per-framework)
   │  parses CLI output
   ▼
CLI subprocess (Claude Code / Codex / OpenCode)
   │  outbound LLM calls (via Envoy + identity-proxy)
   ▼
inference-gateway → LLM provider
```

### orbit-agents Service

New backend service (`backend/cmd/orbit-agents`) following all existing platform conventions.

Responsibilities:
- Agent type CRUD (database-backed registry)
- Grant management — creates and removes grants in policy-service when agent types are created or deleted
- Kubernetes resource lifecycle — creates and manages StatefulSets, headless Services, and RBAC for each agent type
- SPIRE entry registration — registers one entry per agent type keyed to the pod selector
- Run and session lifecycle management
- Session routing — resolves `run_id → pod_ordinal` and calls session-manager via stable DNS
- SSE relay — replays `run_events` on connect, then tails live events via Postgres LISTEN/NOTIFY
- BYOC discovery — watches pod events in tenant namespaces

No CRDs are introduced in v1. The service manages Kubernetes resources imperatively and runs a lightweight drift watch to detect and reconcile externally deleted resources.

### Workspace Model

Each run gets an isolated worktree provisioned within the pod's volume at session start. On run completion, the workspace is snapshotted to object storage (S3, cloud-agnostic interface). The pod volume is cleared for the next session. Run resume (future) restores the snapshot into a fresh worktree before starting the CLI subprocess.

---

## Scope

### In Scope (v1)

- Managed agent type definition and registration (name, description, framework, system prompt, tool scopes)
- Platform provisioning: StatefulSet, SPIRE entry, grants, headless Service, RBAC
- Interactive chat sessions with managed agents (real-time streaming output, user message input)
- Headless run observation (real-time event stream, no user input)
- Run event log with full replay on reconnect
- Run lifecycle management (start, active, complete, failed)
- Workspace provisioning and snapshot on completion
- BYOC agent auto-discovery and portal visibility
- Agent portal: unified view of managed and BYOC agents, run history, usage stats
- Agent type deletion with graceful session drain and full resource cleanup

### Out of Scope (v1)

| Feature | Disposition |
|---|---|
| MCP tool server access for agents | Deferred |
| RFC 8693 delegation / act-on-behalf | Deferred |
| Workspace resume | Designed for, not implemented |
| Triggered runs (scheduled, webhook) | Deferred |
| BYOC agent chat or lifecycle management | Never (BYOC is tenant-owned) |
| Per-agent-type resource limit configuration | Platform defaults apply in v1 |
| Multi-agent orchestration | Future |
| Agent marketplace / template sharing | Future |
| Agent-level rate limiting | Governed by existing policy grants |

### Future Considerations

- Workspace resume from snapshot (fresh CLI + reconstructed conversation context from `run_events`)
- Triggered runs: schedule-based or webhook-initiated runs without user interaction
- MCP tool access: agents connect to MCP servers as configured by tenant policy
- Delegation: RFC 8693 token exchange for agents acting on behalf of users
- Agent-to-agent: managed agents calling other managed agents as tools
- BYOC self-service: tenant teams register their BYOC agents for enhanced portal features

---

## Requirements

### Functional Requirements

**P0 — Must Have (v1):**
- [ ] Create, read, update, delete managed agent types via API and UI
- [ ] Agent type creation provisions StatefulSet, SPIRE entry, grants, headless Service, and RBAC automatically
- [ ] Agent type deletion drains active sessions and removes all provisioned resources
- [ ] Start a run for an agent type — assigns a pod, provisions a workspace, starts the CLI subprocess
- [ ] Interactive chat: send user messages to a running agent, receive streaming output via SSE
- [ ] Headless observation: connect to any active run and receive its full event stream
- [ ] All events persisted to `run_events` table in real time
- [ ] On reconnect: full event log replayed then live stream attached
- [ ] Run lifecycle state machine: pending → starting → active → completed / failed
- [ ] No timeout on active runs; cleanup triggered by CLI subprocess exit
- [ ] Workspace provisioned per run, snapshotted to object storage on completion
- [ ] BYOC agents auto-discovered from tenant namespaces via k8s pod watch
- [ ] BYOC agents surfaced in agent portal with status and LLM usage
- [ ] Agent portal: list all agent types and runs for the authenticated tenant
- [ ] All agent LLM usage attributed to the correct principal in inference usage logs
- [ ] Agent grants created from tool scopes at agent type creation; removed at deletion

**P1 — Post-v1:**
- [ ] Workspace resume: restore snapshot + reconstruct conversation context from `run_events`
- [ ] Triggered runs: schedule-based and webhook-triggered run initiation
- [ ] MCP tool access: agent type definition includes MCP server access configuration
- [ ] Delegation: RFC 8693 act-on-behalf token for agent runs initiated by a user
- [ ] Run pause and resume
- [ ] Agent type versioning: update framework/instructions without deleting the agent type

**P2 — Nice to Have:**
- [ ] Agent run export: download workspace snapshot and event log
- [ ] Agent metrics dashboard: active sessions, runs per day, token usage trends
- [ ] Agent type templates: pre-configured agent types for common use cases

### Non-Functional Requirements

**Performance:**
- Agent type provisioning complete within 60 seconds of creation
- First token delivered to browser within 5 seconds of session start
- Event replay on reconnect within 2 seconds for runs up to 1 hour of history
- `orbit-agents` API response latency ≤ 100ms p95 (excluding streaming)

**Security:**
- All agent-to-platform communication over mTLS (Istio) — no token validation burden on session-manager for intra-cluster calls
- SPIRE entries scoped to pod selector per agent type — no over-broad workload identity
- Agent grants are least-privilege — only the scopes selected at agent type creation
- Workspace directories isolated per run — no cross-session filesystem access
- BYOC agents have no elevated permissions beyond standard SPIFFE attestation

**Reliability:**
- Runs survive `orbit-agents` restarts — state is in PostgreSQL
- Runs survive session-manager pod restarts only via the resume flow (not v1)
- Pod scale-down never terminates a pod with an active run
- Event log writes are durable — no event is emitted to SSE without being persisted

**Scalability:**
- StatefulSet HPA scales on CPU/memory — platform manages pod count transparently
- `orbit-agents` is stateless — scales horizontally; session routing state in PostgreSQL
- Event log read path supports concurrent observers on the same run

**Observability:**
- All run lifecycle transitions emitted as structured log events
- All agent LLM calls attributed in inference-gateway usage records
- `orbit-agents` exposes Prometheus metrics: active runs, active sessions, events written/sec, session routing latency
- BYOC agent discovery events logged on pod add/remove

---

## Dependencies

### Internal Dependencies

| Dependency | Status | Requirement |
|---|---|---|
| Platform JWT / Principal model ([unified-identity-model](../adr/2026-03-05-unified-identity-model.md)) | Implemented | Agent runs as AGENT principal; JWT governs LLM access |
| SPIFFE/SPIRE ([spiffe-spire-workload-attestation](../adr/2026-03-05-spiffe-spire-workload-attestation.md)) | Implemented | Workload attestation for agent pods |
| identity-proxy sidecar ([envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) | Implemented | SVID → JWT exchange; outbound token injection |
| inference-gateway ([inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md)) | Implemented | LLM access for agent workloads |
| Policy service / grants ([policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md)) | Implemented | Grant creation for agent tool scopes |
| Orbit Operator ([orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md)) | Implemented | Operator manages platform infrastructure; untouched by this feature |
| PostgreSQL | Implemented | Agent type registry, run/session state, event log |
| Istio / Envoy | Implemented | mTLS between orbit-agents and session-manager pods |
| Object storage (S3-compatible) | Planned | Workspace snapshot destination |

### External Dependencies

| Dependency | Purpose | Risk |
|---|---|---|
| Claude Code CLI | Primary managed agent framework | CLI behaviour in subprocess/interactive mode — needs validation spike |
| Codex CLI | Secondary managed agent framework | Same — needs validation spike |
| OpenCode | Tertiary managed agent framework | Same — needs validation spike |
| Kubernetes StatefulSet / HPA | Agent pod lifecycle and scaling | Standard k8s primitives |
| SPIRE registration API | Dynamic SPIRE entry management | SPIRE server must be reachable from orbit-system |

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| CLI frameworks don't support interactive stdin/subprocess mode cleanly | High | Medium | Spike each framework before committing the transform layer interface; framework-specific workarounds isolated to the transform layer |
| Event log grows unbounded for long-running agents | Medium | High | Event log retention policy (configurable); archive to object storage beyond threshold |
| StatefulSet scale-down terminates active session pods | High | Low | PodDisruptionBudget preventing eviction of pods with active runs; label-based pod lifecycle awareness |
| Cross-namespace mTLS routing misconfigured for new agent namespaces | Medium | Medium | Istio AuthorizationPolicy template validated as part of agent type provisioning; integration tested |
| Workspace snapshot to object storage fails on run completion | Medium | Medium | Retry with exponential backoff; run marked completed regardless of snapshot result; alert on snapshot failure |
| BYOC agent discovery creates excessive k8s API watch load at scale | Low | Low | Single watch per namespace; filtered by label selector; reconnect with backoff |
| orbit-agents becomes a single point of failure for all active sessions | High | Medium | Stateless design; PostgreSQL as state store; session routing survives restart; SSE client reconnects automatically |

---

## Alternatives Considered

### Alternative 1: Spin-Up-on-Demand (Pod per Session)

**Description:** Provision a new pod for each agent session; tear it down when the session ends.

**Why not chosen:**
- Every pod carries three containers: session-manager + identity-proxy + Envoy sidecar. SPIRE SVID acquisition and Envoy startup make cold starts prohibitive at session granularity
- SPIRE entry must be registered per pod — significant orchestration overhead
- Pod creation latency (image pull, scheduler, SPIRE attestation) is incompatible with the 5-second time-to-first-token target

### Alternative 2: One Session per Pod (Warm Pool)

**Description:** Pre-warm a pool of pods; assign one pod per session; recycle pods between sessions.

**Why not chosen:**
- Requires a custom pool controller to manage available/claimed/draining pod states
- Pod availability tracking adds operational complexity not covered by standard k8s primitives
- A session-manager that handles multiple sessions per pod achieves the same warm pod benefit without a custom controller
- HPA on resource usage is simpler and more standard than pool-size management

### Alternative 3: CRDs and Operator Extension for Agent Lifecycle

**Description:** Introduce `OrbitAgent` CRDs; extend the Orbit Operator to reconcile agent StatefulSets and SPIRE entries.

**Why not chosen:**
- The operator is designed for platform infrastructure reconciliation (SPIRE, Istio, charts) — not for runtime agent lifecycle
- Agent runs have complex state transitions unsuited to declarative reconciliation
- A dedicated service allows the run state machine, event log, and SSE relay to live in one place with appropriate tooling
- CRDs are deferred to a future version when GitOps-based agent type definition becomes a requirement

### Alternative 4: Deployment Instead of StatefulSet

**Description:** Use Kubernetes Deployments with HPA instead of StatefulSets.

**Why not chosen:**
- Deployment pods have random names and ephemeral IPs; routing to specific pods requires tracking pod IPs which change on reschedule
- StatefulSets provide stable, predictable DNS names per pod ordinal — session routing uses `{agent}-{n}.{agent}.{namespace}.svc` without IP tracking
- StatefulSet semantics (stable identity, ordered scaling) are a better fit for long-lived, identity-stable agent workloads

### Alternative 5: Intercepting the Inference-Gateway Stream

**Description:** Tap the LLM token stream at the inference-gateway or Envoy layer and relay it to the browser — avoiding stdout parsing.

**Why not chosen:**
- The LLM stream flows *into* the CLI framework, not out of it. Intercepting it gives raw tokens destined for the agent, not the agent's composed output
- CLI frameworks make multiple LLM calls per user message — an intercepted single call is a fragment, not a coherent response
- Raw token streams include internal scaffolding (tool call markup, system prompt re-injection, conversation history) that should not be visible to users
- Reading the CLI's stdout via the transform layer gives the composed, user-facing output

---

## Open Questions

| # | Question | Status |
|---|---|---|
| 1 | Do Claude Code, Codex, and OpenCode support interactive subprocess mode (long-running process, stdin for user messages, structured stdout output)? | **Open — spike required** |
| 2 | What is the exact stdout format of each CLI in interactive mode, and does it include tool call events? | **Open — spike required** |
| 3 | What is the reconnection behaviour for SSE clients that drop mid-stream? Does the client need to send a `Last-Event-ID` or does `orbit-agents` always replay from start? | Open |
| 4 | What is the object storage interface for workspace snapshots? S3-compatible client? Platform-owned abstraction? | Open — deferred |
| 5 | What are the HPA thresholds for agent StatefulSets? CPU/memory targets need calibration per framework | Open — requires load testing |
| 6 | How does `orbit-agents` authenticate to the SPIRE registration gRPC API? Via its own SVID (SPIFFE x509 mTLS)? | Open |
| 7 | What pod labels should BYOC agents carry to appear in the portal? Platform-defined convention or operator annotation? | Open |

---

## Implementation Phases

### Phase 1: Agent Registry and Identity Foundation

**Goal:** Agent types can be created and deleted; infrastructure is provisioned correctly; agent pods start and receive platform identity.

- `orbit-agents` service scaffold (API server, database, config)
- Agent type CRUD API (`POST`, `GET`, `PATCH`, `DELETE /api/v1/agent-types`)
- StatefulSet + headless Service provisioning on agent type creation
- SPIRE entry registration on agent type creation
- Grant creation in policy-service on agent type creation
- All resource cleanup on agent type deletion (drain → teardown)
- Agent type listing in agent portal (frontend)

**Exit Criteria:** Create an agent type via the API, observe the StatefulSet running in the tenant namespace, confirm the pod receives a platform JWT, confirm grants exist in policy-service.

---

### Phase 2: Session Manager and Transform Layer

**Goal:** Session-manager process running in pods; transform layer implemented for Claude Code; basic session start/stop working end-to-end.

- session-manager binary (session multiplexing, subprocess lifecycle, worktree management)
- Transform layer interface definition
- Claude Code transform layer implementation
- Run and session state machine (pending → starting → active → completed/failed)
- Run and session API (`POST /runs`, `GET /runs`, `POST /runs/{id}/sessions`)
- Workspace provisioning per run
- Workspace snapshot to object storage on run completion

**Exit Criteria:** Start a run via the API, observe Claude Code subprocess starting, workspace created, run transitions to active state, run completes and workspace is snapshotted.

---

### Phase 3: Streaming and Context Retention

**Goal:** Real-time streaming to the browser; full event log persistence; reconnect replays history.

- Event log persistence (`run_events` table, written by session-manager)
- `orbit-agents` SSE endpoint (`GET /runs/{id}/events`)
- Postgres LISTEN/NOTIFY for live event tailing
- Event replay on connect (full log replay then live attach)
- User message relay (`POST /runs/{id}/messages` → session-manager → subprocess stdin)
- Frontend: chat UI with streaming output, tool call rendering, thinking display
- Frontend: headless observation view (same UI, chat input disabled)

**Exit Criteria:** Open chat UI, start a session, see Claude Code streaming output in real time, send a message and see the response stream. Close browser and reopen — full history replays then live stream resumes.

---

### Phase 4: BYOC Discovery and Portal

**Goal:** BYOC agents visible in portal; unified agent portal with usage stats and run history.

- k8s pod watch across `orbit-tenant-*` namespaces
- BYOC agent registration and de-registration on pod lifecycle events
- BYOC agents surfaced in agent portal with usage data from inference-gateway logs
- Agent portal: unified list (managed + BYOC), run history, usage stats
- Codex and OpenCode transform layers

**Exit Criteria:** Deploy a BYOC agent pod in a tenant namespace, observe it appear in the portal within 30 seconds, see usage stats populated from LLM calls. Create Codex and OpenCode agent types and start sessions.

---

## Timeline and Milestones

| Milestone | Target | Status |
|---|---|---|
| PRD Draft Complete | 2026-05-08 | Draft |
| PRD Approved | TBD | Pending |
| ADRs Written (0049–0057) | TBD | Pending |
| CLI framework spike (interactive mode validation) | TBD | Pending |
| Phase 1 Complete | TBD | Pending |
| Phase 2 Complete | TBD | Pending |
| Phase 3 Complete | TBD | Pending |
| Phase 4 Complete | TBD | Pending |
| v1 Launch | TBD | Pending |

---

## Appendix

### Related Documents

- [unified-identity-model](../adr/2026-03-05-unified-identity-model.md) — Unified Principal model
- [agent-execution-flow-patterns-and-delegation](../adr/2026-03-05-agent-execution-flow-patterns-and-delegation.md) — Agent execution flow and delegation
- [spiffe-spire-workload-attestation](../adr/2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE workload identity
- [envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — identity-proxy sidecar
- [inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md) — Inference Gateway
- [orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md) — Orbit Operator
- [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md) — Policy and grant model

### Glossary

| Term | Definition |
|---|---|
| Managed Agent | An agent type defined via the Orbit UI/API and deployed and managed by the platform |
| BYOC Agent | Bring-Your-Own-Container — a tenant-built agent deployed in its own tenant namespace; platform provides identity and LLM access but does not manage lifecycle |
| Run | The unit of work — a server-side entity representing one agent execution; persists independently of browser sessions |
| Session | A browser connection to a run; transient; destroyed on disconnect |
| Session Manager | The Orbit-built main container in each agent pod; manages multiple concurrent sessions, worktrees, and subprocesses |
| Transform Layer | A per-framework adapter within the session-manager that wraps a CLI subprocess and normalises its output to the Orbit event format |
| Orbit Event Format | The standard structured event stream used by the platform: `text_delta`, `thinking_delta`, `tool_use`, `tool_result`, `message_stop` |
| Run Event Log | The persistent, append-only record of all events for a run — the source of truth for replay on reconnect and future resume |
| StatefulSet | Kubernetes workload type used for agent pods; provides stable, predictable pod DNS names |
| Worktree | An isolated filesystem directory provisioned per run within the pod's volume |
| Workspace Snapshot | A point-in-time archive of a run's worktree, persisted to object storage on run completion |
| SPIRE Entry | A SPIFFE identity registration for a workload, keyed to a pod selector; created per agent type |
| Platform JWT | The Orbit-issued bearer token that governs all service-to-service and agent-to-service calls |

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
