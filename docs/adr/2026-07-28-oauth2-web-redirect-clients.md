# OAuth2 Web-Redirect Clients: GitOps Pre-Registration via an OAuthClient CRD

**Status:** Accepted
**Date:** 2026-07-28
**Deciders:** Justin Wilkin, Engineering Team
**Technical Story:** [OAuth2 Web-Redirect Clients PRD](../prd/oauth2-web-redirect-clients.md) — let a hosted web application (e.g. a CRM) log its users in through kora's OAuth authorization server with a fixed HTTPS redirect URI, registered declaratively via GitOps.

---

## Context

[mcp-oauth-authentication](2026-06-29-mcp-oauth-authentication.md) built kora's identity service into an OAuth 2.1 authorization server for **public** clients: PKCE is mandatory, and clients register two ways —

- **Dynamic Client Registration** (RFC 7591): open by default, but every registered redirect URI must be a **loopback** address (`127.0.0.1`/`localhost`/`::1`), per RFC 8252 for native/desktop MCP apps. This prevents authorization-code exfiltration to a remote host.

A **hosted web application** — a server-rendered app a user reaches in their browser, such as an internal dashboard or admin tool — cannot use a loopback redirect. Its callback is a fixed public HTTPS URL (`https://app.example.com/api/auth/callback/orbit`). It needs a redirect URI that DCR structurally refuses to register.

The naive fix is to relax the loopback rule and let hosted apps register their HTTPS callback. But an open registration surface that accepts arbitrary remote redirect URIs is an open-redirect / token-theft risk. Hosted-app registration must be **admin-reviewed**, not self-service.

The question is **where that admin-reviewed registration lives**. kora already has an answer for every other admin-declared registration: a **CRD reconciled by `orbit-operator`**, authored in the deployment's GitOps repo. Connectors are the `Connection` CRD (`oconn`); governed tool servers are the `MCPServer` CRD (`omcp`). Both are aggregated by the operator into an `orbit-system` ConfigMap the consuming service mounts and hot-reloads (see the JIT credential-brokering ADR, Amendment 3). A hosted-app OAuth client is the same class of object — an operator-declared registration — and should take the same path.

## Decision

Add a first-class **`OAuthClient` CRD** and consume it exactly like `Connection`.

1. **CRD `OAuthClient`** (`runkora.io/v1alpha1`, namespaced, shortName `oauthc`) in the tenant namespace. Spec: `clientId`, optional `clientName`, and `redirectUris` (≥1). No secret — all kora OAuth clients are public (PKCE is the proof of possession), so unlike `Connection` there is no `secretRef`.

2. **`OAuthClientAggregator`** in the operator collects every active tenant's `OAuthClient` CRs and renders the `orbit-system` ConfigMap **`orbit-tenant-oauth-clients`** (key `oauth-clients.json`, a JSON array of `{tenant_id, client_id, client_name, redirect_uris}`). It is driven by the `TenantReconciler` and re-runs on any `OAuthClient` change, tenant suspend/reactivate, or tenant delete — mirroring `ConnectionAggregator` minus the Secret half. A `client_id` is globally unique; a duplicate across CRs is dropped (first by sort order) and logged.

3. **Identity consumes the mounted file.** A `StaticClientSource` (`internal/identity/oauth_static_clients.go`) reads `oauth-clients.json` from `ORBIT_OAUTH_CLIENTS_CONFIG`, holds the parsed clients behind an `RWMutex` keyed by `client_id`, and hot-reloads on change via the same fsnotify directory-watch the client-secrets and connection registries use. A `layeredClientRepository` resolves a client from the static source first, then the dynamic (Redis) DCR store; writes (DCR) go only to the dynamic store. The identity service never imports the CRD type — it reads the mounted file, exactly as the egress gateway reads `connections.json`.

4. **The authorize-time gate is exact-match redirect validation, for every client.** `CreateAuthCode` drops the blanket `isLoopbackRedirect` check and gates on `client.AllowsRedirect(redirect_uri)` = exact string match against the client's registered URIs. A registered client — static or dynamic — can only redirect to a URI it registered, verbatim, so there is no open-redirect exposure. PKCE S256 remains mandatory.

5. **DCR keeps the loopback restriction.** `RegisterClient` still rejects non-loopback redirect URIs (`ErrInvalidClientMetadata`). Only admin-reviewed `OAuthClient` CRs may declare non-loopback HTTPS redirects. The relaxation is scoped to the GitOps-declared surface, never the open self-service one.

## Alternatives considered

- **Baked-in config YAML (`OAuthConfig.staticClients`).** Ship the static clients as a list in the identity service's `internal/identity/config/base.yaml`, seeded into Redis at startup. **Rejected.** It is off-pattern: every other admin-declared registration in kora is a CRD reconciled from a GitOps repo, but this would require an orbit code PR + image rebuild + redeploy to add, change, or remove a hosted app — not a GitOps apply. It is additive-only (removing a client from config does not revoke it), environment-agnostic (the same image ships to dev and prod), and forces a TTL hack to fake permanence in Redis. The CRD path is declarative, per-environment, GitOps-managed, and reconciles add/update/**delete** for free.

- **Relax DCR to accept any HTTPS redirect.** **Rejected.** Turns the open self-service registration endpoint into an open-redirect surface. Hosted-app registration must be admin-reviewed.

- **Wildcard/prefix redirect matching.** **Rejected.** Exact match is the OAuth security BCP; prefix matching reintroduces open-redirect classes. Multiple callbacks are expressed as multiple exact `redirectUris` entries.

## Consequences

- A platform operator brings their own hosted-app clients by committing an `OAuthClient` manifest to the GitOps repo in their tenant namespace; the operator aggregates it and identity picks it up within a projection-update cycle, no pod restart. Removing the manifest revokes the client on the next reconcile.
- The identity Deployment must mount `orbit-tenant-oauth-clients` at `ORBIT_OAUTH_CLIENTS_CONFIG` (deployment-side wiring, same shape as the egress gateway's connection mount).
- Hosted-app clients and DCR clients share one lookup surface but two stores; a static `client_id` shadows a dynamic one of the same id (pre-registration is authoritative).
- `client_id` is globally unique across the authorization server; the CRD carries `tenant_id` for provenance only.
