# Agent Marketplace: Skills and Plugins - Product Requirements Document

**Status:** Draft  
**Created:** 2026-08-06  
**Last Updated:** 2026-09-25  
**Owner:** Stoyan Grozdev  
**Stakeholders:** Tenant Members, Platform Operations, Security Reviewers, Frontend, Agent Runtime, Self-hosting Operators

## Executive Summary

Orbit needs Marketplaces where users can find and review agent skills in one place. Users must be able to understand what a package does, where it comes from, and what it contains. The first release is read-only: it supports finding and reviewing packages, but it does not install them or add them to agents. In the first release, a package is a skill. Plugins come in a later release.

## Problem Statement

### Current State

Orbit is a platform for running AI agents safely at work. It controls who each agent is, what it can do, and what it can reach.

Orbit can allow an agent to use skills, but it gives the agent none. Today, each developer finds skills from GitHub and other sources and adds them to their own agent. As a result:

- The company does not know which skills its teams use.
- Nobody checks a skill before use.
- Orbit cannot control what a skill does on a developer's computer.
- Orbit agents cannot use these skills.

With the Marketplace, an administrator approves GitHub repositories, and users find and read the skills in them. 

### User Pain Points

- Users cannot browse skills in Orbit, so they search several GitHub repositories.
- Users cannot see which skills are approved for their team.
- Users cannot see which Marketplace a skill comes from.
- Users cannot read a skill and its files in Orbit before they use it.

### Business Impact

- Teams spend more time configuring Orbit’s AI agents.
- Teams must spend time researching what packages to use and from where to get them.
- Companies that run Orbit on their own servers must build and run their own marketplace service for skills instead of using Orbit.

## Goals and Objectives

### Primary Goals

1. Let users browse and search skills in Orbit.
2. Show only skills from approved sources.
3. Show which Marketplace each skill comes from.
4. Let users read a skill and its files before they use it.

### Success Metrics

- Every users in Orbit can find a given skill and name its Marketplace without leaving Orbit.
- 100% of skill pages show the description, the Marketplace, `SKILL.md`, and all resource files.


### Non-Goals

The first release does not:

- Install, update, roll back, or remove skills.
- Add skills to agents, or load or run skill files.
- Let users add, change, or delete Marketplace sources or skills in the dashboard.

## User Stories

### Primary User Stories

> A tenant is one company or team workspace inside an Orbit installation. 

**As a** tenant member,  
**I want to** browse and filter approved skills,  
**So that** I can find useful packages without searching several repositories.

**Acceptance Criteria:**

- [ ] I can view all skills of the selected Marketplace.
- [ ] I can search by skill name and select a Marketplace.
- [ ] Results are shown one page at a time.
- [ ] The page always shows the selected Marketplace.

**As a** tenant member,  
**I want to** open a skill page,  
**So that** I can understand what it does and review its source and contents.

**Acceptance Criteria:**

- [ ] The page shows the required package information defined in this PRD.
- [ ] Package files are shown as read-only.



### Secondary User Stories

> A self-hosting operator is the person or team that installs and runs Orbit on the company's own servers. An administrator decides which GitHub repositories are approved for a tenant. A security reviewer checks a skill for risks before teams use it.

- As a self-hosting operator, I want Marketplace to behave like part of Orbit, so that users do not need a separate product or sign-in experience.
- As an administrator, I want to choose which GitHub repositories are Marketplace sources for my tenant, so that users see only approved skills.
- As a security reviewer, I want to read a skill's instructions and files in Orbit, so that I can check the skill before a team uses it.


## Proposed Solution

### High-Level Approach

Orbit will add a read-only Marketplace for skills to the Orbit dashboard. It will bring the skills from approved GitHub repositories into one consistent view, where users can browse, search, and read them.

This PRD defines the user experience and required outcomes. It does not choose how the Marketplace is deployed, how data is stored, how source information is refreshed, or how internal Orbit components communicate.

### User Experience

The Marketplace is a page under Control Plane in the Orbit dashboard. It has the same look and sign-in as the rest of Orbit.

- Users can show the results as a list or as cards.
- Users can sort the results by name, A to Z or Z to A.
- A link keeps the selected Marketplace, the search, the sort order, and the page, so users can share it or return to the same results.
- When a search finds nothing, the page says so and offers to clear the search.
- When loading fails, the page keeps the last results and offers to try again.


#### User Flow

1. **Browse Marketplace**
    1. The user opens Control Plane → Marketplace.
    2. The page opens with the first Marketplace in name order. The user can select another Marketplace, and its skills load on the page.
    3. The user searches by skill name.
    4. Orbit shows one page of matching results.
    5. Each result shows its name and summary.
2. **Review a package**
    1. The user selects a skill.
    2. The package page shows its name, description, Marketplace, path, and files.
    3. The user can view read-only text previews of the files.

### Key Components

- **Marketplace overview:** Marketplace selection, search, page-by-page results, and a short summary of each skill.
- **Skill page:** the Marketplace, the path, and read-only previews of the skill files.
- **Approved sources:** the GitHub repositories that an administrator approves for each tenant.
- **Access-aware experience:** users see only the Marketplaces of their tenant.

## Scope

### In Scope

- **Users:** every signed-in member of a tenant.
- **Sources:** approved GitHub repositories for each tenant.
- **Content:** skills only, read-only.
- **Actions:** browse, search, and read skills.

### Out of Scope

- Everything in the Non-Goals and in Future Considerations.
- New roles or permissions, and changes to existing ones.

### Future Considerations

- **Skill management:** install, update, roll back, remove, and approve skills, with a history of these actions.
- **Skills in agents:** add approved skills to agent types and agent runs.
- **Plugins:** agent plugins and their declared parts.
- **More skill details:** publisher, repository, license, needs, and a link to the original source.
- **Sources:** for approved operators, source management in the dashboard and a status view with the date added, freshness, and errors. Also global sources for every tenant, and other providers, including offline sources.
- **Reports:** skill adoption, and trusted reports of skill use in agent runs.

## Requirements

### Functional Requirements

**Must Have (P0):**

- [ ] **FR-1:** Add the Marketplace under Control Plane in the Orbit dashboard.
- [ ] **FR-2:** Allow every signed-in member of a tenant to view the Marketplaces of that tenant.
- [ ] **FR-3:** Let users browse all skills of the selected Marketplace.
- [ ] **FR-4:** Support text search on skill names and selection of one Marketplace.
- [ ] **FR-5:** Show results one page at a time, and keep the search and the selected Marketplace while users move between pages.
- [ ] **FR-6:** Show the name and summary of each skill, and show the selected Marketplace.
- [ ] **FR-7:** Provide a skill page that shows the name, description, Marketplace, path, `SKILL.md`, and resource files.
- [ ] **FR-9:** Show supported skill files as read-only.
- [ ] **FR-14:** Show clear empty and error states, with a way to try again.
- [ ] **FR-15:** Marketplace content must not create or change Orbit pages, controls, scripts, or executable behaviour.

**Should Have (P1):**

- [ ] **FR-16:** Let users show the results as a list or as cards.
- [ ] **FR-17:** Keep the selected Marketplace, the search, the sort order, and the page in the link, so users can share it or return to the same results.
- [ ] **FR-18:** Let users sort the skills by name, A to Z or Z to A. The order applies across all pages. The default is A to Z.

### Non-Functional Requirements

**Performance:**

> p95 means that at least 95 of every 100 loads finish within the limit. Up to 5 can be slower. 

- Marketplace data load time < 300 ms p95.
- The first load of a Marketplace that Orbit has not loaded recently can take longer. Its target is TBD, and the team sets it after a test with real sources. The page shows that it is loading.

**Security:**

- Only signed-in users can open the Marketplace, and each user sees only the Marketplaces of their tenant.


**Reliability:**

- When a source has a temporary problem, the page keeps the last results and explains the problem.
- If one source is slow or unavailable, users can still browse the other sources.
- A Marketplace problem must not affect other Orbit pages.

**Scalability:**

- The load-time target (< 300 ms p95) also applies to a Marketplace with many skills. The number of skills for this test is TBD.

**Observability:**

- Orbit logs each Marketplace error, such as a source that it cannot read, so that operators can find the cause.
- The first release has no operator view for sources. Future Considerations lists this view.


## Dependencies

### Internal Dependencies

- **Orbit identity and tenant membership:** Determines who the user is and which tenant they are using.
- **Orbit dashboard standards:** Provides navigation, wording, accessibility, and responsive behaviour.
- **Source approval:** The administrator of each tenant decides which GitHub repositories are approved, and the operator registers them.

### External Dependencies

- **GitHub:** Hosts the approved sources in the first release. Orbit needs read access to each approved repository.
- **Skill authors:** Each skill needs a `SKILL.md` file with a name and a description. Resource files are optional.
- **Self-hosting environment:** A self-hosted Orbit must be able to reach GitHub.

## Alternatives Considered

### Alternative 1: Keep Repository Search as the Main Experience

**Description:** Users continue finding skills directly in separate repositories.

**Why not chosen:**

- Users must understand different repository structures and package formats.
- Orbit cannot offer one consistent view of source, contents, and package needs.

### Alternative 2: Provide a Links-Only Marketplace

**Description:** Orbit shows package names, short descriptions, and source links but no detailed review information.

**Why not chosen:**

- Users would still leave Orbit to understand package contents and needs.
- Platform and security reviewers would not receive enough information for a consistent review.

### Alternative 3: Include Package Management in the First Release

**Description:** Add installation, updates, removal, AgentType assignment, and runtime use at the same time as discovery.

**Why not chosen:**

- It adds permission, runtime, storage, approval, rollback, and security questions before the discovery experience is validated.
- A read-only first release gives users value while the later package-management requirements are researched separately.


## Open Questions

1. **Who can open the Marketplace?** FR-2 lets every signed-in tenant member in, but the backend ADR requires the `marketplace:read` scope. Either the backend drops the scope, or every member gets it.
   - **Status:** Open
   - **Owner:** Siddhant Pote 
2. **Sorting in the backend:** FR-18 needs the backend to sort skills by name before it splits them into pages. The backend ADR has no sort parameter yet.
   - **Status:** Open
   - **Owner:** Siddhant Pote
3. **Marketplace name on the skill page:** FR-7 shows the Marketplace, but the backend detail response has no Marketplace name.
   - **Status:** Open
   - **Owner:** Siddhant Pote


## Timeline and Milestones

### Phases

- Frontend delivery plan: https://app.notion.com/p/3cbd125495bf816691a4d9552e7cc77d
- Backend delivery plan: https://app.notion.com/p/3cbd125495bf812b9878cc5faa1b923e


### Key Milestones

- [ ] PRD approved: TBD
- [x] ADRs created: (frontend, backend, and GitOps)
- [ ] ADRs accepted (frontend, backend, and GitOps): TBD
- [ ] Marketplace design approved: TBD
- [ ] Real Marketplace service connected (mock data replaced): TBD
- [ ] Launch: TBD


## Appendix

### Related Documents

- **Agent Marketplace Frontend Architecture** (frontend ADR, PR #227) — https://github.com/clear-route/orbit/blob/feat/skills-marketplace-front-end/docs/adr/2026-08-17-agent-marketplace-frontend-architecture.md
- **Agent Marketplace — Cache-Aside, Tenant-Scoped Redis Keys, and Resource-Aware Skills** (backend ADR, PR #224) — https://github.com/clear-route/orbit/blob/siddhant/marketplace-adr-backend/docs/adr/2026-08-28-marketplace_adr_backend.md
- **Skills Marketplace: GitOps Source Registration and Reconciliation** (GitOps ADR, PR #226) — https://github.com/clear-route/orbit/blob/docs/skills-marketplace-gitops-adr/docs/adr/2026-08-19-marketplace-gitops.md
- **Skills gateway and marketplace** (initiative brief, Notion) — https://app.notion.com/p/3a5d125495bf816daa88f17e741bc66d
- **Orbit PRD Template** — https://github.com/clear-route/orbit/blob/main/docs/prd/template.md

### References

- **Orbit GitHub repository** — https://github.com/clear-route/orbit

### Glossary

- **Marketplace:** The Orbit area where signed-in users find and review skills.
- **Marketplace source:** An approved GitHub repository that provides skills for one tenant.
- **Package:** One item in Marketplace. Its type is skill.
- **Skill:** A `SKILL.md` file with instructions, plus optional resource files, that helps an agent perform a task.
- **Agent plugin:** A package that can contain skills and other declared agent parts. Plugins come in a later release.
- **Tenant:** A company or team workspace inside an Orbit installation.
- **Operator:** The person or team that installs and runs Orbit.
- **Administrator:** The person who decides which GitHub repositories are approved for a tenant.


**Approval Signatures:**

- [ ] Product Owner: Stoyan Grozdev Date: __________
- [ ] Technical Lead: ___________________ Date: __________
- [ ] Security Review: __________________ Date: __________
- Prepared by: Stoyan Grozdev Role: Developer/Product Owner
