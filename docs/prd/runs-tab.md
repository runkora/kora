# Runs Tab - Product Requirements Document

**Status:** Approved  
**Created:** 2026-11-25  
**Last Updated:** 2026-11-25  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, people who start and follow agent runs, tenant admins

---

## Executive Summary

The Runs tab will list every run the viewer can read across all agents, newest first, with one line per run. The list will filter on the server and update live as runs start, change state and get their titles. It will replace Dashboard in the sidebar, and `/` will open it.

---

## Problem Statement

### Current State

The sidebar opens a Dashboard overview of agents, runs, connections and spend. The console has no page that lists every run the viewer can read across agents with filters. A person who wants to find a run opens the agent it belongs to and searches that agent's runs.

### User Pain Points

*   **No single place for work in flight:** A person who follows several agents cannot see all of their runs in one list.
*   **No way to find a run by what it is about:** Run lists cannot be searched by the run's title, agent or starter.
*   **No view of what needs attention:** A person cannot list the runs that are waiting for a message or that failed.

---

## Goals and Objectives

### Primary Goals

1.  A viewer sees the runs of every agent they can read in one list.
2.  A viewer narrows the list with quick filters, agent and source selects, and text search.
3.  The list shows new runs, state changes and titles as they happen.

### Success Metrics

None at this time.

### Non-Goals

*   The chat page redesign.
*   The old dashboard overview.
*   Any notion of a run waiting on a specific person.
*   Presence of other viewers.
*   Plan progress on a run.
*   Child runs.

---

## User Stories

### US-001: See every run in one list

**As a** person who works with agents,  
**I want** one list of every run I can read,  
**So that** I can see what all agents are doing without opening each agent.

**Acceptance Criteria:**

*   [ ] The list shows the same runs the viewer can open one by one. A run the viewer cannot read never appears.
*   [ ] Runs replaces Dashboard in the sidebar, and `/` sends the viewer to Runs.
*   [ ] The list loads more runs a page at a time.
*   [ ] Clicking a row opens the run page.

### US-002: Read a run at a glance

**As a** person scanning the list,  
**I want** each run on one line with its state, title, agent, source and starter,  
**So that** I can tell runs apart without opening them.

**Acceptance Criteria:**

*   [ ] Each row shows the run's state icon.
*   [ ] Each row shows the run's title. The title is the run summary. Before the run starts it reads "Waiting to start", and after that a run with no summary reads "No task summary".
*   [ ] Each row shows the agent as a badge.
*   [ ] An idle run shows the marker "Waiting". A run stopped by its budget shows the marker "Stopped at its budget". No other state has a marker.
*   [ ] Each row shows the run's first source as a chip, such as a pull request or a Slack channel.
*   [ ] Each row shows who started the run: a person or a trigger.
*   [ ] Each row shows when the run started in a fixed right-hand column.

### US-003: Narrow the list

**As a** person looking for specific runs,  
**I want** quick filters, selects and search,  
**So that** I can find runs by state, agent, source or what they are about.

**Acceptance Criteria:**

*   [ ] The quick filters are All, Waiting, Mine, Active and Failed. They show no counts.
*   [ ] Waiting lists idle runs. It does not depend on who is viewing.
*   [ ] Mine lists the runs the viewer started.
*   [ ] Active lists pending, starting, active and completing runs.
*   [ ] Failed lists failed runs and runs stopped by their budget.
*   [ ] An Agent select narrows the list to one agent.
*   [ ] A Source select narrows the list to runs started by people, webhooks, schedules or events.
*   [ ] A search box matches the run's title, the agent's name and the starter's name.
*   [ ] Every filter applies on the server, and pages after the first hold only matching runs.

### US-004: Follow runs live

**As a** person with the list open,  
**I want** the list to change as runs change,  
**So that** I do not reload the page to see new work or results.

**Acceptance Criteria:**

*   [ ] On the first page, a new run that matches the filters appears at the top.
*   [ ] On a later page, a bar reads "N new runs" and loads them when clicked.
*   [ ] Returning to the first page while the bar holds new runs reloads the first page.
*   [ ] A run that changes state updates in its row.
*   [ ] A run that gets its title updates in its row.
*   [ ] A run that no longer matches the filters stays in place until the next load. Rows never move.

### US-005: Start new work

**As a** person on the Runs tab,  
**I want** a "Start work" button on the list,  
**So that** I can go from the list to the chat page to start a run.

**Acceptance Criteria:**

*   [ ] A "Start work" button opens the chat page.

---

## Product Decisions

**PD1: Runs replaces Dashboard for now:** The sidebar shows Runs in the place of Dashboard, and `/` opens Runs. **Accepted cost:** the dashboard overview has no sidebar entry.

**PD2: Waiting is the idle state:** Waiting lists runs whose turn has ended and that wait for a message.

**PD3: No counts on quick filters:** The list pages with cursors and holds no total. **Accepted cost:** a viewer cannot see how many runs a filter holds before choosing it.

**PD4: Rows never move:** A live change updates a row in place and never removes or reorders it. **Accepted cost:** a filtered list can show a run that no longer matches until the next load.

**PD5: People covers runs with no trigger:** The People source lists every run that no trigger started. **Accepted cost:** a run that an agent started without a trigger shows under People.

### Decisions delegated to the ADR

The ADR will decide:

*   How the list receives live changes.
*   How live changes keep to the same visibility as the list.
*   How search matches runs and what it costs.
*   How a run's title reaches the list once it is set.
*   How the list resumes after a lost connection.

---

## Open Questions

None at this time.

---

## Related Documents

*   [Runs list live stream ADR](../adr/2026-11-25-runs-list-live-stream.md)
*   [Run Page PRD](run-page.md)
*   [Run Task Summarization PRD](run-task-summarization.md)
*   [Event Spine PRD](event-spine.md)
