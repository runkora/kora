# OAuth2 Web-Redirect Clients — Product Requirements Document

**Status:** Approved
**Created:** 2026-07-28
**Last Updated:** 2026-07-28
**Owner:** Justin Wilkin
**Related ADR:** [OAuth2 Web-Redirect Clients: GitOps Pre-Registration via an OAuthClient CRD](../adr/2026-07-28-oauth2-web-redirect-clients.md)

---

## Problem

kora's identity service is an OAuth 2.1 authorization server for public (PKCE) clients. Registration only admits **loopback** redirect URIs — correct for native/desktop MCP apps (RFC 8252), but it structurally excludes **hosted web applications**. A server-rendered app a user reaches in the browser has a fixed public HTTPS callback such as `https://app.example.com/api/auth/callback/orbit`, which the loopback-only rule refuses.

We need hosted apps to log their users in through kora, without opening a self-service endpoint that would accept arbitrary remote redirect URIs (an open-redirect / code-theft risk).

## Goals

- Let an **admin-reviewed** hosted web app authenticate users through kora with a fixed non-loopback HTTPS redirect URI.
- Register these clients **declaratively via GitOps**, the same way connectors and MCP servers are registered — a platform operator brings their own clients.
- Add/update/remove a client without a kora code change or image rebuild, and without a pod restart.
- Keep the security posture intact: exact-match redirect validation, mandatory PKCE, and no relaxation of the open self-service (DCR) endpoint.

## Non-goals

- Confidential clients / client secrets. All kora OAuth clients remain public.
- Wildcard or prefix redirect matching.
- Per-tenant scoping of `client_id` (it is globally unique on the authorization server).

## Solution

A first-class **`OAuthClient` CRD** (`runkora.io/v1alpha1`, namespaced, shortName `oauthc`), reconciled by `kora-operator` into the `kora-tenant-oauth-clients` ConfigMap that the identity service mounts and hot-reloads. This mirrors the `Connection`/`MCPServer` registration pattern exactly. The authorize endpoint gates every client on exact-match redirect validation; DCR keeps its loopback restriction so only admin-declared CRs may use hosted HTTPS redirects. See the ADR for the full design.

### User flow (operator)

```yaml
apiVersion: runkora.io/v1alpha1
kind: OAuthClient
metadata:
  name: example-app
  namespace: kora-tenant-acme
spec:
  clientId: example-app
  clientName: Example App
  redirectUris:
    - https://app.example.com/api/auth/callback/orbit
```

Commit to the GitOps repo → the operator aggregates it → identity serves the authorize request for `example-app` with the fixed HTTPS callback. Delete the manifest → the client is revoked on the next reconcile.

## Requirements

| # | Requirement |
|---|-------------|
| 1 | `OAuthClient` CRD with `clientId` (required, unique), `clientName` (optional), `redirectUris` (≥1). |
| 2 | Operator aggregates active tenants' `OAuthClient` CRs into `kora-tenant-oauth-clients` (`oauth-clients.json`); re-runs on CR change, tenant suspend/reactivate, and delete. |
| 3 | Identity mounts and hot-reloads the file; static clients resolve ahead of DCR clients; DCR writes never touch static clients. |
| 4 | `CreateAuthCode` gates on exact-match `AllowsRedirect` + mandatory PKCE; the loopback check is removed from the authorize path. |
| 5 | DCR (`RegisterClient`) still rejects non-loopback redirect URIs. |
| 6 | Duplicate/invalid CRs are skipped and logged, never failing the batch. |

## Success criteria

- A hosted app registered via an `OAuthClient` CR completes the full authorize → token round trip with its HTTPS redirect.
- An unregistered redirect URI for that client is rejected.
- DCR of a non-loopback redirect is still rejected.
- Removing the CR revokes the client without a redeploy.
