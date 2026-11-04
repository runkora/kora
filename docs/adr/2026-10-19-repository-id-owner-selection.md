# Repository Id Owner Selection in the Egress Gateway

<!-- Filename: 2026-10-19-repository-id-owner-selection.md -->

**Status:** Accepted
**Date:** 2026-10-19
**Deciders:** Justin Wilkin
**Technical Story:** [#584](https://github.com/runkora/kora/issues/584): paginated REST follow-up pages denied with `connection_unresolved`.

---

## Context

### Background

[Same-Host Connection Selection](2026-10-15-same-host-connection-selection.md) selects among connections that serve one host by the owner in the request path, the owners a GraphQL document or search query names, or the owners its node ids resolve to. A request that names none is served by the one candidate the caller holds, and is denied when the caller holds several.

GitHub returns `Link` headers for paginated REST calls whose next URL uses the form `/repositories/{id}/...` instead of `/repos/{owner}/{repo}/...`. `gh api --paginate` follows that URL as given.

### Problem Statement

`apiOwners` reads owners only from `repos`, `orgs` and `users` paths. A `/repositories/{id}/pulls/{n}/files?page=2` request names no owner, so an agent that holds two GitHub connections is denied with `connection_unresolved` and loses every page after the first.

### Goals

*   A `/repositories/{id}/...` request selects the connection bound to the repository's owner.
*   The lookup adds no new credential surface beyond the node id lookup.

### Non-Goals

*   Owner-less requests that are not addressed by a repository id, such as `/user`.

---

## Which of the three questions this touches

This decision touches **How does it connect?** It changes which connection serves a request.

**Who is this agent?** and **What can it access?** are preserved. The lookup uses the read-only token and held-connection rules of the node id lookup. Its result selects among the caller's held connections and never reaches the agent or a denial.

---

## Decision Drivers

*   The gateway keeps no state that links a follow-up request to the first page.
*   Selection must never use a connection the caller does not hold.
*   The change must reuse the existing lookup, cache and audit path.

---

## Considered Options

### Option 1: Resolve the repository id to its owner (chosen)

**Description:** The adapter reports the repository id in a `/repositories/{id}` path. The broker resolves it with `GET /repositories/{id}` under the read-only lookup token of each held candidate, then selects by the owner as for node ids.

**Pros:**
*   Works for any request, including the first one and unrelated follow-ups.
*   Reuses the token minting, cache, span and log of the node id lookup.

**Cons:**
*   A cache miss adds one GitHub API call per id.

### Option 2: Remember the connection that served the first page

**Description:** The gateway records the connection per request chain and reuses it for follow-up requests.

**Pros:**
*   No extra GitHub call.

**Cons:**
*   The gateway has no request-chain identifier, and `gh` sends none.
*   It needs new shared state keyed by caller, and a wrong match would mint another account's credential.

### Option 3: Require agents to hold one GitHub connection

**Description:** Leave selection unchanged and document the limit.

**Pros:**
*   No code change.

**Cons:**
*   Agents with several GitHub connections cannot paginate, which is the defect.

---

## Decision

**Chosen Option:** Option 1, resolve the repository id to its owner.

### Rationale

It is stateless and fits the selection rules the 2026-10-15 ADR already accepted. Option 2 needs chain identity that does not exist. Option 3 leaves the defect.

### Details

*   Only a numeric second segment of `/repositories/{id}` on the API host, after any `/api/v3` prefix, names a repository id.
*   An owner-addressed path never carries a repository id, so the two do not mix.
*   The lookup runs only when the caller holds several candidates, with the held categories, and a request that names more than 100 ids is unresolved.
*   Results use the existing 10 minute cache, keyed by lookup kind, tenant, host, connection, categories and id. An unseen id is cached as unseen.
*   Denials never name a resolved owner.

---

## Consequences

### Positive

*   `gh api --paginate` on a repository-id follow-up page selects the right connection.

### Negative

*   A cache miss adds one GitHub API call, counted against the candidate installation's rate limit.
*   A private repository that no held installation can see stays unresolved.

### Neutral

*   Other owner-less REST paths still need the agent to hold exactly one connection.

---

## Related Decisions

*   [Same-Host Connection Selection in the Egress Gateway](2026-10-15-same-host-connection-selection.md): the selection rules this extends

**Last Updated:** 2026-10-22
