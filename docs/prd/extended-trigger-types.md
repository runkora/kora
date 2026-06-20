# Extended Trigger Types - Product Requirements Document

**Status:** Draft
**Created:** 2026-06-20
**Last Updated:** 2026-06-20
**Owner:** Platform Engineering
**Stakeholders:** Platform Engineering, Agent Control Plane, Identity & Policy, Security

---

## Executive Summary

Orbit's Triggers framework (v1) shipped with webhook ingestion as the only trigger kind — external systems push events to Orbit, which starts agent runs in response. This PRD extends the framework to two additional trigger kinds: **scheduled triggers** (cron-based, time-driven run initiation) and **manual triggers** (an agent or API caller explicitly fires a named trigger to start a run, enabling agent-to-agent orchestration and programmatic run composition). Together these three kinds — webhook, scheduled, and manual — cover the core event model: push from outside, time, and pull/compose from inside.

---

## Problem Statement

### Current State

- The Triggers framework exists and works well for webhooks. The ADR for v1 (`docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md`) explicitly called out scheduled and other trigger kinds as future work, and noted the planned extraction to `orbit-triggers` when the second trigger kind lands.
- Every non-webhook run initiation today still requires a human with a valid platform token to call `POST /api/v1/runs`. There is no way to:
  - Run an agent on a recurring schedule (e.g. nightly report generation, daily health checks).
  - Have one agent start another agent programmatically — agents cannot compose or delegate to each other.
  - Let an external system or script fire a named, pre-configured trigger on demand without knowing the full run API surface.

### User Pain Points

- **No scheduled automation.** Teams running recurring tasks (nightly summaries, periodic data processing, scheduled reviews) must maintain external cron jobs that call Orbit's run API with a human credential — which ties automation to individual accounts and breaks when people leave.
- **Agents can't delegate to other agents.** Multi-step workflows where Agent A completes and wants to start Agent B require a human in the loop or out-of-band scripting. There is no first-class agent-to-agent handoff.
- **No simple programmatic trigger surface.** Integrators who want to fire a run in response to an internal event (a CI pipeline step completing, a build finishing) must construct the full `StartRun` API call. A named, pre-configured trigger gives them a stable, scoped surface that does not require knowledge of agent slugs or prompt construction.

### Business Impact

- Limits Orbit's value proposition for automation-heavy teams — "set it and forget it" workflows are not possible without scheduled triggers.
- Blocks multi-agent orchestration patterns that are a key differentiator for a hosted agent control plane.
- Forces continued reliance on human credentials for automation, undermining the accountability and governance model.

---

## Goals and Objectives

### Primary Goals

1. **Scheduled triggers.** A trigger kind that fires on a cron schedule, starting an agent run with a configured prompt at each tick. No external push required — Orbit drives the schedule internally.
2. **Manual triggers.** A trigger kind that fires when explicitly invoked via a dedicated API endpoint (`POST /api/v1/triggers/{id}/fire`). The caller may be a human, an external script, or — crucially — a running agent. This is the first-class path for agent-to-agent orchestration.
3. **Extract `internal/triggers/` to `orbit-triggers`.** The v1 ADR committed to extracting the triggers package to a dedicated binary when the second trigger kind landed. Scheduled triggers are that second kind; this PRD fulfils that commitment.
4. **Preserve the three pillars.** Every triggered run — webhook, scheduled, or manual — must have a verifiable identity (*Who*), a bounded scope (*What*), and must reuse the existing egress broker for outbound calls (*How*).

### Success Metrics

- A configured scheduled trigger fires automatically at each cron tick and starts the bound agent run without any human action.
- A running agent can call `POST /api/v1/triggers/{id}/fire` and start a downstream agent run that executes concurrently.
- 100% of triggered runs carry an attributable identity traceable to the trigger source.
- The extraction to `orbit-triggers` passes all existing trigger tests with no regressions.
- Cron schedule accuracy: runs start within 60 seconds of the scheduled tick (best-effort; no strict SLA in v1).

### Non-Goals

- **Complex workflow/DAG orchestration.** Manual triggers enable agent-to-agent handoff but not dependency graphs, fan-in, or conditional branching across runs.
- **Dynamic prompt injection from the calling agent.** The prompt template is pre-configured on the trigger; callers may not override it in v1 (prevents arbitrary prompt injection through the trigger surface). Passing contextual data via `variables` (see below) is in scope.
- **Trigger chaining / loops across many agents.** Best-effort loop detection only; full cycle prevention is future work.
- **Email or SMS ingestion triggers.** A plausible future trigger kind; not in scope here.
- **Self-service trigger creation by individual engineers.** v1 remains platform-admin-governed.
- **GitOps / CRD-based trigger declaration.** REST API + UI is the authoring surface.
- **Cron sub-minute resolution.** Standard 5-field cron; minute-level granularity.
- **Missed-tick backfill.** If the scheduler is down during a scheduled window, that tick is skipped — no catch-up.

---

## User Stories

### Scheduled Triggers

**As a** platform admin,
**I want to** create a trigger that runs an agent on a cron schedule (e.g. every weekday at 09:00),
**So that** recurring automation runs reliably without human action or external cron infrastructure.

**Acceptance Criteria:**
- [ ] Creating a scheduled trigger requires: name, cron expression, timezone, agent type, and prompt template.
- [ ] The scheduler fires the trigger within 60 seconds of each tick and records a delivery.
- [ ] Missed ticks (e.g. during downtime) are skipped, not queued for later replay.
- [ ] The trigger can be enabled/disabled without deleting it or its history.
- [ ] Delivery log shows each tick: status (dispatched / skipped / failed), run ID, and any error.

---

**As a** platform admin,
**I want to** inspect the history of a scheduled trigger's fires and the runs they started,
**So that** I can verify the schedule is running correctly and debug failures.

**Acceptance Criteria:**
- [ ] The delivery log for a scheduled trigger shows each tick with its outcome.
- [ ] Run IDs link through to the agent run detail.

### Manual Triggers

**As a** platform admin,
**I want to** create a manual trigger that can be fired programmatically by calling a dedicated API endpoint,
**So that** CI pipelines, external scripts, and other agents can start a run without knowing the full run API.

**Acceptance Criteria:**
- [ ] Creating a manual trigger requires: name, agent type, and prompt template.
- [ ] Firing the trigger via `POST /api/v1/triggers/{id}/fire` starts a run and returns the run ID.
- [ ] The caller's identity (SERVICE token from a running agent, or a PAT) is recorded as the fire event's initiator.
- [ ] Optional `variables` object in the fire request allows callers to pass contextual values that are substituted into the prompt template's `{{ var.key }}` placeholders.
- [ ] The trigger's own identity (its source principal) is `created_by` on the run — not the caller's identity.

---

**As a** running agent,
**I want to** call `POST /api/v1/triggers/{id}/fire` with context from my current run,
**So that** I can hand off work to a downstream specialist agent without requiring a human in the loop.

**Acceptance Criteria:**
- [ ] An agent with `triggers:fire:{trigger-id}` scope can fire the trigger and receive back the new run ID.
- [ ] The downstream run executes under the trigger's own source identity, not the calling agent's identity — scopes do not bleed across agent boundaries.
- [ ] The fire request may pass a `variables` map; values are substituted into the template's `{{ var.key }}` placeholders alongside the standard pre-configured fields.

---

**As a** security reviewer,
**I want to** verify that firing a manual trigger cannot escalate the caller's scope,
**So that** an agent cannot use a trigger to gain capabilities it was not granted.

**Acceptance Criteria:**
- [ ] The `fire` endpoint requires the caller to hold `triggers:fire:{trigger-id}` — a specific, per-trigger scope.
- [ ] The resulting run executes under the *trigger's* source principal, not the caller's; the trigger source's grants bound what the run can do.
- [ ] A caller cannot override the prompt template, agent type, or source identity on a fire request.

### Secondary User Stories

- **As a** platform admin, I want to see all triggers (webhook + scheduled + manual) in the platform-wide Triggers section, with kind clearly shown, so I have a unified governance view.
- **As an** engineer reviewing a run, I want to see "started by scheduled trigger X" or "started by manual trigger Y (fired by agent Z)" in the run detail, so attribution is clear.
- **As a** platform admin, I want the prompt template for any trigger kind to support `{{ var.key }}` substitution from caller-supplied variables, so templates can be parameterised without being caller-controlled.

---

## Proposed Solution

### High-Level Approach

Extend the `Trigger` model with a `kind` discriminant (`webhook` | `scheduled` | `manual`) and kind-specific configuration. The dispatch pipeline — template rendering, identity minting, run initiation, delivery logging — is shared across all kinds; only *what fires the pipeline* differs:

```
Webhook   ──▶  POST /api/v1/webhooks/{sourceID}   ──▶ ┐
Scheduled ──▶  internal cron scheduler             ──▶ ├──▶ [shared dispatch pipeline] ──▶ run
Manual    ──▶  POST /api/v1/triggers/{id}/fire      ──▶ ┘
```

The `orbit-triggers` extraction (committed in the v1 ADR) is included in this work — the second trigger kind is the extraction trigger. The package's internal boundaries are unchanged; only the binary wiring moves.

### Key Components

- **`kind` field on `Trigger`** — discriminant `"webhook"` | `"scheduled"` | `"manual"`. Existing triggers migrate to `"webhook"`.
- **`ScheduleConfig`** — per-trigger config for the scheduled kind: `cron_expression` (5-field), `timezone` (IANA). Stored in a new `schedule_config` JSONB column (null for non-scheduled kinds).
- **Cron scheduler** — an in-process goroutine (or, later, a distributed leader-elected scheduler) that evaluates active scheduled triggers each minute and fires them. Built on the `robfig/cron` library; starts in `orbit-triggers`.
- **`POST /api/v1/triggers/{id}/fire` endpoint** — the manual-trigger fire surface. Authenticated; requires `triggers:fire:{trigger-id}`. Accepts optional `variables` map. Returns run ID.
- **`triggers:fire:{id}` scope** — a per-trigger scope granted to principals that are allowed to fire a specific manual trigger. Issued via the existing policy grant system.
- **`orbit-triggers` binary** — new `cmd/orbit-triggers/` that wires the existing `internal/triggers/` package. The webhook ingestion endpoint, the scheduler, the management API, and the fire endpoint all live here.
- **Variable substitution in templates** — `{{ var.key }}` placeholders resolved from the caller-supplied `variables` map on fire and from the scheduler tick's context on scheduled. Standard `{{ field }}` placeholders from the source adapter continue to work for webhook triggers.

### User Experience

#### Scheduled Trigger Flow

1. Admin creates a scheduled trigger: name, cron `"0 9 * * 1-5"`, timezone `"Australia/Sydney"`, agent type `daily-standup-reporter`, prompt template `"Generate the daily standup report for {{ var.date }}."`.
2. At 09:00 AEST on each weekday, `orbit-triggers` fires the trigger, substituting today's date from the scheduler context.
3. A run starts; admin can inspect the delivery in the Triggers > Deliveries view.

#### Manual Trigger Flow

1. Admin creates a manual trigger: name `"start-pr-summary"`, agent type `pr-summariser`, prompt template `"Summarise PR #{{ var.pr_number }} in repo {{ var.repo }}: {{ var.title }}."`.
2. Admin grants a PAT or agent principal `triggers:fire:<trigger-id>`.
3. An agent (or CI script) calls `POST /api/v1/triggers/{id}/fire` with `{"variables": {"pr_number": "42", "repo": "org/repo", "title": "Fix login bug"}}`.
4. A new run starts for `pr-summariser` under the trigger's source identity; the caller receives the run ID in the response.

### User Interface

Extend the existing two-surface Triggers UI:

**Platform-wide Triggers section:**
- **Sources:** unchanged (webhook sources only; scheduled/manual triggers have no inbound source to configure).
- **All Triggers:** add a `Kind` column. Creating a trigger now shows a kind selector at the top of the form; the form body adapts to the chosen kind (webhook: existing source/event/condition fields; scheduled: cron + timezone; manual: no extra fields beyond name + agent + template).
- **Deliveries:** unchanged — delivery records look the same regardless of kind.

**Per-agent Triggers tab:** add kind badge alongside each trigger row. Creating a trigger from the agent tab still shows the kind selector.

---

## Scope

### In Scope

- `kind` discriminant on the `Trigger` model and database column.
- Scheduled trigger kind: `schedule_config` (cron + timezone), in-process cron scheduler in `orbit-triggers`, missed-tick skip semantics.
- Manual trigger kind: `POST /api/v1/triggers/{id}/fire` endpoint, `triggers:fire:{id}` scope, optional `variables` map, delivery log entry per fire.
- `{{ var.key }}` variable substitution in prompt templates (all trigger kinds).
- `orbit-triggers` binary extraction from `identity` (the committed v1 follow-up).
- Per-trigger `fire` scope provisioning via the existing policy grant system.
- Management UI updates: kind selector, kind column, kind-specific form fields.
- Delivery log: kind-aware display (scheduled shows tick time; manual shows caller identity).
- Migration of existing `webhook` triggers to `kind = 'webhook'`.

### Out of Scope

- Email / SMS / other ingestion-based trigger kinds.
- Distributed leader election for the cron scheduler (single-instance best-effort in v1).
- Caller override of prompt template or agent type on fire request.
- Dynamic scope provisioning for `triggers:fire` via UI (admin must call the grants API directly in v1).
- Missed-tick backfill / catch-up semantics.
- Sub-minute cron resolution.
- Multi-agent DAG / workflow orchestration.

### Future Considerations

- Distributed/HA cron scheduler (leader election via Kubernetes lease or DB advisory lock).
- Allow callers to supply an `initial_message` override on fire (with an admin-configurable gate).
- Self-service creation of manual triggers by agent owners (below platform-admin).
- UI for granting `triggers:fire` scope without needing to call the grants API directly.
- Email ingest trigger kind (receive email → start run).
- Trigger concurrency limits (max N active runs per trigger at once).

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `kind` field on `Trigger`; existing triggers default to `"webhook"` at migration.
- [ ] Scheduled trigger kind: cron expression + timezone; scheduler fires within 60 s of tick; missed ticks skipped.
- [ ] Manual trigger kind: `POST /api/v1/triggers/{id}/fire` endpoint; requires `triggers:fire:{id}` scope.
- [ ] Optional `variables` map on fire; `{{ var.key }}` substitution in all trigger kind templates.
- [ ] `orbit-triggers` binary extraction; existing webhook ingestion and management API fully functional after extraction.
- [ ] Every delivery row records trigger kind so delivery log can display it.
- [ ] Run attribution surfaced in delivery records: `fired_by` identity on manual triggers.

**Should Have (P1):**
- [ ] UI kind selector and kind-specific form fields in both the platform Triggers section and per-agent tab.
- [ ] Per-trigger `triggers:fire:{id}` scope grant API (admin can grant it to an agent or PAT).
- [ ] Delivery log shows caller identity for manual fires.
- [ ] `orbit-triggers` deployment in the standard Helm chart.

**Nice to Have (P2):**
- [ ] UI for granting `triggers:fire` without the raw grants API.
- [ ] Trigger test-fire button in the UI for scheduled and manual kinds (immediately dispatches one tick/fire without waiting).

### Non-Functional Requirements

**Security:**
- `triggers:fire:{id}` is per-trigger — a caller with access to one manual trigger cannot fire others.
- Caller identity is recorded on every manual fire; the resulting run executes under the *trigger's* source identity, not the caller's.
- Variable values passed on fire are treated as untrusted data (substituted verbatim for now; injection hardening follows the same deferred approach as webhook payload fields).
- The `fire` endpoint requires authentication; unauthenticated callers get 401.

**Reliability:**
- Cron scheduler restarts cleanly after pod restart; no tick double-fire within a single minute window.
- Manual fire requests are synchronous to delivery record creation; run start is async (same pattern as webhook ingest).

**Performance:**
- Manual fire endpoint responds (delivery accepted) in < 500 ms p95.
- Cron scheduler tick evaluation does not block the main request path.

**Observability:**
- Every scheduled tick and every manual fire emits a structured log entry with trigger ID, kind, and resulting delivery ID.
- Delivery records are queryable by trigger kind.

---

## Dependencies

### Internal Dependencies

- **`internal/triggers/`** — extended with `kind` field, `ScheduleConfig`, fire endpoint handler, and cron scheduler. *Status: exists (webhook only).*
- **`cmd/identity/`** — the webhook ingestion endpoint and management API migrate out to `cmd/orbit-triggers/`. *Status: exists.*
- **Policy service** — `triggers:fire:{id}` scope issued and enforced via the existing grant mechanism. *Status: exists.*
- **Agent control plane (`orbit-agents`)** — unchanged; accepts `SERVICE`-principal run requests as today. *Status: exists.*
- **Frontend (Terra)** — UI updates for kind selector and kind column. *Status: exists.*

### External Dependencies

- `robfig/cron` (Go library) — standard cron parsing and scheduling. MIT licence; widely used.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Cron double-fire on pod restart | Medium | Medium | Record each tick in `trigger_deliveries` before starting run; unique index on `(trigger_id, scheduled_at)` prevents duplicates within same tick window |
| Manual trigger used to compose unbounded agent chains | High | Low | `triggers:fire` is a scoped grant; each link in a chain requires an explicit grant; loop detection added in v1 (ignore fires attributable to Orbit's own service identities) |
| Prompt injection via `variables` on fire | High | Medium | Same deferred posture as webhook payloads — values substituted verbatim; hardening tracked as follow-up |
| `orbit-triggers` extraction breaks webhook delivery | High | Low | Extraction is a binary re-wiring; the `internal/triggers/` package is unchanged; full test suite runs against the new binary |
| Single-instance scheduler misses ticks under load | Medium | Low | Documented best-effort guarantee; HA scheduler tracked as follow-up |

---

## Alternatives Considered

### Alternative 1: External cron (Kubernetes CronJob) for scheduled triggers

**Description:** Deploy a Kubernetes CronJob that calls `POST /api/v1/runs` on a schedule, using a pre-provisioned SERVICE token.

**Why not chosen:**
- Externalises scheduling to infrastructure that ops must manage separately; no UI, no delivery log, no enable/disable without kubectl.
- Uses a generic run endpoint, not a named trigger — no attribution, no per-trigger scope boundary, no delivery history.
- Undermines the goal of making triggers first-class platform concepts.

### Alternative 2: Reuse webhook source model for manual triggers (fire = synthetic delivery)

**Description:** A "manual" trigger is just a webhook trigger whose source accepts `POST /fire` calls; the signing secret is the fire credential.

**Why not chosen:**
- HMAC signing secret is an awkward credential for agent-to-agent calls; the platform token + scope model is a better fit.
- Conflates two different trust models (external HMAC vs. internal principal scope).
- Complicates the UI (every manual trigger requires a "source" with a secret).

### Alternative 3: Defer scheduled/manual to a separate service (orbit-scheduler)

**Description:** Build a dedicated scheduler service rather than extending `orbit-triggers`.

**Why not chosen:**
- Premature separation; the cron scheduler is a small component that belongs in `orbit-triggers` alongside the other trigger kinds.
- The v1 ADR already committed to `orbit-triggers` as the home for all trigger kinds.

---

## Open Questions

1. **HA scheduler.** In a multi-replica `orbit-triggers` deployment, which replica fires scheduled triggers? v1 answer: single-replica, best-effort. Follow-up: leader election via Kubernetes Lease.
   - **Status:** Resolved for v1 (single-instance); tracked for follow-up.

2. **`fired_by` attribution on the run.** The run's `created_by` is the trigger's source principal. Where do we surface the calling agent's identity for manual fires? Options: a run metadata field, a delivery record field, or a separate audit event.
   - **Status:** Resolved — record `fired_by` in the delivery row; expose it in the delivery API and UI.

3. **`variables` schema validation.** Should variable keys be validated against the template at fire time, or are extra/missing keys silently ignored (same as unknown `{{ field }}` substitutions today)?
   - **Status:** Resolved — silently ignore unknown keys; missing keys render empty (consistent with existing template behaviour).

---

## Timeline and Milestones

### Phases

**Phase 1: `orbit-triggers` extraction + scheduled triggers**
- Extract `internal/triggers/` from `identity` to `orbit-triggers` binary.
- Add `kind` column; migrate existing triggers to `webhook`.
- Implement cron scheduler + `ScheduleConfig`; scheduled trigger CRUD.
- Delivery log records scheduled ticks.

**Phase 2: Manual triggers + variable substitution**
- `POST /api/v1/triggers/{id}/fire` endpoint.
- `triggers:fire:{id}` scope provisioning.
- `{{ var.key }}` substitution in all templates.
- `fired_by` in delivery records.

**Phase 3: UI + hardening**
- Kind selector + kind column in both UI surfaces.
- Kind-specific form fields.
- Deployment in Helm chart.

### Key Milestones

- [ ] PRD Approved: TBD
- [ ] ADR Created: TBD
- [ ] Implementation Started: TBD
- [ ] Phase 1 complete (extraction + scheduled): TBD
- [ ] Phase 2 complete (manual + variables): TBD
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [PRD: Agent Webhook Triggers](./agent-webhook-triggers.md) — the v1 PRD this extends
- [ADR: Agent Triggers and Webhook Ingestion](../adr/2026-06-17-agent-triggers-and-webhook-ingestion.md) — the v1 ADR; committed to `orbit-triggers` extraction when second kind lands
- [ADR: Run and Session Lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md)
- [ADR: Agent Identity and Policy Provisioning](../adr/2026-05-11-agent-identity-and-policy-provisioning.md)
- [ADR: JIT Credential Brokering / Egress Gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md)

### Glossary

- **Trigger kind:** The mechanism that fires a trigger — `webhook` (inbound HTTP push), `scheduled` (cron), or `manual` (explicit API call).
- **Scheduled trigger:** A trigger that fires automatically on a cron schedule.
- **Manual trigger:** A trigger that fires when explicitly invoked via `POST /api/v1/triggers/{id}/fire`.
- **Variables:** Caller-supplied key/value pairs that are substituted into `{{ var.key }}` placeholders in a trigger's prompt template.
- **`triggers:fire:{id}`:** A per-trigger scope that grants a principal the ability to fire a specific manual trigger.
- **`orbit-triggers`:** The dedicated binary that hosts the triggers package (extracted from `identity` when the second trigger kind ships).

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
