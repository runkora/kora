# Release Asset Downloads Without Connection Selection

<!-- Filename: 2026-10-26-release-asset-downloads-without-connection-selection.md -->

**Status:** Proposed  
**Date:** 2026-10-26  
**Deciders:** Justin Wilkin  
**Technical Story:** [#790](https://github.com/runkora/kora/issues/790)

---

## Context

### Background

`gh release download` requests an asset from `github.com` or `api.github.com`, which names the owner. GitHub answers with a redirect to a signed URL on `release-assets.githubusercontent.com`. That URL path is `/github-production-release-asset/...` and names no owner. The signature in the query string authorizes the download.

`selectConnection` (`backend/internal/egressgw/selection.go`) selects among connections that serve one host. A request that names no owner and reaches an agent holding two connections is denied as ambiguous with `connection_unresolved`. The gateway keeps no state between a request and the redirect it issued.

### Problem Statement

An agent that holds two GitHub connections cannot download a release asset. The redirect target is denied.

### Goals

*   An agent holding several GitHub connections downloads release assets.
*   The download adds no credential and no access.

### Non-Goals

*   Serving release assets to an agent in egress mode `none`.

---

## Considered Options

### Option 1: Carry the connection across the redirect

**Description:** The gateway records the connection of the request that issued the redirect and applies it to the follow-up request.

**Pros:**
*   The redirect target stays under the grant of the original connection.

**Cons:**
*   The gateway needs per-agent redirect state with expiry and a link between two requests.
*   The agent client does not need a credential for a signed URL, so the state adds no protection.

### Option 2: Serve the signed asset host through the anonymous read fallback

**Description:** A request to `release-assets.githubusercontent.com` that names no owner is unresolved but not ambiguous. The existing anonymous read fallback then decides it.

**Pros:**
*   No new state. The change is one condition.
*   Attaches no credential and strips the agent's `Authorization` header.
*   Applies the existing rules: reads only, and only when the agent's own egress posture allows the host.

**Cons:**
*   Agents in mode `none` still cannot download release assets of a held connection.

---

## Decision

**Chosen Option:** Option 2 - Serve the signed asset host through the anonymous read fallback

### Rationale

The signature in the URL authorizes the download, so a connection credential adds nothing. The fallback already defines the policy for reads that need no credential. Option 1 adds state to protect nothing.

---

## Consequences

### Positive

*   `gh release download` succeeds for an agent holding both GitHub connections.

### Negative

*   Downloads from this host share GitHub's unauthenticated rate limit with other anonymous reads.

### Neutral

*   Writes to this host and reads from an agent in mode `none` are denied as before.
*   Amends the connection selection rules in [Same-Host Connection Selection](2026-10-15-same-host-connection-selection.md).
