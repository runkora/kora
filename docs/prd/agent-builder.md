# Agent Builder - Product Requirements Document

**Status:** Implemented  
**Created:** 2026-07-17  
**Last Updated:** 2026-07-17  
**Owner:** Platform Engineering  
**Stakeholders:** Platform Admins, Tenant Users, Platform Engineering

---

## Executive Summary

Creating an agent in kora today means filling in one long form of eight configuration sections presented all at once, with nothing to indicate which decisions carry governance weight and which are cosmetic. This replaces it with a five-step guided builder that asks for one class of decision at a time, explains what each is for at the point the user makes it, and shows a complete review before anything is created. The create flow is the first substantial thing a new user does with kora, and it should teach the platform's model rather than merely collect values.

---

## Problem Statement

### Current State

- `/agents/new` presents eight sections in a single scrolling form: identity, behaviour, tools, connectivity, connectors, MCP tools, capacity, and budget.
- Every section is presented with equal visual weight. The display name and the tool grants look equally consequential.
- There is no review. The user submits and the agent is created.
- Validation runs only on submit, so errors surface after the user has filled in everything.
- Nothing in the form explains what an agent's identity is, what its grants mean, or that access is checked before execution rather than after.
- Leaving the page loses all work.
- When the supporting data behind a field fails to load, the field renders empty with no explanation, and the user cannot tell the difference between "no models available" and "the model list failed to load".

### User Pain Points

- A user creating their first agent has no way to know what a good configuration looks like, or which fields are load-bearing for security.
- Tool and scope selection is the most consequential decision in the flow, and it is presented as a flat list with the same prominence as the description field.
- Users cannot see what they are about to create before they create it, so the first sight of the full configuration is on the detail page after the agent exists.
- Abandoning the form halfway, or losing the tab, discards everything.
- The form asks for connectivity, connectors, and environment separately without conveying that they interact, so a user can configure network access and then pick an environment that changes what that access means.

### Business Impact

- Over-granting is the path of least resistance when scopes are a flat checklist with no explanation. Misconfigured agents are a governance risk, and the create flow is where that risk is introduced.
- kora's argument is that agents get a verified identity and are checked against policy before they act. The create flow is where a user should first encounter that argument, and today it presents as a form with no point of view.
- Abandoned setups and support questions cost time that should not be spent on a create flow.

---

## Goals and Objectives

### Primary Goals

1. Guide a user through agent creation one class of decision at a time, in an order where each step builds on the one before it.
2. Make the governance model legible at the point of decision rather than in documentation.
3. Let a user review the complete configuration, and correct any part of it, before the agent is created.
4. Never silently lose work in progress.
5. Be honest about what kora enforces today and what is planned, so users do not assume a ceiling exists when it does not.

### Success Metrics

- A user can create a correctly scoped agent without leaving the flow or consulting documentation.
- Every step states what it is for and why it matters, in plain language.
- No agent is created without the user having seen its full configuration.
- Draft state survives leaving the flow and returning.
- Planned capabilities are visually distinguishable from enforced ones at a glance, with no user-facing control that does nothing.

### Non-Goals

- **Response creativity control.** The agent frameworks kora runs are agentic harnesses that do not accept a sampling setting, so the control would have no effect for the default framework. A governance control that does not govern is worse than no control.
- **New runtime ceilings.** Rate limit, maximum tokens per run, maximum tool calls per run, and halt-on-repeated-failures are shown as planned and are not built here. They are specified separately.
- **Editing an existing agent.** The agent settings surface already covers this and is unchanged.
- **An agent catalogue or configuration templates.**
- **Creating environments, connections, or MCP servers** from inside the builder. The builder selects from what exists.

---

## User Stories

### Primary User Stories

**As a** platform engineer creating my first agent,  
**I want to** be walked through the decisions one at a time with each explained,  
**So that** I understand what I am configuring instead of guessing at a form.

**Acceptance Criteria:**
- [ ] The flow presents five steps in a fixed order, with the current step, completed steps, and remaining steps all visible.
- [ ] Each step has a title and a plain-language description of its purpose.
- [ ] The flow states, persistently, that an agent gets a verified identity before it can call a tool and that access is checked before execution.

---

**As a** tenant admin,  
**I want to** review the entire configuration before the agent is created,  
**So that** I do not ship an over-scoped or misconfigured agent.

**Acceptance Criteria:**
- [ ] The final step summarises every configured value, grouped by the step that set it.
- [ ] Each group offers a direct route back to the step that owns it.
- [ ] The agent is only created by an explicit action on the review step.

---

**As a** user granting tools to an agent,  
**I want to** see exactly which tools it may call and what each one does,  
**So that** I grant the minimum the agent needs.

**Acceptance Criteria:**
- [ ] Tools are grouped by the server that provides them, and each tool shows its name, its access scope, and a description of what it does.
- [ ] A user can grant or revoke an individual tool, a whole group, or a whole server.
- [ ] Partial selection of a group is visually distinct from full selection and from none.
- [ ] A running count of servers and granted tools is visible while selecting.

---

**As a** user configuring an agent,  
**I want** my progress kept if I leave,  
**So that** I do not have to start again.

**Acceptance Criteria:**
- [ ] Work in progress is saved without the user asking.
- [ ] The flow indicates when a draft has been saved.
- [ ] Returning to the flow restores the draft, including which step the user was on.
- [ ] A draft is resumable from any device the user signs in on.
- [ ] A draft is not a working agent: it cannot be run, and it holds no access, until the user creates it.

### Secondary User Stories

**As a** user,  
**I want to** know which limits kora actually enforces,  
**So that** I do not rely on a ceiling that does not exist.

**Acceptance Criteria:**
- [ ] Enforced limits and planned limits are presented in visually distinct groups.
- [ ] Planned limits are labelled as such and cannot be switched on.

---

## Proposed Solution

### High-Level Approach

Replace the single form with a five-step builder. The steps follow the questions kora exists to answer, in the order a user needs to answer them: who the agent is, how it behaves, what it can reach, what ceilings apply, and finally a review.

Each step carries a heading, a one-sentence statement of what the step decides, and only the fields relevant to that decision. A step rail shows progress and lets the user return to any step already visited. A persistent panel explains the identity-first model. The final step is a read-only summary of everything, and the agent is created from there.

### User Experience

#### User Flow

1. **Identity.** Name, slug, description, identity model, owner, and team. The slug derives from the name until the user edits it. The identity model decides whether the agent acts on behalf of the human who runs it or acts autonomously as itself.
2. **Behaviour.** System instructions, the framework the agent runs on, and the model it uses. The model list states how many models the platform has enabled.
3. **Tools and access.** The MCP servers and individual tools the agent may call, the connections it may use, the network hosts it may reach, and the environment it runs in.
4. **Guardrails.** The ceilings kora enforces: spend per period, spend per run, and concurrency. Planned ceilings are listed separately and marked as not yet enforced.
5. **Review.** Every value, grouped by step, each group linking back to its step. Creating the agent is an explicit action here, and confirmation shows the agent's identity.

Environment sits with tools and access rather than at the end, because it changes which hosts and connections the agent effectively has. Choosing it after configuring those would silently change what the earlier choices mean.

### Key Components

- **Step rail:** progress, completed state, and navigation back to visited steps.
- **Step content:** one class of decision per step, with a purpose statement.
- **Tool tree:** servers, groups within a server, and individual tools, each with its scope and description, selectable at any level.
- **Guardrail rows:** each ceiling independently switchable, with its value inline and its purpose stated.
- **Planned block:** capabilities on the roadmap, visually separated and not interactive.
- **Review summary:** grouped, complete, and editable by returning to the owning step.
- **Draft persistence:** automatic, with a visible saved indicator.

---

## Scope

### In Scope

- Rebuilding the agent create flow as a five-step builder, opened over whatever the user was doing rather than as a separate destination.
- Retaining every configuration option the current form offers: identity, behaviour, tools, connectivity, connectors, MCP tools, capacity, budget, and environment.
- A review step, a planned-capabilities block, and a draft that is kept, resumable on any device, and never turns into a running agent until the user says so.
- The design system components needed to build it, which do not exist today.

### Out of Scope

- Response creativity, and the runtime ceilings covered by a separate PRD.
- **Selecting an agent's identity model.** [Delegated Agent Identity](delegated-agent-identity.md) is being designed separately, and the control is not drawn until the behaviour behind it exists.
- Editing an existing agent.
- Templates, cloning, or a catalogue.
- Creating environments, connections, or MCP servers inline.

### Future Considerations

- Starting a new agent from an existing one.
- Suggesting a minimal tool set from the agent's stated purpose.
- Surfacing the planned ceilings as real controls once they are enforced.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Five steps in the stated order, with the current, completed, and remaining steps visible throughout.
- [ ] Each step states its purpose in plain language.
- [ ] The user cannot advance past identity without a valid name and slug.
- [ ] The review step shows every configured value and offers a route back to each step.
- [ ] The agent is created only by an explicit action on the review step.
- [ ] Draft state is saved automatically and restored on return.
- [ ] Enforced and planned capabilities are visually distinct, and planned ones are not interactive.
- [ ] Every option available in the current form remains available.
- [ ] The flow works in both light and dark themes.

**Should Have (P1):**
- [ ] The step rail navigates directly to any previously visited step.
- [ ] Tools are selectable individually, by group, and by server, with partial selection shown distinctly.
- [ ] The slug derives from the name until the user edits it, then stops.
- [ ] Creation is confirmed with the agent's identity shown.
- [ ] Failures loading supporting data are surfaced to the user rather than presented as empty state.

**Nice to Have (P2):**
- [ ] The flow is completable by keyboard alone.

### Non-Functional Requirements

**Accessibility:**
- Meets WCAG 2.1 AA, including contrast, focus visibility, and screen reader labelling of step state and selection state.
- Every control is reachable and operable by keyboard.

**Usability:**
- The flow is usable at mobile widths.
- No step requires horizontal scrolling.

**Consistency:**
- The flow is built entirely from the Hades design system. Any new component is added to Hades and documented there.

**Reliability:**
- A draft is not lost by navigation, refresh, or a failed create.

---

## Dependencies

### Internal Dependencies

- **[Delegated Agent Identity](delegated-agent-identity.md):** being designed separately. The identity model control is not shipped ahead of its enforcement, so the identity step gains it later rather than shipping it decoratively.
- **Hades design system:** the builder needs components Hades does not currently have. Hades changes require human approval before implementation.
- **Existing agent configuration surfaces:** tools, connectors, connectivity, environments, and budgets are all sourced from capabilities that already exist.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| A stepped flow hides configuration that a single form made visible at once | Medium | Medium | The review step shows everything, and the rail allows jumping to any visited step |
| The identity model control ships before its enforcement, misleading users about what is bound | High | Low | The control is gated on the Delegated Agent Identity work; it is not shipped decoratively |
| Users read planned ceilings as enforced ones | High | Medium | Planned capabilities are visually separated, labelled, and non-interactive |
| Retaining every existing field makes a step too heavy | Medium | Medium | Tools and access groups related decisions behind a single purpose statement; heaviness is measured against the review step rather than the form |
| New design system components diverge from Hades' conventions | Medium | Low | Components are proposed and approved before implementation, and documented in the same change |

---

## Timeline and Milestones

### Phases

**Phase 1: Design system foundations**
- The components the builder needs, added to Hades with documentation.

**Phase 2: The builder**
- The five-step flow, draft persistence, review, and creation.

**Phase 3: Identity model**
- The identity step's model selection, once the delegated identity capability is enforced.

### Key Milestones

- [ ] PRD approved
- [ ] ADR created
- [ ] Implementation started
- [ ] Shipped

---

## Appendix

### Related Documents

- [Delegated Agent Identity](delegated-agent-identity.md) - the identity model this flow selects
- [Agent Control Plane](agent-control-plane.md) - the agent model this flow creates
- [Hades Design System](design-system.md) - the design system this flow is built from

### Glossary

- **Agent type:** the definition of an agent, created by this flow. Runs are started from it.
- **Framework:** the agentic harness an agent runs on.
- **Scope:** the unit of access an agent is granted. Every tool maps to one.
- **Environment:** a named set of network access and connections merged into an agent's effective access.
