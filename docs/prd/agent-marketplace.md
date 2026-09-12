# Agent Marketplace: Skills and Plugins - Product Requirements Document

**Status:** Draft  
**Created:** 2026-08-06  
**Last Updated:** 2026-09-12  
**Owner:** Product Owner (TBD)  
**Stakeholders:** Tenant Members, Platform Operations, Security Reviewers, Frontend, Agent Runtime, Self-hosting Operators

## Executive Summary

Orbit needs Marketplaces where users can find and review agent skills in one place. Users must be able to understand what a package does, where it comes from, what it contains, what it says it needs. The first release is read-only: it supports finding, and reviewing packages, but it does not install them or add them to agents. A package is a skill or plugin for later iterations.

## Problem Statement

### Current State

Orbit manages agent identity, policy, models, connections, MCP tools, environments, and agent runs. It does not give users a number of X Marketplaces for reusable skills and plugins.

Today, users must search across different repositories and interpret package information by themselves.

### User Pain Points

- Users cannot browse skills from the Orbit dashboard.
- Users cannot quickly see which Marketplace and repository a package comes from.
- Package descriptions, files, and plugin parts are not shown.
- Orbit’s mcps cannot load skills on runtime.

### Business Impact

- Teams spend more time configuring Orbit’s AI agents.
- Teams must spend time researching what packages to use and from where to get them.
- Self-hosting organisations need to create their own package directory instead of using Orbit.

## Goals and Objectives

### Primary Goals

1. Give users searchable Marketplaces for skills.
2. Show the Marketplace source on every search result and package page.
3. Give every package a clear detail page with enough information for review.
5. Give a view of Marketplace sources.

### Success Metrics

- Every package page shows its purpose, publisher, Marketplace source, repository, contents, package needs.
- Users in the agreed usability test can find a package, identify its source, and explain its package needs without leaving Orbit.
- All acceptance tests confirm that users can see only Marketplace information allowed for their tenant and role.
- The first release exposes no package install, update, remove, assignment, or run action.

### Non-Goals

The first release does not include:

- Installing, updating, rolling back, or removing skill.
- Adding packages to AgentTypes, agents, environments, or runs.
- Loading or running package files in an agent workspace.
- Giving packages access to tools, models, MCP servers, connections, files, workloads, or networks.
- Letting an agent manage Marketplace packages or permissions.
- Adding, editing, or deleting Marketplace sources from the dashboard.
- Running package scripts, hooks, helpers, containers, URLs, or health commands.
- Package use, success, token, cost, or duration reports.
- A promise that one package works with every agent framework.
- An agent telling a user that it cannot use a package.
- Choosing the backend, storage, deployment, refresh, caching, or service architecture. Those decisions belong in the ADR.

## User Stories

### Primary User Stories

**As a** tenant member,  
**I want to** browse and filter approved skills,  
**So that** I can find useful packages without searching several repositories.

**Acceptance Criteria:**

- [ ] I can view all packages.
- [ ] I can search by text and filter by Marketplace and alphabetical order.
- [ ] Results are shown one page at a time.
- [ ] Every result clearly shows its package type and Marketplace source.

**As a** tenant member,  
**I want to** open a skill page,  
**So that** I can understand what it does and review its source and contents.

**Acceptance Criteria:**

- [ ] The page shows the required package information defined in this PRD.
- [ ] Package files are shown as read-only.



### Secondary User Stories

As a self-hosting operator, I want Marketplace to behave like part of Orbit, so that users do not need a separate product or sign-in experience.


## Proposed Solution

### High-Level Approach

Orbit will add a read-only Marketplace experience for skills. It will bring package information from approved sources into one consistent view.

This PRD defines the user experience and required outcomes. It does not choose how Marketplace is deployed, how data is stored, how source information is refreshed, or how internal Orbit components communicate.

### User Experience

Marketplace appears under Control Plane. The experience is designed for finding, and reviewing packages. Package-management actions are not shown in the first release.

#### User Flow

1. **Browse Marketplace**
    1. The user opens Control Plane → Marketplace.
    2. The user selects a marketplace source then the skills from it load on the page.
    3. The user searches or applies filters.
    4. Orbit shows one page of matching results.
    5. Each result shows its name, type, summary, Marketplace.
2. **Review a package**
    1. The user selects a skill.
    2. The package page shows its description, publisher, source, repository, licence, files.
    3. The user can view safe text previews and open the original source.
    4. For a plugin, the user can review all declared plugin parts.
    5. The page contains no package-management or agent action.

### Key Components

- **Marketplace overview:** Search, filters, page-by-page results, and clear package summaries.
- **Package details:** Source, publisher, contents.
- **Source status:** A read-only view of approved Marketplaces.
- **Access-aware experience:** Users see only information allowed for their tenant and role.

## Scope

### In Scope

- Marketplace access for approved human users.
- Approved global and tenant-only Marketplace sources.
- Search, filters, sorting, and page-by-page results.
- Package pages with source, publisher, files, plugin parts.
- The Marketplace source shown on every result and package page.
- Safe, read-only previews of supported text files.
- Read-only source and freshness information for approved operators.

### Out of Scope

- Package installation, update, rollback, or removal.
- Adding packages to AgentTypes, agents, environments, or runs.
- Package execution or agent runtime changes.
- Package-use permissions or changes to any existing Orbit permission.
- Marketplace source management through the dashboard.
- Automatic deployment of MCP servers or other workloads.
- Package use, success, cost, token, or duration reporting.
- Packages that add code to the Orbit frontend, backend, operator, or control plane.

### Future Considerations

- A separate product phase for package installation, update, rollback, removal, approval, and the history of package-management actions.
- A separate product phase for adding approved packages to AgentTypes and agent runs.
- Marketplace source management for approved operators.
- Additional source providers and offline package sources.
- Package adoption and trusted runtime-use reporting.

## Requirements

### Functional Requirements

#### Must Have (P0)

- [ ] **FR-1:** Add Marketplace under Control Plane in the Orbit dashboard.
- [ ] **FR-2:** Allow users to view Marketplace information available to their tenant and role.
- [ ] **FR-3:** Let users view skills.
- [ ] **FR-4:** Support text search, sorting, and filters for package, Marketplace.
- [ ] **FR-5:** Return results one page at a time and keep the user's filters while moving between pages.
- [ ] **FR-6:** Show package name, publisher, Marketplace source.
- [ ] **FR-7:** Provide a package page showing purpose, publisher, Marketplace, repository.
- [ ] **FR-9:** Show supported package text files as safe, read-only previews.
- [ ] **FR-14:** Show clear states when Marketplace has errors.
- [ ] **FR-15:** Marketplace content must not create or change Orbit pages, controls, scripts, or executable behaviour.

### Non-Functional Requirements

#### Performance

- Marketplace read requests must complete within 300 ms.
- Large result sets must not require the browser to load all Marketplace items at once.

#### Security

- Every Marketplace request must follow Orbit's identity, tenant, and permission rules.
- Repository content must be treated as unsafe and displayed without running it.
- Package needs and service references must never grant access or deploy anything.

#### Reliability

- A temporary problem with one source must not remove previously available information without a clear explanation.
- A Marketplace problem must not stop users from using unrelated Orbit features.
- Marketplace must show a useful empty state when no source is available.

#### Scalability

- Large Marketplaces must not cause large browser memory use.
- If one Marketplace source is very large, slow, or unavailable, users must still be able to browse package information from other sources.

#### Observability

- Approved operators must be able to see source date of addition, package-check state, and useful errors.


## Dependencies

### Internal Dependencies

- **Orbit identity and tenant membership:** Determines who the user is and which tenant they are using.
- **Orbit permissions:** Determines which Marketplace information each user may view.
- **Orbit dashboard standards:** Provides consistent navigation, wording, accessibility, and responsive behaviour.
- **Source approval process:** Platform and tenant owners must agree which Marketplace sources are approved.
- **Framework support definitions:** The product team must define how publisher claims and Orbit-tested support are presented.

### External Dependencies

- **GitHub:** First-release locations for approved Marketplace sources.
- **Package publishers:** Provide package descriptions, source revisions, licences, contents, package needs, and framework claims.
- **Self-hosting environment:** Must allow the organisation to reach its approved source provider.

## Alternatives Considered

### Alternative 1: Keep Repository Search as the Main Experience

**Description:** Users continue finding skills directly in separate repositories.

**Why not chosen:**

- Users must understand different repository structures and package formats.
- Orbit cannot offer one consistent view of source, contents, package needs, checks, and health.
- High usage will hit GIthub’s API limit.

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


## Timeline and Milestones

### Phases

https://app.notion.com/p/clearroute/ADR-Agent-Marketplace-Frontend-Architecture-3bfd125495bf80b8b9ded74d32e3328f


### Key Milestones

- [ ] PRD approved: TBD
- [ ] Marketplace ADR accepted: TBD
- [ ] Marketplace Design Aproved: TBD
- [ ] Implementation started: TBD


## Appendix

### Related Documents

- **Agent Skills and Plugins Marketplace Architecture** —https://github.com/clear-route/orbit/blob/feat/skills-marketplace-front-end/docs/adr/2026-08-17-agent-marketplace-frontend-architecture.md
- **Orbit PRD Template** — https://github.com/clear-route/orbit/blob/main/docs/prd/template.md

### References

- **Orbit’s github repository** — Orbit source code repository .

### Glossary

- **Marketplace:** The Orbit area where approved users find and review skills.
- **Marketplace source:** An approved location that provides Marketplace package information.
- **Package:** One item in Marketplace. Its type is skill.
- **Skill:** Instructions and supporting files that help an agent perform a task.
- **Agent plugin:** A package that can contain skills and other declared agent parts.
- **Tenant:** A company or team workspace inside an Orbit installation.


**Approval Signatures:**

- [ ] Product Owner: Stoyan Grozdev Date: 12/09/2026
- [ ] Technical Lead: ___________________ Date: __________
- [ ] Security Review: __________________ Date: __________
- Prepared by: Stoyan Grozdev Role: Developer/Product Owner
