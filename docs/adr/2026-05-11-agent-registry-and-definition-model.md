# Agent Registry & Definition Model

**Status:** Accepted (Amended 2026-09-29)
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** Users need to define managed agent types via the Orbit UI and API. This ADR establishes the data model, API surface, and how an agent type definition drives the creation of platform resources (grants, SPIRE entries, k8s workloads).

---

## Context

### Background

The Orbit platform already supports agents as first-class principals ([unified-identity-model](2026-03-05-unified-identity-model.md)). Identity, policy, and inference attribution are all agent-aware. What is missing is a control plane for defining agent types — the self-service surface that allows tenant users to create agents without manually configuring SPIRE, Kubernetes, or policy grants.

### Problem Statement

There is no data model or API for agent types. Every agent that exists today requires manual configuration of k8s resources, SPIRE entries, and policy grants by a platform operator. This is not scalable and prevents tenant self-service.

### Goals

- Define the data model for a managed agent type
- Define the API surface for agent type CRUD
- Specify how an agent type definition drives downstream resource creation (grants, SPIRE entry, StatefulSet)
- Keep the user-facing definition simple — no k8s or SPIRE knowledge required

### Non-Goals

- k8s resource provisioning mechanics ([agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md))
- SPIRE entry lifecycle ([agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md))
- Run and session management ([run-and-session-lifecycle](2026-05-11-run-and-session-lifecycle.md))
- BYOC agent registration ([byoc-agent-discovery-and-observability](2026-05-11-byoc-agent-discovery-and-observability.md))

---

## Decision Drivers

* Tenant users must be able to create agent types without platform engineering involvement
* The definition must be simple — no infrastructure concepts exposed to users
* Agent LLM access must be governed by the existing grant and policy system — no new auth surface
* The definition must be extensible (triggers, resource limits) without breaking changes

---

## Considered Options

### Option A: Database-backed registry with API and UI

**Description:** Agent types are records in the `orbit-agents` PostgreSQL database. Users create them via REST API or UI. The `orbit-agents` service translates definitions into k8s and SPIRE resources imperatively.

**Pros:**
- Simple user-facing model — no k8s concepts exposed
- API-driven creation enables UI self-service
- Definition stored in one place with full CRUD semantics
- Extensible schema (add columns for triggers, resource limits without breaking)

**Cons:**
- Not GitOps-native — agent types cannot be version-controlled as YAML
- Requires `orbit-agents` to be running for lifecycle operations

### Option B: OrbitAgent CRD (GitOps-native)

**Description:** Agent types are defined as Kubernetes CRDs. Users apply YAML; the operator reconciles resources.

**Pros:**
- GitOps-native — agent type definitions version-controlled alongside infrastructure
- Operator handles reconciliation automatically

**Cons:**
- Exposes k8s concepts to tenant users — not appropriate for self-service UI
- CRD schema changes require careful migration
- Operator reconcile loop is not suited to run-level state management
- API for the UI would still need to be built on top of the CRD

### Option C: Thin configuration only (name + image)

**Description:** Agent type definition is minimal — just a name and container image. Platform provides identity and LLM access but nothing else.

**Pros:**
- Maximum flexibility — tenants control everything inside the container

**Cons:**
- Does not support managed frameworks (Claude Code, Codex) — no system prompt, no framework selection
- Grants cannot be derived from the definition — manual policy configuration required
- Not compatible with the session-manager model

---

## Decision

**Chosen Option: A — Database-backed registry with API and UI**

### Agent Type Data Model

```
agent_types
├── id                uuid, primary key
├── tenant_id         text, non-nullable — scopes to tenant
├── name              text, non-nullable
├── description       text
├── framework         text, non-nullable — enum: claude-code | codex | opencode
├── system_prompt     text — instructions passed to the framework at session start
├── tool_scopes       text[] — scopes granted; drives grant creation in policy-service
├── state             text — enum: provisioning | active | deleting | error
├── created_at        timestamptz
├── updated_at        timestamptz
└── created_by        text — principal_id of the creating user
```

### API Surface

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/agent-types` | Create agent type; triggers async provisioning |
| GET | `/api/v1/agent-types` | List agent types for the authenticated tenant |
| GET | `/api/v1/agent-types/{id}` | Get agent type by ID |
| PATCH | `/api/v1/agent-types/{id}` | Update system prompt or description (framework and scopes require re-provisioning) |
| DELETE | `/api/v1/agent-types/{id}` | Delete agent type; triggers async teardown |

### Provisioning Flow (on Create)

1. Write agent type record with `state: provisioning`
2. Create grants in policy-service for each scope in `tool_scopes`
3. Register SPIRE entry ([agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md))
4. Provision StatefulSet and headless Service ([agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md))
5. Update state to `active`

Provisioning is asynchronous. The API returns `202 Accepted` with the agent type ID. Clients poll state or subscribe to events.

### Teardown Flow (on Delete)

1. Mark agent type `state: deleting`
2. Drain active runs (no new sessions accepted; wait for active runs to complete or timeout)
3. Remove StatefulSet and headless Service
4. Remove SPIRE entry
5. Remove grants from policy-service
6. Delete agent type record

### Rationale

Option A keeps the user-facing surface clean while giving the platform full control over the resulting infrastructure. The database is the single source of truth for agent type definitions; downstream k8s and SPIRE resources are derived from it. The extensible schema means triggers, resource limits, and other future fields can be added without changing the model's shape.

---

## Consequences

### Positive

- Tenant self-service: create agent types via UI without platform engineering involvement
- Grants derived automatically from `tool_scopes` — no manual policy configuration
- Definition stored in Postgres alongside all other platform data — standard query and audit patterns

### Negative

- Not GitOps-native in v1 — agent type definitions cannot be applied via `kubectl` or ArgoCD
- `orbit-agents` must be healthy for provisioning and teardown — no automatic reconciliation from stored state if the service is down during provisioning

### Neutral

- Agent type `state` field introduces async provisioning semantics — clients must poll or subscribe

---

## Implementation

### Migration

New table `agent_types` in the `orbit-agents` service database. No migration to existing tables required.

### Scope Registration

`tool_scopes` values must be valid scope vocabulary entries from the policy-service scope registry ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)). Validation at creation time returns `422 Unprocessable Entity` for unknown scopes.

---

## Validation

### Success Metrics

- Agent type created via API results in active StatefulSet within 60 seconds
- Agent type deleted via API results in full resource cleanup within 120 seconds
- `tool_scopes` correctly reflected as grants in policy-service for the agent principal

---

## Amendment 1 (2026-09-29): Framework tools are not agent configuration

**Status:** Accepted

Touches *What can it access?* The change removes a setting that never reached policy. Identity (*Who is this agent?*) and credential brokering (*How does it connect?*) are unchanged.

**Context.** `tool_scopes` selected which Claude Code built-in tools a run received (for example `shell` enabled `Bash`, `agents` enabled `Agent` and `SendMessage`). The Decision above says it drives grant creation; it never did. `EffectiveGrantScopes` compiles the grant from `grant_scopes`, connectivity, connectors and `mcp_tools` only, and no policy bundle, delegation intersection or tool-gateway check reads `tool_scopes`. The `mcp` value enabled Claude Code's local `ListMcpResourcesTool` and `ReadMcpResourceTool`; governed MCP access is `mcp_tools` (`mcp:<server>:<tool>:use`) and is unaffected. The setting applied only to Claude Code: the Codex and OpenCode runners never read it, so the same agent type had different tools per framework. Only `Bash` runs in the agent's sandbox, under its egress policy. The other built-ins (`Read`, `Write`, `Edit`, `WebFetch` and the rest) run in the session-manager container, which is not under the sandbox's egress policy, so any built-in that does network I/O must be on the exclusion list.

**Decision.**

- `tool_scopes` is removed from the agent type: the model, the create and update requests, the Postgres queries, `GET /api/v1/agent-types/tool-scopes`, the `toolscopes` package, the `agents.tool_scopes` Orbit MCP tool and the field in the `agents.create` and `agents.update` schemas, the builder's platform-tools picker, and the configuration and overview rows.
- Migration `20260929054647_drop_agent_type_tool_scopes` drops the column.
- Every run gets all of its framework's built-in tools. Claude Code runs get the full set less a fixed platform exclusion list, passed as `--disallowedTools` and defined once as `claudeExcludedTools` in `backend/internal/agents/sessionmanager/runner_claude.go`. The list is platform policy and is not configurable per agent type:

| Tool | Excluded because |
|---|---|
| `WebSearch` | An Anthropic server-side tool, not served through the inference gateway |
| `WebFetch` | It fetches from the session-manager container, not the sandbox, so its requests bypass the agent's egress policy |
| `AskUserQuestion` | Auto-rejected in headless stream-json mode: no one can answer it |
| `PushNotification` | It calls Anthropic's hosted notification service from the session-manager container, outside the agent's egress policy |
| `DesignSync` | It calls Claude Design from the session-manager container, outside the agent's egress policy |

- Codex and OpenCode runs are unchanged: neither runner restricted tools.
- `api.DecodeJSON` does not reject unknown fields, so a REST client that still sends `tool_scopes` on create or update has it ignored. The Orbit MCP `agents.create` and `agents.update` schemas reject unknown properties, so an MCP caller that still passes `tool_scopes` gets a validation error. Responses no longer carry the field. `GET /api/v1/agent-types/tool-scopes` now matches `GET /api/v1/agent-types/{id}` and returns 404.
- orbit-agents keeps sending `allowed_tools` to the session-manager with every scope the removed picker offered. Agent pods keep their session-manager image across orbit-agents upgrades, and an older session-manager builds its Claude Code tool list from this field; newer ones ignore it. The field is removed once no agent pod runs a session-manager that reads it.

**Consequences.**

- The drop ships in the same release as the code removal. This departs from the contract rule in [database-migrations](2026-09-10-database-migrations.md) (Decision 5) for this column: binaries from the previous release name `tool_scopes` in every agent type query, so they fail against the new schema during a rolling upgrade and after a `helm rollback`.
- Operators can no longer narrow an agent's built-in tools. Network access from the sandbox is narrowed through egress, connectors and MCP tool grants, enforced outside the agent; built-ins that run in the session-manager container are held back only by the exclusion list.
- A new Claude Code built-in tool reaches every agent on the next session-manager image, with no configuration change. The exclusion list is the control for a future built-in that makes network calls from the session-manager container or cannot work headless: it must be added there.
- Claude Code's `--bare` mode caps the built-in set at `Bash`, `Read` and `Edit` whatever `--tools` names (Claude Code 2.1.284). The session-manager runs with `claude.bare: false`; enabling bare mode would cut every run to those three tools.

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — AGENT principal type
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Grant model
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Scope vocabulary
- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — StatefulSet provisioning driven by this model
- [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — SPIRE entry and grant lifecycle

---

**Last Updated:** 2026-09-29
