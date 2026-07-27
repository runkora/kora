# Extended Trigger Types: Scheduled, Manual, and an Operable Delivery Pipeline - Product Requirements Document

**Status:** In Review
**Created:** 2026-07-27
**Last Updated:** 2026-07-27
**Owner:** Justin Wilkin
**Stakeholders:** Platform Engineering, Agent Control Plane, Identity & Policy, Security

---

## Executive Summary

Triggers ships one kind today: an inbound webhook that starts an agent run under a bounded, attributable identity. That proved the model works. It left two gaps. There is no way to start a run on a schedule or on demand, so anything time-based or human-initiated still runs off a laptop or a cron job outside the platform. And the webhook path itself has none of what makes a trigger *operable*: a durable, honest record of every attempt (including the ones that were rejected or never ran), protection against a trigger firing faster than its agent can keep up, and a console that shows the fleet's health instead of a bare list.

This PRD supersedes the earlier draft on this branch and covers the full scope in one spec: **two new trigger kinds** (scheduled and manual) alongside webhook, **exact delivery outcomes** so every way a run can fail to start is named and recorded, **admission control** so a trigger can't flood its agent, **delivery safety** (timeout, duplicate suppression, auto-pause), and a **console redesign** that shows fleet health rather than a list. It also names six further kinds the product commits to accommodating without building them.

The starting position is that the existing webhook implementation is right. It gets extended, not replaced: no change to how webhooks authenticate, no change to who a triggered run is, no loss of existing triggers or history, and no behavior change for any trigger already in use. Where the design reference disagrees with what's already built, §Alternatives Considered records why the existing decision stands.

---

## Problem Statement

### Current State

- Triggers (`internal/triggers/`, hosted in `identity`) supports exactly **one** kind: `webhook`. An inbound signed request from a registered source dispatches a run on the target agent. This works and is in use.
- **There is no scheduled trigger.** Anything recurring — a nightly report, an hourly sync, a weekly dependency sweep — is run by a cron job on someone's machine or a CI schedule, outside the platform's identity and policy model entirely.
- **There is no manual trigger.** Kicking off an agent run on demand with a small structured input means either a fake webhook, a `curl` to the run API with a hand-written payload, or asking whoever owns the agent to do it.
- **Delivery outcomes are coarse.** A delivery is recorded as verified, dispatched, skipped, or failed. There is no way to distinguish "the signature was invalid" from "the agent was unreachable" from "policy denied it" — they all collapse toward `failed`, or aren't recorded as a delivery at all in some rejection paths. An operator debugging "why didn't my trigger fire" cannot tell from the record.
- **No admission control.** A webhook trigger receiving a burst of events dispatches every matching delivery immediately and concurrently. There is no per-trigger concurrency ceiling and no defined behavior for what happens when one would be exceeded.
- **No delivery safety net.** A trigger stuck failing every delivery (bad prompt template, agent misconfigured, expired grant) keeps firing indefinitely — there is no auto-pause. There is no dedupe against a resent event. There is no dispatch timeout separate from the run's own execution.
- **Only an excerpt of each payload is kept.** Enough to identify a delivery, not enough to answer "what exactly did the sender send" — so debugging a bad payload means going back to the sending system.
- **The console is a bare list.** The Triggers page shows rows with no aggregate view (how many are healthy, how much traffic in the last 24h, which ones need attention), no per-trigger delivery history at a glance (sparkline), and the deliveries feed and per-trigger drawer described in the v1 PRD were never built.

### One thing the current model already gets right

A single inbound delivery can match **several** triggers — one GitHub push legitimately starting three different agents. Triggers already records this as one delivery with a per-trigger outcome for each match, and that fan-out is correct: it's how a source and the automations built on it actually relate. Admission control, auto-pause, and the console's badges and counts are therefore **per trigger within a delivery**. A delivery that started two runs and was rejected by a third trigger's concurrency limit has to be able to say exactly that.

That means outcomes live at **two levels**, and conflating them is the main way this design could go wrong:

- **Delivery level** — what happened to the arrival itself, before any trigger was involved: it was a duplicate, a scheduled fire was missed, or nothing was listening for it. There is no trigger to attribute these to. An arrival that fails authentication is a separate case, surfaced on the source rather than as a delivery (see below).
- **Trigger level** — what happened for each trigger that matched: filtered out, denied by policy, superseded, dropped for exceeding a limit, dispatched, or failed to dispatch.

Both levels have to be visible. An admin looking at one trigger needs its own outcomes; an admin looking at the feed needs to see the arrivals that never reached a trigger at all.

**Failed authentication is deliberately not a delivery.** A webhook with a bad signature is an unauthenticated request, so recording it as a delivery would let anyone on the internet create records. Instead the source itself carries a rejection count and the most recent reason, which is what an admin with a wrong signing secret needs to see, without an unauthenticated caller ever creating a row. Everything in the delivery history is therefore an arrival that authenticated.

### User Pain Points

- **Recurring agent work lives outside Orbit.** The one class of automation most teams want first — "run this every night" — is the one the platform can't express, so it stays on infrastructure with none of Orbit's identity, policy, or audit guarantees.
- **Starting a run on demand is a hack.** There's no first-class "here is the input this agent takes, go" surface, so operators improvise.
- **"Why didn't my trigger fire?"** is unanswerable today for anything that isn't a clean dispatch — a policy denial, a filtered-out event type, and a genuine failure all look the same or don't show up at all.
- **A noisy or broken trigger has no circuit breaker.** A webhook source retrying an event, or (once schedules exist) a trigger ticking every minute against a wedged agent, burns budget and clutters run history with no way for Orbit to say "stop, something's wrong here" on its own.
- **Operators can't see the fleet at a glance.** Answering "which of our 40 triggers are unhealthy right now" means opening each one.

### Business Impact

- Scheduled work running outside Orbit is agent activity the platform cannot see, govern, or attribute — the exact gap Orbit exists to close. Every nightly job on a laptop is an ungoverned agent.
- Every trigger failure mode without a clear status is a support/debugging cost paid by whoever owns the automation.
- Without admission control, a burst of webhook traffic can put unbounded concurrent runs on one agent — a cost and reliability risk with no platform guardrail.
- The console gap makes Triggers feel unfinished relative to the rest of the control plane, undermining confidence in using it for anything business-critical.

---

## Goals and Objectives

### Primary Goals

1. **Scheduled triggers.** An agent run can be started on a recurring schedule (cron + timezone), inside the platform, with the same identity and policy guarantees a webhook run gets today.
2. **Manual triggers.** An agent can publish a typed input form and an API path so an operator (or another system) can start a run on demand, without inventing a fake webhook.
3. **Exact delivery status semantics.** Every delivery — accepted, rejected, superseded, or dropped — is recorded with a status that unambiguously explains what happened and why, before or instead of any run starting.
4. **Admission control.** Each trigger gets a ceiling on concurrent runs and a chosen behavior for what happens beyond it: wait, replace, or refuse, so bursty or slow-draining triggers behave predictably.
5. **Delivery safety.** Dispatch timeout, delivery dedupe, and auto-pause after repeated consecutive failures — a trigger that's broken stops making it worse on its own.
6. **A console that shows fleet health.** KPI row, per-trigger sparkline and failure indicator, a cross-trigger deliveries feed, and a drawer (Overview / Deliveries / Settings) that surfaces everything above.
7. **Preserve the three pillars.** Every triggered run — regardless of kind — still has a verifiable identity, a bounded scope, and reuses the existing egress broker. Adding two kinds must not introduce a second, weaker way to start a run.

### Success Metrics

- A scheduled trigger fires within one minute of its cron expression's due time, in the configured timezone, with a run attributable to that trigger's own principal.
- A manual trigger's declared input schema is what the console form and the API both validate against — one definition, two surfaces, no drift.
- A rejected delivery (bad signature, denied by policy, filtered event type, queue full) is always recorded with a status that names the specific reason — never silently dropped, never miscategorized as a generic failure.
- A trigger set to run one at a time and replace what is waiting, receiving three overlapping fires produces exactly one further run, with the superseded deliveries visibly marked as such.
- Ten consecutive failed deliveries auto-disables the trigger; the eleventh delivery attempt is rejected with a distinct status, not silently accepted and left to fail again.
- Every number in the console (KPI values, per-trigger failure badge, 24h delivery count) is computed from the same delivery records a user could query directly — never a separately maintained counter that can drift.

### Non-Goals

- **Control-plane events, chained runs, chat commands, queue/stream ingestion, object-storage watches, monitor/alert triggers.** Surfaced in the "New trigger" picker as Planned so the model accommodates them, not built now. Each is a new adapter onto the existing pipeline, not a new pipeline — see §Proposed Solution.
- **Retry of a failed/denied delivery.** Out of scope for this PRD; the admin-initiated Redeliver action covers recovery as a deliberate act rather than an automatic one.
- **Any change to how or where webhooks are received and authenticated.** See §Alternatives Considered, Alternative 3.
- **Cross-tenant / shared ingest infrastructure changes.** No change to tenancy model.
- **Self-service trigger creation below platform-admin**, or a GitOps/CRD authoring surface. Unchanged from prior PRDs.

---

## User Stories

### New trigger kinds

**US-001**
**As a** platform admin,
**I want to** start an agent run on a recurring schedule I define with a cron expression and a timezone,
**So that** recurring agent work runs inside Orbit's identity and policy model instead of on a laptop or a CI cron.

**Acceptance Criteria:**
- [ ] A trigger can be created with a cron expression and an IANA timezone; the console shows the next fire time in that timezone before I save.
- [ ] The schedule fires within one minute of its due time.
- [ ] DST transitions are handled by the configured timezone, not by UTC offset arithmetic — a 02:30 daily schedule does not skip or double-fire across a transition.
- [ ] The minimum interval is one minute; a more frequent expression is rejected at creation with a clear message.
- [ ] A fire missed during downtime is recorded as missed and skipped by default; catching up is a per-trigger opt-in, visible in the creation wizard rather than buried (PD1).
- [ ] The run is attributed to the trigger's own principal, exactly as a webhook-triggered run is.

---

**US-002**
**As an** agent owner,
**I want to** publish a small typed input form for my agent and a matching API path,
**So that** someone can start a run on demand with the right inputs, without me hand-holding them or them faking a webhook.

**Acceptance Criteria:**
- [ ] A manual trigger declares typed fields — name, type (text, number, boolean, choice), required, description, default (PD2); the console renders a form from that declaration and the API validates against the same one.
- [ ] A run can be started from the console ("Run now") or from `POST` to the trigger's run endpoint.
- [ ] The caller is authenticated and authorized to fire that specific trigger; the resulting run still executes under the trigger's own principal, not the caller's.
- [ ] Invalid input is rejected with per-field errors before any run starts, and is recorded as a delivery.

---

### Delivery status & auditability

**US-003**
**As a** platform admin,
**I want to** see the exact reason a delivery did not start a run — policy denied it, the trigger filtered it out, it was superseded, it exceeded a limit, the trigger was paused, or the dispatch itself failed — and to see separately when a sender is failing authentication entirely,
**So that** I can fix the actual problem instead of guessing from a generic "failed" badge.

**Acceptance Criteria:**
- [ ] Each of those reasons is a distinct, visible outcome — none collapses into a generic failure.
- [ ] Outcomes that belong to a trigger (filtered, denied, superseded, dropped, dispatched, failed) are recorded per trigger, so a delivery that started two runs and was rejected by a third says exactly that.
- [ ] Outcomes that belong to the arrival itself (duplicate, missed, nothing listening) are recorded once on the delivery, since there is no trigger to attribute them to.
- [ ] A sender failing authentication is visible on the source it is posting to, with a count and the most recent reason, without creating delivery records.
- [ ] A policy denial keeps the reason policy gave.
- [ ] "A trigger filtered this out" is distinguishable from "nothing was listening" (PD3).
- [ ] An outcome that means "no run will ever start for this" is visibly terminal, not left looking pending.

---

**US-004**
**As an** auditor,
**I want to** answer "what caused this run, who or what authorized it, and what was the payload" for any run started by a trigger,
**So that** I can account for every automated action the platform took, including ones that were stopped.

**Acceptance Criteria:**
- [ ] Every delivery is recorded *before* any authorization or dispatch decision, so a denial is still auditable.
- [ ] Delivery metadata is kept indefinitely; only the payload body is subject to retention (PD4). A purged delivery still answers "did this fire, and what happened".
- [ ] A run started by a trigger links back to its delivery; the run inspector shows "started by trigger X, delivery Y."
- [ ] Deleting a trigger retains its delivery history; the console can still show what that trigger did, named, rather than an orphaned ID.

### Admission control

**US-005**
**As a** platform admin configuring a webhook trigger,
**I want to** cap how many runs from this trigger can be in flight at once, and choose what happens to the overflow,
**So that** a burst of events doesn't put unbounded concurrent load on the target agent.

**Acceptance Criteria:**
- [ ] The concurrency ceiling is configurable per trigger within a sensible range, and defaults to a value that suits most triggers without needing thought.
- [ ] Overflow behavior is configurable per trigger: **wait** (hold in order, discarding the oldest if the backlog grows too large), **replace** (keep only the most recent waiting fire), or **refuse** (reject immediately and tell the sender).
- [ ] Each kind gets the default that suits it: webhooks wait, schedules replace, manual fires refuse.
- [ ] Admission control is scoped per trigger: two triggers targeting the same agent each get their own ceiling, independent of each other.
- [ ] Disabling a trigger stops it accepting new fires and discards anything still waiting, recorded as dropped rather than vanishing. Runs already started are left alone — stopping a run is the agent's business, not the trigger's.
- [ ] Deleting a trigger does the same, and its delivery history survives it (US-004).
- [ ] The drawer shows how many fires are currently waiting, so a backlog is visible before it becomes a surprise.

### Delivery safety

**US-006**
**As a** platform admin,
**I want to** a trigger that's failing every delivery to disable itself after a run of consecutive failures and tell me,
**So that** a broken integration doesn't burn budget indefinitely before anyone notices.

**Acceptance Criteria:**
- [ ] After 10 consecutive failures, the trigger is disabled and the console says it was auto-paused rather than turned off by a person.
- [ ] The counter resets to zero as soon as a run starts successfully.
- [ ] **A policy denial counts as a failure.** A trigger whose grant was revoked would otherwise be denied forever and never pause, which is exactly the case auto-pause exists for.
- [ ] **Dropped and superseded do not count.** Those are admission control working as configured, not the trigger being broken.
- [ ] A fire arriving at an auto-paused trigger is recorded with an outcome that says the trigger is paused — not silently unmatched, which would look identical to a trigger that was never listening.
- [ ] Auto-pause can be turned off per trigger for triggers that are expected to fail intermittently by design.

---

**US-006a**
**As a** platform admin,
**I want to** a trigger that never gets a response from its agent to give up rather than hang,
**So that** a wedged agent shows as a failure I can see instead of a delivery stuck in flight forever.

**Acceptance Criteria:**
- [ ] A dispatch that doesn't start a run within the trigger's timeout is abandoned and recorded as failed, and counts toward auto-pause.
- [ ] The timeout covers starting the run, not running it — a long-running agent is not a failure.
- [ ] The default is sensible without configuration; the setting exists for triggers whose agents are legitimately slow to start.

---

**US-007**
**As a** platform admin,
**I want to** duplicate webhook redeliveries recognized and not re-run,
**So that** a provider's retry doesn't start a second run for the same event.

**Acceptance Criteria:**
- [ ] When duplicate suppression is on (the default) and the same event has already been handled for this trigger within the retention window, the repeat is acknowledged, linked to the original, and starts no second run.

### Console

**US-008**
**As a** platform admin,
**I want to** see active trigger count, 24h delivery volume, success rate, and how many triggers need attention at a glance,
**So that** I can tell the fleet's health without opening every trigger.

**Acceptance Criteria:**
- [ ] The KPI row's values are computed from the same delivery/trigger records the table below it shows — never a value that can disagree with the rows.
- [ ] A trigger row shows a failure badge derived from its own recent outcomes, not a separately stored flag, and distinguishes auto-paused from manually disabled.
- [ ] A trigger row shows a 24h sparkline of delivery outcomes.

---

**US-009**
**As a** platform admin,
**I want to** a cross-trigger feed of every inbound delivery, with a Redeliver action on anything that failed or was denied,
**So that** I can debug across triggers without navigating into each one, and recover from a transient failure without waiting for the next natural fire.

**Acceptance Criteria:**
- [ ] The deliveries feed shows delivery ID, what arrived, which trigger, how long it took, and the outcome, across all triggers. Fields that only apply to an HTTP arrival (response code) are absent for a scheduled tick rather than shown empty or faked.
- [ ] Redeliver is available on any outcome where no run started and the cause might have been fixed since — failed, denied, dropped. It re-runs the delivery against current authorization, not a replay of the original decision.
- [ ] Redeliver into a disabled or auto-paused trigger is refused with a message saying so, not silently accepted.
- [ ] A redelivery is a new delivery that references the original, not an edit of it. The original outcome stays on the record.

---

**US-010**
**As a** platform admin inspecting one trigger,
**I want to** an Overview (endpoint/schedule/input form + filters + target + 24h stats), a Deliveries tab (expandable payload + the run it started), and a Settings tab (timeout, admission control, safety toggles, delete),
**So that** everything about one trigger's behavior and history is in one place.

**Acceptance Criteria:**
- [ ] The Deliveries tab header is honest about sampling — e.g. "Last 4 of 55 deliveries" — never implies it's showing everything when it isn't.
- [ ] Settings changes take effect on the next delivery, not retroactively on work already in flight.

---

## Proposed Solution

### High-Level Approach

**One way in per kind, one shared path after that.** The webhook path already has the spine: something arrives, it's established as genuine, it's recorded, it's matched against the triggers that care about it, and each match either starts a run or doesn't. That spine is not rebuilt. Two new front doors feed into it — a clock, and a person or system calling an endpoint — and the middle gains the steps that are missing today:

| Step | Today | This PRD |
|---|---|---|
| Something arrives | Only a webhook can | A webhook, a scheduled tick, or a deliberate fire |
| It's recorded | Recorded once established as genuine | Recorded on arrival, so a rejection is still on the record |
| Duplicates | Not detected | A repeat of an event already handled doesn't start a second run |
| Matched to triggers | Fan-out to every matching trigger | Unchanged |
| Authorized | Checked per trigger | Unchanged, but a denial is always written down as such |
| Admitted | Every match dispatches immediately | Subject to the trigger's concurrency limit and overflow behavior |
| Run starts | Dispatched to the agent | Unchanged |
| Outcome recorded | Started / didn't start | The specific outcome, per trigger |

**Acknowledging fast still matters.** A sender gets its answer immediately; the work of matching, admitting, and dispatching does not hold the response open. This is true today and stays true.

**Adding a kind is adding a front door.** Everything in §Planned Trigger Kinds is a new way for something to arrive, not a new pipeline. That is the test for whether this design is right, and it's why the creation wizard shows the planned kinds rather than hiding them.

### Key Components

- **A trigger's kind** — `webhook`, `scheduled`, or `manual` — with the configuration each kind needs, and room for the kinds in §Planned Trigger Kinds to be added later without changing what a trigger *is*.
- **A schedule** — a recurring expression plus a timezone, a preview of the next fire time, and a stated position on what happens to a fire that was due while the platform was down.
- **A manual trigger's declared input** — the fields it accepts, their types, and whether each is required. One declaration, rendered as a console form and enforced by the API.
- **Per-trigger delivery outcome** — an outcome for each trigger a delivery matched, drawn from a vocabulary rich enough to name every way a run can fail to start: rejected as inauthentic, denied by policy, filtered out by the trigger's own event selection, superseded by a newer fire, dropped for exceeding a concurrency limit, or failed on dispatch.
- **Admission control** — a per-trigger ceiling on concurrent runs and a stated behavior when it's reached.
- **Delivery safety** — a dispatch timeout, duplicate suppression, auto-pause after sustained failure, and a retention policy for stored payloads.
- **Console redesign** — KPI row, type filter chips, sparkline + failure-badge table rows, cross-trigger deliveries feed, three-tab drawer (Overview / Deliveries / Settings), and a type-picker-first creation wizard that shows Planned kinds so the roadmap is visible in the product.

### Minimal disruption is a requirement, not a preference

Triggers is in use. Webhook sources are registered, deliveries have history, and the identity model behind them is settled. This work is an extension of that, and the following are explicitly **not** on the table:

- **No change to how a webhook proves it's genuine.** The verification scheme and endpoint shape stay as they are. Re-pointing existing sources at a new URL or re-issuing their secrets is not an acceptable cost for this feature. The design reference proposes a different scheme; it is not adopted, and §Alternatives Considered records why.
- **No change to who a triggered run is.** The two new kinds adopt the existing identity model rather than introducing a second way to start a run. Whatever the ADR settles about the details, the invariant is that a scheduled or manual run is attributable and bounded exactly as a webhook run is.
- **No data loss.** Existing triggers and delivery history carry forward. Existing triggers keep behaving exactly as they do now until someone deliberately changes a setting — every new control ships with a default that preserves today's behavior.
- **No renaming for its own sake.** Where the existing vocabulary already works, it stays. New outcomes are added to it, not substituted for it.

Where the design reference and the existing implementation disagree, the existing implementation is the default and the burden is on the change.

### User Interface

Builds on the existing two-surface model (per-agent tab + platform-wide section); the platform-wide **Triggers** page is where this PRD's redesign lands:

1. **KPI row** — active triggers, 24h deliveries, success rate, needs attention, avg dispatch time. Derived, never hand-set.
2. **Type filter chips + search** — filter by kind, search across name/source/agent.
3. **Triggers table** — type icon, name + failure badge, source, fires-on, target agent, 24h sparkline, last fired, inline enable/disable. Pagination: 12/25/50 rows; filter or search resets to page 1.
4. **Recent deliveries feed** — cross-trigger, with Redeliver on failed/denied rows.
5. **Trigger drawer** — Overview (kind-specific config + filters + target + 24h stat trio) / Deliveries (expandable payload + run link) / Settings (timeout, admission control, safety toggles, delete).
6. **Creation wizard** — type picker (Available: Webhook/Schedule/Manual; Planned: the deferred kinds, visibly marked) → typed wizard → success screen.

---

## Scope

### In Scope

- Two new trigger kinds: **scheduled** (recurring expression + timezone, next-fire preview, missed-fire behavior, one-minute minimum interval) and **manual** (declared input, console form, authenticated fire).
- Per-trigger delivery outcomes covering every way a run can fail to start; every rejection is recorded, not silently discarded.
- Admission control: a concurrency ceiling per trigger and a chosen overflow behavior.
- Delivery safety: dispatch timeout, duplicate suppression, auto-pause on sustained failure.
- Full payload capture, kept 30 days and shortenable per tenant (PD4), replacing today's excerpt-only record.
- Redeliver: an explicit admin action on a delivery that didn't start a run.
- Console: KPI row, sparklines, failure badges, deliveries feed, three-tab drawer, creation wizard with the Planned-kinds picker.
- Carrying existing triggers and delivery history forward unchanged.

### Out of Scope

- The six kinds in §Planned Trigger Kinds — the model accommodates them and the product acknowledges them; none is built here.
- Automatic retry of a delivery that failed (distinct from duplicate suppression and from the admin-initiated Redeliver).
- Changing how webhooks are authenticated or where they're received.
- Waiting synchronously for a manually fired run to finish before responding.
- Any change to who may create or manage a trigger.

### Future Considerations

- The six kinds in §Planned Trigger Kinds.
- A multi-agent workflow engine as a separate pillar, with triggers able to start a workflow rather than only an agent.
- Notification for auto-pause beyond the console (PD5).
- Retry with backoff, once there's evidence that Redeliver is being used as a manual substitute for it.

---

## Planned Trigger Kinds

Six further kinds are **designed for but not built**. They appear in the creation wizard's type picker, visibly marked as planned and not selectable. This is deliberate: it tells an admin the roadmap without promising a date, and it holds the design honest — if adding one of these would require reworking the delivery model, the model is wrong now.

Each is a new way for something to arrive. None changes what happens afterwards.

| Kind | Fires when | Why it's deferred |
|---|---|---|
| **Control-plane event** | Something happens inside Orbit itself — a run finishes, a budget threshold is crossed, a grant is revoked, a policy denies an action | Needs a stable, documented catalogue of platform events before admins can bind to them; publishing that catalogue is a commitment to keep it |
| **Chained run** | Another agent's run reaches a given outcome | The most requested and the most dangerous: chains can loop. Needs depth limits and cycle detection designed properly, not bolted on |
| **Chat command** | Someone invokes the agent from Slack or Teams | Depends on a chat integration Orbit doesn't have yet; the trigger is the small part |
| **Queue or stream message** | A message lands on a subscribed queue or topic | Requires outbound connection management and consumer lifecycle — a different operational shape from everything else here |
| **Object storage change** | A file appears or changes in a watched bucket or prefix | Practically a webhook from the storage provider; blocked mainly on credential brokering for the watch itself |
| **Monitor or alert** | An external monitoring system fires an alert | Closest to shipping — Alertmanager is already a recognised webhook provider, so this is largely a presentation and filtering question rather than a new mechanism |

### Triggers are a front door, not an orchestrator

The unifying idea is that Triggers owns **the ways something can start** in Orbit. It does not own what happens next.

Today the only thing a trigger can start is an agent run. A multi-agent workflow engine is a separate future pillar, and when it exists a trigger should be able to start a workflow the same way it starts an agent now — same kinds, same delivery record, same authorization, different target. Nothing in this work should make that a rewrite.

That boundary is what keeps this feature from sprawling. Sequencing, branching, retries between steps, and passing state from one agent to the next are workflow concerns. A trigger fires once and records what happened.

Three consequences for this PRD's scope, all deliberate:

1. **A trigger's target is "something the platform can start", not "an agent".** Nothing in the model, the API, or the console should assume otherwise, even while an agent is the only available answer.
2. **The delivery record must not assume its origin was an HTTP request.** A scheduled tick and a chained run have no sender, no signature, and no response code. The vocabulary for outcomes has to work for all of them, which is why it's expressed in terms of what happened to the run rather than what was returned to a caller.
3. **The trigger's shape must not assume a source.** Webhook triggers hang off a registered source; scheduled, manual, and most of the planned kinds do not. Anything that treats a source as mandatory will have to be undone later.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Scheduled triggers: recurring expression + timezone, next-fire preview before saving, one-minute minimum interval, correct across DST transitions, explicit missed-fire behavior.
- [ ] Manual triggers: a declared input that drives both the console form and API validation; fire from either; the run is attributed to the trigger, not the caller.
- [ ] Every outcome in §Key Components is recorded distinctly, per trigger, including for deliveries that never started a run.
- [ ] A trigger's concurrency ceiling and overflow behavior are enforced exactly as US-005 specifies, per trigger only (PD6).
- [ ] A dispatch that exceeds the trigger's timeout is abandoned and recorded as failed (US-006a).
- [ ] Auto-pause disables a trigger after sustained consecutive failure, resets on success, and is surfaced in the console (PD5).
- [ ] Console KPI row, sparkline and failure badge, deliveries feed, and the drawer's three tabs, all backed by the same records an admin could query directly.

**Should Have (P1):**
- [ ] Duplicate suppression within the retention window.
- [ ] Event filters chosen from a curated per-provider list rather than typed (PD7).
- [ ] Redeliver on a delivery that didn't start a run.
- [ ] Full payload capture, retained 30 days by default, shortenable per tenant, purged automatically (PD4). Delivery metadata is kept indefinitely.
- [ ] Creation wizard with the Planned-kinds picker.

**Nice to Have (P2):**
- [ ] Notification when a trigger auto-pauses, beyond the console — deferred pending evidence that pauses go unnoticed (PD5).

### Non-Functional Requirements

**Security:**
- No triggered run, of any kind, is attributable to less than it is today. Adding kinds must not create a path to start a run that is checked less rigorously than the webhook path.
- Redeliver is evaluated against current authorization, never replayed from the original decision — a revoked grant must stop a redelivery.
- Full payload capture means potentially sensitive sender data at rest. Retention must be enforced automatically, not left to manual cleanup.

**Reliability:**
- A restart must not cause a schedule to double-fire, and must not leave a trigger's concurrency accounting permanently wrong.
- A trigger's settings taking effect on the next delivery is acceptable; silently applying them to in-flight work is not.

**Performance:**
- The sender-facing acknowledgement stays fast enough that no provider treats it as a timeout, under realistic burst load.

**Observability:**
- Every outcome and every admission decision is traceable to a single delivery, so "why didn't this fire" is answerable from logs as well as from the console.

---

## Dependencies

### Internal Dependencies

- **Triggers** — the existing feature, extended. *Status: exists, webhook-only.*
- **Policy** — unchanged evaluation, but a denial must now always leave a record. *Status: exists.*
- **Agent control plane** — unchanged dispatch target for all three kinds. *Status: exists.*
- **Console (Terra)** — the Triggers page, redesigned. *Status: exists, to be extended.*

### External Dependencies

- None new. Schedule evaluation needs a cron implementation; the platform already has one available.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Admission control silently drops deliveries that should have waited | High | Medium | Per-mode behavior is specified exactly in US-005 and is the acceptance bar, not left to implementation judgement |
| Auto-pause disables a trigger failing for a transient reason | Medium | Medium | The counter resets on any success, and auto-pause is a per-trigger opt-out for triggers expected to fail intermittently by design |
| A schedule double-fires or is missed across a restart or a DST transition | Medium | Medium | Timezone correctness is an explicit acceptance criterion, and missed-fire behavior is a deliberate per-trigger setting rather than an emergent one |
| The two new kinds end up with weaker authorization than the webhook kind | High | Low | All three kinds share one path by construction; the ADR must state the identity model for each kind explicitly rather than leave it implied |
| Console numbers drift from the underlying records | Medium | Low | KPIs and badges must be derived from the same records the table shows, never separately maintained counters |
| Full payload capture becomes an unmanaged store of sensitive third-party data | Medium | Medium | Retention is automatic and tenant-shortenable (PD4), and payload capture replaces the excerpt rather than sitting alongside it. A per-trigger opt-out of storing payloads is the first thing to add if a tenant needs it |
| The delivery model quietly assumes an HTTP origin, blocking the planned kinds | Medium | Medium | Called out as an explicit design constraint in §Planned Trigger Kinds; the outcome vocabulary is defined in terms of the run, not the response |
| The curated event-type list goes stale and blocks a legitimate new provider event | Medium | Medium | Accepted cost of PD7. The ADR must make the list updatable without a release, or the risk becomes a support burden rather than a maintenance one |
| Forty concurrent runs reach one agent because ceilings are per trigger only | Medium | Medium | Accepted gap under PD6. The ADR must verify the agent's own limits bound concurrent runs rather than request rate, and state plainly if they don't |

---

## Alternatives Considered

### Alternative 1: Ship the two new kinds only, defer the pipeline and console work

**Description:** Add scheduled and manual triggers to the existing pipeline as-is; leave delivery status, admission control, safety, and the console for a later cut.

**Why not chosen:** Scheduled triggers are precisely what makes the missing guardrails dangerous. A cron ticking every minute against a wedged agent, with no admission control, no auto-pause, and a console that can't show it's failing, is a worse position than not having schedules at all. The safety work is a precondition for the kinds, not a follow-up to them.

### Alternative 2: Ship the pipeline and console work, defer the two new kinds

**Description:** Harden the webhook path only; add schedules and manual fires later.

**Why not chosen:** It leaves the headline user need — "run this every night, inside Orbit" — unmet for another cycle, while the hardening work has no new consumer to prove it against. The two land better together.

### Alternative 3: Adopt the design reference's model wholesale, including its webhook scheme

**Description:** Take the design reference literally, including its proposed change to how webhooks are addressed and authenticated.

**Why not chosen:** Existing decisions stand unless shown to be wrong, and nothing in the design reference argues that the current scheme is. Changing it would force every registered webhook source to be re-pointed and re-secreted — a real, immediate cost to users of the feature, paid for a change with no demonstrated benefit. Revisit if a concrete problem with the current scheme actually surfaces.

### Alternative 4: Start the data model over

**Description:** Rebuild the trigger and delivery records cleanly against the new model rather than carrying the existing ones forward.

**Why not chosen:** There are registered sources and delivery history in use. Every addition here has a default that preserves current behavior, so a reset buys nothing a carry-forward doesn't, and it destroys exactly the audit trail Triggers exists to produce.

### Alternative 5: Flatten the delivery model to one trigger per delivery

**Description:** Follow the design reference in treating a delivery as belonging to a single trigger, which would make status a single value and simplify the console considerably.

**Why not chosen:** It's not true. One inbound event legitimately matches several triggers, and the current model already handles that correctly. Flattening it would either lose fan-out or duplicate the payload once per matched trigger. The extra complexity in the console is the honest cost of a real behavior.

---

## Product Decisions

Seven questions the design raised, settled here so the ADR doesn't have to guess. Each records the trade-off accepted, not just the choice.

**PD1 — A missed schedule is skipped by default.** A fire due during downtime is recorded as missed and does not run on recovery; catching up is a per-trigger opt-in. An agent doing real work twice is worse than not doing it, and a long outage on a frequent schedule would otherwise dump a burst of runs at once. **Accepted cost:** a schedule that genuinely must not skip work needs its owner to know the setting exists, so the creation wizard has to surface it rather than bury it.

**PD2 — A manual trigger declares typed fields, nothing richer.** Name, type (text, number, boolean, choice), required, description, default. That covers the intended use and renders as a plain form. **Accepted cost:** no constraints beyond type, so a trigger needing "must match this pattern" validates inside the agent instead. Revisit only if that turns out to be common; the next step up needs an authoring UI and a validation-error experience, which is a different product.

**PD3 — "Filtered" and "unmatched" are distinct outcomes.** Filtered means a trigger was listening to this source but its event selection excluded the event. Unmatched means nothing was listening at all. They have different fixes — one is a filter setting, the other is a missing trigger — so collapsing them puts the admin back to guessing. **Accepted cost:** one more outcome in the vocabulary and one more badge in the feed.

**PD4 — Payloads are kept 30 days, and a tenant can shorten that.** 30 days is a workable debugging window; a tenant handling sensitive data can set it lower. Retention is enforced automatically, never by manual cleanup. Delivery metadata — what fired, what happened, which run started — is **kept indefinitely** and outlives the payload body, so a purged delivery still answers "did this fire, and what happened" for an auditor. **Accepted cost:** a tenant cannot extend payload retention beyond 30 days, and cannot yet exempt a specific trigger from storing payloads at all. If a trigger handling PII needs that, it's the first thing to add.

**PD5 — Auto-pause is surfaced in the console only.** The failure badge and the "needs attention" KPI already put a paused trigger in front of anyone looking at the page. **Accepted cost:** an admin who isn't looking won't know until they look. Deliberate: notification needs a channel decision and new plumbing, and there's no evidence yet that pauses go unnoticed. Add it when there is.

**PD6 — Concurrency is capped per trigger, not per agent.** Ten triggers each capped at four can still put forty runs on one agent, and this PRD accepts that. **Accepted cost:** a genuine gap, mitigated by the agent's own limits — the ADR must verify those actually bound concurrent runs rather than request rate, and say so plainly if they don't. Two interacting ceilings would also mean a delivery could be rejected for a reason invisible on the trigger the admin is looking at, which is worth avoiding until the gap is real.

**PD7 — Event filters are picked from a curated per-provider list.** Typing an event type invites a silent typo that makes a trigger never fire, with no feedback until someone notices nothing happened. **Accepted cost:** Orbit commits to maintaining the list as providers add event types, and a stale list will eventually block a legitimate event. The ADR should account for that — a list that can't be updated without a release is a liability.

### Decisions delegated to the ADR

Technical decisions this PRD deliberately does not make, listed so they aren't lost: how schedules are evaluated safely when more than one instance is running; the concrete outcome vocabulary and its state transitions; how a manual trigger's fire permission is expressed in the platform's scope vocabulary; whether work waiting on a concurrency limit survives a restart; how the two new kinds obtain their identity; how the per-provider event catalogue is kept current; and how existing records carry forward. Where the ADR revisits an earlier decision rather than building on it, it must say so and justify it.

---

## Timeline and Milestones

### Phases

**Phase 1: Domain model & pipeline invariants**
- Trigger kinds and their per-kind configuration, carrying existing records forward.
- The two-level outcome vocabulary; every rejection recorded before the decision that caused it.
- Duplicate suppression.

**Phase 2: The two new kinds**
- Schedules: recurring expression, timezone, next-fire preview, missed-fire behavior.
- Manual trigger: input schema, fire endpoint, authorization.

**Phase 3: Admission control & delivery safety**
- Concurrency ceiling and overflow behavior.
- Timeout, auto-pause, payload retention.

**Phase 4: Console redesign**
- KPI row, sparklines, failure badges, deliveries feed, three-tab drawer, creation wizard.

### Key Milestones

- [ ] PRD Approved: TBD
- [ ] ADR Created: TBD
- [ ] Implementation Started: TBD
- [ ] Phase 1 complete (model + pipeline): TBD
- [ ] Phase 2 complete (scheduled + manual): TBD
- [ ] Phase 3 complete (admission control + safety): TBD
- [ ] Phase 4 complete (console): TBD

---

## Appendix

### Related Documents

- [PRD: Agent Webhook Triggers](./agent-webhook-triggers.md) — the v1 PRD; its identity/scope/webhook-auth decisions are preserved by this PRD, not revisited.
- [ADR: Agent Triggers and Webhook Ingestion](../adr/2026-06-17-agent-triggers-and-webhook-ingestion.md) — v1 ADR.
- [ADR: Extended Trigger Types: Scheduled and Manual](../adr/2026-06-20-extended-trigger-types.md) — the earlier ADR draft on this branch; this PRD's companion ADR replaces it in place, taking up the decisions above.
- [ADR: Run and Session Lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md)
- [ADR: JIT Credential Brokering / Egress Gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md)
- Design reference: `Triggers.dc.html`, claude.ai/design project `24c98b96-a1bc-4ba2-b744-3e363f9587ca` — the console layout this PRD's UI section describes.

### Glossary

- **Delivery:** A single arrival — an inbound webhook, a scheduled tick, or a manual fire — and its recorded outcomes. One delivery can produce an outcome per trigger it matched.
- **Admission control:** The per-trigger ceiling on concurrent runs, and the chosen behavior when it is reached.
- **Overflow behavior:** wait (hold in order), replace (keep only the most recent waiting fire), or refuse (reject immediately).
- **Auto-pause:** Automatically disabling a trigger after 10 consecutive failures, where a policy denial counts as a failure and an admission-control rejection does not.
- **Redeliver:** An explicit admin action that re-runs a past delivery as a new one, re-evaluated against current authorization — distinct from automatic retry, which is out of scope.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
