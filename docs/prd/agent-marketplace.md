# Agent Marketplace: Skills and Plugins - Product Requirements Document

**Status:** Draft  
**Created:** 2026-08-06  
**Last Updated:** 2026-11-01\
**Owner:** Stoyan Grozdev\
**Stakeholders:** Tenant Members, Platform Operations, Security Reviewers, Frontend, Agent Runtime, Self-hosting Operators

## Executive Summary

Kora needs Marketplaces where users can find and review agent skills in one place. Users must be able to understand what a package does, where it comes from, and what it contains. The first release is read-only: it supports finding and reviewing packages from GitHub sources approved for the user's tenant, but it does not install them or add them to agents. In the first release, a package is a skill. Plugins come in a later release.

## Problem Statement

### Current State

Kora is a platform for running AI agents safely at work. It controls who each agent is, what it can do, and what it can reach.

Kora can allow an agent to use skills, but it gives the agent none. Today, each developer finds skills from GitHub and other sources and adds them to their own agent. As a result:

- The company does not know which skills its teams use.
- Nobody checks a skill before use.
- Kora cannot control what a skill does on a developer's computer.
- Kora agents cannot use these skills.

With the Marketplace, a platform administrator approves GitHub repositories for a tenant, and users with Marketplace access find and read the skills in them. Approval makes a source available for browsing, it does not certify its skills or authorize their execution.

### User Pain Points

- Users cannot browse skills in Kora, so they search several GitHub repositories.
- Users have no shared catalogue of skill sources approved for their team.
- Users cannot see which Marketplace a skill comes from.
- Users cannot read a skill and its files in Kora before they use it.

### Business Impact

- Teams spend more time configuring Kora’s AI agents.
- Teams must spend time researching what packages to use and from where to get them.
- Companies that run Kora on their own servers must build and run their own marketplace service for skills instead of using Kora.

## Goals and Objectives

### Primary Goals

1. Let users browse and search skills in Kora.
2. Show only skills from approved sources.
3. Show which Marketplace each skill comes from.
4. Let users read a skill and its files before they use it.

### Success Metrics

- Tenant members with Marketplace access can find a given skill and identify its source without leaving Kora.
- 100% of skill pages show the source repository, `SKILL.md`, and the supporting text files returned for the skill.

### Non-Goals

The first release does not:

- Install, update, roll back, or remove skills.
- Add skills to agents, or load or run skill files.
- Let users add, change, or delete Marketplace sources or skills in the dashboard.

## User Stories

### Primary User Stories

> A tenant is one company or team workspace inside an Kora installation.

**As a** tenant member,  
**I want to** browse and search skills from approved sources,\
**So that** I can find useful packages without searching several repositories.

**Acceptance Criteria:**

- I can browse the available skills of the selected Marketplace.
- I can select a Marketplace and search its skill names, descriptions, and `SKILL.md` instructions.
- Results are shown one page at a time, with at most 24 skills per page.
- The page always shows the selected Marketplace.

**As a** tenant member,  
**I want to** open a skill page,\
**So that** I can understand what it does and review its source and contents.

**Acceptance Criteria:**

- The skill opens at its own shareable address.
- The page shows `SKILL.md`, the supporting text files returned for the skill, their paths within the skill, and the source repository.
- `SKILL.md` opens first. I can select another file, read it as plain text, and copy its content.
- The Skills link and browser Back return to the same search, sort order, and page while I stay in Marketplace.

### Secondary User Stories

> A self-hosting operator is the person or team that installs and runs Kora on the company's own servers. A platform administrator decides which GitHub repositories are approved for a tenant. A security reviewer checks a skill for risks before teams use it.

- As a self-hosting operator, I want Marketplace to behave like part of Kora, so that users do not need a separate product or sign-in experience.
- As a tenant administrator, I want to register GitHub repositories for my tenant and obtain platform approval, so that users see only skills from approved sources.
- As a security reviewer, I want to read a skill's instructions and files in Kora, so that I can check the skill before a team uses it.

## Proposed Solution

### High-Level Approach

Kora adds a read-only Marketplace for skills to the Kora dashboard. It brings the skills from approved GitHub repositories into one consistent view, where users can browse, search, and read them. Each repository is one Marketplace, available only to the tenants for which it is approved.

### User Experience

The Marketplace is a page under Control Plane in the Kora dashboard. It has the same look and sign-in as the rest of Kora.

- Users can show the results as a list or as cards.
- Users can sort the results by name, A to Z or Z to A.
- A new search shows the most relevant matches first. Users can also choose name order while searching. Clearing the search restores the previous name order.
- A link identifies the selected Marketplace or a skill. Search, sort order, and page stay in memory when users visit a skill and return. Leaving Marketplace or refreshing clears those browse settings.
- When a search finds nothing, the page says so and offers to clear the search.
- When loading fails, the page keeps the last results and offers to try again.

#### User Flow

1. **Browse Marketplace**
   1. The user opens Control Plane → Marketplace.
   2. The page opens the Marketplace named in the address. When the address names none, it uses the user's last selected Marketplace, or the first available one in the order supplied by the platform.
   3. The user can select another Marketplace. Kora remembers a selection after its skills load successfully.
   4. The user enters a search of at least three characters. Search covers names, descriptions, and `SKILL.md` instructions in that Marketplace, excluding supporting files.
   5. Kora shows a numbered page of up to 24 matching skills. A new search uses relevance order; browsing without a search defaults to name order, A to Z.
   6. Each result shows its name, summary, and Marketplace.
2. **Review a package**
   1. The user selects a skill.
   2. The skill opens at its own address. The page shows a file explorer and a plain-text preview, starting with `SKILL.md`. Its name and description stay inside that file rather than appearing as separate fields.
   3. The user can select and copy any returned text file. The page identifies the source repository as `owner/repository` text; it offers no View source action in this release.
   4. The user returns to the remembered results through the Skills link or browser Back.

### Key Components

- **Marketplace overview:** Marketplace selection, search, sorting, list and card views, page-by-page results, and a short summary of each skill.
- **Skill page:** a file explorer, read-only text previews, and the source repository.
- **Approved sources:** the GitHub repositories that a platform administrator approves for each tenant.
- **Access-aware experience:** users with Marketplace read permission see only the sources approved for their tenant.

## Scope

### In Scope

- **Users:** signed-in tenant members authorized to access Marketplace.
- **Sources:** approved GitHub repositories for each tenant.
- **Content:** skills only, read-only.
- **Actions:** browse, search, and read skills.

### Out of Scope

- Everything in the Non-Goals and in Future Considerations.
- Granting skills access to tools, services, or data, or changing what agents are allowed to do.

### First-Release Limitations

- Repository updates may be delayed beyond the default cache lifetime of 12 hours. The dashboard does not show freshness or the source revision.
- Initial loads and refreshes may be slow or time out. Users can retry.
- Only text files can be previewed.
- The Marketplace entry is visible to all signed-in users.
- Sources awaiting approval or marked invalid are unavailable in the dashboard. Operators can check their status using the [source registration guide](../guides/skill-marketplace-sources.md).

### Future Considerations

- **Skill management:** install, update, roll back, remove, and approve skills, with a history of these actions.
- **Skills in agents:** add approved skills to agent types and agent runs.
- **Plugins:** agent plugins and their declared parts.
- **More skill details:** publisher, license, needs, checks, and a link to the original source.
- **Sources:** for approved operators, source management in the dashboard and a status view with the date added, freshness, and errors. Also global sources for every tenant, and other providers, including offline sources.
- **Reports:** skill adoption, and trusted reports of skill use in agent runs.

## Requirements

### Functional Requirements

**Must Have (P0):**

- **FR-1:** Add the Marketplace under Control Plane in the Kora dashboard.
- **FR-2:** Allow authorized tenant members to view only the sources approved for their tenant.
- **FR-3:** Let users browse the available skills of the selected Marketplace.
- **FR-4:** Let users search skill names, descriptions, and instructions within the selected Marketplace. Order search results by relevance by default.
- **FR-5:** Let users browse results one page at a time without losing their selected Marketplace, search, or sort order.
- **FR-6:** Show each skill's name, summary, and Marketplace.
- **FR-7:** Let users review a skill's instructions and available supporting text files, and identify its source repository.
- **FR-9:** Provide read-only previews of supported skill files.
- **FR-14:** Show clear messages for loading, no sources, no skills, no search matches, missing skills, and failures. Let users retry failed loads.
- **FR-15:** Marketplace content must be displayed without executing it or changing Kora's behavior.

**Should Have (P1):**

- **FR-16:** Let users show the results as a list or as cards.
- **FR-17:** Let users share links to Marketplaces and skills, and restore their search, sort order, and result page when returning from a skill within Marketplace.
- **FR-18:** Let users sort skills alphabetically in either direction, with consistent ordering across all result pages.

### Non-Functional Requirements

**Security:**

- **Who is this agent?** Users access Marketplace through their existing Kora identity and sign-in.
- **What can it access?** Users may view only sources permitted by their existing access rights and approved for their tenant. Browsing or source approval must not grant permission to execute skills or access tools.
- **How does it connect?** Repository credentials must remain protected and must not be exposed to users or agents.

**Reliability:**

- A failed update must leave the last available results visible, explain the failure, and allow users to try again.
- A failure in one source must not prevent users from browsing other available sources.
- Marketplace failures must not disrupt other Kora features.

**Observability:**

- Operators must have enough information to diagnose problems accessing sources or loading content.

## Dependencies

### Internal Dependencies

- **Kora identity and tenant membership:** Determines who the user is and which tenant they are using.
- **Kora permissions:** Determines which users may access Marketplace.
- **Kora dashboard standards:** Provides navigation, wording, accessibility, and responsive behaviour.
- **Source approval:** A tenant administrator or operator registers a source. A platform administrator approves the repository for that tenant before users can browse it. This happens outside the dashboard.

### External Dependencies

- **GitHub:** Hosts the approved sources in the first release. Kora needs read access to each approved repository.
- **Skill authors:** Each skill needs its own repository folder and a valid `SKILL.md` file with a name and a description. Supporting files are optional. A `SKILL.md` at the repository root is not listed.
- **Self-hosting environment:** A self-hosted Kora must be able to reach GitHub.

## Alternatives Considered

### Alternative 1: Keep Repository Search as the Main Experience

**Description:** Users continue finding skills directly in separate repositories.

**Why not chosen:**

- Users must understand different repository structures and package formats.
- Kora cannot offer one consistent view of source, contents, and package needs.

### Alternative 2: Provide a Links-Only Marketplace

**Description:** Kora shows package names, short descriptions, and source links but no detailed review information.

**Why not chosen:**

- Users would still leave Kora to understand package contents and needs.
- Platform and security reviewers would not receive enough information for a consistent review.

### Alternative 3: Include Package Management in the First Release

**Description:** Add installation, updates, removal, AgentType assignment, and runtime use at the same time as discovery.

**Why not chosen:**

- It adds permission, runtime, storage, approval, rollback, and security questions before the discovery experience is validated.
- A read-only first release gives users value while the later package-management requirements are researched separately.

## Timeline and Milestones

### Phases

- Frontend delivery plan: https://app.notion.com/p/3cbd125495bf816691a4d9552e7cc77d
- Backend delivery plan: https://app.notion.com/p/3cbd125495bf812b9878cc5faa1b923e
- Skill registration (GitOps) delivery plan: https://app.notion.com/p/3cbd125495bf81e897e9d8d0a3fcf7d7

### Key Milestones

- PRD approved: TBD
- ADRs created: (frontend, backend, and GitOps)
- ADRs accepted (frontend, backend, and GitOps): TBD
- Marketplace design approved: TBD
- Release validation against the real Marketplace service: TBD
- Launch: TBD

## Appendix

### Related Documents

- **[Agent Marketplace Frontend Architecture](../adr/2026-08-17-agent-marketplace-frontend-architecture.md)** (frontend ADR, PR #227).
- **[Agent Marketplace backend architecture](../adr/2026-08-28-marketplace-adr-backend.md)** (backend ADR, PR #224).
- **[Skills Marketplace: GitOps Source Registration and Reconciliation](https://github.com/runkora/kora/blob/487eab6d647d654fe11127ecb0eadc2639be3b24/docs/adr/2026-08-19-marketplace-gitops.md)** (historical GitOps ADR, PR #226).
- **[Skill marketplace sources](../guides/skill-marketplace-sources.md)** (current registration and approval procedure).
- **[Skills gateway and marketplace](https://app.notion.com/p/3a5d125495bf816daa88f17e741bc66d)** (initiative brief, Notion).

### Glossary

- **Marketplace:** The Kora area where users with Marketplace access find and review skills. Each approved repository provides one selectable Marketplace.
- **Marketplace source:** An approved GitHub repository that provides skills for one tenant.
- **Package:** One item in Marketplace. Its type is skill.
- **Skill:** A `SKILL.md` file with instructions, plus optional supporting files, that helps an agent perform a task. The dashboard previews returned text files and does not run them.
- **Agent plugin:** A package that can contain skills and other declared agent parts. Plugins come in a later release.
- **Tenant:** A company or team workspace inside an Kora installation.
- **Operator:** The person or team that installs and runs Kora.
- **Tenant administrator:** A person who can register Marketplace sources in their tenant's namespace.
- **Platform administrator:** A person who decides which GitHub repositories are approved for each tenant.

**Approval Signatures:**

- Product Owner: Stoyan Grozdev — Approved 2026-11-01
- Technical Lead: TBD — Pending
- Security Reviewer: TBD — Pending
