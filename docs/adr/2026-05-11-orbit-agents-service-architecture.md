# orbit-agents Service Architecture

**Status:** Accepted
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** The agent control plane requires a new backend service to own agent type registration, run lifecycle, session routing, event streaming, k8s resource management, and BYOC discovery. This ADR establishes that service's architecture and scope.

---

## Context

### Background

The existing platform services each have narrow, well-defined scopes: identity issues tokens, inference-gateway routes LLM calls, policy-service manages grants. No existing service is appropriate to own the agent control plane's concerns — agent lifecycle, run state, session routing, and event streaming are runtime control plane concerns, not infrastructure or policy concerns.

### Problem Statement

The agent control plane needs a home. The question is whether to extend an existing service (the operator), introduce a CRD-driven approach, or create a new dedicated service. The decision affects operational complexity, deployment independence, and how cleanly concerns are separated.

### Goals

- Establish a single service that owns the full agent control plane lifecycle
- Keep k8s and SPIRE management internal — not exposed to callers
- Allow the service to scale independently of other platform services
- Follow existing platform conventions (controller-based HTTP, pgx, YAML config, structured logging, OTel)

### Non-Goals

- Implementing MCP tool access (deferred)
- Implementing delegation/act-on-behalf (deferred)
- Building a CRD-based GitOps surface (deferred to v2)

---

## Decision Drivers

* The operator manages platform infrastructure (SPIRE, Istio, Helm charts). Agent runtime lifecycle — run state machines, session routing, event streaming — is a different concern and should not bloat the operator.
* Run state transitions are frequent and event-driven — not well suited to the declarative reconciliation model of a k8s controller.
* The service must scale independently to handle concurrent SSE connections and session routing load.
* SPIRE entry management is the main argument for operator involvement — but SPIRE provides a registration gRPC API that any authenticated service can call directly.

---

## Considered Options

### Option A: Extend the Orbit Operator with OrbitAgent CRDs

**Description:** Add `OrbitAgent` and `OrbitAgentRun` CRDs to the existing operator. The operator controller manages StatefulSets, SPIRE entries, and run state.

**Pros:**
- Consistent with existing GitOps model
- Operator already manages SPIRE — single owner of SPIRE management

**Cons:**
- Operator reconcile loop is not designed for run-level state machines (frequent transitions, event-driven)
- Bloats the operator's mission — it becomes responsible for platform infrastructure AND runtime agent lifecycle
- Operator errors in run management could affect SPIRE/Istio reconciliation (shared blast radius)
- CRD schema must be stable before the data model is proven

### Option B: New service + OrbitAgent CRD for k8s reconciliation

**Description:** `orbit-agents` service owns business logic. A lightweight `OrbitAgent` CRD is created for k8s reconciliation. The operator watches CRDs and manages StatefulSets/SPIRE; the service manages everything else.

**Pros:**
- Clean separation: service owns runtime, operator owns k8s reconciliation
- Automatic drift recovery — operator reconciles if StatefulSet is deleted externally

**Cons:**
- More moving parts — two components must stay in sync
- CRD introduces schema stability requirements before the data model is proven
- The additional reconciliation benefit is marginal for a small set of static resources (one StatefulSet per agent type)

### Option C: New service, fully imperative k8s management, no CRDs

**Description:** `orbit-agents` service owns everything — agent type registry, run lifecycle, k8s resource management (via k8s API), SPIRE entry registration (via SPIRE registration API), and BYOC discovery. No CRDs. No operator extension.

**Pros:**
- Single service, single source of truth
- No CRD schema stability requirement — data model can evolve freely
- Service has direct control over all resources it creates
- Lightweight drift detection via k8s resource watch inside the service
- SPIRE registration API is a standard gRPC API — no operator intermediary needed

**Cons:**
- Not GitOps-native — agent type definitions cannot be applied via `kubectl`
- Requires the service to hold k8s RBAC permissions to create StatefulSets, Services, etc. in tenant namespaces
- No automatic reconciliation if `orbit-agents` is down when a resource is deleted externally

---

## Decision

**Chosen Option: C — New service, fully imperative k8s management, no CRDs**

CRDs are deferred to v2 when GitOps management of agent types becomes a stated requirement. The data model must be proven in v1 before being locked into a CRD schema.

### Service Scope

`orbit-agents` owns:

| Responsibility | Mechanism |
|---|---|
| Agent type registry | PostgreSQL — `agent_types` table |
| Run and session lifecycle | PostgreSQL — `runs`, `sessions` tables; state machine in service |
| Session routing | `run_id → pod_ordinal` in DB; routes via StatefulSet stable DNS |
| SSE relay | Postgres LISTEN/NOTIFY for live events; event log replay on connect |
| Grant management | Calls policy-service HTTP API on agent type create/delete |
| k8s resource management | k8s API (StatefulSet, Service, HPA, PDB, ServiceAccount, AuthorizationPolicy) |
| Agent SPIFFE label | Sets `spiffe.io/agent={agent_type_id}` on StatefulSet pod template; ClusterSPIFFEID handles SVID issuance |
| BYOC discovery | k8s pod watch across `orbit-tenant-*` namespaces |
| Drift detection | k8s resource watch; reconciles missing resources on detection |

### What orbit-agents Does NOT Own

- SPIRE server or agent (managed by operator)
- Istio/Envoy configuration (managed by operator)
- Identity issuance (identity-service)
- Policy evaluation (policy-gateway)
- LLM routing (inference-gateway)
- Grant schema or OPA bundles (policy-service)

### SPIRE Authentication

`orbit-agents` runs in `orbit-system` with its own SPIFFE SVID: `spiffe://{trust-domain}/system/orbit-agents`, issued via the `orbit-system-component` ClusterSPIFFEID (pod label `spiffe.io/component=orbit-agents`). Istio mTLS uses this SVID for all inter-service calls. `orbit-agents` does not call the SPIRE registration gRPC API — agent identity is provisioned via pod labels on the StatefulSet ([agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md)).

### k8s RBAC

`orbit-agents` service account is granted:
- `create`, `update`, `delete`, `watch` on `StatefulSet`, `Service`, `HPA`, `PodDisruptionBudget`, `ServiceAccount` in `orbit-tenant-*` namespaces
- `create`, `delete` on `AuthorizationPolicy` in `orbit-tenant-*` namespaces
- `list`, `watch` on `Pod` in `orbit-tenant-*` namespaces (for BYOC discovery and session-state label queries)
- `patch` on `Pod` is not required — session-manager patches its own pod label via its own service account

### Pod Selection Algorithm

When a new run is created (`POST /api/v1/runs`), `orbit-agents` must select a pod ordinal to assign it to. The selection algorithm:

1. Query the `runs` table for active run counts per pod ordinal for the given agent type: `SELECT pod_ordinal, COUNT(*) FROM runs WHERE agent_type_id = ? AND state IN ('starting','active','completing') GROUP BY pod_ordinal`
2. Fetch the current StatefulSet replica count from k8s (or from the `agent_types` table's last-known replica count, refreshed by drift detection)
3. Select the pod ordinal with the fewest active runs (round-robin among tied ordinals)
4. If all pods are at the configured `max_concurrent_runs_per_pod` limit (stored on the agent type), reject with `429 Too Many Requests`

The selected `pod_ordinal` is written atomically to `runs.pod_ordinal` when the run transitions `pending → starting`. Routing thereafter uses the stable StatefulSet DNS: `{agent-type-id}-{pod_ordinal}.{agent-type-id}.orbit-tenant-{tenant-id}.svc.cluster.local`.

### Session-Manager Internal API

`orbit-agents` communicates with the session-manager running inside each agent pod via an internal HTTP API. These are intra-cluster calls protected by Istio mTLS — no platform JWT is required.

| Method | Path | Caller | Description |
|---|---|---|---|
| POST | `/session` | orbit-agents | Start a Claude Code subprocess for the given run |
| POST | `/session/{run_id}/messages` | orbit-agents | Relay user message to the CLI subprocess |
| POST | `/session/{run_id}/tool-results` | orbit-agents | Inject a tool result for a pending `AskFollowupQuestion` |
| DELETE | `/session/{run_id}` | orbit-agents | Terminate the CLI subprocess and clean up the session |
| GET | `/health` | orbit-agents | Session-manager liveness check |
| POST | `/internal/session/{run_id}/provision-sandbox` | orbit-runtime-mcp | Request sandbox pod provisioning from inside Claude |

The session-manager calls back to orbit-agents to signal state changes:

| Method | Path | Caller | Description |
|---|---|---|---|
| POST | `/internal/runs/{run_id}/state` | session-manager | Signal run state transition (e.g. `active`, `completing`, `failed`) |
| POST | `/internal/runs/{run_id}/events` | session-manager | Push a durable or ephemeral event for SSE fan-out |
| POST | `/internal/runs/{run_id}/provision-sandbox` | session-manager | Request K8s sandbox pod provisioning |

These internal endpoints are not part of the public API surface and are not authenticated with platform JWTs. Istio `AuthorizationPolicy` restricts `/internal/*` to traffic from session-manager service accounts only, and `/session/*` and `/internal/session/*` to traffic from `orbit-agents`' service account only.

### Conventions

Follows all existing platform conventions:
- Controller-based HTTP routing (`api.Controller` interface)
- `pgx/v5` for all database access
- YAML config via `config.Load[T]()`
- `*logging.Logger` injected into all components
- OTel traces and structured logs on all operations
- Typed HTTP clients with trace propagation for calls to policy-service and identity-service

### API Surface

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/agent-types` | Create agent type |
| GET | `/api/v1/agent-types` | List agent types (tenant-scoped) |
| GET | `/api/v1/agent-types/{id}` | Get agent type |
| PATCH | `/api/v1/agent-types/{id}` | Update agent type |
| DELETE | `/api/v1/agent-types/{id}` | Delete agent type |
| POST | `/api/v1/runs` | Start a new run |
| GET | `/api/v1/runs` | List runs (tenant-scoped, filterable by agent type) |
| GET | `/api/v1/runs/{id}` | Get run state |
| DELETE | `/api/v1/runs/{id}` | Terminate a run |
| GET | `/api/v1/runs/{id}/events` | SSE stream — event replay + live tail |
| POST | `/api/v1/runs/{id}/messages` | Send user message to active run |
| GET | `/api/v1/agents` | List all agents (managed + BYOC) for portal |

---

## Consequences

### Positive

- Clean service boundary: `orbit-agents` owns the full control plane; other services are untouched
- Data model can evolve without CRD schema constraints
- Independent deployability and scalability
- Drift detection via internal watch loop provides basic reconciliation safety

### Negative

- Not GitOps-native in v1 — agent type definitions cannot be version-controlled as k8s manifests
- Broad k8s RBAC permissions across tenant namespaces — scope must be documented and audited
- If `orbit-agents` is unavailable, new agent type provisioning and teardown are blocked (runs in progress are unaffected — they run in pods)

### Neutral

- CRDs deferred to v2 — if GitOps becomes a requirement, the service's imperative management layer becomes a CRD reconciler with moderate refactoring

---

## Implementation

### Service Layout

Follows `backend/internal/{feature}` package structure:

```
backend/internal/agents/
├── models.go          — AgentType, Run, Session, RunEvent domain types
├── repository.go      — Repository interface
├── postgres.go        — pgx implementation
├── service.go         — Business logic, lifecycle orchestration
├── handlers.go        — HTTP controllers
├── k8s.go             — k8s resource management (StatefulSet, Service, HPA, etc.)
├── routing.go         — Pod selection and session routing (run → pod ordinal → StatefulSet DNS)
├── streaming.go       — SSE relay, Postgres LISTEN/NOTIFY, event replay
└── discovery.go       — BYOC pod watch and registration
```

---

## Validation

### Success Metrics

- Agent type created and all k8s resources provisioned within 60 seconds
- SPIRE entry registered and pod receives SVID within 30 seconds of StatefulSet pod reaching Running
- `orbit-agents` restart does not affect in-progress runs
- Drift detection reconciles a deleted StatefulSet within 60 seconds

---

## Related Decisions

- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — Operator scope (explicitly not extended)
- [agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md) — Agent type data model
- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — k8s resources this service provisions
- [run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md) — Run/session state machine
- [agent-event-streaming-protocol](2026-05-11-agent-event-streaming-protocol.md) — SSE relay and event log
- [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — SPIRE and grant lifecycle

---

**Last Updated:** 2026-05-08
