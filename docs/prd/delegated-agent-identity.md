# Delegated Agent Identity - Product Requirements Document

**Status:** Draft  
**Created:** 2026-07-17  
**Last Updated:** 2026-07-17  
**Owner:** Platform Engineering  
**Stakeholders:** Platform Admins, Tenant Users, Security, Platform Engineering

---

## Executive Summary

Every agent in Orbit acts as itself, with its own standing access, regardless of who starts it. A user with read-only access can start an agent that holds write access, and the agent will write, so the user has escalated their access by proxy. This introduces a second kind of agent that acts on behalf of the person who runs it: bounded by exactly that person's access, attributed to them, and with its runs visible only to them. It makes Orbit's central claim, that an agent can never access more than the person who delegated to it, true for the human-initiated case.

---

## Problem Statement

### Current State

- Every agent is autonomous. It holds its own access, granted when it was created, and it acts with that access whoever starts it.
- When a person starts a run, the agent's access is unchanged by who they are. The agent's permissions are the agent's, not the runner's.
- Every run for an agent is visible to anyone who can read that agent's runs. A run does not belong to a person.
- Runs record which person started them, but no surface shows it.
- Automated sources can start runs on any agent they are bound to, with no human involved.

### User Pain Points

- **A user can exceed their own access through an agent.** Someone with read-only access who starts an agent holding write access gets writes performed on their behalf. Nothing prevents this, and nothing records it as an escalation.
- **A shared agent must be granted the union of what every user needs**, so the least-privileged member of a team effectively has the most-privileged member's access whenever they use it.
- **Runs are not private.** Anyone in the tenant who can read an agent's runs can read every colleague's prompts and outputs. Users will not put sensitive work through an agent they cannot keep to themselves.
- **Attribution is invisible.** When a run does something unexpected, the surfaces answer "which agent" and never "which person", so the human accountable for a run cannot be found without going outside the product.

### Business Impact

- Orbit's stated position is that a person can delegate to an agent with defined scope and full accountability, and that the agent can never access more than the delegator. For human-initiated runs that is not true today, because no delegation takes place.
- Access-by-proxy escalation is a compliance problem. Asked whether a user can exceed their permissions by going through an agent, the honest answer today is yes.
- Agents cannot be shared safely. Teams either over-grant a shared agent or maintain one agent per person, and both are bad outcomes.
- Without private runs, the product cannot be used for work that involves anything sensitive, which excludes most of the work customers would want to put through it.

---

## Goals and Objectives

### Primary Goals

1. Offer an agent that acts on behalf of the person who runs it and is bounded by exactly that person's access.
2. Attribute every run to the person accountable for it, and show that attribution.
3. Keep a delegated agent's runs private to the person who started them.
4. Guarantee a delegated agent cannot act without a person having asked it to.
5. Leave autonomous agents behaving exactly as they do now.

### Success Metrics

- A delegated agent cannot complete an action the person running it is not permitted to perform, and the refusal states why.
- A delegated agent cannot be started by anything other than a person.
- A user listing runs for a delegated agent sees their own runs and no one else's.
- Every run surface shows the person who started it.
- No change in behaviour for existing agents, all of which remain autonomous.

### Non-Goals

- **Chained delegation.** An agent delegating onward to another agent is not addressed here.
- **Escalation and mid-run approval.** Pausing a run to ask a human to authorise an action it cannot perform is a separate capability.
- **Per-run scope selection.** A person cannot choose to lend an agent less than their full access for a given run. The bound is their access, in full.
- **Changing existing agents.** Every agent that exists today stays autonomous, and this introduces no migration.
- **Making autonomous agents private.** Run privacy is a property of delegation, not a general feature.

---

## User Stories

### Primary User Stories

**As a** tenant admin,  
**I want to** create an agent that can never exceed the access of whoever runs it,  
**So that** sharing the agent does not hand out permissions the user does not already hold.

**Acceptance Criteria:**
- [ ] The identity model is chosen when the agent is created, from delegated and autonomous.
- [ ] A delegated agent performing an action the runner is not permitted to perform is refused.
- [ ] The refusal identifies whether the agent, the runner, or the delegation was the limiting factor.
- [ ] The agent's granted access acts as a ceiling and never as an entitlement on its own.

---

**As a** user of a delegated agent,  
**I want** my runs to be visible only to me and to a tenant admin,  
**So that** I can put real work through an agent without exposing it to the whole tenant.

**Acceptance Criteria:**
- [ ] Listing runs for a delegated agent returns only the runs the requesting person started.
- [ ] Opening another person's run for a delegated agent is refused, unless the requester is a tenant admin.
- [ ] Autonomous agent runs remain visible exactly as they are today.

---

**As a** tenant admin reviewing what happened,  
**I want to** see which person is behind every run, and to read any run in my tenant,  
**So that** I can hold the accountable human accountable rather than only the agent.

**Acceptance Criteria:**
- [ ] Every run surface identifies the person who started it, by a name a human recognises.
- [ ] Attribution is shown for both delegated and autonomous agents.
- [ ] Runs started by an automated source identify the source.
- [ ] A tenant admin can list and read every run in the tenant, including delegated runs they did not start.

---

**As a** tenant admin,  
**I want** a delegated agent to be unusable by automation,  
**So that** it cannot run with nobody accountable for it.

**Acceptance Criteria:**
- [ ] A delegated agent cannot be bound to an automated source.
- [ ] Attempting to start a delegated agent without a person is refused, with a reason that explains the constraint.
- [ ] The constraint is stated in the product when the identity model is chosen, not discovered on failure.

### Secondary User Stories

**As a** user of an autonomous agent,  
**I want** nothing about my agents to change,  
**So that** this capability costs me nothing.

**Acceptance Criteria:**
- [ ] Existing agents behave identically after this ships.

---

## Proposed Solution

### High-Level Approach

An agent declares one of two identity models when it is created, and that choice determines who it acts as, who may start it, and who can see what it did.

**Delegated.** The agent acts on behalf of the person who runs it. Its access for that run is the overlap between what the agent is granted and what the person holds, so it can never do something the person could not do themselves, and it can never do something outside what the agent was configured for. Only a person can start it. Its runs belong to the person who started them and are visible only to them.

**Autonomous.** The agent acts as itself, with its own access, exactly as every agent does today. Automated sources can start it. Its runs are visible according to existing access rules.

The distinction is a governance posture, not an implementation detail, and it is presented that way: the choice is made in the agent builder with both options explained, and it is visible on the agent afterwards.

### User Experience

#### User Flow

1. A tenant admin creates an agent and chooses its identity model, with each option explaining what it means for access, for who can start the agent, and for who can see its runs.
2. A user starts a run on a delegated agent. The run proceeds with the agent acting on their behalf.
3. The agent attempts an action. If the person running it holds the access, the action proceeds. If not, it is refused and the run records why.
4. The user lists runs for that agent and sees only their own.
5. A tenant admin reviewing activity sees which person started each run.
6. An attempt to bind the delegated agent to an automated source is refused, and the reason given is the agent's identity model.

### Key Components

- **Identity model:** a property of an agent, chosen at creation, that determines its governance posture.
- **Delegated access:** an agent's access for a run is the overlap of its own grants and the runner's access.
- **Run ownership:** a delegated agent's run belongs to the person who started it and is private to them.
- **Attribution:** every run identifies the person or source accountable for it, shown as a recognisable name.
- **Start constraint:** delegated agents are startable by people only.

---

## Scope

### In Scope

- Choosing an identity model when creating an agent, and seeing it on the agent afterwards.
- Bounding a delegated agent's actions by the runner's access.
- Refusing to start a delegated agent without a person, and refusing to bind one to an automated source.
- Keeping a delegated agent's runs private to the person who started them.
- Showing, on every run surface, the person or source accountable for the run.
- Explaining both models in the agent builder at the point of choice.

### Out of Scope

- Chained delegation, escalation, and mid-run approval.
- Per-run scope selection.
- Migrating or changing existing agents.
- Privacy for autonomous agent runs.
- Changing how a person's own access is granted or reviewed.

### Future Considerations

- Escalation, where a delegated agent pauses and asks a person to authorise an action it cannot perform. This is the natural next capability and this work is a prerequisite for it.
- Letting a person lend an agent a subset of their access for a single run.
- A shared view of delegated runs for the people who need it, if run privacy proves too strict.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] An agent declares exactly one identity model, delegated or autonomous, at creation, and it cannot be changed afterwards.
- [ ] A delegated agent's access for a run is bounded by the person who started the run.
- [ ] A delegated agent holds no standing access that lets it act outside a person's request.
- [ ] A delegated agent cannot be started without a person, and cannot be bound to an automated source.
- [ ] A delegated agent's runs are listable and readable only by the person who started them and by a tenant admin.
- [ ] A run records the identity of the principal accountable for it, and every run surface resolves that to a name a human recognises.
- [ ] Revoking a person's access takes effect on their in-flight runs, without ending the run by any separate mechanism.
- [ ] Existing agents are unaffected and remain autonomous.

**Should Have (P1):**
- [ ] A refusal caused by delegation distinguishes whether the agent, the person, or the delegation was the limiting factor.
- [ ] The agent builder explains both models at the point of choice, including the constraint on automated starts.
- [ ] The agent detail surface shows the identity model.

**Nice to Have (P2):**
- [ ] A person can filter their own runs across all agents.

### Non-Functional Requirements

**Security:**
- A delegated agent cannot perform an action the person running it cannot perform, and this holds regardless of what the agent was granted at creation.
- A person's access is evaluated at the moment the agent acts, not captured when the run starts, so revoking someone's access applies to their work already in flight.
- Attribution cannot be set or altered by the caller.
- A delegated agent must not retain access that survives the end of a person's request.

**Reliability:**
- A run outlives any single interaction with it, and delegation must remain valid for the life of the run without weakening the bound.
- Loss of the delegated context must fail the run rather than fall back to the agent's own access.

**Observability:**
- A refusal caused by delegation is distinguishable in the audit trail from any other refusal.
- Every run is traceable to the accountable person or source without leaving the product.

---

## Dependencies

### Internal Dependencies

- **[Agent Builder](agent-builder.md):** the identity model is chosen there, and that flow is where both models are explained.
- **Agent triggers:** the constraint that automated sources cannot start delegated agents applies to that capability.
- **Existing access and grant model:** delegated bounding intersects an agent's grants with a person's access, both of which already exist.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| A delegated agent retains standing access and acts outside a person's request, defeating the model entirely | High | Medium | A delegated agent is provisioned with no standing access that permits autonomous action; this is the first acceptance criterion, not an afterthought |
| Run privacy hides activity that a tenant admin legitimately needs to review | Medium | Low | Tenant admins can read every run in the tenant, including delegated runs they did not start |
| Delegation lapses during a long run and the agent silently falls back to its own access | High | Medium | Loss of delegated context fails the run; falling back is never permitted |
| A person's access is captured when the run starts, so a revocation does not reach work already in flight | High | Medium | Access is evaluated each time the agent acts rather than captured up front, which makes revocation apply to in-flight runs with no separate mechanism to build or fail |
| Users choose delegated without understanding it cannot be triggered, then find their automation broken | Medium | Medium | The constraint is stated at the point of choice and enforced when binding a source, not only at run time |
| The two models diverge in behaviour in ways users cannot predict | Medium | Low | The difference is expressed as three stated properties: who it acts as, who can start it, who can see its runs |

---

## Open Questions

1. **Can a tenant admin see another person's delegated runs?**
   - **Status:** Resolved. Yes. Run privacy keeps a delegated run from the rest of the tenant, not from a tenant admin. Governance visibility wins over privacy at the admin level, and an admin who can already read every autonomous run gains nothing by being blocked from delegated ones.
   - **Owner:** Platform Engineering

2. **Can an agent's identity model be changed after creation?**
   - **Status:** Resolved. No. The identity model determines what the agent's existing runs meant, and changing it would rewrite the meaning of history as well as the agent's security posture. It is fixed at creation, at the cost of recreating an agent configured wrongly.
   - **Owner:** Platform Engineering

3. **What happens to an in-flight run when the person's access is revoked?**
   - **Status:** Resolved. The strict reading, and it requires nothing to build. A person's access is evaluated each time the agent acts rather than captured when the run starts, so a revocation reaches in-flight runs on their next action. The agent stops being able to do what the person can no longer do, at the moment they can no longer do it.
   - **Owner:** Platform Engineering

4. **How is a person identified on a run surface?**
   - **Status:** Resolved. A run records the principal accountable for it, and the surface resolves that to a recognisable name at the point of display. Recording the principal rather than a name keeps attribution stable when a person's name or email changes, and keeps the record meaningful when the person no longer exists.
   - **Owner:** Platform Engineering

---

## Timeline and Milestones

### Phases

**Phase 1: Identity model and bounding**
- An agent declares its identity model, and a delegated agent's actions are bounded by the person running it.

**Phase 2: Constraints and privacy**
- Delegated agents are startable by people only, and their runs are private to the person who started them.

**Phase 3: Attribution**
- Every run surface shows the accountable person or source.

### Key Milestones

- [ ] PRD approved
- [ ] ADR created
- [ ] Implementation started
- [ ] Shipped

---

## Appendix

### Related Documents

- [Agent Builder](agent-builder.md) - where the identity model is chosen
- [Agent Control Plane](agent-control-plane.md) - the agent model this extends
- [Agent Triggers](agent-webhook-triggers.md) - the automated starts a delegated agent refuses
- [Personal Access Tokens](personal-access-tokens.md) - existing precedent for separating the accountable person from the credential used

### Glossary

- **Identity model:** whether an agent acts on behalf of the person running it or as itself.
- **Delegated:** an agent that acts as the person who runs it, bounded by their access.
- **Autonomous:** an agent that acts as itself, with its own access. Every agent today is autonomous.
- **Attribution:** the person or automated source accountable for a run.
