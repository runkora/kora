# GitHub Connector Route Coverage and Repository Qualifiers for ID-Addressed Requests

<!-- Filename: 2026-11-15-github-connector-route-coverage.md -->

**Status:** Accepted  
**Date:** 2026-11-15  
**Deciders:** Justin Wilkin  
**Technical Story:** [#486](https://github.com/runkora/kora/issues/486), [#1026](https://github.com/runkora/kora/issues/1026)  

---

## Context

Touches *What can it access?* Identity and credential brokering are unchanged.

### Background

The `github_app` classifier maps each request to a category action and a repository qualifier (`backend/internal/credentials/adapters/githubapp/githubapp.go`, `graphql.go`). Three accepted decisions bound it:

*   The [category-scoped actions ADR](2026-07-22-github-connection-category-scoped-actions.md) (Decision C) sends a request outside the category table to the unqualified `repo:read` or `repo:write` catch-all.
*   The [GraphQL field classification ADR](2026-09-29-github-graphql-field-classification.md) leaves a request unqualified when it addresses objects by node ID (Option 4, rejected). An unrecognized field makes the request need the `repo:<verb>` umbrella.
*   [JIT brokering Amendment 5](2026-06-06-jit-credential-brokering-egress-gateway.md) rejects resolving `/repositories/{id}` paths to a repository.

The [same-host connection selection ADR](2026-10-15-same-host-connection-selection.md) added `NodeOwners` and `RepoOwners`. They resolve node IDs and repository IDs through GitHub with a read-only lookup token limited to the caller's categories.

### Problem Statement

Agents hold category actions qualified by repository. Between 2026-11-08 and 2026-11-15, 126 of 559 scanned production runs hit a GitHub denial for work their grant describes. A replay of 102 requests captured from gh 2.103.3 classifies 25 of them so that a per-repository category grant denies them:

*   Thirteen are node-ID addressed and unqualified: every gh mutation (`gh pr create`, `gh pr edit`, `gh pr ready`, `gh pr comment`, `gh pr review`, `gh pr merge`, `gh issue create`, `gh issue edit --add-label`) and the `gh pr checks` status query.
*   Eight select a GraphQL field outside the maps: `Query.search`, `Query.repositoryOwner`, `Repository.sshUrl` and `Repository.parent`.
*   Four reach the REST catch-all: `/repos/{o}/{r}/readme`, `/repos/{o}/{r}/tarball/{ref}`, `/repositories/{id}/...` pagination and `/orgs/{org}/teams`.

Agents work around some of these through REST. `gh pr edit`, `gh pr ready` and `gh pr comment` have no REST fallback in gh, and runs end blocked. Grants have gained `repo:*` actions to get past denials. The denial body states the action and qualifier but not the request path or whether the catch-all produced the action.

### Goals

*   Classify every captured gh request to the category action GitHub requires, qualified by its repository.
*   Qualify node-ID and repository-ID requests by the repository they address.
*   Deny an unrecognized request with a message that states the request.
*   Return a denial that states the missing grant when a read of a private resource is refused.

### Non-Goals

*   Connections for owners other than the connection's owner.
*   Changing which permissions GitHub requires for a field or route.

---

## Decision Drivers

*   A grant narrowed to repositories must cover the gh commands its categories describe.
*   An unrecognized request must not be allowed by a grant that was not reviewed for it.
*   A denial must state whether the grant or the classifier needs the change.

---

## Considered Options

### Decision A: Qualifier for ID-addressed requests

*   **Option 1 (chosen):** Resolve node IDs and repository IDs to their owner and repository through the existing lookup, and qualify the request by that repository. The lookup uses the documented GraphQL `nodes` and REST `/repositories/{id}` responses.
*   **Option 2 (rejected):** Decode the repository from the node ID. GitHub does not document the node ID layout.
*   **Option 3 (rejected):** Keep ID-addressed requests unqualified. Every gh mutation would need an unqualified grant over all of the owner's repositories.

### Decision B: Requests outside the category table

*   **Option 1 (chosen):** Deny a request the classifier does not recognize, with a denial that states the request. `repo:read` and `repo:write` remain grantable as an umbrella that covers every category.
*   **Option 2 (rejected):** Keep the verb-classified `repo:<verb>` catch-all. A grant holding the umbrella allows requests nobody reviewed, and a category grant gets a denial that names an action it was never meant to hold.

### Decision C: Anonymous fallback for a private resource

*   **Option 1 (chosen):** Pass GitHub's refusal of an anonymous read back as the kora denial. A clone of a private repository outside the grant then states the missing grant.
*   **Option 2 (rejected):** Forward GitHub's anonymous response. git reports `could not read Username` and the agent cannot tell a missing grant from a missing repository.

### Decision D: Check run workflow fields

*   **Option 1 (chosen):** Keep `actions:read` as the requirement for `workflowRun` and `workflowRuns`. With a token that lacks `actions`, GitHub answers the `gh pr view` document with HTTP 200, a null `workflowRun` and a `FORBIDDEN` error. gh 2.103.3 treats the error as fatal, and `gh pr view` and `gh pr checks` exit 1.
*   **Option 2 (rejected):** Drop the requirement and forward the request. The command still fails at GitHub, and the failure no longer states the missing grant.

---

## Decision

The GitHub adapter and the broker will change as follows:

1.  The classifier will gain these entries:
    *   `/repos/{o}/{r}/readme` and `/repos/{o}/{r}/tarball/{ref}` and `/repos/{o}/{r}/zipball/{ref}` will classify as `contents:read`.
    *   `/orgs/{org}/...` and `/users/{user}/...` will classify by the category GitHub requires for the route, unqualified.
    *   `Query.search` will classify by the types the search selects, qualified by the `repo:` terms of its query as REST search is.
    *   `Query.repositoryOwner`, `Repository.sshUrl` and `Repository.parent` will classify as `metadata:read`.
    *   `branchProtectionRule` and `/branches/{b}/protection` will require `protection:read` or `protection:write`, which mints administration only.
    *   `/orgs/{org}/teams`, `/orgs/{org}/members`, `/orgs/{org}/memberships` and `/orgs/{org}/invitations` will classify as `members:read` or `members:write`, unqualified. The `members` category will mint the organization Members permission.
    *   A search query with an `OR` or `NOT` operator, a parenthesised `repo:` term, or an `org:` or `user:` term will classify unqualified.
2.  `/repositories/{id}/...` will classify as the `/repos/{owner}/{repo}/...` path it resolves to.
3.  A request that addresses objects by node ID will be qualified by their repository when every resolved ID belongs to one repository of the connection's owner. An ID that does not resolve, or IDs across several repositories, leave the request unqualified.
4.  Lookup results will be cached by ID for the lifetime of the lookup token.
5.  A request the classifier does not recognize will be denied as `unclassified`. A GraphQL field outside the maps is unrecognized. The denial will state the method and path, and the field for GraphQL.
6.  No request will classify as `repo:read` or `repo:write`. A GraphQL body that cannot be read or parsed will be denied as `unclassified`. `repo:read` and `repo:write` will remain grantable as the legacy umbrella that requires every category action of their verb.
7.  Every `not_granted` denial will state the method and path of the request.
8.  An anonymous fallback read that GitHub answers with 401, 403 or 404 will return the original denial.
9.  A table test will classify every fixture in `testdata/gh/` and every captured REST request, and assert the action, alternates, also-required actions and qualifier.

This decision supersedes Decision C of the category-scoped actions ADR, Option 4 and the node-ID clause of "Qualifier and owner" in the GraphQL field classification ADR, and the "Resolve repository IDs for paginated links" rejection in JIT brokering Amendment 5.

---

## Consequences

### Positive

*   Agents with per-repository category grants can run the gh mutations and `gh pr checks`.
*   `gh api --paginate` works on a private granted repository past the first page.
*   `repo:*` actions added only to get past classification can be removed from grants.

### Negative

*   A request that the old catch-all allowed for an agent holding `repo:read` or `repo:write` is denied when the classifier does not recognize it. Each such request needs a classifier entry.
*   A node-ID or repository-ID request costs a GitHub lookup on a cache miss.
*   A new gh release that selects an unmapped field is denied until the classifier maps it.
*   Org team and invitation routes need the GitHub App installation to hold the Members permission.

### Neutral

*   `gh pr view` and `gh pr checks` on a pull request with Actions checks need `actions:read`. `gh pr status` needs `protection:read`.

---

## Related Decisions

*   [GitHub connection category-scoped actions](2026-07-22-github-connection-category-scoped-actions.md)
*   [GitHub GraphQL field classification](2026-09-29-github-graphql-field-classification.md)
*   [JIT credential brokering egress gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
*   [Same-host connection selection](2026-10-15-same-host-connection-selection.md)
*   [Connection scope qualifiers](2026-07-22-connection-scope-qualifiers.md)
