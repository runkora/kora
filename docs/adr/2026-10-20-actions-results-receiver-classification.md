# Actions Results Receiver Classification

<!-- Filename: 2026-10-20-actions-results-receiver-classification.md -->

**Status:** Accepted
**Date:** 2026-10-20
**Deciders:** Justin Wilkin
**Technical Story:** [#610](https://github.com/runkora/kora/issues/610)

---

## Context

### Problem Statement

`results-receiver.actions.githubusercontent.com` matches `*.githubusercontent.com`, so the GitHub App adapter sends it to `classifyContentHost`. That classifier allows only GET and HEAD and returns `contents:read`. The Actions log service is called with Twirp POSTs, which fail with `unsupported action for adapter`. Agents cannot read CI job logs.

### Goals

*   An agent holding the Actions read grant can fetch job logs.
*   Write RPCs on the host stay denied.

---

## Considered Options

### Option 1: Dedicated classifier mapping read RPCs to `actions:read`

**Pros:**
- Matches the data served and the existing Actions category.
- Write RPCs stay denied.

**Cons:**
- Agents with only `contents:read` do not gain log access.
- Relies on the Twirp `Get`/`List` naming convention.

### Option 2: Allow POST in `classifyContentHost`

**Pros:**
- One-line change.

**Cons:**
- Allows write RPCs on every content host under `contents:read`.

### Option 3: Map the host to `contents:read`

**Pros:**
- Matches the quoted denial text.

**Cons:**
- Logs are Actions data. Grants `contents:read` more than it names.

---

## Decision

**Chosen Option:** Option 1 - Dedicated classifier.

### Rationale

Logs belong to the Actions category, and a narrow method rule keeps the write surface closed.

---

## Consequences

### Positive
- Agents with unqualified `actions:read` can run `gh run view --log-failed`.

### Negative
- A grant qualified to a repository does not match, because the repository is in the RPC body.

---

## Related Decisions
- [github-connection-category-scoped-actions](2026-07-22-github-connection-category-scoped-actions.md) - Defines the categories.

**Last Updated:** 2026-10-20
