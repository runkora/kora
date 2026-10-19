# Explain GitHub's Workflow-File Push Refusal at the Egress Gateway

**Status:** Proposed
**Date:** 2026-10-19
**Deciders:** justinwilkin
**Technical Story:** [#565](https://github.com/runkora/kora/issues/565)

---

## Context

[Category-scoped GitHub actions](2026-07-22-github-connection-category-scoped-actions.md) deliberately leave the `workflows: write` App permission out of the token minted for `contents:write`. A push that touches `.github/workflows/` is therefore refused by GitHub with `refusing to allow a GitHub App to create or update workflow ... without workflows permission`. The agent reads that as a fault and works around it, as in run 8557c3b5, which restored the file and pushed again.

This touches **What can it access?** and **How does it connect?**. The access boundary is unchanged. Only the message the agent reads changes.

### Goals

- The refusal states that workflow files are out of scope by policy.

### Non-Goals

- Granting `workflows: write` to category-granted agents.

## Considered Options

### Option 1: Add `workflows: write` to the contents:write mint

Pros: pushes of workflow files succeed.
Cons: reverses the ADR's CI-rewrite exfiltration defence for every agent that can push code.

### Option 2: Deny workflow pushes at classification

Pros: the denial comes from kora with a full message.
Cons: the path of a smart-HTTP push does not name the files. They are inside the pack, which the gateway cannot read cheaply.

### Option 3: Annotate GitHub's refusal in the receive-pack response

Pros: no change to permissions. Works with the information the gateway has. The agent sees the policy reason next to GitHub's message.
Cons: the gateway buffers and edits a small git protocol response.

## Decision

Option 3. `ProxyHandler` inspects `git-receive-pack` responses of at most 1 MiB. When a side-band report contains GitHub's `workflows` permission refusal, it appends one band-2 packet before the closing flush. Git prints it as `remote: ...` beside GitHub's own line. All other responses pass through unchanged.

## Consequences

- Agents learn the refusal is deliberate and stop retrying.
- The match depends on GitHub's refusal wording. If it changes, the push still fails as before and only the added note is lost.
- A connection that opts in through `workflows: write` and the `repo:write` umbrella still pushes workflow files.
