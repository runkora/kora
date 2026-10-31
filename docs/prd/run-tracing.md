# Run Tracing - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-31  
**Last Updated:** 2026-10-31  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, platform operators, agent developers

---

## Executive Summary

Every agent run will be one trace that follows the run from the event that started it to its last model and tool call. A person looking at a run will open its trace from the run itself, and a person holding a run ID will find the trace by that ID.

---

## Problem Statement

### Current State

kora records traces for its services, and an operator can view them in the trace viewer. A single agent run today appears as about 15 unrelated traces. The run start request, the run itself, each status update the agent sends, the run summary, each step of a trigger fire and each credential lookup are separate traces. Nothing connects a follow-up message to the turn it started. The run start response and the run page give no reference to any trace.

### User Pain Points

- **No end-to-end view:** An operator cannot see one agent session from start to finish. Following a run means guessing which traces belong to it by time and by searching for its ID inside request paths.
- **Lost cause:** A run started by a trigger cannot be traced back to the event that fired it.
- **No way in from the run:** An agent developer debugging a slow or failed run has no link from the run to its trace.

### Business Impact

- Debugging a run takes longer for platform operators and agent developers.
- kora promises that every agent action traces back to the human who initiated it. The trace data does not show that chain for a run today.

---

## Goals and Objectives

### Primary Goals

1.  One trace per run covers the trigger fire, the run start, the wait for capacity, the session, every turn, and every model and tool call the run makes.
2.  A run's trace is reachable from the run in the API and on the run page.
3.  A run's trace is findable by its run ID.

### Success Metrics

None at this time.

### Non-Goals

- Tracing in the browser and the dashboard frontend.
- Service mesh proxy traces.
- Shipping logs to the trace viewer.

---

## User Stories

### US-001: See a run end to end

**As a** platform operator,  
**I want** a run's whole session in one trace,  
**So that** I can see where its time went and where it failed.

**Acceptance Criteria:**

- [ ] The trace starts at the request or trigger fire that started the run.
- [ ] The trace shows each attempt to start the run, the session start, and each status update and summary the session sends.
- [ ] The trace shows every turn, including turns started by follow-up messages.
- [ ] The trace shows each model call and each governed tool call the run makes, and each credential lookup made for them.
- [ ] An outbound internet call from the agent's tools is found by the run's ID.
- [ ] A run is either recorded whole or not recorded.

### US-002: Trace a run back to its trigger

**As a** platform operator,  
**I want** a triggered run's trace to start at the trigger fire,  
**So that** I can see which event caused the run and how long it waited.

**Acceptance Criteria:**

- [ ] A run started by a webhook, a schedule or a manual fire has the fire in its trace.
- [ ] The trigger and the delivery appear on the trace.
- [ ] A fire that replaced an earlier waiting fire shows the replacing fire in the run's trace.

### US-003: Open a run's trace from the run

**As an** agent developer,  
**I want** the run to give me its trace,  
**So that** I can debug it without searching.

**Acceptance Criteria:**

- [ ] Starting a run returns a reference to its trace.
- [ ] Reading a run returns the same reference.
- [ ] The run page links to the trace in the operator's trace viewer.
- [ ] A run that was not recorded shows no trace reference.

### US-004: Find a run's trace by run ID

**As a** platform operator,  
**I want** to search the trace viewer by run ID,  
**So that** I can find a run's trace from an alert, a log line or a support request.

**Acceptance Criteria:**

- [ ] Every part of a run's work carries the run ID in a field the trace viewer can search.
- [ ] The run ID field has the same name on every service.

### US-005: Follow a message into the run

**As an** agent developer,  
**I want** a follow-up message to point to the run it went to,  
**So that** I can go from the message to the turn it started.

**Acceptance Criteria:**

- [ ] The trace of a follow-up message, an interrupt or a stop links to the run's trace.

---

## Proposed Solution

### High-Level Approach

The platform carries one trace through every step of a run, including the steps that wait in a queue and the processes the agent starts. The platform records the trace reference on the run and returns it with the run.

### User Experience

#### User Flow

1.  A developer starts a run through the API or the dashboard, or a trigger fires.
2.  The API response and the run page show the run's trace reference.
3.  The developer opens the link on the run page and sees the run's whole session in the trace viewer.

### Key Components

- Run record: holds the run's trace reference.
- Trace viewer: the operator's existing viewer, unchanged.

---

## Scope

### In Scope

- Runs on the Claude Code runner.
- The run ID as a searchable field.

### Out of Scope

- Codex and OpenCode runners.
- Trace references for runs started before this release.
- Outbound internet calls and sandbox commands as part of the run's trace. A later release covers them.

### Future Considerations

None at this time.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Runs started through the API, the dashboard and every trigger kind each produce one trace.
- [ ] Turns and tool calls the agent's own processes make join the run's trace.

**Should Have (P1):**
- [ ] None at this time.

**Nice to Have (P2):**
- [ ] None at this time.

### Non-Functional Requirements

**Security:**
- The trace reference grants no access. No part of the platform reads it to decide identity, access or credentials.
- The trace reference is visible to anyone who can read the run.

**Observability:**
- A run is sampled once at its start, and the whole run follows that decision.

---

## Dependencies

### Internal Dependencies

- Telemetry collector: agent pods export to the platform collector ([2026-09-18: Telemetry Collector as a Declared Platform Destination](../adr/2026-09-18-telemetry-collector-platform-destination.md)). Implemented.
- Shared span vocabulary: one run ID field name on every service ([2026-03-21: OpenTelemetry Observability Discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md), amendment proposed).

### External Dependencies

- A trace viewer that the operator runs and that kora's collector exports to.

---

## Risks and Mitigations

- **Very large traces:** A long session can produce a trace larger than the trace store keeps. Search by run ID still finds the recorded parts.
- **All or nothing:** A run that was not sampled at its start has no trace for its whole life. The run then shows no trace reference.

---

## Alternatives Considered

### Alternative 1: Separate traces joined by run ID

**Description:** Each step keeps its own trace, and the trace viewer joins them by run ID.

**Why not chosen:**
- The run stays about 15 traces, and the operator still assembles the session by hand.
- Each step can be recorded or dropped on its own, and a run can be partly recorded.

### Alternative 2: A trace per turn

**Description:** Each message to the agent starts its own trace.

**Why not chosen:**
- The agent would restart for every message.
- The run start and the session lifecycle would still be separate traces.

---

## Open Questions

1. **Trace viewer address:** Where does an operator set the address of the trace viewer for the dashboard?
   - **Status:** Resolved. The operator sets it in the platform's deployment configuration.
   - **Owner:** Justin Wilkin

2. **Long sessions:** Should a session that stays open for days stay one trace, or start a new trace after a limit?
   - **Status:** Resolved. A session stays one trace for its whole life.
   - **Owner:** Justin Wilkin

---

## Timeline and Milestones

None at this time.

---

## Appendix

### Related Documents

- [One Trace per Agent Run](../adr/2026-10-31-one-trace-per-run.md): architecture decision
- [2026-09-18: Trace Propagation into the Claude Code Agent Runtime](../adr/2026-09-18-agent-runtime-trace-propagation.md)

### Glossary

- **Trace:** The record of one piece of work across every service it touched, viewed as a tree of steps.
- **Trace reference:** The ID that opens a run's trace in the trace viewer.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
