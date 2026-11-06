# Agent Marketplace: Iteration Two - Product Requirements Document

**Status:** Draft  
**Created:** 2026-11-02  
**Last Updated:** 2026-11-02  
**Owner:** Stoyan Grozdev  
**Stakeholders:** Users, bundle curators, administrators, security reviewers, operators, and product and engineering teams. Contacts to confirm: Justin Wilkin, Siddhant Pote, and Qj.

## Executive Summary

Iteration two lets users choose skills, plugins, and curated bundles when creating Kora agents, and see what each agent uses. It adds plugins to the Marketplace and supports agents on Claude Code, Codex, and OpenCode. Selected content remains subject to the agent's existing identity, permissions, and connection controls.

## Problem Statement

### Current State

- The Marketplace is a read-only skill catalogue. Users can browse approved GitHub sources and read skill instructions and supporting text files.

- Users cannot choose Marketplace skills or plugins for Kora agents.

- Plugins do not appear as complete packages with their contents and compatibility explained.

- There are no curated bundles that users can reuse across agents.

- Users cannot see which Marketplace skills and plugins an agent uses.

- Users cannot identify the exact source version of a skill they reviewed.

- Source registration and approval happen outside the dashboard.

### User Pain Points

- Agent creators can find useful skills but cannot give them to a Kora agent.

- Users cannot understand a plugin's contents or tell whether it works with their agent's runtime.

- Teams must choose the same group of skills separately for each agent.

- Reviewers cannot assess which agents a package change, source removal, or security finding would affect.

### Business Impact

- The Marketplace does not yet reduce the work needed to configure Kora agents.

- Teams repeat package review and setup work instead of reusing curated bundles.

- Missing package and version records make it harder to explain which content influenced an agent's behavior.

## Goals and Objectives

### Primary Goals

1.  Show plugins alongside skills, with their contents and runtime compatibility.

2.  Let agent creators choose skills, plugins, and bundles for Claude Code, Codex, and OpenCode agents.

3.  Let authorized curators create and maintain reusable bundles.

4.  Show what each agent is configured to use and what each run actually loaded.

5.  Preserve agent identity, permission boundaries, and governed connections.

### Success Metrics

These are proposed release checks for iteration two. They describe what must work before launch.

- **Plugins appear correctly:** Every supported plugin from an approved source appears in the Marketplace as a plugin, with its contents listed. Check that all supported plugins in an approved test repository appear correctly in the Marketplace.

- **Skills are available on all three runtimes:** A newly created agent has its chosen supported skills available from its first run. Check this by starting a new agent run on Claude Code, Codex, and OpenCode.

- **Compatibility is clear before creation:** Every chosen package shows whether it is supported, partly supported, unsupported, or has unknown compatibility with the chosen runtime. Check these labels and explanations in the agent creation flow.

- **Permissions stay unchanged:** Choosing packages does not give an agent extra permissions. Compare permissions with and without chosen packages, keeping the rest of the agent's configuration the same.

- **Each run shows what loaded:** Every run with chosen packages lists the exact version of each package it loaded. Any chosen package that was skipped or failed to load has a reason shown in the run's details.

- **Changes have a clear record:** Every package choice and bundle change has a record showing who made it, what changed, when it happened, and whether it was allowed. Check that each change record includes these details.

### Non-Goals

- Granting extra permissions through a skill, plugin, or bundle.

- Installing packages into a person's local agent, such as Claude Code on a laptop.

- Creating bundles automatically, including through ML clustering.

- Authoring, editing, or publishing skills and plugins in Kora.

- Managing Marketplace sources in the dashboard.

- Using packages from unapproved sources.

- Running Marketplace content in the dashboard.

- Adding a general version history for agent definitions.

## User Stories

### Primary User Stories

#### Scope Item 1: Plugins in the Marketplace

**As a** tenant member,  
**I want to** browse and search plugins alongside skills,  
**So that** I can find a plugin and understand what it contains.

**Acceptance Criteria:**

- Each result identifies whether it is a skill or a plugin.

- I can filter by package type and search by name and description.

- A plugin page lists its parts and available files with their locations, including which skills belong to it.

- I can read supported text files and identify files that cannot be previewed.

- The page shows compatibility with Claude Code, Codex, and OpenCode, with reasons for limitations.

- Browsing a plugin never runs its content.

**As a** security reviewer,  
**I want to** understand what a plugin can do and what access it needs,  
**So that** I can assess its risks before an agent uses it.

**Acceptance Criteria:**

- The page identifies parts that run commands, perform automatic actions, start services, or need tools, network access, or credentials.

- The page shows whether required integrations are approved and available in Kora.

- Unsupported parts have a clear reason.

- I can identify the source and exact version of the content I review.

**As a** tenant administrator,  
**I want to** use the existing source approval process for plugin repositories,  
**So that** plugins do not require a separate approval process.

**Acceptance Criteria:**

- A platform administrator can approve a plugin repository through the existing process.

- An approved source can provide both skills and plugins.

- Users see only content approved for their tenant and allowed by their permissions.

#### Scope Item 2: Choosing Skills and Plugins During Agent Creation

**As an** agent creator,  
**I want to** choose skills, plugins, and bundles when creating an agent,  
**So that** the agent can use them from its first run.

**Acceptance Criteria:**

- After choosing a runtime, I can search and choose packages and bundles from approved Marketplaces.

- I can filter by Marketplace and package type.

- Each package shows compatibility with the chosen runtime before I select it.

- The Review step shows my choices, their sources, compatibility, and version or update behavior.

- Drafts keep my choices without changing an agent's permissions or active runs.

- The effective-access view shows the same permissions before and after I choose packages.

- A failed package list shows an error and retry action, while other builder steps remain usable.

- If I lack permission to choose packages, the builder explains why.

- The first run uses the supported chosen content and records its exact versions.

**As an** agent creator,  
**I want to** understand runtime incompatibilities before creating an agent,  
**So that** I know which content the agent can use.

**Acceptance Criteria:**

- Unsupported items show "Not supported on {runtime}" with a reason and cannot be chosen.

- Partly supported items explain what will be omitted and require confirmation.

- Items with unknown compatibility show a warning and require confirmation.

- Changing the runtime checks all choices again. I must remove unsupported items before creating the agent.

- The same compatibility rules apply to all three runtimes.

**As an** agent creator,  
**I want to** see conflicting package names before creating an agent,  
**So that** the agent does not use the wrong item.

**Acceptance Criteria:**

- The builder identifies items that would conflict when used by the agent, including items from bundles.

- Each conflict names the affected items and their sources.

- I must resolve conflicts before creating the agent.

#### Scope Item 3: Skill Bundles

**As a** bundle curator,  
**I want to** maintain a named group of skills and plugins,  
**So that** teams can reuse a curated set across agents.

**Acceptance Criteria:**

- I can give a bundle a name and description and add or remove packages from approved Marketplaces.

- The bundle page lists its items and their runtime compatibility.

- Before saving a change, I can see affected agents that I have permission to read.

- Kora records each bundle change in its audit history.

- Only authorized curators can create, change, or delete bundles. Open Question 3 decides who can curate them.

**As an** agent creator,  
**I want to** choose a bundle,  
**So that** I can give an agent a curated set of packages in one step.

**Acceptance Criteria:**

- Before confirming, I can review the bundle's items and their compatibility with my agent's runtime.

- Kora explains which items the agent can use and which it cannot, with reasons.

- The agent page shows the bundle and the packages the agent is configured to use.

#### Scope Item 4: Visibility into Agent Skills and Plugins

**As an** agent owner or editor,  
**I want to** see my agent's chosen packages and bundles,  
**So that** I understand what it is configured to use.

**Acceptance Criteria:**

- The agent detail page shows chosen packages and bundles, with their types, sources, versions or update behavior, and compatibility.

- Each item shows whether it is available, changed, missing, no longer approved, or unsupported on the agent's runtime.

- Run details show what actually loaded, its exact versions, and reasons for anything that did not load.

**As a** tenant administrator or security reviewer,  
**I want to** see which agents use a package or bundle,  
**So that** I can assess the impact of changes and security findings.

**Acceptance Criteria:**

- Package and bundle pages list agents configured to use the item.

- The list respects my agent read permissions.

- I can filter the agents list by a chosen package or bundle (P1).

**As a** platform administrator,  
**I want to** understand a source's impact before revoking approval,  
**So that** I can identify affected teams and agents.

**Acceptance Criteria:**

- I can see counts of affected agents and bundles within my permissions (P1).

- Revoked content is marked "Source not approved" and is not loaded into new runs.

- Kora records the effect of the revocation on affected agents.

**As a** security reviewer,  
**I want to** trace package choices and loaded versions to the responsible person,  
**So that** I can explain who changed an agent's content and what it used.

**Acceptance Criteria:**

- Each change records the responsible person's verified identity, the affected agent or bundle, the previous and new choices and versions, the time, and whether the change was allowed.

- The person making a change cannot attribute it to someone else.

- Each run records the exact versions it loaded.

### Secondary User Stories

- As an agent owner or editor, I want to change packages on an existing agent. Open Question 5 decides whether editing is included.

- As an agent creator using Kora's MCP tools, I want to choose packages without the dashboard. Open Question 11 decides whether this workflow is included.

- As a self-hosting operator, I want agents to use approved packages without extra internet access or handling repository credentials.

- As a bundle curator, I want to know when an item changes, disappears, or loses approval.

## Proposed Solution

### High-Level Approach

Extend the Marketplace to include plugins and let users choose approved skills, plugins, and bundles for their agents. Make compatibility and access needs clear before confirmation, and show both configured choices and actual use in runs.

The feature preserves the three Kora questions:

- **Who is this agent?** Choosing packages does not change the agent's identity. Package choices and run records remain attributable to the people responsible.

- **What can it access?** Packages use the agent's existing permissions. Kora checks policy before actions execute, and choosing packages grants no additional access.

- **How does it connect?** Packages use Kora's governed connections. Agents do not need new internet access or handle source credentials to receive approved content.

### User Experience

- The Marketplace shows skills and plugins together, with filters and package detail pages.

- The agent builder offers package choices after the runtime is chosen and summarizes them before creation.

- The agent detail page shows chosen packages, bundles, and availability. Editing remains an open decision.

- Package and bundle pages show which agents are configured to use them, within the viewer's permissions.

- Bundle curators can review the impact of changes before saving.

#### User Flow

1.  **Find a plugin:** Open the Marketplace, search or filter results, and review a plugin's contents, source, compatibility, and access needs.

2.  **Create an agent:** Choose a runtime, choose packages or bundles, resolve compatibility warnings and conflicts, review the choices, and create the agent.

3.  **Curate a bundle:** Name the bundle, choose approved packages, review compatibility and affected agents, and save.

4.  **Review usage:** Open an agent to see its configured packages, a package to see its agents, or a run to see the exact content it loaded.

### Key Components

- **Package catalogue:** Skills and plugins with source information, contents, compatibility, and access needs.

- **Agent package chooser:** Package and bundle choices in agent creation, including review, warnings, conflicts, and drafts.

- **Bundle library:** Named groups of packages maintained by authorized curators.

- **Usage views:** Agent configuration, package and bundle "used by" lists, and run content records.

### Runtime Compatibility

The feature supports agents on Claude Code, Codex, and OpenCode. Each package and plugin part shows its compatibility with each runtime. Plugin support may differ between runtimes; Open Questions 1 and 2 define the supported parts and compatibility rules.

| State                 | What the user sees                                                 | Selection behavior                         |
|-----------------------|--------------------------------------------------------------------|--------------------------------------------|
| Supported             | The item can be used on the chosen runtime.                        | Can be chosen.                             |
| Partly supported      | Some parts cannot be used, with an explanation of what is omitted. | Requires confirmation.                     |
| Not supported         | The item cannot be used on the chosen runtime, with a reason.      | Cannot be chosen.                          |
| Compatibility unknown | Compatibility has not been established.                            | Shows a warning and requires confirmation. |

### Edge Cases

- **Source content changes:** Show the change and record the exact version each run uses. Open Question 6 decides whether agents keep the chosen version or follow updates.

- **Source approval is revoked:** Mark affected content and stop it from loading into new runs. Open Question 9 decides the effect on active runs; Open Question 7 decides whether new runs can proceed without it.

- **A package disappears or is renamed:** Explain the change and never silently substitute another package. Missing packages do not load into new runs. Open Question 10 decides how renames appear.

- **A bundle item becomes unavailable or incompatible:** Mark the item on the bundle and affected agents, with a reason. Open Question 7 decides the effect on runs.

- **Chosen items have conflicting names:** Show the items and their sources and require the user to resolve the conflict before saving. Include choices made through bundles.

- **A plugin needs additional access:** Show what is needed and what the agent is allowed to use. Choosing the plugin grants no access and cannot bypass integration approval or permission checks.

- **The agent's runtime changes:** Check compatibility again and require unsupported items to be removed before saving.

- **Content cannot load at run start:** Record what did not load and why. Open Question 7 decides whether the run continues or fails.

## Scope

### In Scope

- Skills and plugins from approved GitHub sources.

- Agents on Claude Code, Codex, and OpenCode.

- Package and bundle choices during agent creation.

- Bundle curation and review of affected agents.

- Views of each agent's configured packages and each run's loaded versions.

- Package and bundle "used by" views, subject to permissions.

- Editing existing agents and choosing packages through Kora's MCP tools only if Open Questions 5 and 11 include them.

### Out of Scope

- The capabilities listed under Non-Goals.

- Bundles that contain other bundles (proposed limit).

- Sources from providers other than GitHub.

- Use of plugin features that none of the three runtimes support, or features outside Kora's governed tool access, including MCP prompts and resources. Unsupported parts may still be listed for review.

### Future Considerations

- Suggested bundles or related packages, including suggestions from ML clustering.

- Local installation into a person's agent.

- Package update, rollback, and approval workflows with history.

- Aggregate package adoption and usage reports.

- Additional source providers, offline sources, shared global sources, and source management in the dashboard.

- General history of agent configuration changes.

## Requirements

### Functional Requirements

All priorities are proposed. Requirement IDs use FR2 to distinguish them from iteration one.

**Must Have (P0):**

*Plugins in the Marketplace*

- **FR2-1:** Show plugins from approved sources as distinct packages alongside skills.

- **FR2-2:** Identify each package's type and let users filter by type.

- **FR2-3:** List each plugin's parts and files with their locations, provide read-only text previews, and identify files that cannot be previewed.

- **FR2-4:** Show which plugin contains a skill.

- **FR2-5:** Show package and plugin-part compatibility for all three runtimes, with reasons for limitations or unknown compatibility.

- **FR2-6:** Identify parts that run commands, perform automatic actions, start services, or need tools, network access, or credentials.

*Choosing packages during agent creation*

- **FR2-7:** Let authorized users choose approved skills, plugins, and bundles after choosing the agent's runtime.

- **FR2-8:** Block unsupported choices and require confirmation for partial or unknown compatibility.

- **FR2-9:** Check compatibility again after a runtime change and require unsupported items to be removed before creating or saving the agent.

- **FR2-10:** Summarize chosen items before creation, including type, Marketplace, compatibility, and version or update behavior.

- **FR2-11:** Keep package choices in drafts without affecting active runs or granting access.

- **FR2-12:** Make supported chosen content available from the first run and in each new run on all three runtimes. Offer package choices only where that behavior works.

- **FR2-13:** Keep each chosen package tied to its source. A rename or duplicate name must not silently replace the chosen content with another package.

- **FR2-14:** Identify conflicting names, including items from bundles, and require resolution before creating or saving the agent.

- **FR2-15:** Show required access and whether the agent has it. Never grant access because a package was chosen.

- **FR2-16:** Record each run's loaded packages and exact versions, plus anything that did not load and why.

*Bundles*

- **FR2-17:** Let authorized curators create, change, and delete named bundles of skills and plugins, with descriptions.

- **FR2-18:** Let users choose a bundle after reviewing its items and runtime compatibility.

- **FR2-19:** Before saving a bundle change, show affected agents that the curator has permission to read.

*Visibility*

- **FR2-20:** Show each agent's chosen packages and bundles, including types, sources, versions or update behavior, compatibility, and availability.

- **FR2-21:** Show agents configured to use a package or bundle, within the viewer's permissions.

- **FR2-22:** Show availability as Available, Changed in source, Not found in source, Source not approved, or Not supported on {runtime}.

*Governance*

- **FR2-23:** Check permissions before package choices, bundle changes, or usage views are accessed or changed. Apply the same rules across all supported workflows.

- **FR2-24:** Audit each package choice and bundle change, including the responsible person's verified identity, the agent or bundle, previous and new items and versions, the time, and whether it was allowed.

- **FR2-25:** Stop revoked or removed content from loading into new runs, mark affected choices, and record the impact.

**Should Have (P1):**

- **FR2-26:** Let users search plugins by their parts and bundles by name and description.

- **FR2-27:** Let users filter agents by a chosen package or bundle.

- **FR2-28:** Show on agent and package pages when chosen content changes in its source.

- **FR2-29:** For each Marketplace source, show how many agents and bundles use its packages, within the viewer's permissions.

- **FR2-30:** Show each bundle's curator, last change time, and change history.

- **FR2-31:** Show a compatibility summary for each bundle and runtime.

**Nice to Have (P2):**

- **FR2-32:** Notify agent owners and bundle curators when chosen content changes, becomes unavailable, or loses approval.

- **FR2-33:** Show how many agents use each package in Marketplace results.

- **FR2-34:** Show differences between a chosen package version and its current source version.

### Non-Functional Requirements

**Performance:**

- A slow or failed package list must not block unrelated agent creation steps.

- Proposed target: the first page of packages appears within 2 seconds of the requests.

- Proposed target: preparing chosen content adds no more than 5 seconds to run start runs with 20 packages.

- Adding plugin support must not slow browsing and search for sources from the first iteration.

**Security:**

- **Who is this agent?** Package choices do not change agent identity. Changes and loaded content remain traceable to the responsible person's verified identity.

- **What can it access?** Packages grant no extra permission. Every action remains subject to the agent's existing policy limits. For delegated agents, the delegating person's current permissions also apply.

- **How does it connect?** Packages use governed connections. Agents must not need extra internet access or handle repository credentials to receive approved content. Plugins must not bypass credential controls.

- All package, bundle, and usage actions respect tenant boundaries and user permissions.

- Choosing content for an autonomous agent requires authorization appropriate to its standing permissions.

- Automatic plugin actions have the same permission limits as other agent actions and are checked before execution.

- An agent must not modify selected package content during a run.

- The dashboard displays Marketplace content without running it.

**Reliability:**

- Marketplace failures must not stop agents without chosen packages or disrupt unrelated Kora features.

- A failure in one source must not prevent users from choosing content from other available sources.

- Saved choices, bundles, audit history, and run content records must survive service restarts and temporary catalogue unavailability.

- Every run records whether its chosen content loaded successfully, including any failure reasons.

**Scalability:**

- Proposed target: support Marketplaces with at least 1,000 packages.

- Proposed target: support at least 50 chosen packages per agent.

- If agents follow bundle changes, those changes must reach all affected agents without a manual update for each agent.

**Observability:**

- Authorized reviewers can inspect package choices, bundle changes, and run content records.

- Operators can identify package-loading failures and source problems, with useful reasons.

- Diagnostic information must protect credentials and respect content access permissions.

## Dependencies

### Internal Dependencies

- **Marketplace and source approval:** Existing discovery and tenant approval capabilities provide the approved content.

- **Agent creation and details:** Existing agent workflows need to support package choices and visibility.

- **Agent runtimes:** Claude Code, Codex, and OpenCode must support the agreed package behavior.

- **Identity and permissions:** Existing identity and access controls must cover package choices, bundles, and usage views.

- **Governed integrations:** Plugins that need tools or external services depend on Kora's existing approval, permission, connection, and credential controls.

- **Audit and run records:** These capabilities must provide the required change history and evidence of loaded content.

### External Dependencies

- **GitHub:** Approved repositories must remain accessible to Kora.

- **Package authors:** Sources must provide usable packages and enough information to review their contents and compatibility.

- **Runtime vendors and package standards:** Package support depends on the capabilities of Claude Code, Codex, and OpenCode and the package formats they support.

## Risks and Mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                                                       |
|----------------------------------------------------------|--------|-------------|----------------------------------------------------------------------------------------------------------------------------------|
| Harmful content misuses an agent's existing permissions. | High   | Medium      | Keep permission checks before actions, grant no access through packages, and record content versions and responsible users.      |
| Source changes reach agents without review.              | High   | Medium      | Agree the update policy in Open Question 6.                                                                                      |
| Users expect every plugin to work on every runtime.      | Medium | High        | Show compatibility before confirmation and block unsupported choices.                                                            |
| Automatic plugin actions bypass governance.              | High   | Medium      | Support only parts that remain subject to Kora's permission and connection controls. Resolve supported parts in Open Question 1. |
| An agent changes the content recorded for a run.         | Medium | Medium      | Prevent changes to selected package content during the run.                                                                      |
| Conflicting names cause the wrong content to be used.    | Medium | Medium      | Identify conflicts before saving and retain source information for each choice.                                                  |
| A bundle change unexpectedly affects many agents.        | Medium | Medium      | Show affected agents before saving and whats the update behavior in Open Question 12.                                            |
| Large catalogues make agent creation slow.               | Medium | Medium      | Apply the performance targets and keep unrelated creation steps usable.                                                          |

## Alternatives Considered

### Alternative 1: Support Only One Runtime

**Description:** Offer package use for one runtime first.

**Why not chosen:**

- The agreed scope includes Claude Code, Codex, and OpenCode.

- Teams should be able to use approved skills across their supported agent runtimes.

### Alternative 2: Use Runtime Marketplaces Directly

**Description:** Let agents obtain packages from the marketplaces provided by their runtimes without Kora's source approval and package review.

**Why not chosen:**

- Administrators would lose control over approved sources.

- Users and reviewers could not rely on Kora to show chosen packages, versions, and affected agents.

### Alternative 3: Automatic Bundles from the Start

**Description:** Use ML clustering to create bundles automatically.

**Why not chosen:**

- The agreed scope starts with bundles curated by people.

- Suggested bundles can be considered after the curation and review experience is established.

## Open Questions

All questions remain open. Proposed defaults are suggestions for review.

1.  **Which plugin parts are included in iteration two?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Skills only; skills plus commands and subagents; approved tool integrations; or automatic actions as well. Every supported part must remain governed by Kora.

2.  **How should partial runtime compatibility affect package choices?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Allow compatible parts with a clear warning, or require the whole plugin to be compatible.

    - **Proposed default:** Show compatibility for each part, explain omissions, and require confirmation for partial or unknown compatibility.

3.  **Who owns and manages bundles?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Tenant administrators, other authorized tenant curators, platform curators, or personal bundles shared by their creators.

    - **Proposed default:** Tenant bundles managed by authorized curators.

4.  **Who may choose skills and plugins for an agent?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Anyone who can edit that agent, users who can also access the Marketplace, or users with separate package-selection permission.

    - **Proposed default:** Users who can edit the agent and access the Marketplace.

5.  **Can users change packages on existing agents in this iteration?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Allow edits for future runs, also affect active runs, or limit this iteration to agent creation.

    - **Proposed default:** Allow edits on the agent's Configuration tab that apply to newly started runs, with an audit record.

6.  **Do agents keep the chosen package version or receive later source updates?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Keep the chosen version, follow updates automatically, or require approval for each update. Users must understand the chosen behavior before confirming.

7.  **What happens when chosen content cannot be loaded?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Fail the run, continue with a visible warning and record what is missing, or let users choose the behavior for the agent.

8.  **Does approving a source also approve other sources it refers to?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Approve each source separately or explicitly allow the original approval to cover those sources.

9.  **How does source revocation affect active runs?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Stop affected active runs, let them finish, or require an administrator's decision. New runs must not load revoked content.

10. **How should renamed or duplicate packages appear?**

    - **Status:** Open

    - **Owner:** TBD

    - **Decision needed:** Define how users recognize a renamed package and resolve duplicate names without silently changing an agent's chosen content.

11. **Can users choose packages through Kora's MCP tools?**

    - **Status:** Open

    - **Owner:** TBD

    - **Decision needed:** Confirm whether this workflow is included. The same permissions and audit requirements would apply.

12. **Do later bundle changes affect agents that already use the bundle?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Follow bundle changes, keep the items originally chosen, or require review before applying changes. Also decide what happens to affected agents when a bundle is deleted.

13. **What usage information may users see?**

    - **Status:** Open

    - **Owner:** TBD

    - **Decision needed:** Confirm whether counts include only agents the viewer can read or whether additional permission is required for broader summaries.

14. **Which supporting files are available for an agent to use?**

    - **Status:** Open

    - **Owner:** TBD

    - **Options:** Instructions only, or instructions plus supporting scripts, images, and other assets. Users must be told when required content is omitted.

15. **Can users choose an individual skill from a plugin without choosing the whole plugin?**

    - **Status:** Open

    - **Owner:** TBD

16. **Who approves iteration two, and what is the target date?**

    - **Status:** Open

    - **Owner:** TBD

    - **Decision needed:** Confirm accountable approvers, delivery owners, and the release target.

## Timeline and Milestones

### Phases

To be done, when the functionalities are agreed upon.

### Key Milestones

- PRD approved: TBD

- ADRs created: TBD

- ADRs accepted: TBD

- Implementation started: TBD

- Beta: TBD

- Launch: TBD

## Appendix

### Related Documents

- [Agent Marketplace: Skills and Plugins](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/agent-marketplace.md) — iteration-one PRD.

- [Agent Marketplace Frontend Architecture](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/adr/2026-08-17-agent-marketplace-frontend-architecture.md) — iteration-one frontend ADR.

- [Agent Marketplace Backend Architecture](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/adr/2026-08-28-marketplace-adr-backend.md) — iteration-one backend ADR.

- [Skill marketplace sources](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/guides/skill-marketplace-sources.md) — existing source registration and approval process.

- [Agent Builder](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/agent-builder.md) — existing agent creation experience.

- [Codex and OpenCode Agent Runners](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/codex-opencode-agent-runners.md) — runtime product requirements.

- [Delegated Agent Identity](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/delegated-agent-identity.md) — identity and permission boundaries.

- [MCP / Tool Gateway](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/mcp-tool-gateway.md) and [kora MCP](https://github.com/runkora/kora/blob/39e45397135ad0fe2367cbdcbd9ac6982d77a54b/docs/prd/orbit-mcp.md) — governed tools and agent management workflows.

- [Skills gateway and marketplace](https://app.notion.com/p/3a5d125495bf816daa88f17e741bc66d) — initiative brief.

- [Skills runtime proposal](https://app.notion.com/p/09ed125495bf82c5b9c18126e24ff12b) — prior validation work.

- [Skills Marketplace capability](https://app.notion.com/p/3c9d125495bf80d7bbe6ea8d7327d83a) — capability page.

### References

- [Agent Skills specification](https://agentskills.io/specification)

- [Claude Code skills](https://code.claude.com/docs/en/skills)

- [Claude Code plugins](https://code.claude.com/docs/en/plugins-reference)

- [Codex skills](https://learn.chatgpt.com/docs/build-skills)

- [OpenAI plugins](https://developers.openai.com/plugins/build/plugins)

- [OpenCode skills](https://opencode.ai/docs/skills/)

- [OpenCode plugins](https://opencode.ai/docs/plugins/)

### Glossary

- **Marketplace:** The Kora area for finding and reviewing approved skills and plugins.

- **Marketplace source:** An approved repository that provides packages to a tenant.

- **Package:** A skill or plugin listed in the Marketplace.

- **Skill:** Instructions and supporting material that help an agent perform a task.

- **Plugin:** A package that groups skills and other agent capabilities.

- **Plugin part:** One capability inside a plugin, such as a skill, command, subagent, automatic action, or tool integration.

- **Bundle:** A named group of skills and plugins maintained by a curator, such as a frontend bundle.

- **Bundle curator:** A person authorized to create and maintain bundles.

- **Selection:** A skill, plugin, or bundle chosen for an agent. It provides content the agent can use and grants no extra permission.

- **Runtime:** The program that runs the agent: Claude Code, Codex, or OpenCode.

- **Compatibility:** Whether the runtime can use a package or plugin part fully, partly, or at all, or whether support is unknown.

- **Source version:** The exact version of the source content a package came from.

- **Update behavior:** Whether an agent keeps a chosen version or receives later updates (Open Question 6).

- **Agent creator:** A person who creates an agent.

- **Agent owner or editor:** A person authorized to change the agent's configuration.

- **Tenant:** A company or team workspace in Kora.

- **Operator:** A person or team that runs Kora.

- **Tenant administrator:** A person who manages their tenant's Marketplace sources.

- **Platform administrator:** A person who approves Marketplace sources for tenants.

- **Security reviewer:** A person who reviews packages and agents for risks.

**Approval Signatures:**

- Product Owner: TBD — Pending

- Technical Lead: TBD — Pending

- Security Reviewer: TBD — Pending
