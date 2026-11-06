# Event Spine - Product Requirements Document

**Status:** Approved  
**Created:** 2026-11-06  
**Last Updated:** 2026-11-06  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Engineering, Agent Control Plane, Identity & Policy  

---

## Executive Summary

The event spine is a durable, tenant-scoped record of what happens on the platform: runs starting and failing, triggers pausing, budgets running out, tokens being issued and revoked, and administrators changing grants, roles, agents and triggers. Each event records who caused it. The spine serves three uses with equal weight: a source that triggers can fire on, an audit record of platform activity, and a live feed for administrators and agents through a new Events page and the kora MCP.

---

## Problem Statement

### Current State

*   kora keeps a durable history in two places only. `run_events` records activity inside one run, and the trigger delivery tables record inbound fires and their outcomes.
*   Run lifecycle changes are partly recorded. A run entering `pending` or `starting` writes no event, and some failure paths write none.
*   Terminating a run deletes the run and its event history (`backend/internal/agents/postgres.go`).
*   Changes to grants, roles, budgets, tokens and agents keep the current state only. Nothing records who made a change or what it was before.
*   Identity activity (sign-ins, token issuance and revocation, delegation, tenant switches) appears only in service logs.
*   Triggers fire on external webhooks, schedules and manual fires. A trigger cannot fire on something that happened inside kora.
*   About ten PRDs and ADRs defer audit and event work to an event spine that does not exist. `AGENTS.md` requires every action to emit to it.

### User Pain Points

*   An administrator cannot see platform activity in one place. Answering "what changed and who changed it" means reading service logs.
*   An operator cannot automate a response to a platform condition, such as starting a diagnosis agent when a run fails or a trigger auto-pauses.
*   An agent cannot ask the platform what happened recently. The kora MCP exposes current state only.
*   A terminated run leaves no record.

---

## Goals and Objectives

### Primary Goals

1.  Record platform events durably, with tenant and actor attribution on every event.
2.  Let triggers fire on platform events through the existing trigger pipeline.
3.  Give administrators an Events page with a live feed and history.
4.  Give agents read access to events through the kora MCP.
5.  Keep run history after a run is terminated.

### Success Metrics

None at this time.

### Non-Goals

*   **Policy decision events:** Allow and deny decisions at the tool gateway, egress gateway, policy gateway and edge OPA are a separate pillar.
*   **Outbound delivery:** No event leaves the cluster in v1. Signed customer webhooks and SIEM export are deferred.
*   **Platform-level events:** Operator and system events that belong to no tenant are deferred. This includes changes to virtual models, providers and price overrides, and provider refresh failures.
*   **Git-only resources:** MCP servers, skill marketplaces and connections are defined only in git. Git review is their change record.
*   **Per-category access:** One permission covers all events in v1. Per-category permissions are deferred.
*   **Workflows:** Multi-step workflows built on events are future work. The trigger loop guards in this PRD are their foundation.
*   **Per-call activity:** Individual tool calls, tool results, provider retries and inference requests are not events. `run_events` and the inference usage log keep that record.
*   **Provider degradation:** An event for sustained provider errors over a time window is deferred.

---

## User Stories

### US-001: Platform Activity Feed

**As a** tenant administrator,  
**I want to** see a live feed of platform events with history,  
**So that** I can see what happened on the platform without reading service logs.  

**Acceptance Criteria:**

*   [ ] The Events page shows new events as they happen and pages back through history.
*   [ ] The feed can be filtered by category, event type, agent and principal.
*   [ ] Each event shows its type, time, the principal that caused it and the resource it concerns.
*   [ ] An event detail view shows the full event and links to the related run, trigger or trace.

### US-002: Change Audit

**As a** tenant administrator,  
**I want to** see who created, changed or deleted a platform resource, and what the resource looked like after the change,  
**So that** I can audit configuration changes.  

**Acceptance Criteria:**

*   [ ] Every create, update and delete of those resources records an event with the actor and the resource state after the change.
*   [ ] An update event lists the fields that changed.
*   [ ] No event contains a secret value (token values, signing secrets, OAuth codes).

### US-003: Trigger on a Platform Event

**As a** platform administrator,  
**I want to** create a trigger that fires an agent when a platform event occurs,  
**So that** the platform can respond to its own conditions automatically.  

**Acceptance Criteria:**

*   [ ] A trigger can select an event type and match on event fields with the conditions webhook triggers support today.
*   [ ] A matching event starts a run through the same admission, coalescing and delivery history as other triggers.
*   [ ] The run carries the event as data in its prompt with the same prompt-injection hardening as webhook payloads.
*   [ ] The run executes under the trigger's own identity.

### US-004: Loop Protection

**As a** platform administrator,  
**I want** event triggers to stop themselves from looping,  
**So that** a trigger on "run failed" whose own run fails does not fire forever.  

**Acceptance Criteria:**

*   [ ] Every event records the event that led to it, when there is one.
*   [ ] An event trigger ignores events caused by its own runs by default. An administrator can turn this filter off per trigger.
*   [ ] No trigger fires on an event that is more than five steps down a chain of trigger-caused events.
*   [ ] The event is always recorded. The guards decide only whether a trigger fires on it.

### US-005: Agent Reads Events

**As an** agent,  
**I want to** list and read recent platform events through the kora MCP,  
**So that** I can diagnose and act on what happened.  

**Acceptance Criteria:**

*   [ ] The kora MCP exposes tools to list events with the page's filters and to read a single event.
*   [ ] The tools require the `events:read` permission and return only the agent's tenant's events.

### US-006: Run History Survives Termination

**As a** tenant administrator,  
**I want** a terminated run to keep its record and event history,  
**So that** termination does not erase the audit trail.  

**Acceptance Criteria:**

*   [ ] Terminating a run marks it terminated and keeps the run and its events.
*   [ ] Terminated runs stay out of the default run lists.

### US-007: Retention

**As a** platform operator,  
**I want to** control how long events are kept,  
**So that** storage stays bounded and meets each tenant's audit needs.  

**Acceptance Criteria:**

*   [ ] The chart sets the default retention for the install. The chart default is 30 days.
*   [ ] A tenant's definition can override the retention for its own events.
*   [ ] Events older than the retention period are deleted.

---

## Proposed Solution

### Events

An event records one thing that happened. Every event carries:

*   A unique id, a versioned type and the time it happened.
*   The tenant it belongs to.
*   The principal that caused it (a person, an agent, a trigger or the platform itself) and the principal's type.
*   The delegator, when an agent acted for a person.
*   The run and agent it concerns, when there is one.
*   The trace it belongs to, linked from the trace view.
*   The event that caused it, when there is one.
*   The state of the resource after the change and the list of fields that changed.

Events use the CloudEvents 1.0 standard. A recorded event commits together with the change it describes. Neither is ever recorded without the other.

### Event Catalog

Events record changes of state and outcomes, and each event type has the form `<domain>.<entity>.<event>.v<N>`. The v1 catalog covers these events:

*   **Runs:**
    *   `agents.run.created.v1`: a run was created and queued.
    *   `agents.run.started.v1`: a run began executing.
    *   `agents.run.completed.v1`: a run finished, including a run that ended after its idle timeout.
    *   `agents.run.failed.v1`: a run failed.
    *   `agents.run.budget_exhausted.v1`: a budget ran out and stopped a run.
    *   `agents.run.terminated.v1`: a person or the platform terminated a run.
    *   `agents.run.idle.v1`: a run finished its turn and is waiting for input.
    *   `agents.run.interrupted.v1`: a person stopped the run's turn.
    *   `agents.run.errored.v1`: a turn ended in an error and the run continued.
    *   `agents.run.guardrail_warned.v1`: the loop guard warned the agent.
    *   `agents.run.guardrail_paused.v1`: the loop guard paused the run.
    *   `agents.run.message_received.v1`: a person or a trigger sent the run a message.
*   **Sandboxes:**
    *   `agents.sandbox.created.v1`: a sandbox was provisioned for a run.
    *   `agents.sandbox.replaced.v1`: a run's sandbox was replaced with a different environment or image.
    *   `agents.sandbox.ready.v1`: a sandbox finished provisioning and records whether its egress check passed or was skipped.
    *   `agents.sandbox.failed.v1`: a sandbox failed to provision.
    *   `agents.sandbox.deleted.v1`: a run's sandbox was removed.
*   **Triggers:**
    *   `triggers.delivery.failed.v1`: a fire could not start its run.
    *   `triggers.delivery.denied.v1`: a fire was refused for lack of permission.
    *   `triggers.trigger.autopaused.v1`: a trigger paused itself after repeated failures.
*   **Budgets:**
    *   `inference.budget.threshold_crossed.v1`: spend against a budget passed 50%, 80% or 100% of its limit. Each level fires once per budget period, and the levels are the same for every budget.
    *   `inference.budget.exceeded.v1`: an exhausted budget refused a request.
*   **Identity:**
    *   `identity.session.created.v1`: a person signed in or an agent session started.
    *   `identity.pat.created.v1` and `identity.pat.revoked.v1`: a personal access token was issued or revoked.
    *   `identity.pat.updated.v1`: a personal access token's scopes changed.
    *   `identity.delegation.issued.v1`: a person delegated to an agent.
    *   `identity.tenant.switched.v1`: a person switched tenant.
*   **Administrative changes:** `created`, `updated` and `deleted` for each of these:
    *   `policy.grant`, `policy.role`, `policy.tenant_budget`
    *   `agents.agent`, `agents.environment`
    *   `triggers.trigger`, `triggers.source`
*   **Declared resources:**
    *   `declarative.resource.failed.v1`: a declared resource could not be applied (e.g., a name conflict).

### Event Triggers

A new trigger kind, `event`, joins `webhook`, `scheduled` and `manual`. An event trigger selects an event type, matches on event fields and starts a run under its own identity. Event triggers can be created through the API, the UI and the declarative `Trigger` resource. Event triggers use the delivery history, admission, coalescing and auto-pause that other triggers use.

### Events Page

A new Events page in the dashboard carries US-001 and requires `events:read`. The page layout will be iterated in Storybook before it is built.

### Agent Access

An agent reads events by calling the kora MCP tools from US-005. Triggers remain the way the platform pushes events to agents.

### Retention

Retention from US-007 applies to events only. Run history follows the run's own lifecycle.

---

## Scope

### In Scope

*   The event record, attribution and the v1 catalog.
*   Recording every v1 event together with its change.
*   The `event` trigger kind with both loop guards, across the API, UI and declarative resource.
*   The Events page with live feed, filters and detail.
*   The kora MCP tools `events.list` and `events.get`.
*   The `events:read` permission.
*   Retention set by the chart with per-tenant override.
*   Keeping terminated runs and their history.

### Out of Scope

*   Policy decision events.
*   Delivery of events outside the cluster.
*   Platform-level events with no tenant.
*   Per-category event permissions.
*   Workflows.

---

## Open Questions

None at this time.

---

## Links & References

*   `docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md`
*   `docs/adr/2026-06-20-extended-trigger-types.md`
*   `docs/adr/2026-05-11-agent-event-streaming-protocol.md`
*   `docs/adr/2026-09-29-run-activity-events.md`
*   `docs/adr/2026-04-14-policy-gateway-and-authorisation-model.md`
*   `docs/adr/2026-10-24-declarative-resources.md`
*   CloudEvents 1.0 specification: https://github.com/cloudevents/spec
