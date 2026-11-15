# API Key Connection Adapter

<!-- Filename: 2026-11-11-apikey-connection-adapter.md -->

**Status:** Accepted
**Date:** 2026-11-11
**Deciders:** Platform Engineering

---

## Context

### Background

The egress gateway injects credentials just-in-time through connection adapters (`github_app`, `kubernetes`). Agents never hold a real credential. The sandbox holds a placeholder, and the gateway swaps in the real credential on the re-originated leg.

### Problem Statement

Some hosts authenticate with a static secret in custom request headers. The header is often `Authorization: Bearer`, and sometimes another name such as `x-api-key`. The first case is the GitHub Packages npm registry (`npm.pkg.github.com`). A sandbox needs it to install `@clear-route` packages. Neither existing adapter fits:

- **`github_app`** mints GitHub App installation tokens. The GitHub Packages npm registry accepts only a personal access token with `read:packages`.
- **`kubernetes`** injects a static bearer token. Its `Classify` parses Kubernetes API paths and denies every other path. It cannot serve another host.

### Goals

- An agent reaches a token-authenticated host through a Connection. The gateway injects the credential.
- The sandbox never holds the real token.

### Non-Goals

- Path-category, qualifier or per-package authorization.
- Token rotation tooling. Rotation is a Secret update, the same as for `kubernetes`.
- Any change to `github_app`, `kubernetes` or `passthrough`.

---

## Decision Drivers

- Reuse the existing adapter interface. A new adapter is a new subpackage and leaves the parent package unchanged.
- One Connection, one secret, one host.
- A typo in config fails with an error and never sends the wrong header.

---

## Considered Options

### Option 1: Extend `kubernetes` with a generic mode (rejected)

Couples an unrelated host to Kubernetes path classification and the cluster action vocabulary.

### Option 2: Use `github_app` with a stored PAT (rejected)

`github_app` mints tokens and carries a repository qualifier and a GitHub host family. A static PAT does not fit that model.

### Option 3: A new `apikey` adapter (chosen)

A small adapter modelled on `kubernetes` without path categories. Header names and prefixes follow MCPServer `spec.auth.headers` ([MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md)). The name describes what the adapter holds: a static secret for an HTTP API.

---

## Decision

Add an `apikey` adapter type under `backend/internal/credentials/adapters/apikey`.

**Authorization.** Authorization is coarse and set by HTTP method. `GET` and `HEAD` classify as `read`, and `POST`, `PUT`, `PATCH` and `DELETE` classify as `write`. Every other method, including `OPTIONS` and protocol upgrades, is `ErrUnsupportedAction`. The vocabulary is `connection:<name>:read` and `connection:<name>:write`, with no qualifier and no baseline actions. The upstream applies finer authorization to the injected token.

**Hosts.** The connection serves exactly its declared `baseHost` (`declaredHostOnly`). `baseHost` must be a DNS hostname, and an IP literal is refused. TLS to an IP carries no SNI, and the sandbox's egress interception cannot authorize it. The `kubernetes` adapter applies the same rule. Hosts without an apikey connection fall through to passthrough.

**Config.** `Connection.config` is JSON with two optional keys.

- `headers` lists 1 to 8 headers to inject, and defaults to `[{"name":"Authorization","prefix":"Bearer "}]` when omitted. Each item in the list has a `name` and an optional `prefix`. A name uses letters, digits and hyphens, has at most 64 characters, is not reserved, and appears once (case-insensitive). A prefix has at most 256 characters and is empty by default, and an empty prefix sends the raw token. The injected value is the prefix followed by the Connection's secret.
- `placeholderEnv` names a sandbox env var. The agents service sets it to the placeholder. Clients such as npm (`_authToken=${NPM_TOKEN}`) send a header only when the variable has a value. The name is an identifier of letters, digits and underscores, does not start with a digit, and has at most 128 characters. Loader and process variables are reserved: `PATH`, `HOME`, `USER`, `SHELL`, `PWD`, `IFS`, `ENV`, `BASH_ENV`, `PYTHONPATH`, `NODE_OPTIONS`, `KUBECONFIG`, and every name starting with `LD_`, `DYLD_` or `KORA_`. The value is the fixed placeholder `placeholder-credential-injected-by-kora-egress-gateway`, the same value written into rendered kubeconfigs. The agents service reads the name through the connection catalog. The adapter ignores it.

The header rules reuse the MCPServer limits and reserved-header list. They live in `internal/platform/connections`. The adapter, the operator webhook and the MCPServer validation import them from there. No feature package imports another.

Items carry no per-header `secretRef`. A Connection has one `secretRef`, and every header is built from that secret, for example `Authorization: Bearer X` and `x-api-key: X`. A different secret per header is out of scope.

`Open` rejects unknown keys. A config error wraps `credentials.ErrInvalidConfig`, and the gateway reports it as a configuration error.

**Mint.** `Mint` resolves `conn.SecretRef` once. It returns a `Credential` whose `Headers` hold the prefix plus the secret for each configured header, with a zero `ExpiresAt`. `Headers` is a new field that the gateway injects in addition to `Header` and `Value`. The gateway deletes each configured header from the outbound request and then sets it. A client value such as the placeholder is replaced, and a missing header is added. The token is never logged.

### Rationale

The upstream authorizes by token scope. The platform distinguishes reads from writes to express a read-only grant. Each client's config file picks its own variable name. `placeholderEnv` is therefore per-Connection config.

---

## Consequences

### Positive

- Any static-secret HTTP host, with any header format, is reachable through a governed Connection.
- A read-only grant denies `PUT`. An agent with read access cannot publish a package.

### Negative

- Authorization is coarse. There is no per-package or per-path control.
- A static token has no expiry, and rotation is a manual Secret update.

---

## Related Decisions

- [MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md)
- [Kubernetes Connection](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md)
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
- [Same-Host Connection Selection](2026-10-15-same-host-connection-selection.md)
