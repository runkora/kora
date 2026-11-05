# Agent Skills - Product Requirements Document

**Status:** Approved  
**Created:** 2026-08-06  
**Last Updated:** 2026-11-05  
**Owner:** Stoyan Grozdev  
**Stakeholders:** Tenant Members, Tenant Administrators, Platform Operations, Security Reviewers

## Executive Summary

Kora will provide a Skills page where tenant members find and read agent skills from Marketplaces, the GitHub repositories their tenant registers. The first release is read-only and covers browse, search and read. It does not install skills or add them to agents.

## Problem Statement

### Current State

Kora can allow an agent to use skills, but it provides none. Each developer finds skills on GitHub and other sources and adds them to their own agent. As a result:

*   The company does not know which skills its teams use.
*   Nobody reviews a skill before use.
*   Kora agents cannot use these skills.

### User Pain Points

*   Users cannot browse skills in Kora and search several GitHub repositories instead.
*   Users have no shared catalogue of the skill sources their team uses.
*   Users must leave Kora to read a skill's files.

### Business Impact

*   Teams spend time researching which skills to use and where to get them.
*   Companies that run Kora on their own servers must build their own skills catalogue.

## Goals and Objectives

### Primary Goals

1.  Let users browse and search skills in Kora.
2.  Show each tenant only the skills from the sources registered for that tenant.
3.  Let users read a skill and its files before they use it.

### Success Metrics

*   Tenant members with Skills access can find a given skill and identify its source repository without leaving Kora.

### Non-Goals

The first release does not:

*   Install, update, roll back or remove skills.
*   Add skills to agents, or load or run skill files.
*   Let users add, change or delete Marketplaces in the dashboard.

## User Stories

### Primary User Stories

**As a** tenant member,  
**I want to** browse and search skills from my tenant's Marketplaces,  
**So that** I can find useful skills without searching several repositories.

**Acceptance Criteria:**

*   [ ] I can browse the skills of all my tenant's Marketplaces together, or of one Marketplace, in title order, A to Z.
*   [ ] I can search skill names, descriptions and `SKILL.md` instructions across all Marketplaces or in the one I selected.
*   [ ] Results are shown one numbered page at a time with the total count.
*   [ ] The address holds the Marketplace or Favourites view, the search and the page, and refresh, Back and shared links restore them.
*   [ ] I can mark a skill as a favourite and see my favourites together.
*   [ ] I can see whether each skill comes from an internal or an external Marketplace.

**As a** tenant member,  
**I want to** open a skill page,  
**So that** I can understand what the skill does and review its source and contents.

**Acceptance Criteria:**

*   [ ] The skill opens at its own shareable address.
*   [ ] The page shows `SKILL.md` first, the skill's other text files and their paths, and the source repository.
*   [ ] `SKILL.md` opens rendered, with a raw view. I can select another file, read it as plain text and copy its content.
*   [ ] I can open the skill's folder in its source repository at the commit the page shows.

**As a** tenant administrator,  
**I want to** register GitHub repositories as Marketplaces for my tenant through GitOps,  
**So that** my tenant's members can browse the skills in them.

**Acceptance Criteria:**

*   [ ] I declare each Marketplace as a resource in my tenant's namespace, with its repository and the GitHub App that reads it.
*   [ ] I declare whether each Marketplace holds my organisation's own skills or third-party skills.
*   [ ] I can register several Marketplaces from different GitHub organizations, each with its own GitHub App.
*   [ ] I can pin a Marketplace to one commit or have it follow a branch.
*   [ ] The resource status shows whether the Marketplace is served and whether its credential is available.

### Secondary User Stories

*   As a security reviewer, I want to read a skill's instructions and files in Kora, so that I can check the skill before a team uses it.

## Proposed Solution

### High-Level Approach

Kora adds a read-only Skills page to the dashboard. Tenant administrators declare Marketplaces through GitOps, and the Skills page reads them.

### User Experience

The Skills page sits under Control Plane in the Kora dashboard.

*   Results show ten skills per page as cards or as a list.
*   When a search finds nothing, the page says so and offers to clear the search.
*   When a Marketplace cannot be read, the page shows it as unavailable with the reason and offers to try again.

#### User Flow

1.  **Browse skills:**
    1.  The user opens Control Plane, then Skills.
    2.  The page opens all Marketplaces together, or the Marketplace or Favourites view in the address.
    3.  The user can narrow the results to one Marketplace or to their favourites.
    4.  The user enters a search of at least three characters.
    5.  Kora shows a numbered page of matching skills in title order, A to Z.
    6.  Each result shows the skill's title, name, version, summary and the tags its frontmatter declares.
2.  **Review a skill:**
    1.  The user selects a skill.
    2.  The page shows the skill's details, the open file and a file explorer. `SKILL.md` opens rendered.
    3.  The user can select and copy any text file. The page shows the source repository as `owner/repository`.
    4.  The user returns to the results through the Skills link or browser Back.

### Key Components

*   **Skills overview:** Marketplace and favourite filters, search, list and card views, and numbered pages.
*   **Skill page:** The skill's details, a rendered `SKILL.md`, a file explorer, read-only text previews, and the source repository and commit with a link to the source.
*   **Marketplaces:** GitHub repositories declared in a tenant namespace, each with its own GitHub App.

## Scope

### In Scope

*   **Users:** Signed-in tenant members with Skills read access.
*   **Sources:** GitHub repositories.
*   **Content:** Skills only, read-only.
*   **Actions:** Browse, search and read skills.

### Out of Scope

*   Everything in the Non-Goals and in Future Considerations.
*   Granting skills access to tools, services or data, or changing what agents are allowed to do.

### First-Release Limitations

*   Repository changes appear after the cache lifetime expires. The skill page shows the commit it was read at and when the Marketplace was last read.
*   Initial loads of a large repository may be slow. Users can retry.
*   Only text files can be previewed, and binary files are not listed.
*   Very large repositories are served up to a fixed limit on skills and file size. Skills and files over the limit are not shown.

### Future Considerations

*   **Skill management:** Approve skills and keep a history of install, update, rollback and removal.
*   **Skills in agents:** Add skills to agent types and agent runs.
*   **Plugins:** Agent plugins and their declared parts.
*   **Search ranking:** Order search results by relevance.
*   **More skill details:** Publisher, license, needs and checks.
*   **Shared favourites:** How many people in the tenant favourite each skill.
*   **Local install:** Download a skill or copy it into a local client such as Claude Code.
*   **Sources:** A source status view in the dashboard, Marketplaces shared by every tenant, and providers other than GitHub.
*   **Reports:** Skill adoption, and reports of skill use in agent runs.

## Requirements

### Functional Requirements

**Must Have (P0):**

*   Show the Skills navigation entry only to users with Skills read access.
*   Show tenant members only the Marketplaces declared in their tenant's namespace.
*   Read each Marketplace with the GitHub App declared on it, limited to that one repository and read-only access.
*   Identify each skill by its folder path in the repository.
*   Show clear messages for loading, no Marketplaces, no skills, no search matches, a missing skill, and an unavailable Marketplace. Let users retry.
*   Display Marketplace content without executing it or changing Kora's behavior.

**Should Have (P1):**

*   Let users show the results as a list or as cards.

### Non-Functional Requirements

**Security:**

*   **Who is this agent?** Users access the Skills page through their existing Kora identity and sign-in.
*   **What can it access?** Users see only the Marketplaces of their own tenant, and only with Skills read access. Browsing a skill grants no permission to run it or to use tools.
*   **How does it connect?** Each Marketplace's GitHub App credential stays in its tenant's namespace and is never shown to users or agents. Kora reads each repository with that repository's own credential.

**Reliability:**

*   A failure in one Marketplace must not prevent users from browsing other Marketplaces.
*   Marketplace failures must not disrupt other Kora features.

**Observability:**

*   Operators must have enough information to diagnose problems reading a repository or loading content.

## Dependencies

### Internal Dependencies

*   **Kora identity and tenant membership:** Determines who the user is and which tenant they are using.
*   **Kora permissions:** Determines which users may access the Skills page.
*   **Kora dashboard standards:** Provides navigation, wording, accessibility and responsive behavior.
*   **GitOps:** Tenant administrators declare Marketplaces in their tenant namespace outside the dashboard.

### External Dependencies

*   **GitHub:** Hosts the Marketplace repositories in the first release. Each Marketplace needs a GitHub App. The App reads any public repository and must be installed on a private one.
*   **Skill authors:** Each skill needs its own repository folder and a valid `SKILL.md` with a name and a description. Supporting files are optional. A `SKILL.md` at the repository root is not listed.
*   **Self-hosting environment:** A self-hosted Kora must be able to reach GitHub.

## Risks and Mitigations

None at this time.

## Alternatives Considered

### Alternative 1: Keep Repository Search as the Main Experience

**Description:** Users continue finding skills directly in separate repositories.

**Why not chosen:**

*   Users must understand different repository structures and package formats.
*   Kora cannot offer one consistent view of source and contents.

### Alternative 2: Provide a Links-Only Marketplace

**Description:** Kora shows skill names, short descriptions and source links but no contents.

**Why not chosen:**

*   Users would still leave Kora to understand a skill's contents.
*   Security reviewers would not receive enough information for a consistent review.

### Alternative 3: Include Skill Management in the First Release

**Description:** Add installation, updates, removal, agent type assignment and runtime use together with discovery.

**Why not chosen:**

*   It adds permission, runtime, storage, rollback and security questions before discovery is validated.

### Alternative 4: Platform Approval of Each Repository

**Description:** A platform administrator approves each repository for each tenant before its members can browse it.

**Why not chosen:**

*   Access to the tenant namespace already decides who can register a Marketplace for that tenant.

## Open Questions

None at this time.

## Timeline and Milestones

None at this time.

## Appendix

### Related Documents

*   **[Agent Skills](../adr/2026-08-17-agent-skills.md):** Architecture decision record.
*   **[Skill marketplace sources](../guides/register-skill-marketplace.md):** How to register a Marketplace.

### Glossary

*   **Marketplace:** One GitHub repository of skills, declared as a resource in a tenant namespace with its GitHub App and the commit or branch it serves. Only that tenant can browse it.
*   **Skill:** A folder with a `SKILL.md` file of instructions and optional supporting files that helps an agent perform a task. The dashboard previews text files and does not run them.
*   **Tenant:** A company or team workspace inside a Kora installation.
*   **Tenant administrator:** A person who can create resources in their tenant's namespace.
