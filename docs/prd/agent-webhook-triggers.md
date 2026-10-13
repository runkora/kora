# Agent Triggers - Product Requirements Document

**Status:** Draft
**Created:** 2026-06-16
**Last Updated:** 2026-06-16
**Owner:** Justin Wilkin
**Stakeholders:** Platform Engineering, Agent Control Plane, Identity & Policy, Security

---

## Executive Summary

kora can run agents on demand, but only when a human starts them — every run today is created by an interactive principal through `POST /api/v1/runs`. This PRD introduces **Triggers**: a framework that starts agent runs automatically in response to external events. "Trigger" is the umbrella concept (scheduled and event-spine kinds can follow); the **webhook trigger** is the first and only kind in v1 — a source-agnostic inbound-event pipeline. A platform admin registers a webhook source (e.g. a GitHub repository), binds events to an agent, and matching deliveries start a run with a templated prompt populated from the event payload.

The first and motivating use case is **triggering agents on pull requests** to accelerate the team's own development flow (e.g. auto-review, auto-triage, CI follow-up). The framework is deliberately generic — GitHub is the first *source adapter*, not the design center. **Each registered webhook source is mapped to its own platform identity at registration**; triggers on that source create runs under that identity, and a run can read/write the target agent's session if that identity has access to the agent. Configuration and governance are platform-admin-level.

---

## Problem Statement

### Current State

- The agent control plane (`orbit-agents`) creates runs exclusively via `POST /api/v1/runs`, deriving `tenant_id` and `created_by` from a verified human/PAT platform token (`backend/internal/agents/run_service.go`). There is **no non-interactive path** to start a run.
- The run-and-session lifecycle ADR (`docs/adr/2026-05-11-run-and-session-lifecycle.md`) explicitly lists *"Scheduled or trigger-based run initiation (deferred)"* as a non-goal. This is the gap we are now closing.
- There is **zero inbound webhook infrastructure** anywhere in the codebase — no signature verification, no event ingestion, no delivery logging. This is greenfield.
- The GitHub App connector (`internal/credentials/adapters/githubapp`) exists, but it is an **egress** credential-injection point. It does not (and should not) receive or govern inbound webhooks.

### User Pain Points

- **Agents can't react to real-world events.** Valuable, repeatable work (review this PR, triage this issue, re-run the failing job) requires a human to manually start a run every time.
- **No way to wire agents into the development loop.** The team wants agents to act on pull requests automatically, but there is no mechanism to connect a Git event to an agent run.
- **Automation today would mean misusing credentials.** Without a first-class trigger, the only workaround is scripting `POST /api/v1/runs` with a human's PAT, which muddies personal credentials with automation and breaks the accountability chain.

### Business Impact

- Slows kora's own engineering velocity — the dogfood case (agents on PRs) is blocked.
- Limits the product's reach: "agents that respond to events" is table stakes for an agent control plane and a frequent prospect ask.
- Every day without it pushes teams toward ungoverned, out-of-band automation that kora is meant to replace.

---

## Goals and Objectives

### Primary Goals

1. **Generic inbound trigger framework.** A source-agnostic pipeline — receive delivery → verify → parse → match triggers → start run → log — with pluggable per-source adapters. GitHub is the first adapter; the framework must not be GitHub-shaped.
2. **Trigger agents on pull requests.** Ship GitHub `pull_request` and `push` support end-to-end as the proof and the first real use case.
3. **Preserve the three pillars.** A triggered run must have a verifiable identity (*Who*), a bounded scope it cannot exceed (*What*), and must reuse — not bypass — the existing egress broker for outbound calls (*How*).
4. **Platform-governed configuration.** Registering sources and triggers is a platform-admin capability, gated by authorization, with a REST API and a management UI.

### Success Metrics

- A GitHub pull request opened on a configured repo starts the bound agent run automatically, end-to-end, in the local/k3d environment.
- 100% of inbound deliveries have their signature verified before any side effect; unverified deliveries are rejected and logged.
- 100% of triggered runs carry an attributable identity and a scope that is a subset of what the configuring platform admin was permitted to grant.
- Every delivery produces a durable, queryable record (verified / matched / runs-started / skipped / failed) for debugging and audit.
- Zero raw secrets in YAML or logs (signing secrets stored as Kubernetes secrets, referenced indirectly).

### Non-Goals

- **Scheduled / cron-based triggers.** Time-based triggers are a future *trigger kind*; v1 ships the webhook trigger kind only.
- **Coupling webhooks into the egress credential broker.** Inbound governance and outbound credential injection stay separate planes (see Alternatives).
- **Self-service trigger creation by individual engineers.** v1 is platform-admin-governed only.
- **Outbound webhooks / kora-emitted events** (notifying external systems) — that belongs to the event-spine work.
- **A full third-party source catalog.** GitHub is the only shipped adapter in v1; the framework is built so GitLab, generic HMAC sources, etc. can follow.
- **Multi-agent orchestration / fan-out workflows** triggered by a single event.
- **Prompt-templating injection safety** — deferred to a later iteration (payload treated as data; hardening comes later).

---

## User Stories

### Primary User Stories

**As a** platform admin,
**I want to** register a GitHub repository as a webhook source and get back an endpoint URL and signing secret,
**So that** I can configure GitHub to deliver events to kora securely.

**Acceptance Criteria:**
- [ ] Creating a source returns a stable inbound URL and a generated signing secret (shown once).
- [ ] The signing secret is stored as a Kubernetes secret, never in YAML or the database in plaintext.
- [ ] Only principals with the platform-admin trigger-management scope can create a source.

---

**As a** platform admin,
**I want to** create a trigger that binds "PR opened on repo X targeting `main`" to a specific agent type with a templated prompt,
**So that** the agent runs automatically with the right context whenever a matching PR is opened.

**Acceptance Criteria:**
- [ ] A trigger references one source, an event type, an optional match condition (repo/branch/label/action), a target agent type, and a prompt template.
- [ ] When a matching delivery arrives, a run starts with the rendered prompt as its initial message.
- [ ] Non-matching deliveries start no run and are recorded as `skipped`.

---

**As a** platform admin,
**I want to** see a log of recent webhook deliveries and the runs they spawned,
**So that** I can debug why a trigger did or didn't fire.

**Acceptance Criteria:**
- [ ] Each delivery records: source, event type, verification result, matched trigger(s), run ID(s) (if any), status, and any error.
- [ ] The delivery log is queryable via API and visible in the UI.

### Secondary User Stories

- **As a security reviewer,** I want every triggered run to trace to the trigger and the admin who configured it, so the accountability chain is intact.
- **As a platform admin,** I want to disable or delete a trigger without deleting its history, so I can safely turn automation off.
- **As an engineer,** I want to see that a run was started by a trigger (not a person) in the run list, so attribution is unambiguous.

---

## Proposed Solution

### High-Level Approach

A generic inbound pipeline owned by the platform, with thin per-source adapters:

```
External system ──▶ POST /webhooks/{source}/{id}
                        │
                        ▼
              ┌───────────────────────┐
              │  Ingestion + verify   │  per-source adapter:
              │  (HMAC / signature)   │  - signature scheme
              └───────────┬───────────┘  - event-type extraction
                          ▼
              ┌───────────────────────┐
              │   Trigger matching    │  event type + condition
              │  (source-agnostic)    │  (repo/branch/label/...)
              └───────────┬───────────┘
                          ▼
              ┌───────────────────────┐
              │  Prompt templating    │  render template from payload
              └───────────┬───────────┘
                          ▼
              ┌───────────────────────┐
              │   Start agent run     │  non-interactive run path,
              │  (source identity)    │  under the source's mapped identity
              └───────────┬───────────┘
                          ▼
                  Delivery log (durable)
```

Outbound calls the resulting run makes (e.g. commenting on the PR) flow through the **existing egress broker** — the trigger framework never touches outbound credentials.

### Key Components

- **Webhook source** — a registered inbound endpoint with a source type (e.g. `github`), a signing-secret reference, and verification config. **Each source is mapped to a platform identity at registration** — every trigger on that source runs under that identity. Platform-admin managed.
- **Source adapter (interface)** — per-source logic: verify signature, extract canonical event type, and expose payload fields to the templating layer. GitHub adapter ships in v1.
- **Trigger** — binds `(source, event type, match condition)` → `(agent type, prompt template)`. Platform-admin managed.
- **Trigger matcher** — source-agnostic evaluation of a delivery against registered triggers.
- **Prompt templater** — renders a trigger's prompt template against a documented, per-source set of payload fields, producing the run's initial message. *(Mechanism is an open design question — see below.)*
- **Non-interactive run initiator** — a new path that creates a run from a trigger under the source's mapped identity rather than a human token. The Triggers feature lives in its own package (`internal/triggers/`) and calls the agent control plane via a typed platform client — it is **not** folded into `internal/agents`.
- **Delivery log** — durable record of every inbound delivery and its outcome.
- **Management API + UI** — CRUD for sources and triggers, plus delivery inspection, split across a per-agent surface and a platform-wide surface (see *User Interface*).

### User Experience / Flow

1. Platform admin creates a **source** (`github`, repo `org/repo`) → kora returns the inbound URL + signing secret.
2. Admin pastes URL + secret into the repo's GitHub webhook settings (or, later, kora configures it via the GitHub App).
3. Admin creates a **trigger**: source = that repo, event = `pull_request` action `opened`/`synchronize`, condition = base branch `main`, target = agent type `pr-reviewer`, prompt template = e.g. `Review PR #{{ pr.number }} "{{ pr.title }}" on branch {{ pr.head_ref }}.`
4. A PR is opened → GitHub delivers the event → kora verifies, matches, renders the prompt, and starts the run.
5. The run executes; if it comments on the PR, the egress broker injects the GitHub App token.
6. Admin can inspect the delivery and the spawned run in the UI.

### User Interface

The UI has two surfaces that mirror the governance model. A **source** is shared trust infrastructure (one GitHub repo source can feed many agents' triggers) and owns the security-sensitive endpoint URL + signing secret, so it lives at the platform level. A **trigger** is "this event runs this agent," so it is authored at the agent. Both are platform-admin-gated in v1; the per-agent location is about *where the work is done*, not a lower permission bar.

> **Inbound, not outbound.** This is not an outbound-webhook/subscription UI. The user never enters a destination URL to call. kora *issues* an inbound endpoint URL + signing secret (per source) for the external system to post to; the UI manages the inbound triggers those deliveries fire.

**Part 1 — Per-agent "Triggers" tab** (agent detail page, alongside Overview / Runs / Connectivity / Settings):
- Lists triggers that target this agent: source, event + condition, prompt template, enabled state.
- Create/edit a trigger by selecting from already-registered sources (no secret management here).
- Shows recent deliveries that fired *this* agent — answers "why did/didn't my agent run?"

**Part 2 — Platform-wide "Triggers" section** (top-level Control Plane nav, scope-gated to platform admins), with three sub-views:
- **Sources** — registered inbound origins (e.g. a GitHub repo). Manages the generated endpoint URL and signing secret (stored as a Kubernetes secret, shown once).
- **All Triggers** — cross-agent inventory/audit: every trigger with its source, target agent, owner, enabled state, and last-fired time; bulk enable/disable.
- **Deliveries** — global delivery log (verified / matched / run-started / skipped / failed) for cross-agent debugging.

Implementation follows existing frontend precedents: the per-agent tab mirrors the connectivity tab; the platform section mirrors the `environments/` feature (list + modal CRUD) and registers a scope-gated nav entry like other Control Plane areas.

---

## Scope

### In Scope

- Generic inbound webhook ingestion + signature verification, with a pluggable source-adapter interface.
- GitHub source adapter: `pull_request` and `push` events, HMAC-SHA256 (`X-Hub-Signature-256`) verification.
- Trigger registry: source binding, event-type filter, match condition, target agent type, prompt template.
- Source-agnostic trigger matching.
- Templated-prompt run initiation via a new non-interactive run path; the run executes under the webhook source's mapped identity.
- Durable delivery log + query API.
- Platform-admin authorization gate on all source/trigger management.
- REST API + management UI (Hades).

### Out of Scope

- Scheduled/cron triggers (separate capability).
- Source adapters other than GitHub (GitLab, Bitbucket, generic HMAC) — framework-ready, not shipped.
- Outbound/emitted webhooks.
- Automatic GitHub webhook registration via the App API (admin pastes config manually in v1; auto-registration is a future enhancement).
- Loop/fan-out orchestration (one trigger → at most one run per matched delivery in v1).

### Future Considerations

- Additional source adapters; a generic "any HMAC webhook" adapter.
- Auto-registering the webhook on GitHub via the existing App installation.
- Scheduled triggers sharing the same run-initiation path.
- Conditional templating / richer expression language.
- Routing trigger activity through the platform event spine once it exists.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Inbound endpoint that accepts deliveries and verifies signatures via a per-source adapter before any side effect.
- [ ] Reject + log deliveries that fail verification.
- [ ] Source-agnostic trigger model and matcher (event type + condition).
- [ ] GitHub adapter for `pull_request` and `push`.
- [ ] Templated prompt rendered from payload → run initial message.
- [ ] Non-interactive run creation under the webhook source's mapped identity (attributable; bounded by that identity's access to the agent).
- [ ] Durable delivery log with outcome status.
- [ ] Platform-admin authorization on all management operations.
- [ ] REST API for source/trigger CRUD and delivery inspection.

**Should Have (P1):**
- [ ] Per-agent "Triggers" tab on the agent detail page (author triggers for that agent + recent deliveries that fired it).
- [ ] Platform-wide "Triggers" section: Sources (with endpoint URL + signing-secret management), All Triggers (cross-agent inventory + bulk enable/disable), Deliveries (global log).
- [ ] Idempotent handling of GitHub redeliveries (dedup on delivery ID).
- [ ] Enable/disable a trigger without deleting it.
- [ ] Run list surfaces "started by trigger" attribution.

**Nice to Have (P2):**
- [ ] Replay a logged delivery for debugging.
- [ ] Per-trigger concurrency/debounce controls (e.g. one active run per PR).

### Non-Functional Requirements

**Security:**
- All deliveries signature-verified; constant-time comparison; replay protection (timestamp/delivery-ID window).
- Signing secrets stored as Kubernetes secrets, referenced indirectly; never in YAML, DB plaintext, or logs.
- A triggered run is bounded by its source's mapped identity — it can do only what that identity is permitted to do on the target agent (read/write); it cannot escalate beyond that identity.
- Templating-injection safety (untrusted payload fields such as PR title/body) is **deferred to a later iteration** — see Open Questions.
- Loop-prevention is a known consideration (an agent's own PR activity could re-fire a trigger); v1 uses a best-effort guard and revisits in implementation.

**Reliability:**
- Tolerate provider redelivery/retries idempotently.
- Verification/matching failures degrade safely (logged, no partial run).

**Performance:**
- Verify-and-accept the inbound request fast; do run initiation asynchronously so the provider gets a timely 2xx.

**Observability:**
- Every delivery and triggered run emits structured logs + traces, correlated by delivery ID and run ID.

**Scalability:**
- Inbound endpoint handles bursty delivery volume independent of run-execution capacity.

---

## Dependencies

### Internal Dependencies

- **Agent control plane (`internal/agents`)** — needs a new non-interactive run-initiation path. *Status: exists, human-only today.*
- **Identity & token (`internal/platform/token`)** — each webhook source is mapped to a platform identity at registration; triggered runs execute under it. *Status: principal types exist (`SERVICE`, `PAT`, etc.); the source→identity mapping is net-new.*
- **Policy (`policy-service` / PEP)** — authorizes trigger management (platform admin) and bounds triggered-run scope. *Status: exists.*
- **Egress credential broker (`internal/credentials`)** — used by the triggered run for outbound calls; not modified by this feature. *Status: shipped.*
- **Frontend (Hades)** — management UI. *Status: exists.*

### External Dependencies

- GitHub webhook delivery + HMAC signing (`X-Hub-Signature-256`, `X-GitHub-Event`, `X-GitHub-Delivery`).
- A publicly reachable inbound endpoint (ingress) for providers to POST to.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Triggered-run identity weakens the human-accountability chain | High | Low | Resolved: each webhook source maps to a dedicated platform identity at registration; every run traces to that identity and its source, and is bounded by that identity's access to the target agent |
| Prompt-injection via attacker-controlled payload fields (PR title, body) | High | High | **Deferred** — payload fields are attacker-controllable, but templating-injection safety is explicitly out of scope for v1 and will be addressed in a later iteration |
| Trigger loops (agent acts on PR → fires webhook → new run → …) | High | Medium | Loop-prevention guardrails: ignore events from kora's own bot identity, per-trigger concurrency/debounce, delivery dedup |
| Public inbound endpoint becomes an attack/DoS surface | Medium | Medium | Mandatory signature verification before side effects; rate limiting; async run initiation; isolate ingestion from control-plane internals |
| Coupling inbound webhooks to the egress GitHub App by accident | Medium | Low | Explicit architectural separation (this PRD); separate secrets and code paths |
| Secret sprawl across many sources | Medium | Low | Standardize on Kubernetes secret references; rotate-able signing secrets |

---

## Alternatives Considered

### Alternative 1: Extend the GitHub App connector to handle webhooks

**Description:** Add webhook reception/verification to the existing `internal/credentials` GitHub App adapter.

**Why not chosen:**
- Conflates two orthogonal planes — inbound event governance vs. outbound credential injection.
- Uses different secrets (webhook signing secret vs. App private key); overloading one connector muddies both.
- Makes the framework GitHub-shaped from day one, defeating the generic goal.

### Alternative 2: GitOps CRD configuration (like Connections)

**Description:** Declare sources/triggers as Kubernetes CRDs reconciled by the operator.

**Why not chosen:**
- Heavier authoring loop for a fast-moving, per-agent dev-flow feature.
- Less suited to self-service inspection of deliveries.
- Chosen instead: REST API + UI (matching the Connectivity Environments pattern). Can revisit GitOps later.

### Alternative 3: Poll the source instead of receiving webhooks

**Description:** Periodically poll GitHub for new PRs.

**Why not chosen:**
- Higher latency, rate-limit pressure, and statefulness; webhooks are the standard, lower-latency mechanism.

---

## Decisions Resolved

Settled during PRD review; the ADR builds on these:

1. **Triggered-run identity is mapped at the webhook source.** Each registered webhook source is mapped to its own platform identity at registration. Every trigger on that source creates a run/session under that identity. A run can read/write the target agent's session if the source's mapped identity has access to that agent — no finer-grained per-trigger scope delineation in v1.
2. **Triggers is its own feature package** (`internal/triggers/`), **not folded into `internal/agents`.** It starts runs by calling the agent control plane through a typed platform client.
3. **"Trigger" is the umbrella noun; "webhook" is reserved for the webhook kind.** The feature, the nav, and the API use "trigger." "Webhook" appears only when specifically referring to the webhook source/trigger kind — scheduled and other kinds may follow.
4. **No read/edit role split in v1.** Both UI surfaces are platform-admin-gated for configuration; a triggered run's read/write capability follows from the source identity's access to the agent.

## Assumptions (best-effort; revisit in implementation)

Reasonable defaults so the ADR/implementation can proceed; revisit only if they bite:

- **Async initiation.** The inbound endpoint verifies and returns a fast 2xx, then initiates the run asynchronously. Delivery states: `received → verified → matched → dispatched` (or `skipped` / `failed`).
- **Dedup.** Redeliveries are de-duplicated on the provider's delivery ID (idempotency).
- **Concurrency.** One run per matched trigger per delivery; concurrent runs allowed. No debounce / one-run-per-PR coalescing in v1.
- **Loop-prevention.** Best-effort only in v1 (e.g. ignoring deliveries attributable to kora's own bot identity); fuller guarding revisited if loops appear.
- **Binary / ingress placement.** The Triggers package is hosted so its inbound endpoint is publicly reachable while the rest of the control plane stays internal; exact binary/ingress wiring confirmed in implementation.

## Open Questions

1. **Prompt templating mechanism.**
   - What templating syntax, and which payload fields are exposed per source? How are missing fields and the rendered output (the run's initial message) handled?
   - **Safety is explicitly deferred** — payload-injection hardening is out of scope for v1 and addressed later. This question is only about the *mechanism* needed to ship.
   - **Status:** Open — needs a minimal design to implement.
   - **Owner:** Agent Control Plane

---

## Timeline and Milestones

### Phases

**Phase 1: Framework + GitHub PR (Target: TBD)**
- Generic ingestion + verification + source-adapter interface
- GitHub adapter (`pull_request`, `push`)
- Trigger registry + matcher + templated-prompt run initiation
- Delivery log + REST API + platform-admin authz

**Phase 2: UI + hardening (Target: TBD)**
- Management UI (sources, triggers, delivery log)
- Idempotency, loop-prevention, enable/disable, run attribution

### Key Milestones

- [ ] PRD Approved: TBD
- [ ] ADR Created: TBD
- [ ] Implementation Started: TBD
- [ ] Alpha (GitHub PR end-to-end in k3d): TBD
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [ADR: Run and session lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md) — defers trigger-based initiation (the gap this closes)
- [ADR: Agent event streaming protocol](../adr/2026-05-11-agent-event-streaming-protocol.md)
- [ADR: Agent identity and policy provisioning](../adr/2026-05-11-agent-identity-and-policy-provisioning.md)
- [ADR: JIT credential brokering / egress gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md) — the egress plane this feature composes with, not modifies
- [PRD: Credential broker and outbound connections](./credential-broker-and-outbound-connections.md)
- ADR for Agent Webhook Triggers — *to be written after PRD approval*

### Glossary

- **Trigger:** A binding that starts an agent run automatically when a condition fires. The umbrella concept; the **webhook trigger** is the first (v1) kind.
- **Webhook source:** A registered inbound origin of webhook deliveries (e.g. a GitHub repo) with a signing secret, source type, and a **mapped platform identity** under which its triggers' runs execute.
- **Source adapter:** Per-source code that verifies signatures and extracts canonical event type + fields.
- **Delivery:** A single inbound webhook request and its processing outcome.
- **Egress broker:** The existing outbound JIT credential-injection layer (`internal/credentials`) — distinct from inbound trigger ingestion.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
