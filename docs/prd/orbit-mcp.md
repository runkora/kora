# Orbit MCP — Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-30  
**Last Updated:** 2026-06-30  
**Owner:** Justin Wilkin  
**Stakeholders:** Orbit Core Team, Platform Administrators, Agent Developers

---

## Executive Summary

The Orbit MCP is the first **platform-global MCP server** Orbit ships: a deployed, in-cluster MCP server that exposes Orbit's own management surface — agents, runs, policy, budgets, models, triggers, environments, identity — as tools. It lets an agent **operate the Orbit control plane** through MCP, turning ~50 dashboard operations spread across seven UI areas into a single governed toolset.

Its defining property is that it is **uniquely Orbit's to build safely.** The server itself makes no access decisions; it is governed by **two existing layers**. The gateway + OAuth + `mcp:orbit:<tool>:use` grants decide, per caller, which tools are even *visible and reachable* (a coarse pre-filter). The MCP then calls each Orbit API **carrying the caller's own identity** (passthrough), so the operation only *succeeds* if the caller's real platform scopes (`platform:grants:write`, `agents:types:write`, …) also allow it — the same enforcement the dashboard relies on today. Because both layers are external, the server can expose its *full* surface without self-censoring: the same MCP serves a read-only "platform analyst", an agent-write "fleet-manager", and an admin — each bounded by its grants at both layers. This closes Orbit's own loop: Orbit governs what agents can do, and now an agent can govern Orbit, governed by the very same machinery.

The MCP is **not a dual-persona feature.** It is always called by an agent (an MCP client); whether a human is driving that agent interactively or it runs autonomously does not change the build, because policy scopes — not the caller's "type" — govern every tool.

MVP exposes the **full read + write surface** across the non-infrastructure domains, anchored on three workflows: *stand up and configure an agent end-to-end*, *audit who can access what*, and *find and fix overspend*. It is deployed in-cluster and registered as a global MCPServer behind the gateway.

---

## Problem Statement

### Current State

Orbit's management surface is large — a dozen-plus domains and 50+ operations — and entirely **dashboard-and-API-bound**. Every operation exists as an HTTP API and a dashboard control, but there is no way for an agent to *operate the platform*. The platform that exists to run agents in production cannot itself be run by one.

### Pain Points

- **Cross-domain operations are tedious.** Standing up one agent touches model selection, environment/egress, connectors, a grant, and a budget — across four or five separate dashboard sections. A human clicks through all of them; nothing can do it in one step.
- **No conversational operation.** An operator who wants "show me agents over budget and cap them," or "who can access inference?", has to assemble the answer by hand from several pages.
- **Agents can't manage agents.** There is no governed way to let an agent provision, configure, or audit other agents — even though Orbit's whole identity/policy model is built to make exactly that kind of delegation safe.
- **The gateway has no first-party server to govern.** The accepted MCP/tool gateway needs a real global MCP server to front, exercise, and prove end-to-end.

### Business Impact

- **The control plane should be agent-operable.** Orbit's thesis is that agents do real work in production under governance. The most on-brand demonstration — and a genuinely useful capability — is an agent that operates Orbit itself, within exactly the authority it was granted.
- **Differentiated by governance.** Anyone can wrap APIs in an MCP. Only Orbit can expose its *full* management surface to agents *safely*, because it owns the per-principal access layer. The Orbit MCP is where that advantage becomes a product.

---

## Goals and Objectives

### Primary Goals

1. **Make the Orbit control plane agent-operable.** Expose Orbit's management operations as MCP tools an agent can call.
2. **Full surface, governed externally.** Expose read + write across the non-infrastructure domains; rely on the gateway + OAuth + `mcp:orbit:<tool>:use` grants to decide who can call what. The MCP never makes its own access decisions.
3. **Nail the high-value workflows.** Stand up/configure agents, audit access, and cost/budget ops should each be achievable conversationally in one session.
4. **Be the canonical first global MCP server.** Deployed in-cluster, registered as a global MCPServer, fronted by the gateway — the dogfood that exercises registration → introspection → scope-registration → PEP → proxy.

### Success Metrics

- An operator (human-driven or autonomous agent), granted the matching `mcp:orbit` tool scopes *and* the underlying platform scopes (`agents:types:write`, `platform:grants:write`, …) bundled as a role, can stand up a fully-configured agent (model + connectivity + grant + budget) in one conversation.
- An agent granted only read scopes can answer "who can access inference and what are they spending?" and can perform **no** mutation — gated at both layers (gateway pre-filter + downstream enforcement), with no authorisation logic in the MCP.
- Registering the Orbit MCP as a global server surfaces its tools as `mcp:orbit:<domain>:<op>:use` scopes that admins grant (and roll up by domain) like any other.

---

## Non-Goals (MVP)

- **Not persona-aware.** The MCP does not distinguish "human" vs "agent" callers; it is one toolset governed by scopes.
- **No access decisions in the MCP.** Authorisation, tool filtering, and who-sees-what are the gateway's job, not the server's. The MCP exposes tools; the gateway governs them.
- **Event-spine-dependent capabilities deferred.** Run **event streaming / live triage** (which depends on the not-yet-architected event spine) is out of MVP. Basic run read (list/get/state/summary) is in; live event tails and event-driven diagnostics are deferred.
- **Infrastructure-driven domains are read-only or out.** Tenant onboarding/registration, connection *credential* storage (Kubernetes/GitOps-driven), and MCP server registration itself are not mutated through this MCP in MVP — at most surfaced read-only.
- **Not the gateway or its auth.** The gateway, the OAuth model, and registration are their own accepted ADRs; this PRD assumes them.
- **No new management capability.** The MCP wraps operations that already exist as APIs; it does not invent new platform features.

---

## User Stories

Stories are framed around the *granted capability*, not the caller — every caller is an agent governed by its granted `mcp:orbit` scopes (and the underlying platform scopes).

### Stand up & configure agents
- **US-001 — Create a configured agent end-to-end.** *As an operator granted agent + policy + budget tools, I want to create an agent with a model, connectivity, a grant, and a budget in one flow, so that I don't click across five dashboard sections.*
  - [ ] Tools exist to create/update an agent type, attach an environment/connectivity, create a grant, and set a budget.
  - [ ] A single conversation can chain them; each step reports what it did.
- **US-002 — Reconfigure and inspect an agent.** *As an operator, I want to view and update an agent's model, prompt, tools, connectivity, and budget, so that I can tune or deprecate it.*
  - [ ] Get/update tools cover model, prompt, tool/grant scopes, connectivity, and budget.

### Audit who can access what
- **US-003 — Answer "who can access X?"** *As a security-minded operator, I want to list grants, roles, PATs, and effective scopes and run a policy-explain, so that I can see and justify who can do what.*
  - [ ] Read tools cover grants, roles, scope vocabulary, PAT inventory + attribution, and policy explain/simulate.
- **US-004 — Adjust access.** *As an admin operator, I want to create/revoke grants and manage roles, so that I can onboard or off-board principals.*
  - [ ] Write tools cover create/delete grant and create/delete role (gated downstream by `platform:grants:write`/`platform:roles:write`; escalation caveat in Risks).

### Cost & budget ops
- **US-005 — Find overspend.** *As an operator, I want usage/analytics and per-agent/per-principal spend, so that I can find what's costing too much.*
  - [ ] Read tools cover usage analytics, the usage log, and per-agent/per-principal spend.
- **US-006 — Set limits.** *As an admin operator, I want to set grant/role/tenant budgets and per-run caps, so that I can stop runaway spend.*
  - [ ] Write tools cover grant/role/tenant budgets and per-run caps.

### General operation
- **US-007 — Observe the platform.** *As an operator with read scopes, I want to list/inspect agents, runs, models, triggers, and environments, so that I have a conversational window into platform state.*
  - [ ] Read tools span all operable domains; a read-only grant performs no mutation.
- **US-008 — Governed by scopes alone.** *As a platform owner, I want a caller to be able to do exactly — and only — what their `mcp:orbit` and platform grants allow, with the MCP enforcing nothing itself, so that one server safely serves read-only and admin callers alike.*

---

## Proposed Solution

### One governed toolset, deployed in-cluster

A new in-cluster service (an MCP server) that exposes Orbit's management operations as MCP tools and is **registered as a global MCPServer** behind the gateway. It speaks **Streamable HTTP** (the current MCP remote transport — streams over SSE when needed) — it is not a stdio shim and is never run locally per-session. Consumers reach it only through the gateway, which authenticates, filters `tools/list`, and checks `tools/call` against the caller's `mcp:orbit:<tool>:use` grants before proxying.

### Governance is external, and it is two layers — the MCP makes no access decisions

Authorisation is **not** the MCP's job, and — importantly — an `mcp:orbit:<tool>:use` grant is **not** the whole story. Two existing layers govern every call:

1. **Gateway (coarse pre-filter):** every tool maps to an `mcp:orbit:<domain>:<op>:use` scope registered by the gateway's introspection. The gateway filters `tools/list` and checks `tools/call` against the caller's `mcp:orbit:<…>:use` grants (a broader covering scope like `mcp:orbit:agents:use` rolls up the agent tools) — deciding which tools are visible and reachable at all.
2. **Downstream API (authoritative gate):** the MCP calls each Orbit API by **forwarding the caller's own platform token** (passthrough — the model the dashboard BFF already uses). The relevant service then enforces the *real* operation scope (`platform:grants:write`, `agents:types:write`, …) against the caller's grants. The MCP holds **no standing authority**: it can never do anything the caller couldn't do by calling the API directly.

The practical consequence — stated plainly because it shapes how operators grant access: **using a tool requires *both* grants.** `mcp:orbit:agents:create:use` (reach the tool) *and* `agents:types:write` (perform the operation). Operators bundle the two via **roles** (e.g. an "agent-fleet-manager" role carrying the `mcp:orbit:agents:use` rollup *and* the underlying `agents:types:write` platform scope); the PRD does not pretend an orbit-MCP grant alone confers capability.

### Tool surface — by domain (full read + write, non-infra)

| Domain | Observe | Control |
|---|---|---|
| **Agents** | list / get agent types, effective-access | create / update / delete agent type |
| **Runs** | list / get runs, state, summary, per-run spend | start run, send message, interrupt, terminate (live **event tails deferred**) |
| **Policy — grants** | list / get grants | create / update-budget / delete grant |
| **Policy — roles & vocab** | list roles, scope vocabulary, route→scope | create / delete role |
| **Policy — evaluate** | explain effective scopes, simulate check | — |
| **Budgets** | personal / per-principal / tenant status | set grant / role / tenant budgets, per-run caps |
| **PATs** | list PATs, attribution | create / update-scopes / revoke PAT |
| **Models & pricing** | list models / virtual models / provider models / pricing | create-virtual-model, enable/disable provider model, pricing overrides |
| **Triggers** | list sources / triggers / deliveries | create/update source & trigger, rotate secret |
| **Environments** | list / get | create / replace / delete |
| **Analytics & logs** | usage analytics, usage log, per-principal/model breakdowns | — |
| **Identity** | list users/profiles | — (provider-driven) |
| **Connections** _(read-only)_ | list adapters + tenant connections | — (credential storage is GitOps) |
| **MCP servers** _(read-only)_ | list registered MCP servers + their tools | — (registration is CR/GitOps) |

Tools are **one-per-operation** (not coarse workflow macros). Tool names map to a **domain-hierarchical** scope path — `mcp:orbit:<domain>:<op>:use` — so the platform's existing prefix coverage gives free domain-level rollups: `mcp:orbit:agents:use` grants every agent tool, `mcp:orbit:policy:grants:use` every grant tool, `mcp:orbit:use` the whole server. (Multi-segment scope paths are native to the policy model — exactly how `inference:<family>:<model>:use` works; the registration ADR needs only a one-line clarification that a tool may register at a multi-segment path. Read-only vs read-write is *not* a path level, so a "read-only analyst" grant is a **role** bundling the read scopes, not a single prefix.)

### Relationship to the gateway pillar

The Orbit MCP is the first concrete consumer of the accepted gateway work: registering it as a global `MCPServer` CR drives introspection of its `tools/list`, registration of its `mcp:orbit:<domain>:<op>:use` scopes, and per-tool enforcement — making it the end-to-end test vehicle as well as a product capability.

---

## Scope

### In Scope (MVP)

- An in-cluster MCP server (**Streamable HTTP** transport) exposing the full read + write surface across the operable domains above.
- **Read-only** surfacing of two infra-driven domains: the connections catalog and the MCP server registry.
- One tool per operation; domain-hierarchical scope-path naming (`mcp:orbit:<domain>:<op>:use`).
- Registration as a global MCPServer fronted by the gateway; access governed externally (gateway pre-filter + downstream passthrough).
- The three anchor workflows (agent stand-up, access audit, cost/budget ops) achievable end-to-end.

### Out of Scope (MVP)

- Run **event streaming / live triage** (depends on the not-yet-architected event spine).
- **Mutating** infra-driven domains — tenant onboarding/config, connection *credential* storage, and MCP server registration stay GitOps/CR-driven (connections + MCP registry are *read-only* here; tenant config is out entirely).
- Any authorisation/filtering logic inside the MCP (the two governance layers own it).
- An **anti-escalation rule** on grant/role/PAT creation — a pre-existing platform-level gap (see Risks); its fix is a separate future ADR, not this change.
- Persona-specific behaviour.
- New platform capabilities beyond existing APIs.

---

## Requirements

### Functional

**P0 (MVP)**
- FR-1 Expose read + write tools across agents, runs (non-event), policy (grants/roles/vocab/evaluate), budgets, PATs, models/pricing, triggers, environments, analytics/logs, identity (read), and read-only connections catalog + MCP server registry.
- FR-2 One tool per operation; domain-hierarchical names mapping to `mcp:orbit:<domain>:<op>:use` (multi-segment, charset-safe `[a-z0-9_-]` per segment).
- FR-3 Each tool calls the relevant Orbit API by **forwarding the caller's platform token** (passthrough); the downstream service is the authoritative scope/budget gate.
- FR-4 The server applies no authorisation of its own — it holds no standing authority and can do nothing the caller couldn't do directly.
- FR-5 Deployable in-cluster, registrable as a global MCPServer, frontable by the gateway (Streamable HTTP).
- FR-6 Destructive operations (delete agent/grant/role/environment, terminate run) are clearly marked in tool descriptions; there is no dry-run/confirm in MVP (rely on grants).

**P1 (Fast-follow)**
- FR-7 Run event streaming / live triage (once the event spine exists).
- FR-8 Read-only surfacing of tenant config; mutation of infra-driven domains (pending their own designs).

### Non-Functional

- **Security:** the server holds no standing authority; every operation is gated twice — the gateway's `mcp:orbit:<tool>:use` pre-filter and the downstream service enforcing the caller's real platform scope. No tool can exceed the caller's grants. (Caveat: the platform lacks an anti-escalation rule on grant/PAT creation — see Risks.)
- **Recursion safety:** the MCP exposes start-run / send-message; an agent could start a run that calls this MCP that starts a run. The ADR must define a depth/budget guardrail; until then, run-start tools rely on the caller's budget grants as the backstop.
- **Observability:** every tool call traced and logged (principal, tool, outcome), consistent with platform OTel discipline.
- **Compatibility:** speaks standard MCP (Streamable HTTP) so the gateway (and, in dev, a direct MCP client) can consume it unmodified.

---

## Dependencies

| Dependency | Status | Note |
|---|---|---|
| MCP / Tool Gateway (PEP, proxy) | Accepted, not built | Fronts and governs this server |
| MCP Server Registration & Introspection | Accepted, not built | Registers it as a global server; turns its tools into `mcp:orbit:<domain>:<op>:use` scopes |
| MCP OAuth Authentication | Accepted, not built | How human-driven callers authenticate to the gateway |
| Existing platform APIs (agents, policy, identity, inference, …) | Implemented | The operations the tools wrap |
| Identity / platform tokens | Implemented | Carry the caller's identity to downstream APIs |

> The gateway pillar is accepted but unbuilt. The Orbit MCP can be **built and exercised standalone** (direct MCP client + a platform token) before the gateway lands, then registered behind it.

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| **Privilege escalation via grant/role/PAT/budget write** | **High** | The platform has **no anti-escalation rule** today: a caller with `platform:grants:write` can grant *any* scope to *any* principal — including escalating itself (verified: `CreateGrant` validates only that scopes exist in the vocabulary). The Orbit MCP makes this **agent-callable**, which can break the "an agent can never exceed its delegator" invariant. **This is a pre-existing platform-level gap, not introduced here; its fix (an anti-escalation rule) is a separate future ADR.** Interim mitigation: under passthrough a caller already needs `platform:grants:write` to reach these tools, so exposure is bounded to principals an admin has already trusted with that scope — admins must grant the access-control *write* scopes deliberately. |
| Broad write surface exposed ungoverned | High | The server is only reachable through the gateway in production; access is dual-gated (gateway pre-filter + downstream platform scope). Standalone/dev use still requires a real platform token whose scopes the downstream enforces. |
| Dual-grant drift (gateway scope vs platform scope out of sync) | Medium | Bundle the `mcp:orbit:<domain>:<op>:use` tool scope and the underlying `platform:…:write` scope into a **role**; document that operators grant both. |
| Tool sprawl (~50 tools) is hard to navigate | Medium | One-per-operation + domain-hierarchical naming; the gateway filters to the caller's granted subset; domain rollup scopes (`mcp:orbit:<domain>:use`) make grants manageable. |
| Recursion (start-run tool can re-enter the MCP) | Medium | Depth/budget guardrail defined in the ADR; caller budget grants are the backstop. |

---

## Open Questions

Most of this PRD's questions resolved during review:

1. **Caller-identity propagation** — _Resolved:_ **passthrough** (forward the caller's token; downstream is the authoritative gate). Matches the BFF and keeps standalone testing safe.
2. **Transport** — _Resolved:_ **Streamable HTTP** (aligns with the gateway; SSE is the deprecated transport).
3. **Binary/package placement** — _Resolved:_ new standalone `cmd/orbit-mcp` + `internal/` package, mirroring existing service binaries.
4. **Tool naming** — _Resolved:_ domain-hierarchical `mcp:orbit:<domain>:<op>:use` with `:` path segments. _ADR follow-up:_ a one-line clarification to the registration ADR that a tool may register at a multi-segment scope path.
5. **Read-only infra domains** — _Resolved:_ connections catalog + MCP server registry (read-only); tenant config out.

Remaining for the ADR / future:
6. **Anti-escalation rule** — the grant/role/PAT creation escalation gap (Risks) is a **platform-level concern needing its own future ADR**; this change does not address it.
7. **Recursion guardrail** — depth/budget limit for start-run tools re-entering the MCP. _ADR._

---

## Appendix — Related Documents

- [MCP / Tool Gateway PRD](mcp-tool-gateway.md) — the governing pillar this server is the first consumer of.
- [MCP / Tool Gateway ADR](../adr/2026-06-29-mcp-tool-gateway.md) — PEP + proxy that fronts this server.
- [MCP Server Registration & Introspection ADR](../adr/2026-06-29-mcp-server-registration-and-introspection.md) — registers it as a global server.
- [MCP OAuth Authentication ADR](../adr/2026-06-29-mcp-oauth-authentication.md) — human-driven caller auth.
