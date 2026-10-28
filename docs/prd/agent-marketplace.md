# Agent Marketplace: Skills and Plugins - Product Requirements Document

**Status:** Draft  
**Created:** 2026-08-06  
**Last Updated:** 2026-08-10  
**Owner:** Product Owner (TBD)  
**Stakeholders:** Tenant Members, Platform Operations, Security Reviewers, Frontend, Agent Runtime, Self-hosting Operators

## Executive Summary

Orbit needs Marketplaces where users can find and review agent skills and plugins in one place. Users must be able to understand what a package does, where it comes from, what it contains, what it says it needs, and whether it has current check information. The first release is read-only: it supports finding, and reviewing packages, but it does not install them or add them to agents.

## Problem Statement

### Current State

Orbit manages agent identity, policy, models, connections, MCP tools, environments, and agent runs. It does not give users a Marketplace for reusable skills and plugins.

Today, users must search across different repositories and interpret package information by themselves.

### User Pain Points

- Users cannot browse skills and plugins from the Orbit dashboard.
- Users cannot quickly see which Marketplace and repository a package comes from.
- Package descriptions, files, and plugin parts are not shown in one consistent format.
- Orbit’s mcps cannot load skills or plugins on runtime.

### Business Impact

- Teams spend more time configuring Orbit’s AI agents.
- Teams must spend time researching what packages to use and from where to get them.
- Self-hosting organisations may need to create their own package directory instead of using Orbit.

## Goals and Objectives

### Primary Goals

1. Give users searchable Marketplaces for skills and plugins.
2. Show the Marketplace source on every search result and package page.
3. Give every package a clear detail page with enough information for review.
5. Give a view of Marketplace sources.

### Success Metrics

- Every package page shows its purpose, publisher, Marketplace source, repository, licence, contents, package needs, framework support, checks, health, and last check time.
- Every plugin page shows all plugin parts declared by the package.
- Users in the agreed usability test can find a package, identify its source, and explain its package needs without leaving Orbit.
- All acceptance tests confirm that users can see only Marketplace information allowed for their tenant and role.
- The first release exposes no package install, update, remove, assignment, or run action.

### Non-Goals

The first release does not include:

- Installing, updating, rolling back, or removing skills or plugins.
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
**I want to** browse and filter approved skills and plugins,  
**So that** I can find useful packages without searching several repositories.

**Acceptance Criteria:**

- [ ] I can view all packages, skills only, or plugins only.
- [ ] I can search by text and filter by Marketplace, category, framework support.
- [ ] Results are shown one page at a time.
- [ ] Every result clearly shows its package type and Marketplace source.

**As a** tenant member,  
**I want to** open a skill or plugin page,  
**So that** I can understand what it does and review its source and contents.

**Acceptance Criteria:**

- [ ] The page shows the required package information defined in this PRD.
- [ ] A plugin page shows every declared plugin part.
- [ ] Package files that are available for preview are shown as read-only.
- [ ] The page explains that package needs do not give the package any access.
- [ ] The page provides a View source action, when clicked it navigates to the exact folder in the repository where the package is from.

**As a** platform or security reviewer,  
**I want to** review package origin, contents, needs, framework claims.  
**So that** I can make a consistent review before installation is considered in a later release.

**Acceptance Criteria:**

- [ ] Publisher claims and Orbit check results are clearly separated.
- [ ] Every check result includes a state, time, and useful explanation.
- [ ] Old or missing check information does not look current.

**As an** approved platform or tenant operator,  
**I want to** view the approved Marketplace sources and their status,  
**So that** I can understand whether package information is current and where a problem started.

**Acceptance Criteria:**

- [ ] I can see the Marketplace name, source location.
- [ ] I can see a useful error when a source cannot be read or checked.
- [ ] Credentials are never shown.
- [ ] Source settings are read-only in the first release.

### Secondary User Stories

As a self-hosting operator, I want Marketplace to behave like part of Orbit, so that users do not need a separate product or sign-in experience.

As a support engineer, I want clear source, package-check, and Marketplace states, so that I can explain whether a problem affects one package, one source, or the Marketplace experience.

## Proposed Solution

### High-Level Approach

Orbit will add a read-only Marketplace experience for skills and plugins. It will bring package information from approved sources into one consistent view.

This PRD defines the user experience and required outcomes. It does not choose how Marketplace is deployed, how data is stored, how source information is refreshed, or how internal Orbit components communicate.

### User Experience

Marketplace appears under Control Plane. The experience is designed for finding, and reviewing packages. Package-management actions are not shown in the first release.

#### User Flow

1. **Browse Marketplace**
    1. The user opens Control Plane → Marketplace.
    2. The user selects All, Skills, or Plugins.
    3. The user searches or applies filters.
    4. Orbit shows one page of matching results.
    5. Each result shows its name, type, summary, Marketplace.
2. **Review a package**
    1. The user selects a skill or plugin.
    2. The package page shows its description, publisher, source, repository, licence, files, package needs, framework support.
    3. The user can view safe text previews and open the original source.
    4. For a plugin, the user can review all declared plugin parts.
    5. The page contains no package-management or agent action.

### Key Components

- **Marketplace overview:** Search, filters, page-by-page results, and clear package summaries.
- **Package details:** Source, publisher, contents, package needs, framework support, checks, and health.
- **Plugin contents:** A clear list of every declared skill, command, agent definition, setting, hook, helper, or service reference.
- **Source status:** A read-only view of approved Marketplaces, freshness, package count, state, and errors.
- **Access-aware experience:** Users see only information allowed for their tenant and role.

## Scope

### In Scope

- Marketplace access for approved human users.
- Approved global and tenant-only Marketplace sources.
- Skills and plugins as separate package types.
- Search, filters, sorting, and page-by-page results.
- Package pages with source, publisher, files, plugin parts, package needs, framework support, checks.
- The Marketplace source shown on every result and package page.
- Safe, read-only previews of supported text files.
- Read-only source and freshness information for approved operators.
- Clear empty, unavailable, stale, pending, degraded, and error states.

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
- [ ] **FR-3:** Let users view all packages, skills only, or plugins only.
- [ ] **FR-4:** Support text search, sorting, and filters for package type, Marketplace, category, framework support.
- [ ] **FR-5:** Return results one page at a time and keep the user's filters while moving between pages.
- [ ] **FR-6:** Show package name, type, summary, publisher, Marketplace source.
- [ ] **FR-7:** Provide a package page showing purpose, publisher, Marketplace, repository, source revision, licence, files, package needs, framework support, checks.
- [ ] **FR-8:** Show all declared plugin parts on every plugin page.
- [ ] **FR-9:** Show supported package text files as safe, read-only previews and provide a View source action.
- [ ] **FR-10:** Show required and optional package needs separately and explain that they do not grant access.
- [ ] **FR-11:** Separate publisher claims from Orbit check results for framework support.
- [ ] **FR-12:** Give every check a state, check time, and useful explanation.
- [ ] **FR-13:** Provide an approved operator with a read-only Sources view showing source location, visibility, freshness, package count, state, and errors.
- [ ] **FR-14:** Show clear states when Marketplace is empty, not configured, unavailable, pending, degraded, or showing old information.
- [ ] **FR-15:** Marketplace content must not create or change Orbit pages, controls, scripts, or executable behaviour.
- [ ] **FR-19:** Support multiple sources for the Marketplaces, instead of only from GitHub.

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
- Support teams must be able to tell whether a problem affects one package, one source, or the wider Marketplace.
- Support information must not expose credentials or package file contents.
- The product team must be able to measure Marketplace use, search success, page performance, and error rates without claiming that a package was used by an agent.

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

**Description:** Users continue finding skills and plugins directly in separate repositories.

**Why not chosen:**

- Users must understand different repository structures and package formats.
- Orbit cannot offer one consistent view of source, contents, package needs, checks, and health.
- High usage will hit GitHub’s API limit.

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

1. **Which filters must appear by default?** Package type and Marketplace are required, the product team must confirm the first visible set of other filters.
    - **Status:** Open
    - **Owner:** Product Owner (TBD)
2. **Which plugin-part types should the first release recognise?** The page may show a part without claiming Orbit can run it.
    - **Status:** Open
    - **Owner:** Product and Agent Runtime (TBD)
3. **What proof is required before Orbit marks a publisher or source as verified?**
    - **Status:** Open
    - **Owner:** Security Review (TBD)
4. **Will Orbit include an official global Marketplace?** The product must still provide a clear empty state if none is included.
    - **Status:** Open
    - **Owner:** Product Owner (TBD)
5. **How fresh must Marketplace information be?** Product and operations must agree on the user-visible freshness expectation without choosing the technical refresh method in this PRD.
    - **Status:** Open
    - **Owner:** Product and Platform Operations (TBD)

## Timeline and Milestones

### Phases

**Phase 1: Product Definition and Approval** (Target: TBD)

- Agree on first-release users, problems, scope, package information, source information, and success metrics.
- Resolve the product questions that can change the first-release experience.
- Review and approve this PRD.

**Phase 2: User Experience Validation** (Target: TBD)

- Create Marketplace overview, package detail, source-status, and edge-state designs.
- Test the designs with tenant members, platform reviewers, and self-hosting operators.
- Confirm wording for package needs, publisher claims, checks and stale information.
- Complete accessibility and responsive-design review.

**Phase 3: Architecture Decision** (Target: TBD)

- Use the approved PRD as input to evaluate technical options.
- Record service boundaries, data flow, source access, storage, security, failure handling, and deployment choices in the Marketplace ADR.
- Accept the ADR before implementation starts.

**Phase 4: First-Release Implementation and Validation** (Target: TBD)

- Build the approved Marketplace experience according to the PRD and ADR.
- Complete functional, security, tenant-isolation, performance, accessibility, and failure-state tests.
- Run the agreed usability test and resolve blocking findings.
- Release to a controlled group before general availability.

**Later Product Phases** (Target: TBD)

- Create separate PRDs and ADRs for package management and agent runtime use.
- Do not treat those features as approved by this document.

### Key Milestones

- [ ] Product requirements agreed: TBD
- [ ] PRD approved: TBD
- [ ] User experience reviewed: TBD
- [ ] Accessibility review completed: TBD
- [ ] Marketplace ADR accepted: TBD
- [ ] Implementation started: TBD
- [ ] Security and tenant-isolation review completed: TBD
- [ ] Usability target met: TBD
- [ ] Alpha or beta released: TBD
- [ ] General availability: TBD

## Appendix

### Related Documents

- **[Marketplace frontend architecture](../adr/2026-08-17-agent-marketplace-frontend-architecture.md):** Records the current Skill overview and dedicated detail-page behavior. The frontend amendments do not approve the broader draft product scope in this PRD.
- **[Marketplace backend architecture](../adr/2026-08-28-marketplace-adr-backend.md):** Source access, read APIs, and caching.
- **Orbit PRD Template** — https://github.com/clear-route/orbit/blob/main/docs/prd/template.md

### References

- **Orbit’s github repository** — Orbit source code repository .

### Glossary

- **Marketplace:** The Orbit area where approved users find and review skills and plugins.
- **Marketplace source:** An approved location that provides Marketplace package information. A source can be global or tenant-only.
- **Package:** One item in Marketplace. Its type is skill or plugin.
- **Skill:** Instructions and supporting files that help an agent perform a task.
- **Agent plugin:** A package that can contain skills and other declared agent parts.
- **Plugin part:** A declared skill, command, agent definition, setting, hook, helper, or service reference inside a plugin.
- **Package need:** A tool, service, model, connection, host, or other capability that the publisher says the package needs. It does not grant access.
- **Publisher claim:** Information provided by the package publisher that Orbit has not necessarily tested.
- **Check:** An Orbit result that explains what was reviewed, when it was reviewed, and what was found.
- **Health:** A user-facing state that summarises current check information. It is not a promise that package code is safe or successful.
- **Framework support:** Information about whether a package is expected or known to work with an agent program such as Claude Code, Codex, or OpenCode.
- **Tenant:** A company or team workspace inside an Orbit installation.
- **Install:** A possible future action that would approve and keep a package for use. It is not included in this PRD.

**Approval Signatures:**

- [ ] Product Owner: ____________________ Date: __________
- [ ] Technical Lead: ___________________ Date: __________
- [ ] Security Review: __________________ Date: __________
- Prepared by: Stoyan ____________________ Role: Developer
