# Generic Webhook Attribution - Product Requirements Document

**Status:** In Review  
**Created:** 2026-11-23  
**Last Updated:** 2026-11-23  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, trigger authors, open source contributors

---

## Executive Summary

Generic webhook attribution will let an admin set which payload fields describe a generic delivery: the item it is about and who caused it. A run that a generic delivery starts or joins will show links back to the item and a summary of the event, the same as a GitHub, Slack or Alertmanager run.

---

## Problem Statement

### Current State

A run started by a GitHub, Slack or Alertmanager delivery links to the pull request, thread or alert and summarizes who did what. Each built-in provider builds these links and summaries in code. A run started by a generic delivery has no links and no summary, and the run page shows the trigger's prompt template in place of the summary.

### User Pain Points

*   **No way back to the item:** A person looking at a run that an issue tracker event started has to find the issue in the tracker by hand.
*   **Attribution needs a built-in provider:** Links and a summary for a new system need a provider written in Go and a kora release.

---

## Goals and Objectives

### Primary Goals

1.  An admin maps payload fields to links on the run without a kora release.
2.  An admin maps payload fields to the event summary: who acted, what they did, and what they acted on.
3.  Attribution is set the same way in the console and in a `TriggerSource` resource.
4.  An admin can change attribution on a source after create.

### Success Metrics

None at this time.

### Non-Goals

*   Mapping a sender to a kora user, or attributing a run to a kora person.
*   Sender restriction on generic sources.
*   Looking up data the payload does not carry from the sending system's API, such as a user's name or a project's short identifier.
*   Repeat detection by a delivery id in the payload.
*   Fallback values in templates.
*   Editing verification or event type settings after create.
*   Editing a declared source in the console.

---

## User Stories

### US-001: Link a run to the item it came from

**As a** tenant admin,  
**I want** to set link templates on a generic source,  
**So that** a run started by a delivery links to the item the delivery is about.

**Acceptance Criteria:**

*   [ ] A generic source takes up to 10 links, and each link has a label template and a URL template.
*   [ ] A template mixes fixed text with field paths in `{{ }}`, e.g. `https://tracker.example.com/projects/{{data.project_id}}/issues/{{data.id}}`.
*   [ ] A field the delivery does not contain resolves to an empty value.
*   [ ] A link whose URL is empty or is not an https URL is left off the run.
*   [ ] A delivery that joins an existing run adds its links to that run.
*   [ ] The run list and the run page show the links.

### US-002: Summarize the event behind a run

**As a** tenant admin,  
**I want** to set who caused an event, what they did and what they acted on,  
**So that** the run page describes the event behind each message.

**Acceptance Criteria:**

*   [ ] The admin sets an actor name template, an actor kind (person, bot or system) and an optional actor URL template.
*   [ ] The admin sets an object label template and an optional object URL template.
*   [ ] The admin maps event types to verbs, e.g. `issue.comment.created` to "commented on".
*   [ ] An event type with no mapped verb uses the event type as the verb.
*   [ ] A delivery whose actor name, verb or object label resolves empty records no summary, and the run page shows the trigger template for it.

### US-003: Set and change attribution in the console

**As a** tenant admin,  
**I want** to set attribution when I create a generic source and change it later,  
**So that** I can correct templates after the first deliveries arrive.

**Acceptance Criteria:**

*   [ ] The generic source form takes links, the actor, the object and the verb map.
*   [ ] The console lets the admin edit attribution on a source created in the console or the API.
*   [ ] A source declared in a `TriggerSource` shows its attribution read-only.

### US-004: Declare attribution in a resource

**As a** platform engineer,  
**I want** to declare attribution in a `TriggerSource` resource,  
**So that** it is reviewed and deployed through GitOps with the rest of the source.

**Acceptance Criteria:**

*   [ ] A `TriggerSource` with provider `generic` carries the same attribution settings as the console.
*   [ ] A change to the resource updates the attribution of the source.

---

## Product Decisions

**PD1: Attribution on the source:** Templates are set once per source and apply to every trigger on the source. **Accepted cost:** every event type on a source shares one set of link and object templates.

**PD2: Payload only:** kora fills templates from the delivery and calls no API of the sending system. **Accepted cost:** a label shows only what the payload carries. A sender whose payload has a user id and no user name cannot show a person as the actor, and its admin sets a fixed actor such as the sender's product name.

**PD3: No fallback values:** A field path the delivery does not contain renders empty. **Accepted cost:** an event type that lacks a field gives a partial label or no summary.

**PD4: Partial edit:** The console edits attribution and shows the rest of the config read-only. **Accepted cost:** an admin who needs a different verification method or event type location creates a new source.

**PD5: One generic icon:** Links from every generic source show the same generic icon, and the label identifies the item. **Accepted cost:** the run page does not show which system a generic link points into.

### Decisions delegated to the ADR

The ADR will decide:

*   How attribution is stored in the source config and validated.
*   The template syntax and how field paths resolve.
*   The length limits on labels and templates.
*   How the API changes attribution and refuses every other config change.

---

## Open Questions

None at this time.

---

## Related Documents

*   [Generic Webhook Source PRD](generic-webhook-source.md)
*   [Run Page PRD](run-page.md)
*   [Run sources ADR](../adr/2026-11-07-run-sources.md)
*   [Run page record ADR](../adr/2026-11-11-run-page-record.md)
