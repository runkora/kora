# Run Page - Product Requirements Document

**Status:** Approved  
**Created:** 2026-11-11  
**Last Updated:** 2026-11-11  
**Owner:** Platform Team  
**Stakeholders:** Platform Team  

---

## Executive Summary

The run page shows one agent run: what started it, what the agent did, and what it cost. The redesign makes the page read as a record of work. It shows trigger messages as the events that caused them, lists everyone involved, and stops a run with a clear reason when its budget runs out. It works on a desktop, an iPad and a phone.

---

## Problem Statement

### Current State

The run page shows a trigger's message as the raw prompt the trigger rendered for the model. A person's message carries no sender. Tool calls render out of order, and rows drop the command, path or result that matters. The context strip, mode pills and approval prompt do nothing. The top bar breaks below 1200px, and the page has no layout for an iPad or a phone. A run stopped by its budget says only that it ended.

### User Pain Points

*   A reader cannot tell who or what started a run, or who steered it later, without opening the prompt.
*   A GitHub, Slack or Alertmanager message reads as a wall of `Key: value` lines.
*   Nothing on the page says whose budget stopped a run, or when that budget resets.
*   The page is unusable on an iPad in portrait and on a phone.

### Business Impact

*   Operators open the trace or the delivery list to answer questions the run page should answer.
*   A run that stops on budget looks like a failure.

---

## Goals and Objectives

### Primary Goals

1.  Show every message as who sent it, what happened and what it was about.
2.  Show the run's state, its spend and the reason it stopped.
3.  Keep the page readable at every width from a phone to a desktop.

### Success Metrics

None at this time.

### Non-Goals

*   A list of files the run changed.
*   Live status of linked objects. A preview shows the object as it was when the event arrived.
*   A timeline of the run's turns.
*   The policy decision for each tool call.

---

## User Stories

### US-001: See what started a run

**As an** operator,  
**I want to** see the event that started a run,  
**So that** I understand why the agent is working.

**Acceptance Criteria:**

*   [ ] A trigger's message shows one line: who acted, what they did and a link to the object.
*   [ ] A preview under the line shows the object's title and facts, such as a pull request's repository, branch, lines added and removed, and state.
*   [ ] Text a person wrote, such as a comment or an issue body, shows as a quote.
*   [ ] The rendered prompt opens from a toggle under the event.
*   [ ] A generic webhook shows the trigger template's first sentence and the template's own fields.

### US-002: See who steered a run

**As an** operator,  
**I want to** see who sent each message to a run,  
**So that** I know who changed what the agent did.

**Acceptance Criteria:**

*   [ ] A person's message shows the sender's name.
*   [ ] A message a trigger sent to a working run is marked as joined.
*   [ ] The rail lists everyone who started or messaged the run, including people outside the platform who acted through a trigger.

### US-003: Steer or stop a working run

**As a** person who can message a run,  
**I want to** write a message or stop the agent from the page,  
**So that** I can redirect the run without leaving it.

**Acceptance Criteria:**

*   [ ] The composer shows the step the agent is running and the messages waiting for its next step.
*   [ ] The message field starts at three lines and grows as text is typed.
*   [ ] Stop replaces Send while the agent works and the field is empty.

### US-004: Understand a budget stop

**As an** operator,  
**I want to** see which budget stopped a run and when it resets,  
**So that** I can decide whether to raise it or wait.

**Acceptance Criteria:**

*   [ ] The page states whose budget stopped the run, its period, its limit and when it resets.
*   [ ] A person who can change budgets sees a link to raise it.

### US-005: Follow a run on an iPad or a phone

**As an** operator away from a desktop,  
**I want to** read and steer a run on an iPad or a phone,  
**So that** I can respond without a laptop.

**Acceptance Criteria:**

*   [ ] The thread takes the full width below 1024px.
*   [ ] The sessions list opens from a button below 1280px.
*   [ ] The run's details open in a side sheet on an iPad and a bottom sheet on a phone.

---

## Proposed Solution

### High-Level Approach

The page has three panes: earlier sessions on the left, the run as a thread in the middle, and the run's details on the right. Panes fold into a drawer and a sheet as the width shrinks.

### User Experience

The page will show:

*   A header with the run's source links, branch, title, actions, elapsed time, spend and state.
*   A thread in a reading column: trigger events, people's messages, the agent's text, and tool calls as quiet blocks.
*   A composer that floats over the foot of the thread while the run is live.
*   A rail with the people involved, the run's links, what the run runs on and what it spent.
*   An end line for a finished run: the result for a completed run, and the budget, its reset and a Raise budget action for a run stopped by its budget.

The designs are in Storybook under `Pages/Agents/Run (mix)`.

---

## Scope

### In Scope

*   The redesigned run page at desktop, iPad and phone widths.
*   Recording the sender of every message sent to a run.
*   Recording a summary of the event behind each GitHub, Slack and Alertmanager trigger message.
*   Recording the budget that stopped a run.

### Out of Scope

*   The items under Non-Goals.

### Future Considerations

*   Read changed files from the run's workspace.

---

## Requirements

### Functional Requirements

*   The page works the same for Claude Code, Codex and OpenCode runs.
*   A trigger message with no event summary renders from the trigger's name and its rendered prompt.
*   A connector shows the resources it can write. Its full access list opens on demand.
*   Green marks an open, merged, completed or resolved object, and red marks a failure, a firing alert, requested changes or a spent budget. No other status uses color.
*   The Raise budget action shows only to a person who holds `platform:budgets:write`.
*   Trace sits in the run's actions menu at every width.
*   A message sent before senders were recorded shows no sender name.
*   Token counts read the same in the rail and the end line: tokens in, tokens out and cached tokens.

### Non-Functional Requirements

None at this time.

---

## Dependencies

### Internal Dependencies

*   Run sources (`docs/adr/2026-11-07-run-sources.md`).
*   The trigger kind recorded on a run.
*   The agent harness protocol (`docs/adr/2026-11-07-acp-agent-harness-protocol.md`).

---

## Open Questions

None at this time.
