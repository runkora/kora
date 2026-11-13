# API Key Connection Adapter

<!-- Filename: 2026-11-11-apikey-connection-adapter.md -->

**Status:** Accepted
**Date:** 2026-11-11
**Deciders:** Platform Engineering

---

## Context

### Background

The egress gateway injects credentials just-in-time through connection adapters (`github_app`, `kubernetes`). Agents never hold a real credential. The sandbox holds a placeholder and the gateway swaps in the real one on the re-originated leg.

### Problem Statement

Some hosts authenticate with a static secret sent in custom request headers, not always `Authorization: Bearer`. The first case is the GitHub Packages npm registry (`npm.pkg.github.com`), which a sandbox needs to install `@clear-route` packages. Neither existing adapter fits:

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

- Reuse the existing adapter interface. A new service is a new subpackage, not a change to the parent package.
- One Connection, one secret, one host.
- A typo in config fails with an error and never sends the wrong header.

---

## Considered Options

### Option 1: Extend `kubernetes` with a generic mode (rejected)

Couples an unrelated host to Kubernetes path classification and the cluster action vocabulary.

### Option 2: Use `github_app` with a stored PAT (rejected)

`github_app` mints tokens and carries a repository qualifier and a GitHub host family. A static PAT does not fit that model and would blur what the adapter means.

### Option 3: A new `apikey` adapter (chosen)

A small adapter modelled on `kubernetes` without path categories. Header names and prefixes are configured the way MCPServer `spec.auth.headers` configures them ([MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md)). The name says what the adapter holds, a static secret for an HTTP API, not one header format.

---

## Decision

Add an `apikey` adapter type under `backend/internal/credentials/adapters/apikey`.

**Authorization is coarse, by HTTP method.** `GET` and `HEAD` classify as `read`. `POST`, `PUT`, `PATCH` and `DELETE` classify as `write`. Everything else, including `OPTIONS` and protocol upgrades, is `ErrUnsupportedAction`. The vocabulary is `connection:<name>:read` and `connection:<name>:write`, with no qualifier and no baseline actions. Finer authorization is the upstream's own on the injected token.

**Hosts.** The connection serves exactly its declared `baseHost` (`declaredHostOnly`). `baseHost` must be a DNS hostname, not an IP literal: TLS to an IP carries no SNI, so the sandbox's egress interception cannot authorize it. This is the same rule as `kubernetes`. Hosts without an apikey connection keep falling through to passthrough.

**Config.** `Connection.config` JSON, all keys optional:

| Key | Default | Meaning |
| --- | --- | --- |
| `headers` | `[{"name":"Authorization","prefix":"Bearer "}]` | 1 to 8 headers to inject. Each item has a `name` (letters, digits and hyphens, at most 64 characters, not reserved, no case-insensitive duplicates) and an optional `prefix` (at most 256 characters, empty by default, so a raw token is sent as is). The injected value is the prefix followed by the Connection's secret. |
| `placeholderEnv` | none | Name of a sandbox env var set to the placeholder, so clients such as npm (`_authToken=${NPM_TOKEN}`) send a header at all. The name is an identifier (letters, digits and underscores, not starting with a digit) of at most 128 characters. Loader and process variables are reserved: `PATH`, `HOME`, `USER`, `SHELL`, `PWD`, `IFS`, `ENV`, `BASH_ENV`, `PYTHONPATH`, `NODE_OPTIONS`, `KUBECONFIG`, and any name starting with `LD_`, `DYLD_` or `KORA_`. The value is the fixed placeholder `placeholder-credential-injected-by-kora-egress-gateway`, the same one written into rendered kubeconfigs. The agents service reads the name through the connection catalog; the adapter ignores it. |

The header rules reuse the MCPServer limits and reserved-header list. They live in one place, `internal/platform/connections`, which the adapter, the operator webhook and the MCPServer validation all import, so no feature package imports another. Unlike MCPServer, items carry no per-header `secretRef`: a Connection has one `secretRef`, and every header is built from that one secret (for example `Authorization: Bearer X` and `x-api-key: X`). A different secret per header is out of scope.

Unknown keys are rejected in `Open`. A config error wraps `credentials.ErrInvalidConfig`, so the gateway reports it as a config error.

**Mint.** Resolve `conn.SecretRef` once and return a `Credential` whose `Headers` (a new additive field, injected in addition to `Header`/`Value`) hold prefix plus secret for each configured header, with a zero `ExpiresAt`. The gateway deletes each configured header from the outbound request before setting it, so client-supplied values such as the placeholder are replaced and the header is added when absent. The token is never logged.

### Rationale

The upstream authorizes by token scope; the platform only needs to distinguish reads from writes so a read-only grant can be expressed. The `placeholderEnv` name is per-Connection config because each client's config file picks its own variable name.

---

## Consequences

### Positive

- Any static-secret HTTP host, with any header format, becomes reachable through a governed Connection.
- A read-only grant denies `PUT`, which stops an agent publishing a package.

### Negative

- Authorization is coarse; there is no per-package or per-path control.
- A static token has no expiry; rotation is manual.

---

## Related Decisions

- [MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md)
- [Kubernetes Connection](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md)
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
- [Same-Host Connection Selection](2026-10-15-same-host-connection-selection.md)
