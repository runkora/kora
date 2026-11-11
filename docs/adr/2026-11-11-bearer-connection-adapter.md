# Bearer Connection Adapter

<!-- Filename: 2026-11-11-bearer-connection-adapter.md -->

**Status:** Accepted
**Date:** 2026-11-11
**Deciders:** Platform Engineering

---

## Context

### Background

The egress gateway injects credentials just-in-time through connection adapters (`github_app`, `kubernetes`). Agents never hold a real credential; the sandbox holds a placeholder and the gateway swaps in the real one on the re-originated leg.

### Problem Statement

Some hosts authenticate with a static token in a request header. The first case is the GitHub Packages npm registry (`npm.pkg.github.com`), which a sandbox needs to install `@clear-route` packages. Neither existing adapter fits:

- **`github_app`** mints GitHub App installation tokens. GitHub Packages does not accept installation tokens for the npm registry, only a personal access token with `read:packages`.
- **`kubernetes`** injects a static bearer token, but its `Classify` parses Kubernetes API paths and denies anything it does not recognize, so it cannot serve another host.

### Goals

- Let an agent reach a token-authenticated host through a Connection, with the gateway injecting the credential.
- Keep the sandbox free of the real token.

### Non-Goals

- Path-category, qualifier or per-package authorization.
- Token rotation tooling. Rotation is a Secret update, as for `kubernetes`.
- Any change to `github_app`, `kubernetes` or `passthrough`.

---

## Decision Drivers

- Reuse the existing adapter interface; a new service is a new subpackage, not a change to the parent package.
- Least surprise for admins: one Connection, one secret, one host.
- A typo in config must fail loudly rather than silently send the wrong header.

---

## Considered Options

### Option 1: Extend `kubernetes` with a generic mode (rejected)

Couples an unrelated host to Kubernetes path classification and the cluster action vocabulary.

### Option 2: Use `github_app` with a stored PAT (rejected)

`github_app` mints tokens and carries a repository qualifier and a GitHub host family. A static PAT does not fit that model and would blur what the adapter means.

### Option 3: A new `bearer` adapter (chosen)

A small adapter modelled on `kubernetes` without path categories.

---

## Decision

Add a `bearer` adapter type under `backend/internal/credentials/adapters/bearer`.

**Authorization is coarse, by HTTP method.** `GET` and `HEAD` classify as `read`. `POST`, `PUT`, `PATCH` and `DELETE` classify as `write`. Everything else, including `OPTIONS` and protocol upgrades, is `ErrUnsupportedAction`. The vocabulary is `connection:<name>:read` and `connection:<name>:write`, with no qualifier and no baseline actions. Finer authorization is the upstream's own on the injected token.

**Hosts.** The connection serves exactly its declared `baseHost` (`declaredHostOnly`). Hosts without a bearer connection keep falling through to passthrough.

**Config.** `Connection.config` JSON, all keys optional:

| Key | Default | Meaning |
| --- | --- | --- |
| `header` | `Authorization` | Header to inject. Must be a valid HTTP header token and not `Host`, `Content-Length` or a hop-by-hop header. |
| `prefix` | `Bearer ` | Prepended to the token. An explicit empty string injects the raw token. |
| `placeholderEnv` | none | Name of a sandbox env var set to the placeholder, so clients such as npm (`_authToken=${NPM_TOKEN}`) send a header at all. Read by the agents service through the connection catalog, ignored by the adapter. |

Unknown keys are rejected in `Open`.

**Mint.** Resolve `conn.SecretRef` and return `Credential{Header, Value: prefix+token, Token}` with a zero `ExpiresAt`. The token is never logged.

### Rationale

The upstream authorizes by token scope; the platform only needs to distinguish reads from writes so a read-only grant can be expressed. The `placeholderEnv` name is per-Connection config because each client's config file chooses its own variable name.

---

## Consequences

### Positive

- Any static-token HTTP host becomes reachable through a governed Connection.
- A read-only grant denies `PUT`, which stops an agent publishing a package.

### Negative

- Authorization is coarse; there is no per-package or per-path control.
- A static token has no expiry; rotation is manual.

### Neutral

- Gateway registration, the Connection CRD rules and the sandbox placeholder env are delivered in follow-up changes.

---

## Related Decisions

- [Kubernetes Connection](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md)
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
- [Same-Host Connection Selection](2026-10-15-same-host-connection-selection.md)
