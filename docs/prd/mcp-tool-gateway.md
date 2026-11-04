# MCP / Tool Gateway — Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-29  
**Last Updated:** 2026-06-30  
**Owner:** Justin Wilkin  
**Stakeholders:** kora Core Team, Platform Administrators, Agent Developers, Human Tool Consumers (IDE / desktop MCP client users)

---

## Executive Summary

The MCP / Tool Gateway is kora's governed front door to MCP (Model Context Protocol) tool servers. It is a single endpoint that both **humans** (via desktop/IDE MCP clients such as Claude Desktop, Cursor, or VS Code) and **agents** (via their injected platform token) connect to in order to reach MCP tools. Every `tools/list` is filtered to what the caller is allowed to see, and every `tools/call` is policy-checked *before* it executes — down to the individual tool.

Its defining move is to lift the **second tier of enforcement out of the MCP server and into the platform**. Today, per-tool authorisation is only possible if you own and modify the MCP server's code ([scope-gated-tool-exposure-via-mcp](../adr/2026-03-05-scope-gated-tool-exposure-via-mcp.md)). The gateway removes that requirement: kora introspects each registered MCP's `tools/list`, **registers every tool as a platform scope** (`mcp:<server>:<tool>:use`), and enforces access at the gateway using the existing grant model ([policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md)). This means any MCP server — including unmodified open-source ones — can be governed at the tool level without changing its code.

MCP servers are **registered in infra, never created in the UI** — declared as operator-reconciled resources (CRs). A **tenant** registers its own servers (tenant-scoped, private to it); the only **global** servers are platform-owned — the kora MCP and any platform-level MCPs kora registers itself — and these are available to every tenant. The platform then introspects each server to register its scopes. MVP governs **kora-hosted servers only** (first-party and kora-deployed wrapped open-source); remote third-party SaaS MCP servers are an explicit fast-follow.

The gateway adds a new Policy Enforcement Point, not a new access-management surface — access is administered through the **existing** grant model. For **humans**, an admin grants tool scopes through the policy dashboard, and the human's MCP client authenticates via the **MCP OAuth flow**. For **agents**, MCP/tool access is selected in the agent's settings UI and reconciled into the agent's **single auto-managed grant** — exactly as model access works today — and a generated session config (detailed in §4) lets the agent reach the gateway with zero credential handling. The MVP ships **both personas**.

Per-call audit rides the **existing OpenTelemetry** discipline (span + structured log per decision) — it does not wait on the event spine. The technical design is split across **three ADRs**, all built as part of this feature: **MCP Server Registration & Introspection** (the CR + scope registration), the **MCP / Tool Gateway** (the PEP, proxy, agent provisioning, audit), and the **MCP OAuth Authentication Model** — a new platform auth model with its own spec, called by the gateway's human path.

---

## Problem Statement

### Current State

kora can deploy and register MCP servers as first-class `SERVICE` principals, and a server *that kora owns* can scope-gate its own tool surface at connection time. But there is no consumption plane:

- There is **no single endpoint** a human's MCP client or an agent can point at to reach governed tools. Consumers would have to connect to each MCP server individually.
- Per-tool enforcement currently **requires owning the MCP's code** — the server itself must read the delegation token and filter its `tools/list`. Open-source and third-party MCP servers cannot be governed this way.
- The introspected tool manifest is treated as **discovery metadata only, not security-validated** ([mcp-server-registration ADR](../adr/2026-03-05-mcp-server-registration-deployment-and-governance.md)). Nothing turns "this server exposes tool X" into "principal P may call tool X."
- **Humans have no first-class path at all.** Today's MCP design is agent-centric (pull/push discovery into a task). A developer who wants to use a governed kora tool from their own IDE has nowhere to connect.

### User Pain Points

- **Platform admin:** "I want to let the team use the GitHub MCP, but I can only allow or block the whole server — I can't say 'read issues, yes; delete repos, no' unless I fork and modify the server."
- **Security/compliance:** "An open-source MCP server is a black box. I can't put per-tool authorisation in front of it without trusting code I don't control."
- **Agent developer:** "Every agent re-discovers and connects to MCP servers individually. There's no one governed surface, and no consistent audit of which tool was called by whom."
- **Human developer:** "I want the same governed tools my agents use, available from my own editor, scoped to *my* permissions — not a separate ungoverned MCP config on my laptop."

### Business Impact

- **Adoption ceiling.** The MCP ecosystem's value is the long tail of open-source servers. If kora can only govern code it owns, it cannot govern the ecosystem — undercutting the "What can it access?" pillar at exactly the layer customers care about.
- **Security exposure.** All-or-nothing server access forces admins to over-grant. Per-tool control is the difference between "the team has the GitHub MCP" and "the team can read issues but not force-push."
- **Incomplete identity story.** kora's promise is one identity model for humans and agents. Without a human consumption path for tools, that promise is only half-delivered.

---

## Goals and Objectives

### Primary Goals

1. **One governed front door.** A single MCP endpoint that both human MCP clients and agents connect to, speaking standard MCP — no consumer-side kora code required.
2. **Per-tool enforcement at the platform layer.** Introspect each registered MCP, register its tools as scopes, and check every `tools/list` and `tools/call` against the caller's grants — without modifying the MCP server.
3. **Govern code we don't own.** Bring unmodified open-source / wrapped MCP servers under the same per-tool policy as first-party ones.
4. **Dual persona, one enforcement spine.** Humans authenticate via the MCP OAuth flow from their client and discover tools via the UI; agents connect with their auto-injected platform token via a generated config. Both hit the *same* filter/check logic.
5. **Reuse, don't reinvent, access management.** Tool access is granted through the existing policy-service grants and policy dashboard. The gateway is a new PEP only.
6. **Tenant-isolated, infra-declared registration.** Tenants register their own servers (tenant-scoped, infra-declared, never UI-created); kora registers the platform/global ones (e.g. the kora MCP). A tenant can never see or grant another tenant's servers.

### Success Metrics

- An admin can register an unmodified open-source MCP server and, within one screen, grant a role access to a subset of its tools — with the rest denied by default.
- A human pointing Claude Desktop at the kora gateway sees exactly the tools their grants allow, and a denied `tools/call` is rejected by the gateway before reaching the server.
- An agent calling the same gateway with its platform token gets the same per-tool decisions, with every call attributed to its principal in the audit trail.
- Re-introspecting a changed MCP idempotently adds new tools and removes vanished ones with no manual scope bookkeeping; a new tool is reachable only where the existing coverage rule already allows it (server-level grant), never by the platform auto-creating a grant.

---

## Non-Goals (MVP)

- **Remote third-party SaaS MCP servers.** MVP fronts kora-hosted/registered servers only. Proxying remote MCP endpoints (e.g. a hosted GitHub MCP URL) is a fast-follow.
- **Backend-credential injection for the gateway→upstream hop.** A kora-hosted MCP server is a managed microservice that handles its own downstream credentials — it is *not* an untrusted agent sandbox, so it does not need forced routing through the credential-aware outbound gateway. The MCP gateway governs the call *to* the tool; how the tool authenticates its own *backend* is the server's concern, not the gateway's. (This is distinct from forwarding the *caller's* identity: by default the gateway does **not** forward the caller's token to an upstream either, except to first-party servers explicitly flagged for it — see the gateway ADR's `callerIdentity` opt-in.)
- **Delegation-chain modelling for agents.** MVP authorises on the principal in the agent's presented token. Nested `act`-chain enforcement across the gateway hop is a later platform constraint, not MVP.
- **Per-tool argument / payload policy.** MVP authorises *which tool*, not *with what arguments*. Field-level / content policy is future work.
- **MCP resources and prompts.** MVP governs **tools**. `resources/*` and `prompts/*` MCP surfaces are out of scope for the first cut.
- **A new access-management UI.** No parallel grant model — access is administered through the existing policy dashboard.
- **UI-created MCP servers.** Servers are *declared in infra* (CR, operator-reconciled) — there is no "add a server" form. The UI only browses the catalog and drives grants. This is a deliberate security posture, not a gap.

---

## Personas

| Persona | Who they are | How they reach the gateway |
| --- | --- | --- |
| **Tenant Administrator** | Registers their tenant's own MCP servers in infra (CR), reviews discovered tools, grants tool scopes to roles | kora web UI (policy dashboard + a read-only tool-catalog view) — no "create server" form |
| **kora (platform operator)** | Registers the global/platform MCPs (the kora MCP and any platform-level servers) available to all tenants | Infra/CR managed by kora itself |
| **Human Tool Consumer** | A developer using a desktop/IDE MCP client who wants governed tools in their own workflow | MCP client (Claude Desktop / Cursor / VS Code) → gateway, authed via the **MCP OAuth flow** |
| **Agent** | A kora-run agent that calls tools during a run | In-pod MCP connection to the gateway via an auto-generated, **unauthenticated** MCP config (platform token auto-injected). Access is set in the agent's settings UI, not by the agent |
| **Security / Compliance** | Needs assurance every tool call is authorised and attributed | Reads the audit/event trail; sets default-deny posture |

---

## User Stories

### Registration & Discovery

- **US-001 — Declare and introspect a server via infra.** *As a platform admin, I want to declare an MCP server as infra (a CR) and have kora call its `tools/list` and record every tool, so that its tools become governable platform scopes without me editing the server or hand-building a UI entry.*
  - [ ] An MCP server is declared as an operator-reconciled resource (CR), not created in the UI.
  - [ ] Reconciliation triggers introspection of the server's tool surface.
  - [ ] Each discovered tool is registered idempotently as a scope `mcp:<server>:<tool>:use`.
  - [ ] The tool name, description, and input schema are captured for display.

- **US-002 — Platform-global tools plus my own tenant servers.** *As a tenant admin, I want my tenant to see the platform's global MCPs (like the kora MCP) alongside the servers I register myself — and never another tenant's — so that shared tools are available without my managing them and my own servers stay private.*
  - [ ] Global servers (the kora MCP and any platform-level MCPs) are registered by kora and available to every tenant.
  - [ ] A tenant registers its own servers as tenant-scoped CRs, private to that tenant.
  - [ ] A tenant can never see, introspect, or grant another tenant's servers or their tool scopes.

- **US-003 — Idempotent re-sync on drift.** *As a platform admin, I want re-introspection to add newly-appeared tools and remove vanished ones, so that the scope catalog tracks the server without manual bookkeeping.*
  - [ ] New tools are added as scopes; removed tools are retired.
  - [ ] Re-running introspection with no changes is a no-op.
  - [ ] The platform never auto-creates a grant for a new tool. Its reachability follows the existing coverage rule: a tool-level grant doesn't cover it (default-deny until granted), while a pre-existing **server-level** grant (`mcp:<server>:use`) *does* cover it automatically — the admin opted into the whole surface, future tools included. New tools are flagged in the catalog either way.

- **US-004 — Browse the catalog.** *As a tenant admin, I want a catalog view of every registered server and its tools with grant status, so that I can see at a glance what exists and what's reachable.*
  - [ ] Catalog lists servers → tools, each showing whether any role/principal is granted it.
  - [ ] Newly-discovered, ungranted tools are visibly flagged for review.

### Access Control

- **US-005 — Grant at server or tool granularity.** *As a tenant admin, I want to grant either a whole server or specific tools, so that I can say "all of GitHub" or "read issues but not delete repo" — using the same scope model as everything else.*
  - [ ] An individual tool is granted as `mcp:<server>:<tool>:use`.
  - [ ] A whole server is granted as `mcp:<server>:use`, which covers every tool on it via the existing scope-hierarchy prefix rule (not a `*` glob).
  - [ ] Both are grantable through the existing policy dashboard / grant model with no new mechanism.

- **US-006 — Default-deny.** *As security, I want any tool with no covering grant to be unreachable, so that registering a server never silently exposes capability.*
  - [ ] `tools/list` omits tools the caller has no grant for.
  - [ ] `tools/call` on an ungranted tool is rejected at the gateway before reaching the server.

### Human Consumption

- **US-007 — Connect a desktop/IDE MCP client.** *As a human developer, I want to connect my MCP client to kora and get the tools I'm allowed, so that I use governed tools from my own workflow.*
  - [ ] The gateway authenticates a human via the MCP OAuth flow.
  - [ ] `tools/list` returns exactly the caller's granted tools.
  - [ ] A denied `tools/call` returns a clear MCP error.

- **US-008 — Get a ready-to-paste client config.** *As a human developer, I want the kora UI to give me the connection config for my MCP client, so that setup is copy-paste, not hand-rolled.*
  - [ ] UI surfaces the gateway endpoint and a generated client config snippet.

### Agent Consumption

- **US-009 — Configure an agent's MCP/tool access.** *As an agent developer, I want to pick which gateway MCPs an agent can use — and which tools within each — from the agent's creation/settings UI, so that I scope an agent's tools the same way I scope its models.*
  - [ ] The agent settings UI lists gateway MCP servers and their tools as selectable access.
  - [ ] Selection is per-tool within each server (not all-or-nothing per server).
  - [ ] The selected tool scopes are reconciled into the agent's **single auto-managed grant** (delete-and-recreate, alongside its model scopes), the same atomic reconciliation used for model access today — no separate hand-authored grant per agent.

- **US-010 — Auto-generated, unauthenticated MCP config at session spin-up.** *As an agent developer, I want each new agent session to receive a ready MCP config pointing at the gateway with no embedded credentials, so that the agent gets governed tools with zero auth handling.*
  - [ ] Spinning up a session generates an MCP client config listing the gateway as the tool source.
  - [ ] The config carries **no** credentials; the platform auto-injects the agent's token on outbound calls.
  - [ ] The config reflects the MCPs configured for that agent.

- **US-011 — Agent calls are authorised and attributed.** *As security, I want an agent's tool calls through the gateway to be checked against its grant and attributed to its principal, so that an agent can never exceed its configured tool set.*
  - [ ] `tools/list` and `tools/call` are enforced against the agent's grant.
  - [ ] No agent-side kora code is required beyond the generated config.

### Audit

- **US-012 — Every call is attributed.** *As security, I want each `tools/call` to emit who called which tool on which server and whether it was allowed, so that tool use is fully auditable via our existing telemetry.*
  - [ ] Every allow/deny decision is emitted as an OTel span + structured log carrying principal, server, tool, and decision.
  - [ ] No dependency on the event spine; spine emission is additive when it ships.

---

## Proposed Solution

The gateway is a new control-plane component (a greenfield `internal/toolgateway` package, plus a new binary) that is simultaneously an **MCP server to consumers** and an **MCP client to upstream servers** — a governing proxy. It has five parts.

### 1. Registration & Introspection

MCP servers are declared in infra as operator-reconciled resources (CRs) — tenant-scoped by a tenant, or global when kora registers a platform server (extending the registry from [mcp-server-registration-deployment-and-governance](../adr/2026-03-05-mcp-server-registration-deployment-and-governance.md)). On reconciliation, the platform connects to the server as an MCP client and calls `tools/list`. For each tool it registers a scope `mcp:<server>:<tool>:use` into the policy-service catalog, capturing the tool's name, description, and input schema for display. Re-introspection is **idempotent**: appeared tools are added, vanished tools are retired, unchanged surfaces are no-ops. This is the step that turns a previously non-security tool manifest into the **authoritative, governable** surface — and the reason an unmodified open-source server can be governed.

### 2. Authorisation (the new PEP)

The gateway is a Policy Enforcement Point built on the existing authorisation model. It enforces at two MCP touchpoints:

- **`tools/list` → filter.** The advertised tool set is reduced to those the caller is granted (mirrors the policy-gateway `query` pattern), so out-of-scope tools never enter the consumer's possibility space.
- **`tools/call` → check.** Before proxying a call, the gateway checks `mcp:<server>:<tool>:use` against the caller's grants (the `check` pattern). No grant → reject, default-deny.

Access is granted through the **existing** grant model — humans by admin grant in the policy dashboard, agents by the auto-managed grant driven from the agent settings UI (see §4). The gateway introduces no parallel access model.

### 3. Dual-Persona Authentication

One enforcement spine, two front doors:

| | Human path | Agent path |
| --- | --- | --- |
| **Authenticates with** | the **MCP OAuth flow** from an MCP client (net-new build) | platform token auto-injected into the agent's outbound request (sidecar) |
| **Discovers tools via** | kora web UI + generated client config | auto-generated, unauthenticated MCP config at session spin-up |
| **Granted via** | admin grants tool scopes through the policy dashboard | selection in the agent's settings UI, reconciled into the agent's single auto-managed grant |
| **Authorised on** | the human principal's grants | the agent principal's grant |

Both resolve to a platform principal, and both hit the identical filter/check logic. No delegation-chain modelling in MVP — the agent path authorises the token's principal directly.

### 4. Agent MCP Provisioning

Agents never hand-configure tools. An agent's MCP/tool access is selected in its creation/settings UI (which MCP servers from the gateway, and which tools within each). That selection is **reconciled into the agent's single auto-managed grant** — the grant is immutable, so a scope change is a delete-and-recreate of the one grant carrying the agent's model *and* tool scopes, exactly the atomic reconciliation model access uses today ([agent-identity-and-policy-provisioning](../adr/2026-05-11-agent-identity-and-policy-provisioning.md)). At session spin-up, kora generates an **unauthenticated** MCP client config that points the agent at the gateway and lists its configured tool sources; the config carries no credentials because the platform auto-injects the agent's token on every outbound call ([envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md)). The result: an agent gets exactly its configured tools, governed and attributed, with zero auth handling on the agent side.

### 5. Governing Proxy

Authorised `tools/call`s are forwarded to the upstream kora-hosted MCP server over its existing in-mesh connection; the response streams back to the consumer. The MCP gateway governs *the call to the tool*, not the tool's downstream credentials — a kora-hosted MCP is a managed microservice responsible for its own backend authentication, so there is no forced egress routing or credential injection on this hop.

### Request lifecycle (sketch)

Steps 1, 3 and 4 are §3 (auth) and §2/§5 (check + proxy):

```
  human MCP client ─┐
                    ├─▶ MCP/Tool Gateway ──▶ Orbit-hosted MCP server
  Orbit agent ──────┘    1. authN (MCP OAuth | injected platform token)
                         2. tools/list → filter to granted scopes
                         3. tools/call → check mcp:<server>:<tool>:use
                         4. allow → proxy upstream;  deny → MCP error
                         5. emit OTel span + structured log (principal, server, tool, decision)
```

---

## Scope

### In Scope (MVP)

- Infra/CR-based server registration (tenant-scoped + kora-registered global), greenfield `internal/toolgateway`.
- Governing proxy fronting **kora-hosted** MCP servers.
- Introspection of `tools/list` and idempotent registration of `mcp:<server>:<tool>:use` scopes.
- `tools/list` filtering and `tools/call` pre-check via existing grants (new PEP).
- Human authentication via the **MCP OAuth flow** (net-new) and agent authentication (injected platform token). **Both personas in MVP.**
- Read-only admin catalog view of servers/tools with grant status + drift flagging.
- Agent settings UI for per-tool MCP access, reconciled into the agent's auto-managed grant.
- Auto-generated, unauthenticated MCP config injected at agent session spin-up.
- Per-decision audit via OTel span + structured log.

### Out of Scope (MVP)

- Remote third-party SaaS MCP servers (fast-follow).
- Credential injection for the gateway→upstream hop (fast-follow).
- Delegation `act`-chain enforcement across the gateway.
- Argument/payload-level policy.
- MCP `resources/*` and `prompts/*` surfaces.
- Any new access-management UI outside the existing policy dashboard.

---

## Requirements

### Functional

**P0 (MVP)**
- FR-1 Register an MCP server via operator-reconciled CR (tenant-scoped or kora-registered global); a tenant cannot reference another tenant's server.
- FR-2 On reconciliation, introspect the server's `tools/list` and register each tool as a scope; mark the CR un-ready and retain prior scopes if the server is unreachable or returns a malformed list.
- FR-3 Idempotent re-sync: add appeared tools, retire vanished tools, no-op on no change.
- FR-4 The platform never auto-creates grants; a new tool is reachable only where the existing coverage rule already allows it.
- FR-5 Filter `tools/list` to the caller's granted tools.
- FR-6 Check `tools/call` against `mcp:<server>:<tool>:use`; reject when ungranted.
- FR-7 Authenticate humans via the MCP OAuth flow and agents via the injected platform token, resolving each to a platform principal.
- FR-8 Proxy authorised calls to the upstream server and stream the response back.
- FR-9 Emit an OTel span + structured log for every allow/deny decision.
- FR-10 Read-only admin catalog view (servers → tools → grant status, drift flagged).
- FR-11 Agent settings UI to select per-tool MCP access, reconciled into the agent's single auto-managed grant (delete-and-recreate, alongside model scopes).
- FR-12 Generate an unauthenticated MCP client config for an agent session reflecting its configured tools.
- FR-13 Generated MCP client-config snippet for human MCP clients in the UI.

**P1 (Fast-follow)**
- FR-14 Remote third-party MCP server support.

**P2 (Later)**
- FR-15 Argument/payload-level policy.
- FR-16 `resources/*` and `prompts/*` governance.
- FR-17 Delegation `act`-chain enforcement across the gateway.

### Non-Functional

- **Security:** default-deny; no consumer ever sees or calls an ungranted tool; every decision audited; tenant isolation holds across registration, introspection, catalog, and grants.
- **Compatibility:** speak standard MCP to both consumers and upstreams — no kora-specific client required.
- **Observability:** every decision is an OTel span + structured log (no event-spine dependency); distributed tracing across the consumer→gateway→server hop; decision metrics.
- **Reliability:** streaming responses with a defined timeout / cancellation / backpressure story for long-running `tools/call`s; upstream failures surface as clean MCP errors, never silent hangs.
- **Latency:** authorisation adds negligible overhead relative to tool execution (reuse local PDP path); target **authz < 10 ms p95** added to a `tools/call`. The per-caller `tools/list` filter must not fan out an uncached PDP call per advertised tool.

---

## Dependencies

| Dependency | Type | Status | Note |
| --- | --- | --- | --- |
| Policy-service grants + scope vocabulary | Internal | Implemented | Source of truth for `mcp:<server>:<tool>:use` grants ([policy-gateway ADR](../adr/2026-04-14-policy-gateway-and-authorisation-model.md)) |
| `internal/toolgateway` package + binary | Internal | **Greenfield (net-new)** | Does not exist — created by this feature; not an existing stub |
| MCP server registration (CR + introspection) | Internal | **Designed, not built** | The 2026-03-05 registration ADR is a paper design; this feature builds it ([registration ADR](../adr/2026-03-05-mcp-server-registration-deployment-and-governance.md)) |
| MCP OAuth flow | Internal | **Net-new build** | OAuth 2.1 + dynamic client registration + resource metadata; its own MCP OAuth Authentication ADR |
| Kubernetes operator (CR reconciliation) | Internal | Implemented | Reconciles the new MCP server CR ([operator ADR](../adr/2026-03-31-orbit-kubernetes-operator.md)) |
| Unified identity / platform tokens + PATs | Internal | Implemented | Agent path resolves to a principal; PATs exist ([PAT ADR](../adr/2026-04-02-personal-access-tokens.md)) |
| Policy dashboard | Internal | Implemented | Where tool scopes are granted |
| OpenTelemetry observability | Internal | Implemented | Audit rides existing spans + structured logs; no event-spine dependency |

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| **Server-level grant is a standing opt-in to *future* tools** | High | This is the price of `mcp:<server>:use`: a server that later adds a destructive tool (e.g. `delete_repo`) has it covered by the prior "all of GitHub" grant automatically. Mitigation: flag newly-discovered tools in the catalog *even when a server grant covers them*; ADR to consider requiring re-affirmation of server-level grants on drift |
| Introspection drift silently exposes new capability | High | New tools register ungranted (default-deny) at the tool level; flagged for admin review |
| Introspection failure / malformed `tools/list` | Medium | CR goes un-ready; prior scopes retained (no silent removal); surfaced in catalog |
| Tenant isolation breach (cross-tenant server/tool leakage) | High | CR-apply authority scoped to the tenant namespace and tied to the identity model; registration, introspection, catalog, and grants all tenant-filtered |
| Gateway becomes a single bottleneck/SPOF for all tool use | High | Stateless authz via local PDP; horizontal scaling; clean upstream-failure errors |
| Scope explosion (servers with many tools) | Medium | Server-level grant `mcp:<server>:use` covers the whole surface in one scope (existing hierarchy); catalog groups tools by server for per-tool grants when finer control is wanted |
| Overlap/confusion with the existing scope-gated-MCP ADR | Medium | ADR explicitly states the gateway supersedes MCP-side filtering for governed servers |
| MCP spec churn (transports, semantics) | Medium | Implement against a pinned spec version; isolate transport behind the proxy layer |

---

## Alternatives Considered

1. **Keep enforcement inside each MCP server (status quo).** Rejected: only works for code we own; cannot govern the open-source ecosystem — the whole point of this feature.
2. **Per-server gateways (no aggregation).** A PEP per server, consumers connect per-server. Rejected for MVP UX: no single front door, and humans would juggle many endpoints. The proxy model gives one endpoint while still allowing per-server upstreams.
3. **Catalog/registry only, enforce elsewhere.** Just publish a browsable catalog and leave calls direct-to-server. Rejected: doesn't deliver per-tool enforcement for unowned servers — the core requirement.
4. **A bespoke access model in the gateway.** Rejected: duplicates the grant/scope machinery and splits the admin's mental model. Reusing policy-service keeps one source of truth.

---

## Open Questions

All of the questions this PRD raised have been resolved by the three accepted ADRs. Recorded here for traceability:

1. **CR-apply authority / tenant isolation** → Registration ADR: registration is GitOps `kubectl apply` into the tenant namespace, governed by tenant RBAC; there is no create API, and isolation rests on tenant-scoped grants + the tenant-dimensioned catalog.
2. **How the PEP evaluates** → Gateway ADR: the in-process `policy.Evaluator` (OPA bundle), `Check` for `tools/call` and `Query` for `tools/list`; the Layer-1 sidecar keeps only a coarse "may reach the gateway" gate.
3. **Scope vocabulary** → `mcp:<server>:<tool>:use` with prefix coverage (`mcp:<server>:use`, `mcp:use`); no tenant in the path — tenancy is on the grant, mirroring `connection:`/`inference:`. Published globally via the connection `ScopePublisher` pattern.
4. **Server/tool naming & stability** → Registration ADR: stable CR slug for the server; MCP tool names normalised to scope-path-safe segments; a rename is a new scope (old pruned → clean default-deny).
5. **Transport** → Gateway ADR: Streamable HTTP.
6. **Streaming & long-running tools** → Gateway ADR: per-call timeout, cancellation propagated upstream, backpressure via the streamed channel.
7. **`tools/list` filter performance** → Gateway ADR: one `Query` over the server's candidate set, cacheable per `(principal, server, bundle-revision)`.
8. **MCP OAuth scope of MVP** → OAuth ADR: OAuth 2.1 + PKCE + DCR ship in MVP; the issued token is an ordinary `aud=kora-platform` platform token (per-resource audiences deferred to a future ADR).

> Also resolved: binary placement (new `cmd/tool-gateway`); audit (OTel span + structured log, no event-spine dependency); who introspects/publishes (the tool-gateway in a `replicas=1` publisher deployment, *not* the operator — the operator only aggregates CRs).

---

## Implementation Phases

The MVP is **Phase 1 + Phase 2** (both personas), mapping onto the three ADRs (registration; gateway; OAuth).

- **Phase 1 — Registration & introspection (MCP Server Registration ADR, P0):** the MCP server CR (tenant + global) aggregated by the operator into a mounted registry; the tool-gateway's publisher introspects `tools/list` and registers `mcp:<server>:<tool>:use` scopes idempotently; failure handling (retain prior scopes on unreachable, prune-guard against cold/partial keep-lists); read-only catalog view with drift flagging. Exit: declaring a server registers its tools as scopes; drift re-syncs idempotently; tenant isolation holds.
- **Phase 2 — Gateway & both personas (MCP / Tool Gateway ADR + MCP OAuth ADR, P0):** the `internal/toolgateway` binary; the content-aware PEP (`tools/list` filter, `tools/call` check); the governing proxy + OTel audit; **agent path** (settings-UI tool selection → reconciled grant + generated unauthenticated session config); **human path** (MCP OAuth flow + generated human client config). Exit: an agent reaches only its configured tools, and a human connects a desktop MCP client and sees exactly their granted tools; default-deny holds for both.
- **Phase 3 — Fast-follow:** remote third-party MCP servers.
- **Phase 4 — Later:** argument-level policy; resources/prompts; delegation-chain enforcement.

---

## Appendix

### Related Documents

- **MCP Server Registration & Introspection ADR** (to be written) — the CR + introspection + scope-registration foundation Phase 1 builds.
- **MCP / Tool Gateway ADR** (to be written) — the PEP, proxy, agent provisioning, audit (Phase 2).
- **MCP OAuth Authentication ADR** (to be written) — new platform auth model (OAuth 2.1 + dynamic client registration + protected-resource metadata) consumed by the gateway's human path.
- [mcp-server-registration-deployment-and-governance](../adr/2026-03-05-mcp-server-registration-deployment-and-governance.md) — the prior paper design the registration ADR supersedes/implements
- [orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md) — reconciles the new MCP server CR
- [personal-access-tokens](../adr/2026-04-02-personal-access-tokens.md) — existing human credential; MCP OAuth is the net-new addition
- [scope-gated-tool-exposure-via-mcp](../adr/2026-03-05-scope-gated-tool-exposure-via-mcp.md) — MCP-side filtering this supersedes for governed servers
- [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md) — grants, scopes, PEP pattern reused here
- [jit-credential-brokering-egress-gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md) — the agent-sandbox egress model this feature deliberately does *not* impose on managed MCP servers
- [unified-identity-model](../adr/2026-03-05-unified-identity-model.md) — principals for both personas
- [agent-identity-and-policy-provisioning](../adr/2026-05-11-agent-identity-and-policy-provisioning.md) — the auto-managed per-agent grant model this mirrors
- [envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — why the agent's MCP config needs no credentials

### Glossary

| Term | Definition |
| --- | --- |
| **MCP** | Model Context Protocol — the standard for exposing tools/resources to AI clients |
| **MCP / Tool Gateway** | kora's governing proxy that both humans and agents connect to for tools |
| **Introspection** | The platform calling an MCP server's `tools/list` to discover its tools |
| **Tool scope** | A platform scope `mcp:<server>:<tool>:use` registered from a discovered tool. Covered by the broader `mcp:<server>:use` (whole server) via the existing scope-hierarchy prefix rule — no `*` glob |
| **Tenant server** | An MCP server a tenant registers itself (CR), private to that tenant |
| **Global server** | A platform-owned MCP server kora registers (e.g. the kora MCP), available to all tenants |
| **MCP OAuth** | The MCP authorization flow (OAuth 2.1) a human's MCP client uses to authenticate to the gateway — net-new build |
| **PEP** | Policy Enforcement Point — where an access decision is enforced |
| **Default-deny** | A tool with no covering grant is unreachable |
| **Dual persona** | The human and agent consumption paths over one enforcement spine |
| **Governed server** | A kora-hosted/registered MCP server fronted by the gateway |
