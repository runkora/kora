# GitHub GraphQL Authorised by the Granular Actions of Selected Fields

<!-- Filename: 2026-09-29-github-graphql-field-classification.md -->

**Status:** Accepted
**Date:** 2026-09-29
**Deciders:** Justin Wilkin
**Technical Story:** Extends the `github_app` classifier from the [category-scoped actions ADR](2026-07-22-github-connection-category-scoped-actions.md), which left GraphQL on the umbrella and deferred parsing it.

---

## Context

**Which of the three questions this touches:** *What can it access?* A GraphQL request is checked against the granular actions of what it reads or writes instead of the `repo:*` umbrella, and the minted token narrows to the same actions. *Who is this agent?* is unchanged. *How does it connect?* gains one broker capability: a classification may require more than one action.

### Background

The `github_app` adapter classifies REST and git calls into category actions (`metadata`, `contents`, `issues`, `pulls`, `checks`, `actions`, `admin`) with a repository qualifier, and mints an installation token narrowed to the authorised categories and repository. GraphQL was excluded: `classifyGraphQL` reads the leading operation keyword and returns `repo:read` for a query and `repo:write` for anything else, with the qualifier taken from a regex over the first `repository(owner:, name:)` call or the `owner`/`name` variables.

A granular grant never covers the umbrella: `repo:read` confers every category read through vocabulary `requires` edges, but `pulls:read` does not confer `repo:read`. An agent attached with granular actions is therefore denied every GraphQL request. The GitHub CLI uses GraphQL for `gh pr view`, `gh pr diff`, `gh pr checks`, `gh pr list` and `gh issue view`, so an agent with a code-reviewer attachment (`metadata:read`, `contents:read`, `pulls:read`, `pulls:write`, `issues:read`, `issues:write` on one repository) cannot run any of them.

### Problem Statement

GraphQL requests are authorised against an action that granular grants never hold, so least-privilege GitHub attachments cannot use the GitHub CLI's read commands, and the only way to unblock them is the umbrella grant that least privilege exists to avoid.

### Goals

- A granular grant satisfies the GraphQL documents the GitHub CLI sends for the operations the grant covers.
- A GraphQL request never needs less than the REST call for the same operation needs.
- The owner check, the repository qualifier and byte-identical body forwarding keep their current semantics.
- Anything the classifier does not recognise or cannot parse fails closed.
- A regression test replays the exact documents `gh` sends against a code-reviewer grant and an issues-only grant.

### Non-Goals

- `search`, `organization`, `user`, projects, discussions and every other GraphQL surface without a category. They keep the umbrella until a category exists for them.
- Classifying nested selections. What a nested field can reach is bounded by the minted token (see Decision).
- Schema-aware validation. The classifier does not load GitHub's schema.

---

## Decision Drivers

- The REST classifier is the reference. A GraphQL field that reads pull requests needs what `GET /repos/{o}/{r}/pulls` needs.
- GitHub is the second enforcement point. The token is minted with only the authorised permissions and repository, so the classifier decides what to authorise and mint, and GitHub refuses whatever the token does not carry.
- Unrecognised input must degrade to the behaviour before this ADR, never to something wider.

---

## Considered Options

### Option 1: Parse the document and map selected fields to granular actions

**Description:** Parse the GraphQL document with a real parser. Map each field the operation selects at its entry points (top-level fields, the fields of `repository`, the type conditions of `node`/`nodes`, and each mutation) to a category action. Require all of them. Any unrecognised field sends the whole request to the umbrella.

**Pros:**
- Least privilege: an issues-only agent cannot read pull requests or code through GraphQL, and the token it gets carries only `issues`.
- Denials happen at kora with the exact action in the log, consistent with REST.

**Cons:**
- A field map to maintain as agents use more of the GraphQL surface.
- The broker must learn to require several actions for one request.

### Option 2: Any granular read on the repository satisfies a GraphQL read

**Description:** Classify a GraphQL query as satisfied by any category read the grant holds on the repository, and mint the token from whatever the grant holds.

**Pros:**
- Small change, no parser.

**Cons:**
- Over-grants at the gate: an issues-only agent is authorised for a query that reads code, and kora's audit log records it as allowed. Enforcement moves entirely to GitHub.
- The minted permissions depend on the grant rather than the request, unlike every other GitHub classification.

### Option 3: Keep GraphQL denied

**Description:** Leave GraphQL on the umbrella and direct agents to REST.

**Pros:**
- No change.

**Cons:**
- The GitHub CLI's pull request and issue read commands stay unusable for every least-privilege agent. Agents waste calls on the denial before falling back to REST, and prompts carry workarounds.

### Option 4: Qualify node-ID requests by decoding the repository from the ID

**Description:** As option 1, and additionally decode the repository database ID from GitHub global node IDs in `node`/`nodes` lookups and mutation inputs, resolve it to a repository name through the installation's repository list, and qualify the request by it.

**Pros:**
- Repository-qualified grants could use `gh pr checks` and the gh mutations, which address objects by node ID.

**Cons:**
- Depends on GitHub's node ID layout (a type prefix and a MessagePack payload), which is not a documented contract.
- Adds a MessagePack decoder, a list of repository-scoped ID prefixes, an installation repository listing on the classification path, and a cache for it. That is too much complexity for the value.

---

## Decision

**Chosen Option:** Option 1 - Parse the document and map selected fields to granular actions. Option 4 is rejected.

### Parser

The classifier parses the query with `github.com/vektah/gqlparser/v2` (`parser.ParseQueryWithTokenLimit`), without a schema. The module is already in kora's build: OPA's GraphQL built-ins depend on it, so every binary that links the policy package already compiles it. Making it a direct dependency adds no new module to `go.sum`. A hand-written scanner is what the previous classifier used; it could not see aliases, fragments, nested repository calls or variable defaults, which is how a second `repository(owner: "evil")` went unchecked.

### Requirements per field

Each selected entry-point field contributes a requirement: one action, or two alternatives where the object can be an issue or a pull request (the REST shared issue-number routes carry the same alternate). The request needs every requirement.

| Entry point | Field or type | Requirement |
|---|---|---|
| Query root | `viewer`, `rateLimit`, `__schema`, `__type`, `__typename` | `metadata:read` |
| Query root | `repository`, `node`, `nodes` | walked below, plus `metadata:read` |
| `Repository` | `pullRequest`, `pullRequests` | `pulls:read` |
| `Repository` | `issue`, `issues`, `label(s)`, `milestone(s)`, `assignableUsers` | `issues:read` |
| `Repository` | `issueOrPullRequest` | `issues:read` or `pulls:read` |
| `Repository` | `object`, `ref`, `refs`, `defaultBranchRef`, `release`, `releases`, `latestRelease` | `contents:read` |
| `Repository` | scalar metadata (`id`, `name`, `nameWithOwner`, `owner`, `description`, `url`, visibility and feature flags, timestamps, merge settings) | `metadata:read` |
| `node`/`nodes` | `... on PullRequest` and its reviews, review comments, threads, commits | `pulls:read` |
| `node`/`nodes` | `... on Issue`, `Label`, `Milestone` | `issues:read` |
| `node`/`nodes` | `... on IssueComment` | `issues:read` or `pulls:read` |
| `node`/`nodes` | `... on Commit`, `Tree`, `Blob`, `Ref`, `Tag`, `Release` | `contents:read` |
| `node`/`nodes` | `... on CheckRun`, `CheckSuite`, `StatusContext` | `checks:read` |
| `node`/`nodes` | `... on Repository` | the `Repository` rows |
| `node`/`nodes` | `id`, `__typename` only | `metadata:read` |
| Any depth | `statusCheckRollup`, `checkSuites`, `checkRuns` | `checks:read` |
| Any depth | `workflowRun`, `workflowRuns` | `actions:read` |
| Mutation | pull request lifecycle and reviews (`createPullRequest`, `updatePullRequest`, `closePullRequest`, `addPullRequestReview`, `requestReviews`, and the rest of that family) | `pulls:write` |
| Mutation | `mergePullRequest` | `contents:write` |
| Mutation | `createIssue`, `updateIssue`, `closeIssue`, `reopenIssue`, sub-issues | `issues:write` |
| Mutation | `addComment`, comment edits, assignees | `issues:write` or `pulls:write` |
| Mutation | labels and locking (`addLabelsToLabelable`, `lockLockable`, and their inverses) | umbrella: their targets include discussions |

Fragment spreads and inline fragments are looked through. A named fragment is expanded in every context it is spread into, so the same fragment under `repository` and under `node` is classified in both. A fragment cycle, an undefined fragment, or more than 1,000 expansions in one request makes the whole request need the unqualified umbrella.

**Unknown fields.** A field, type condition or mutation outside the table, or a spread of a fragment the document does not define, makes the whole request need `repo:read` (query) or `repo:write` (mutation), unqualified or qualified exactly as before. The umbrella holder keeps working, and no granular holder gains anything.

**Nested selections.** Apart from the check fields in the table, the classifier reads the entry points, not the fields beneath them, the same way a REST path decides the category and the response embeds related objects. Nested reach is bounded by the minted token: it carries only the permissions of the authorised actions and only the qualified repository, and GitHub refuses a nested field the token does not cover. A pull request token also carries `contents:read` (the floor GitHub needs to read a pull request's refs), which a nested `repository { object }` could use, so any GraphQL request with a pull request requirement also requires `contents:read`. The check and workflow run fields are the exception to reading entry points only: GitHub refuses a pull request's `statusCheckRollup` to a token without the checks permission, and a check suite's `workflowRun` to one without the actions permission (`Resource not accessible by integration`). `gh pr view` and `gh pr checks` always select both, so wherever they appear the request requires `checks:read` and `actions:read`.

**Reduction.** Every single-action requirement is required. `metadata:read` is dropped when any category is present, because every category mint carries it. An alternatives requirement already met by a required action is dropped. One remaining alternatives requirement becomes the classification's action and alternate; any further one is held to its first action, which only narrows.

### Broker: required actions

`credentials.Classification` gains `AlsoRequired []string`: actions the request needs in addition to `Action` or one of its `Alternates`. The broker checks each against the grant with the same qualifier. A missing one denies and is logged as `egress denied by policy` with that action. An evaluation error on one fails closed. When all are held they are appended to the authorised list, so the adapter mints the token for the union, exactly as held alternates are today.

### Qualifier and owner

On a connection that declares its `owner`:

- Every `repository` field with arguments, at any depth and inside fragments, is a repository reference. `owner` and `name` are resolved from literals, supplied variables, and the operation's variable defaults. An owner other than the connection's is denied outright, as REST and git are. A call whose owner or name does not resolve to a string cannot be attributed.
- A root `node` or `nodes` lookup names objects by global ID, whose repository is not decoded (option 4), so it cannot be attributed. Mutations address their targets by node ID too, so a mutation names no repository.
- One repository across all references qualifies the request. None, several, or any reference that cannot be attributed leaves it unqualified, which only an unqualified grant covers.

A connection without a declared owner extracts no qualifier, unchanged.

### Fail-closed parsing and limits

The body is read up to 512 KiB and restored byte for byte, with `GetBody` replaying the same bytes. The envelope's `query`, `variables` and `operationName` keys are matched exactly: Go's JSON decoder matches keys case-insensitively, so a body carrying a case variant such as `QUERY` could be classified on a document GitHub does not run, and is rejected. An absent, unreadable or larger body, non-JSON, an empty `query`, a parse error, a document over 20,000 tokens, several operations without an `operationName`, an envelope key differing from `query`, `variables` or `operationName` only by case, an `operationName` the document does not define, and a subscription all classify as the unqualified `repo:write`. An oversized body is still forwarded whole.

### Regression guard

`backend/internal/credentials/adapters/githubapp/testdata/gh/` holds the request bodies `gh` 2.101.0 sends for `gh pr view` (by number and for the current branch), `gh pr diff`, `gh pr checks` (including its introspection probes and the `node` status-check query), `gh pr list`, `gh issue view` and `gh repo view`, captured with `GH_DEBUG=api`, and the comment, review, merge, create-PR and create-issue mutations reconstructed from the gh source. A table test classifies each one and asserts which a code-reviewer grant on one repository, the same grant on every repository, and an issues-only grant allow.

---

## Consequences

### Positive

- Least-privilege GitHub attachments can use `gh pr view`, `gh pr diff`, `gh pr list` and `gh issue view`, and unqualified ones also `gh pr checks` and the gh pull request and issue mutations.
- An issues-only agent is denied pull request and code reads through GraphQL at the gate, and its token carries only `issues`.
- Repository-qualified agents can use GraphQL queries addressed by `repository(owner:, name:)`, which the qualifier ADR accepted losing.
- The owner check now covers every `repository` call in a document, not only the first.

### Negative

- A field that can be an issue or a pull request (`issueOrPullRequest`, `IssueComment`, `addComment`, assignees) is satisfied by either category. When the grant holds the pull request action, the token carries it and its `contents:read` floor, so a grant holding pulls but not contents can read file contents through a nested selection of such a field. Strict pull request fields close this by requiring `contents:read`; the alternates cannot without denying issues-only agents.

- The field and mutation tables need entries as agents use more of the GraphQL surface. Until then such requests need the umbrella, as today.
- `gh pr view` needs `checks:read` and `actions:read` as well as `pulls:read` and `contents:read`, because it always selects the checks rollup and its workflow runs. A code-reviewer attachment needs both.
- Repository-narrowed grants cannot use `gh pr checks`, whose status query is a `node` lookup, or any gh mutation (`gh pr comment`, `gh pr review`, `gh pr create`, `gh issue create`), whose targets are node IDs. Those need an unqualified grant of the same granular actions; the REST API remains available to qualified agents.

### Neutral

- `gh pr status` still needs the umbrella: it uses `search`.

---

## Related Decisions

- [github-connection-category-scoped-actions](2026-07-22-github-connection-category-scoped-actions.md): the category vocabulary and minting this extends; its GraphQL non-goal is resolved here.
- [connection-scope-qualifiers](2026-07-22-connection-scope-qualifiers.md): the qualifier rule GraphQL now follows instead of being repo-less.
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the broker gaining `AlsoRequired`.

---

## References

- [vektah/gqlparser](https://github.com/vektah/gqlparser)

**Last Updated:** 2026-09-29
