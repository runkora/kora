# GitHub Connector Coverage of the GitHub API

<!-- Filename: 2026-11-15-github-connector-route-coverage.md -->

**Status:** Accepted  
**Date:** 2026-11-15  
**Deciders:** Justin Wilkin  
**Technical Story:** [#486](https://github.com/runkora/kora/issues/486), [#1026](https://github.com/runkora/kora/issues/1026)  

---

## Context

Touches *What can it access?* Identity and credential brokering are unchanged.

### Background

The `github_app` connector classifies each request to an action and a repository qualifier, and mints an installation token for it (`backend/internal/credentials/adapters/githubapp/`). Three accepted decisions shape it today:

*   The [category-scoped actions ADR](2026-07-22-github-connection-category-scoped-actions.md) defines the categories `contents`, `issues`, `pulls`, `actions`, `checks` and `admin`. It keeps `repo:read` and `repo:write` as a catch-all for routes outside a hand-written route table and as a legacy umbrella.
*   The [GraphQL field classification ADR](2026-09-29-github-graphql-field-classification.md) maps selected GraphQL fields to categories through hand-written field maps. An unmapped field needs the `repo:<verb>` umbrella, and a node-ID addressed request is unqualified.
*   [JIT brokering Amendment 5](2026-06-06-jit-credential-brokering-egress-gateway.md) rejects resolving `/repositories/{id}` paths to a repository.

GitHub publishes, for every REST operation, the alternative permission sets an installation token can use (`progAccess.permissions` in `src/rest/data/fpt-2022-11-28/*.json` in the `github/docs` repository). It lists 1,025 operations that installation tokens can call. GitHub publishes no equivalent data for GraphQL fields.

The [same-host connection selection ADR](2026-10-15-same-host-connection-selection.md) added lookups that resolve node IDs and repository IDs through GitHub with a read-only lookup token.

### Problem Statement

Agents hold category actions qualified by repository. Between 2026-11-08 and 2026-11-15, 126 of 559 scanned production runs hit a GitHub denial for work their grant describes. A replay of 102 requests captured from gh 2.103.3 classifies 25 of them so that a per-repository grant denies them. The causes are node-ID addressed mutations, GraphQL fields outside the maps, and REST routes outside the table. Each fix so far has covered one route, and grants have gained `repo:*` actions to get past denials.

### Goals

*   Classify every GitHub REST endpoint an installation token can call.
*   Authorize every GraphQL document by the repository it targets and the grants held on it.
*   Qualify node-ID and repository-ID requests by the repository they address.
*   Deny an unrecognized request with a message that states the request.

### Non-Goals

*   Connections for owners other than the connection's owner.
*   User-level and enterprise-level GitHub permissions.

---

## Decision Drivers

*   A grant narrowed to repositories must cover the gh commands and API calls its categories describe.
*   Coverage must follow GitHub's API without a hand-written entry per route.
*   A token must carry only the permissions the request needs.

---

## Considered Options

### Decision A: Action vocabulary

*   **Option 1 (chosen):** Group GitHub App permissions into categories that agents use, and leave permissions agents do not need out of the vocabulary.
*   **Option 2 (rejected):** One action per GitHub App permission. It adds about 128 actions per connection to the grant picker.
*   **Option 3 (rejected):** Keep the current categories and the `repo:<verb>` catch-all. Coverage stays limited to the hand-written route table.

### Decision B: REST classification

*   **Option 1 (chosen):** Generate the route table from GitHub's published server-to-server permission data.
*   **Option 2 (rejected):** Extend the hand-written route table. Every GitHub API addition needs a manual entry.

### Decision C: GraphQL authorization

*   **Option 1 (chosen):** Authorize a GraphQL document by its target repository and operation type, and mint a token with the permissions of the categories the agent holds on that repository. GitHub refuses any field outside the token's permissions.
*   **Option 2 (rejected):** Map every type and mutation in GitHub's public schema to a permission. GitHub publishes no field permission data, and each of about 1,000 entries would be inferred.
*   **Option 3 (rejected):** Keep the hand-written field maps. Every field that agents select and the maps omit denies the whole document.

### Decision D: Qualifier for ID-addressed requests

*   **Option 1 (chosen):** Resolve node IDs and repository IDs through the existing lookup, and qualify the request by the repository they resolve to.
*   **Option 2 (rejected):** Decode the repository from the node ID. GitHub does not document the node ID layout.
*   **Option 3 (rejected):** Keep ID-addressed requests unqualified. Every gh mutation would need an unqualified grant.

### Decision E: Anonymous fallback for a private resource

*   **Option 1 (chosen):** Pass GitHub's refusal of an anonymous read back as the kora denial. A clone of a private repository outside the grant then states the missing grant.
*   **Option 2 (rejected):** Forward GitHub's anonymous response. git reports `could not read Username` and the agent cannot tell a missing grant from a missing repository.

---

## Decision

The GitHub connector will change as follows.

### Vocabulary

1.  Each GitHub connection will publish these categories, each with `read` and `write`:
    *   `contents`: contents.
    *   `workflows`: workflows.
    *   `issues`: issues.
    *   `pulls`: pull_requests.
    *   `ci`: actions, checks, statuses, artifact_metadata and attestations.
    *   `deployments`: deployments, environments and pages.
    *   `security`: security_events, vulnerability_alerts, secret_scanning_alerts, repository_advisories and code_quality.
    *   `secrets`: secrets, actions_variables, dependabot_secrets, agent_secrets and agent_variables.
    *   `admin`: administration, repository_hooks, repository_custom_properties, repository_creation and installation_repositories.
    *   `members`: members.
    *   `org_projects`: organization_projects.
2.  The repository categories can be qualified by repository. `members` and `org_projects` are organization-wide and unqualified.
3.  `metadata` stays the baseline every grant receives.
4.  `repo:read`, `repo:write`, `actions`, `checks` and the earlier category definitions will be removed. The covering parents `connection:<name>:read` and `connection:<name>:write` are the grant for every category. Publishing the vocabulary purges a removed action from every stored grant and role that holds it.

### REST

5.  A generator will build the route table from GitHub's REST operation data for API version 2022-11-28, pinned to one `github/docs` commit. The generated table and the generator are committed, and a test checks that every operation in the data classifies.
6.  An endpoint classifies to the smallest of GitHub's alternative permission sets for it. Alternatives that each need one permission become alternate actions. A set that needs several permissions requires every category in it. A `write` grant on a category also authorizes that category's reads, and the token for such a read carries the read permissions.
7.  The minted token carries the permissions of the set the grant satisfied, plus metadata. A write to contents, refs or releases, and a git push, also carries `workflows` when the agent holds `workflows:write` on the repository.
8.  An endpoint that needs no permission classifies as `metadata:read`. An endpoint that needs `admin` access classifies as `write` and mints `admin` access. An endpoint whose permission is in no category is denied as unclassified.
9.  Surfaces outside the data keep hand-written entries: git smart HTTP and git LFS (`contents`), `codeload.github.com` and `*.githubusercontent.com` (`contents`), `uploads.github.com` release assets (`contents`), and the Actions results receiver (`ci`).
10.  A search query with an `OR` or `NOT` operator, a parenthesised `repo:` term, or an `org:` or `user:` term will classify unqualified.

### GraphQL

11.  A GraphQL query requires a `read` grant on the target repository or the metadata baseline, and a mutation requires a `write` grant on the target repository.
12.  The target repository comes from `repository()` arguments, resolved node IDs and search `repo:` terms, by the qualifier rules below. A document with no single target repository is authorized against unqualified grants only.
13.  The minted token carries the GitHub permissions of every category the agent holds on the target repository at the operation's level, limited to the permissions the installation holds, plus metadata.
14.  A document that cannot be read, parsed or attributed to one operation, and a subscription, are denied as unclassified.

### Qualifiers

15.  `/repositories/{id}/...` will classify as the `/repos/{owner}/{repo}/...` path it resolves to.
16.  A release asset download from `release-assets.githubusercontent.com` is qualified by the repository its path names, through the same lookup. This supersedes the anonymous fallback in the [release asset downloads ADR](2026-10-26-release-asset-downloads-without-connection-selection.md) for that host.
17.  A request that addresses objects by node ID will be qualified by their repository when every resolved ID belongs to one repository of the connection's owner. An ID that does not resolve, or IDs across several repositories, leave the request unqualified. Ids of users, bots and teams address no repository and do not affect the qualifier.
18.  Lookup results will be cached by ID for the lifetime of the lookup token.

### Denials and fallback

19.  A request the connector does not recognize will be denied as `unclassified`. The denial will state the method and path, and never the query string.
20.  Every `not_granted` denial will state the method and path of the request.
21.  An anonymous fallback read that GitHub answers with 401, 403 or 404 will return the original denial. A 403 that reports an exhausted rate limit is relayed.

This decision supersedes the [category-scoped actions ADR](2026-07-22-github-connection-category-scoped-actions.md) Decisions A, B, C and D, the [GraphQL field classification ADR](2026-09-29-github-graphql-field-classification.md), and the "Resolve repository IDs for paginated links" rejection in JIT brokering Amendment 5.

---

## Consequences

### Positive

*   Agents with per-repository grants can run the gh mutations, `gh pr checks` and `gh pr view` with `ci:read`.
*   `gh api --paginate` works on a private granted repository past the first page.
*   Coverage of a new GitHub REST endpoint is a regeneration of the route table.

### Negative

*   Grants and roles that hold `repo:*`, `actions`, `checks` or the earlier categories fail at the next sync until they are rewritten.
*   A GraphQL field outside the agent's grants fails at GitHub with `Resource not accessible by integration`. The kora denial does not state the missing category.
*   The audit record of a GraphQL request states the repository and the permissions minted. It does not list the fields selected.
*   A node-ID or repository-ID request costs a GitHub lookup on a cache miss.
*   Organization routes need the GitHub App installation to hold the matching organization permission.

### Neutral

*   Codespaces, Copilot, self-hosted runner, organization secret and organization administration endpoints are denied as unclassified.

---

## Related Decisions

*   [GitHub connection category-scoped actions](2026-07-22-github-connection-category-scoped-actions.md)
*   [GitHub GraphQL field classification](2026-09-29-github-graphql-field-classification.md)
*   [JIT credential brokering egress gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
*   [Same-host connection selection](2026-10-15-same-host-connection-selection.md)
*   [Connection scope qualifiers](2026-07-22-connection-scope-qualifiers.md)
