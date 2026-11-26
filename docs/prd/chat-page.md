# Chat Page - Product Requirements Document

**Status:** Approved  
**Created:** 2026-11-26  
**Last Updated:** 2026-11-26  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, people who start and follow agent runs

---

## Executive Summary

The chat page will let a person start a run with any agent they can run and then work with that run on the same page. It will use the run page's own frame, list the viewer's own runs beside the work, and replace the Start run modal as the one place where people start work.

---

## Problem Statement

### Current State

People start a run from a Start run modal on the Agents list and on an agent's page. The modal asks for an agent in a dropdown and a task, and then opens the run page. The Runs tab's "Start work" button opens a chat page that is a demo: it answers with canned replies, offers a model picker that does nothing and starts no run.

### User Pain Points

*   **No place to start work:** The only page built for starting work is a demo that starts no run.
*   **Agent first, task second:** The modal asks a person to pick an agent from a bare list before they write what they want done, and the list does not say what each agent does.
*   **No history beside the work:** A person who starts several runs has no list of their own runs next to the run they are working in.

---

## Goals and Objectives

### Primary Goals

1.  A person starts a run with any agent they can run from one page.
2.  A person keeps working with the run on the same page once it starts.
3.  A person returns to their own recent runs from the same page.

### Success Metrics

None at this time.

### Non-Goals

*   Choosing a repository or branch for a run.
*   Choosing a model for a run.
*   Suggested tasks or an agent list under the composer.
*   Runs started by other people or by triggers in the history list.
*   Changes to who may start a run.

---

## User Stories

### US-001: Start a run from one page

**As a** person who works with agents,  
**I want** one page where I write what I want done and send it,  
**So that** starting work is as quick as writing a message.

**Acceptance Criteria:**

*   [ ] An empty chat page shows a heading, the product mark in front of it, and the composer below it. Nothing else shows below the composer.
*   [ ] The heading reads "What should we work on next, <first name>?". A viewer with no name sees "What should we work on next?".
*   [ ] The heading plays once on load: a disc drops into place and splits into the product mark, and the words rise in one by one with the name last.
*   [ ] With reduced motion turned on, the heading and the mark show in their final state with no movement.
*   [ ] A screen reader reads the heading once as one sentence.
*   [ ] The composer's placeholder reads "What should <agent> work on?" for the selected agent.
*   [ ] Enter sends the message, and Shift+Enter adds a line.
*   [ ] Sending the first message starts a run with the selected agent, and the page becomes that run's page without a reload.
*   [ ] As the run page appears, the composer moves to its place at the foot of the run's thread in 280 ms. With reduced motion turned on, it takes its place with no movement.
*   [ ] A run that fails to start shows the error under the composer and keeps the message in the field.
*   [ ] A viewer with no agent sees "No agents to start" and a link to the Agents list.

### US-002: Choose the agent

**As a** person starting work,  
**I want** to choose the agent from the composer and see what each one does,  
**So that** I pick the right agent without leaving the page.

**Acceptance Criteria:**

*   [ ] The agent picker sits in the composer's footer and shows the selected agent's name.
*   [ ] The picker lists every active agent with its name, "Acts as you" or "Acts as itself", and a one-line description.
*   [ ] An agent the viewer cannot start shows as disabled with "No access" in place of who it acts as.
*   [ ] A search field shows when the list holds 8 agents or more. It matches the agent's name and description.
*   [ ] A search with no match reads "No agent matches "<search>"."
*   [ ] The picker shows no model names.
*   [ ] On open, the selected agent is the agent of the viewer's most recent run when the viewer can still run it. Otherwise it is the first agent the viewer can run.
*   [ ] Opening the page from an agent's page selects that agent, also when the viewer cannot run it.
*   [ ] When the selected agent is one the viewer cannot start, the composer is disabled and reads "You cannot start runs of <agent>".

### US-003: Return to my runs

**As a** person who starts several runs,  
**I want** my own runs listed beside the page,  
**So that** I can go back to earlier work and start new work from one place.

**Acceptance Criteria:**

*   [ ] The left pane shows a Home link, a "New run" row and a "Your runs" list.
*   [ ] "Your runs" lists the runs the viewer started, newest first. A run that is working shows a running mark.
*   [ ] With no runs, the list reads "Runs you start appear here."
*   [ ] The run open on the page shows at the top of the list with its subagents under it.
*   [ ] "New run" opens an empty chat page.
*   [ ] Below the widest layout, the pane opens as a drawer: from a "Your runs" button on the empty page, and from the Sessions button on a run.
*   [ ] Reloading a run started from the chat page keeps the "Your runs" pane beside it.

### US-004: Start work from anywhere it starts today

**As a** person on the Runs tab, the Agents list or an agent's page,  
**I want** the start action to open the chat page,  
**So that** every way to start work leads to the same page.

**Acceptance Criteria:**

*   [ ] The Runs tab's "Start work" button opens the chat page.
*   [ ] The Agents list's "New Run" button opens the chat page.
*   [ ] The start action on an agent's row or card on the Agents list opens the chat page with that agent selected.
*   [ ] An agent's "Start Run" button and the start action on its Runs tab open the chat page with that agent selected.
*   [ ] The Start run modal no longer exists.

### US-005: Recognize a new run before it has a summary

**As a** person who just started a run,  
**I want** the run titled by what I asked for,  
**So that** I can tell it apart from my other runs before its summary arrives.

**Acceptance Criteria:**

*   [ ] Until a run a person started has a summary, its title is the first line of its first message, cut to about one line.
*   [ ] Once the summary is set, the title is the summary.
*   [ ] A run a trigger started keeps its current title until its summary arrives.
*   [ ] Other runs in "Your runs" that have no summary keep their current label.

---

## Product Decisions

**PD1: The chat page replaces the Start run modal:** Every start action opens the chat page. **Accepted cost:** starting a run from an agent's page leaves that page, and the notice that explains a delegated run no longer shows before the run starts. "Acts as you" in the picker is the only hint before the run starts.

**PD2: The chat demo is removed:** The demo with canned replies and a model picker goes away with no replacement.

**PD3: No repository selection:** The platform has no repository primitive. **Accepted cost:** a person names the repository in the message.

**PD4: Nothing under the composer:** The empty page shows no suggestions and no agent list. **Accepted cost:** a new viewer finds the agents only through the picker.

**PD5: Agents without access stay in the list:** The picker hides no active agent that the viewer can see. **Accepted cost:** the list is longer for a viewer with narrow access.

**PD6: The page becomes the run page:** After the first message, the page is the run page, and its address is the run's own address. A reload or a copied link opens the run.

**PD7: The first message titles a run until its summary:** The person's own words name a new run before the platform summarizes it. **Accepted cost:** the title changes once the summary arrives, and the other runs in "Your runs" that have no summary keep a generic label.

**PD8: The heading uses the product's own mark:** The mark in the heading is the same on every tenant. **Accepted cost:** a tenant with its own logo sees the product mark in the heading.

### Decisions delegated to the ADR

The ADR will decide:

*   Where the page lives and how it gets the viewer's session.
*   How the page reads the agents and the viewer's own runs.
*   How the page starts the run and becomes the run page without a reload.
*   How the run page shows the "Your runs" pane after a reload.
*   Which shared components the agent picker and the heading reveal use.

---

## Open Questions

None at this time.

---

## Related Documents

*   [Chat page ADR](../adr/2026-11-26-chat-page.md)
*   [Runs Tab PRD](runs-tab.md)
*   [Run Page PRD](run-page.md)
*   [Delegated Agent Identity PRD](delegated-agent-identity.md)
