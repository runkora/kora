# [Feature Name] - Product Requirements Document

> **Discovery PRD.** This mirrors `docs/prd/template.md` (the source of truth — re-check it for
> drift before writing). Two additions the discovery skill requires:
> 1. **Dual-user framing** — every value statement and User Story covers the **human** operator
>    *and* the **agent** consuming Orbit. Agents are first-class users, not just subjects.
> 2. **Pillar + coherence tag** — the Executive Summary names the **product pillar** this
>    extends (or the new pillar it opens), notes its relationship to the three questions where
>    relevant (don't force a fit — net-new ground is allowed), and confirms it does not erode
>    the security guarantees (verifiable identity, pre-execution policy, no-creds-to-agents,
>    traceability).
>
> **Stay out of architecture.** No schemas, service design, APIs, or ADR content. "Proposed
> Solution" stays conceptual / UX-level. Architecture is a separate, later step.
> File name: `docs/prd/YYYY-MM-DD-<slug>.md`.

**Status:** Draft
**Created:** YYYY-MM-DD
**Last Updated:** YYYY-MM-DD
**Owner:** [Name/Role]
**Stakeholders:** [List]
**Discovery source:** [pillar map `docs/strategy/pillars/...` + dossier `docs/strategy/research/...`]

---

## Executive Summary

[2-3 sentences: what this is and why it matters. **Name the product pillar** it extends (or
opens), e.g. "Extends the Audit & Governance pillar." Note any three-questions relationship if
natural, and confirm it doesn't erode the security guarantees. Don't force a Who/What/How fit.]

---

## Problem Statement

### Current State
[Current situation; what's missing/broken. Reference real platform state.]

### User Pain Points
**Human operators/admins/devs:**
- Pain point 1
**Agents consuming Orbit:**
- Pain point 1

### Business Impact
- Impact 1

---

## Goals and Objectives

### Primary Goals
1. Goal 1

### Success Metrics
- Metric 1 (measurable; include agent-experience metrics, not only human/ops ones)

### Non-Goals
- Non-goal 1 (explicitly include any "this does not turn Orbit into a …" guardrails)

---

## User Stories

> Write stories for **both** populations. At least one `As an autonomous agent…` story is required.

### Primary User Stories

**As a** [human operator / admin / developer],
**I want to** [action],
**So that** [benefit].

**Acceptance Criteria:**
- [ ] Criterion 1

---

**As an** autonomous agent consuming Orbit,
**I want to** [action],
**So that** [benefit].

**Acceptance Criteria:**
- [ ] Criterion 1

### Secondary User Stories
[Lower-priority stories, both populations.]

---

## Proposed Solution

### High-Level Approach
[Conceptual only — no architecture.]

### User Experience
#### User Flow (human)
1. Step 1
#### Agent Flow
1. Step 1

### Key Components
[Major conceptual pieces — capabilities, not services.]
- Component 1: [description]

---

## Scope

### In Scope
- Feature 1

### Out of Scope
- Feature X

### Future Considerations
- Enhancement 1

---

## Requirements

### Functional Requirements
**Must Have (P0):**
- [ ] Requirement 1

**Should Have (P1):**
- [ ] Requirement 1

**Nice to Have (P2):**
- [ ] Requirement 1

### Non-Functional Requirements
**Performance:** -
**Security:** - (least privilege, no creds to agents, validate external input)
**Reliability:** -
**Scalability:** -
**Observability:** - (all actions emit structured events to the spine)

---

## Dependencies

### Internal Dependencies
- Dependency 1: [description + built/stubbed status]

### External Dependencies
- Dependency 1

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| | | | |

*(Carry the pre-mortem findings from the discovery STRESS phase into this table.)*

---

## Alternatives Considered

### Alternative 1: [Name]
**Description:** …
**Why not chosen:** …

*(Pull from the discarded candidate directions in the ideation logs — shows the divergence
behind the choice.)*

---

## Open Questions
1. **Question 1:** … — **Status:** Open — **Owner:** …

---

## Timeline and Milestones
**Phase 1: [Name]** (Target: YYYY-MM-DD)
- Deliverable 1

### Key Milestones
- [ ] PRD Approved: YYYY-MM-DD
- [ ] ADR Created: YYYY-MM-DD
- [ ] Implementation Started: YYYY-MM-DD
- [ ] Launch: YYYY-MM-DD

---

## Appendix

### Related Documents
- [Pillar map](../strategy/pillars/...) - long-term product fit
- [Research dossier](../strategy/research/...) - background research
- [ADR-XXXX](../adr/XXXX-title.md) - architecture decision (created later)

### References
- [Reference 1](url)

### Glossary
- **Term 1:** Definition

---

**Approval Signatures:**
- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
