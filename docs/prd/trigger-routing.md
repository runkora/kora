# Trigger Routing - Product Requirements Document

**Status:** Draft  
**Created:** 2026-10-19  
**Last Updated:** 2026-10-19  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, trigger authors across engineering teams

---

## Executive Summary

A trigger will be able to route each delivery to the most suitable of several agents and to decide whether the delivery starts a new run or joins a run that is already working. kora makes the choice with a decision model from the tenant's model catalog and asks its own questions. The trigger author picks the candidate agents and a few settings, and every delivery records what kora decided and how sure it was.

---

## Problem Statement

### Current State

A trigger binds a firing condition to one agent. Every delivery that matches it starts a run of that agent with the trigger's prompt. Coalescing can fold a burst of deliveries about one subject into one run before the run starts, and nothing sends a later delivery to a run that is already working.

Some events have a fixed shape and an unknown intent. A GitHub comment that mentions `@kora` always arrives in the same format. The person may want a code review, a fix, a docs change or an answer about an earlier request. A Slack mention would be the same. One trigger can start only one agent and cannot serve that mention.

The platform already serves decision models. A decision model reads a state and typed questions and returns a probability for each answer. The [Decision Models PRD](systemone-decision-models.md) left the classifier primitive to a separate PRD.

### User Pain Points

- **One mention, one agent:** A team that wants a single `@kora` entry point has to pick one agent for it or build a router agent that starts other agents.
- **Follow-ups start fresh:** A follow-up message about work in progress starts a second run that repeats the first run's setup and loses its context.
- **Duplicate triggers:** A team that splits intent by keyword writes one trigger per keyword and keeps the keyword lists in step by hand.

### Business Impact

- A natural-language entry point into kora needs routing to exist.
- Duplicate runs cost inference spend and sandbox time.

---

## Goals and Objectives

### Primary Goals

1.  A trigger author can let kora choose among several agents for each delivery.
2.  A delivery can join an open run of a candidate agent, started by a trigger, when it continues that run's work.
3.  The author configures routing without writing questions or prompts for the decision model.
4.  Routing uses only decision models that the tenant has enabled in its model catalog.
5.  Every routed delivery records the decision, the confidence and the outcome.

### Success Metrics

- A GitHub comment trigger with three candidate agents (code reviewer, coder, docs writer) starts a run of the agent whose description matches the request.
- A follow-up comment on the same issue joins the run that the first comment started, without a decision model call, when the trigger's join key names the issue.
- A delivery that kora is unsure about goes to the configured fallback or starts nothing, and the delivery says which.
- A tenant with no decision model sees why routing is unavailable before configuring anything.

### Non-Goals

- Classifiers that tenants define with their own questions and thresholds.
- A workflow primitive with classifier steps.
- Fetching more context than the delivery carries, such as earlier comments on an issue.
- A Slack webhook provider. Routing works on any webhook source, and a Slack provider is separate work.
- Asking the person who caused the event to clarify when kora is unsure.
- Classifiers in egress or tool policy.
- Joining runs that a person started.

---

## User Stories

### US-001: Route a mention to the right agent

**As a** trigger author,  
**I want** kora to pick the agent for each `@kora` mention from a list I choose,  
**So that** one entry point serves several kinds of request.

**Acceptance Criteria:**

- [ ] The target step offers "Route to one agent" and "Let kora choose".
- [ ] "Let kora choose" requires at least two candidate agents.
- [ ] Only agents the author may run can be candidates. Other agents show the reason they cannot be picked.
- [ ] Each candidate shows its description. A missing or very short description carries a warning that kora routes on it.
- [ ] The chosen agent receives the trigger's prompt.

### US-002: Continue work already in progress

**As a** person working with an agent through GitHub comments,  
**I want** my follow-up to reach the run that is already handling my request,  
**So that** the agent keeps its context and no second run repeats the work.

**Acceptance Criteria:**

- [ ] The author can set a join key, such as the repository and issue number. A delivery whose join key matches an open run of this trigger joins that run.
- [ ] Without a key match, kora can send the delivery to an open run of one of this trigger's candidate agents, started by any trigger, when it judges the delivery continues that run's work.
- [ ] A run that a person started is never joined.
- [ ] The author can turn joining off.

### US-003: Decide what happens when kora is unsure

**As a** trigger author,  
**I want** to choose how confident kora must be and what happens below that,  
**So that** an unclear request does not reach the wrong agent.

**Acceptance Criteria:**

- [ ] Confidence offers Strict, Balanced and Loose. Balanced is the default.
- [ ] Below the threshold, the delivery goes to a fallback agent chosen from the candidates or starts no run.
- [ ] A failed decision model call follows the same rule.

### US-004: See why a delivery went where it did

**As a** trigger author,  
**I want** the delivery detail to show kora's decision,  
**So that** I can fix a bad route by improving agent descriptions or settings.

**Acceptance Criteria:**

- [ ] The delivery detail shows the chosen agent, the confidence and the score of every candidate.
- [ ] It shows whether the delivery started a new run or joined one, and whether a key match decided it.
- [ ] It shows whether the fallback was used and why.
- [ ] It shows the decision model used.

### US-005: Know when routing is unavailable

**As a** trigger author in a tenant with no decision model,  
**I want** to be told what is missing,  
**So that** I know to ask the platform team for a decision model.

**Acceptance Criteria:**

- [ ] "Let kora choose" shows a callout that asks the platform team to enable a decision model in the model catalog.
- [ ] The routing settings are hidden and the step cannot be completed in routing mode.

---

## Proposed Solution

### Routing Mode

The target step of the trigger wizard offers two modes. "Route to one agent" is today's behavior. "Let kora choose" shows the routing settings:

- **Candidate agents:** A multi-select of the agents the author may run, each with its description.
- **Decision model:** A paginated, searchable list of the tenant's catalog models that have the decision capability. The field is required and has no default.
- **Confidence:** Strict, Balanced (default) or Loose, each with one sentence on its effect.
- **When kora is unsure:** Send to a fallback agent from the candidates, or do not start a run. "Ask in the conversation" is shown as coming later.
- **Join open runs:** On by default, with an optional join key that names the subject, such as `{{ repository.full_name }}#{{ issue.number }}`.

### How a Delivery Is Routed

1.  If joining is on and the trigger has a join key, a key match on an open run of this trigger decides first (US-002).
2.  Otherwise kora asks the decision model which candidate agent fits and whether the delivery continues an open run of one of the candidate agents started by a trigger.
3.  If the answer clears the confidence threshold, the delivery starts a run of the chosen agent or joins the chosen run.
4.  If it does not, or the model call fails, the fallback applies.

kora gives the model the trigger's rendered prompt, the candidates' descriptions and a summary of each joinable run. Delivery content is marked as untrusted data in the same way as in a run's first message today.

### Delivery Detail

A Routing section sits above the existing delivery detail. It shows the agent that received the delivery with its confidence, the decision (new run or joined run), the key match, the fallback, the decision model and a score bar per candidate. A failed model call shows the error.

### Designs

The Storybook stories under `Pages/Triggers (proposed)` show the wizard step and the delivery detail in every state above, including no decision models, a search with no match, a low-confidence fallback and a failed model call.

---

## Scope

### In Scope

- "Let kora choose" for webhook triggers.
- Candidate agents, decision model, confidence presets, fallback and joining.
- Join-key joining and model-decided joining.
- The routing record on every routed delivery and the Routing section of the delivery detail.

### Out of Scope

- Routing for scheduled and manual triggers.
- Context retrieval beyond the delivery.
- The "Ask in the conversation" fallback.

---

## Product Decisions

**PD1:** kora owns the questions. The author picks agents and settings and never writes a question or a prompt for the decision model. **Accepted cost:** an author cannot tune routing beyond agent descriptions and the settings.

**PD2:** No platform default model. Models come from the tenant's own catalog. A tenant without a decision model cannot use routing until one is enabled. **Accepted cost:** routing needs a catalog change before first use in most tenants.

**PD3:** The agent description is the routing text. No separate routing field is added. **Accepted cost:** editing an agent's description changes how deliveries route to it.

**PD4:** The join key decides first. A join key match joins the run without a model call. The join key is separate from coalescing. Routing sees every delivery, and none is merged or dropped first. **Accepted cost:** a delivery about the same subject that asks for something new joins the existing run.

**PD5:** A delivery can only reach this trigger's candidate agents. It may start a run of a candidate agent, or join an open run of a candidate agent that any trigger started. Runs people started are never joined. **Accepted cost:** a person cannot continue their own dashboard run from a webhook, and a run of an agent outside the candidates is never joined.

**PD6:** The question version is recorded and not shown. Each decision stores the version of kora's questions in its audit record. The delivery detail does not display it.

### Decisions Delegated to the ADR

The ADR decides the following:

- Where routing runs and how it calls the decision model.
- The questions and the state kora sends.
- The thresholds behind each confidence preset.
- How a delivery joins a run, and the access rule that keeps a joined run within the routing trigger's access.
- The grants a routing trigger holds and who may create one.
- The storage and API of the routing record.

---

## Related Documents

- [Decision Models and the System One API PRD](systemone-decision-models.md)
- [Agent Webhook Triggers PRD](agent-webhook-triggers.md)
- [Trigger Payload Templating PRD](trigger-payload-templating.md)
- [Trigger Fire Coalescing ADR](../adr/2026-10-13-trigger-fire-coalescing.md)
- [Trigger Routing ADR](../adr/2026-10-19-trigger-routing.md)
