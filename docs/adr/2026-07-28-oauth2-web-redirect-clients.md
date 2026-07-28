# OAuth2 Web-Redirect Clients: Static Pre-Registration for Hosted Apps

<!-- Filename: 2026-07-28-oauth2-web-redirect-clients.md -->

**Status:** Accepted
**Date:** 2026-07-28
**Deciders:** Justin Wilkin, Orbit Core Team
**Technical Story:** [OAuth2 Web-Redirect Clients PRD](../prd/oauth2-web-redirect-clients.md), [Issue #149](https://github.com/clear-route/orbit/issues/149)

---

## Context

### Background

The [MCP OAuth Authentication ADR](2026-06-29-mcp-oauth-authentication.md) built Orbit's OAuth 2.1 authorization server for the MCP profile. Its redirect-URI validation is intentionally narrow:

- Dynamic client registration (DCR, RFC 7591) rejects any `redirect_uri` that is not a loopback address (`127.0.0.1`, `localhost`, `::1`), matching RFC 8252 for desktop/IDE MCP clients.
- `CreateAuthCode` enforces the same loopback restriction before storing an authorization code.
- The ADR explicitly called out *"a general-purpose, externally-consumable Orbit OAuth provider beyond what the MCP spec requires"* as a **non-goal**.

That constraint is correct for the MCP-client use case, but it blocks a different, equally legitimate use case: **Constellation-hosted web applications** (e.g. Twenty CRM, the Spaceforce PoC) wanting per-user Orbit authentication. These apps run at a fixed HTTPS URL, not on localhost, and they need their users to sign in with Orbit so that AI usage is attributed to individual identities (budget + policy tracking) rather than a shared PAT.

### Problem Statement

Extend the OAuth authorization server to support hosted web apps with non-loopback redirect URIs, without opening DCR to arbitrary redirect targets or turning Orbit into a general-purpose public IdP.

### Goals

- Allow platform operators to pre-register OAuth clients with non-loopback redirect URIs via YAML config.
- Enable the full PKCE authorization-code + refresh flow for those clients.
- Leave DCR behaviour unchanged (loopback-only, 30-day TTL).

### Non-Goals

- A UI for client registration.
- Confidential clients (no client secrets).
- Per-client scope or lifetime customization.
- Opening DCR to non-loopback redirect URIs.
- A general-purpose public OAuth IdP.

---

## Decision Drivers

- **Security surface must not expand beyond config-declared URIs.** Unlike DCR (where any caller can register a client), static clients are admin-reviewed YAML entries. The exact-match `AllowsRedirect` check remains the enforcement gate.
- **Minimal change.** The existing `ClientRepository` interface, `RegisteredClient` type, and redirect-URI matching are all correct. The only required changes are: (1) a new config field; (2) seeding static clients at startup; (3) relaxing the loopback check in `CreateAuthCode` for registered clients (the `AllowsRedirect` check already enforces URI exactness).
- **Backwards compatibility.** Zero changes to DCR, dynamic clients, token shape, the MCP OAuth flow for desktop clients, or the inference gateway.
- **Config-driven, not DB-driven.** Aligns with Orbit's pattern for operator-level configuration (tenants, OIDC providers). A DB-backed store is a future escalation path if the number of clients grows.

---

## Considered Options

### Option A — Static pre-registered clients in YAML config (chosen)

Add `oauth.staticClients` to `OAuthConfig`. On `NewService`, seed each static client into the `ClientRepository` with no TTL (permanent). In `CreateAuthCode`, remove the unconditional `isLoopbackRedirect` call: the `AllowsRedirect` check already restricts codes to registered URIs, so any registered URI — loopback or not — is safe. DCR (`RegisterClient`) retains the loopback restriction unchanged.

- **Pros:** minimal change; admin-reviewed config; no new API, DB table, or migration; the loopback restriction for dynamic clients is unchanged; security gate is `AllowsRedirect` (exact match).
- **Cons:** requires a service restart to add a client (not a UI flow). Acceptable for an operator-level concern.

### Option B — Relax DCR to accept non-loopback URIs

Remove the loopback restriction from `RegisterClient`.

- **Pros:** simpler — zero config changes.
- **Cons:** any caller can self-register a client pointing at an arbitrary HTTPS URL. This is exactly the "general-purpose public IdP" non-goal the MCP OAuth ADR rejected. Rejected.

### Option C — Per-tenant PostgreSQL table for client registrations

New DB table and management API for registered clients.

- **Pros:** dynamic updates without restarts; UI-manageable.
- **Cons:** migration, new repo, new API surface — disproportionate for a handful of clients. Config-driven is the right first cut; escalate to DB if the surface grows. Rejected for now.

---

## Decision

| # | Decision |
|---|----------|
| 1 | Add `oauth.staticClients` list to `OAuthConfig` (YAML-sourced). Each entry: `clientId` (stable slug), `clientName` (display name for consent screen), `redirectUris` (exact HTTPS URLs; non-loopback permitted). |
| 2 | On `NewService` startup, seed each static client into the `ClientRepository` with no TTL (permanent, not Redis-backed). |
| 3 | In `CreateAuthCode`, remove the unconditional `isLoopbackRedirect` check on `p.RedirectURI`. The prior `client.AllowsRedirect(p.RedirectURI)` call (exact match against the registered list) is the security gate. `isLoopbackRedirect` moves to being a DCR-only guard. |
| 4 | `RegisterClient` (DCR) retains the loopback restriction: dynamically-registered clients may only have loopback redirect URIs. |
| 5 | Static clients are served by the existing `ClientRepository.Get` path, so `GetClient` (used by the consent screen) works with no changes. |

### Rationale

The security invariant is: *a code can only be issued to a redirect URI the client was registered for*. That invariant is enforced by `AllowsRedirect` (exact-match `slices.Contains`), not by `isLoopbackRedirect`. The loopback check was a belt-and-suspenders guard for DCR (where any caller can register without admin review). For config-declared static clients, the admin review of the YAML entry is the equivalent protection — the loopback check is redundant and counterproductive. Removing it from `CreateAuthCode` (while keeping it in `RegisterClient`) is a precise, minimal change that directly maps to the threat model.

---

## Architecture

### Config extension

```yaml
# identity/config/base.yaml (and env overlays)
oauth:
  # Open DCR for public PKCE clients (MCP desktop). Set true in strict deployments.
  requirePreRegistration: false
  # Static pre-registered clients for hosted web apps with fixed callback URLs.
  # Unlike DCR clients, these have no TTL and accept non-loopback redirect URIs.
  # Only public clients (PKCE) — no client_secret.
  staticClients: []
  # Example:
  # staticClients:
  #   - clientId: spaceforce-twenty
  #     clientName: "Spaceforce / Twenty CRM"
  #     redirectUris:
  #       - "https://twenty.constellation.example.com/api/auth/callback/orbit"
```

```go
// OAuthConfig extension:
type StaticOAuthClient struct {
    ClientID     string   `yaml:"clientId"`
    ClientName   string   `yaml:"clientName,omitempty"`
    RedirectURIs []string `yaml:"redirectUris"`
}

type OAuthConfig struct {
    RequirePreRegistration bool               `yaml:"requirePreRegistration"`
    StaticClients          []StaticOAuthClient `yaml:"staticClients"`
}
```

### Startup seeding

```go
// In NewService (or an initOAuthClients helper):
for _, sc := range cfg.OAuth.StaticClients {
    client := RegisteredClient{
        ClientID:     sc.ClientID,
        ClientName:   sc.ClientName,
        RedirectURIs: sc.RedirectURIs,
        CreatedAt:    time.Now().UTC(),
    }
    _ = clients.Store(ctx, client, 0) // 0 TTL = permanent in-memory; Redis ignores 0 TTL
}
```

Static clients use `ttl=0` in Redis (`SET key value` without EX), making them persistent in Redis. Alternatively, use a separate in-memory map layered over the Redis store — see implementation note below.

### Implementation note: in-memory layer for zero-TTL clients

Redis does not support truly permanent keys via the `SET … EX 0` form (a zero duration is invalid). Two implementation strategies:

**Option A (chosen for simplicity):** Use a very large TTL (e.g. 100 years) for static clients. Redis supports arbitrarily large EX values; the operator rotates the config long before the key expires.

**Option B:** Wrap the `redisClientRepository` with an in-memory `staticClientRepository` that holds the config-seeded clients. `Get` checks the in-memory map first; falls back to Redis for dynamic clients. This is marginally more correct but adds a layer of indirection for a negligible practical gain.

The implementation uses **Option A** (large TTL via a sentinel constant `staticClientTTL = 100 * 365 * 24 * time.Hour`) because it is the smallest diff and keeps the single-interface `ClientRepository` clean. Static clients are re-seeded on every service restart from config, so the expiry is a non-issue.

### Redirect check restructure

**Before:** `CreateAuthCode` calls `isLoopbackRedirect(p.RedirectURI)` before looking up the client. Any non-loopback URI fails immediately.

**After:** The loopback check is removed from `CreateAuthCode`. The security gate is:
1. `s.clients.Get(ctx, p.ClientID)` — the client must be registered.
2. `client.AllowsRedirect(p.RedirectURI)` — the URI must be in the registered list (exact match).

`isLoopbackRedirect` is retained as a helper used only by `RegisterClient` to enforce the DCR restriction.

```
CreateAuthCode flow (after):
  1. PKCE S256 present?  → ErrInvalidOAuthRequest
  2. client_id present?  → ErrInvalidOAuthRequest
  3. clients.Get(client_id)  → ErrInvalidOAuthRequest if not found
  4. client.AllowsRedirect(redirect_uri)  → ErrInvalidOAuthRequest if not in list
  5. Store code, return

RegisterClient flow (unchanged):
  1. DCR disabled?  → ErrDCRDisabled
  2. redirect_uris present?  → ErrInvalidClientMetadata
  3. isLoopbackRedirect for each URI  → ErrInvalidClientMetadata (DCR loopback guard unchanged)
  4. public client only  → ErrInvalidClientMetadata
  5. Store client, return
```

---

## Consequences

### Positive

- Hosted web apps (Spaceforce/Twenty, and any future Constellation app) can complete per-user OAuth2 flows against Orbit.
- The security gate (`AllowsRedirect`) is the same exact-match check used for loopback clients — no weaker.
- DCR behaviour is completely unchanged; existing MCP desktop client integrations are unaffected.
- No new token type, no new scope model, no new infrastructure component.

### Negative

- Adding a static client requires a service restart (config change + redeploy). Acceptable for an operator-level concern.
- Static `client_id` values are in plain-text YAML; care is needed to avoid hardcoding them in places that expect the random DCR-generated format. (Documented in operator guidance.)

### Neutral

- The `isLoopbackRedirect` function is retained for DCR but no longer called from `CreateAuthCode`. Its name and semantics remain accurate for its sole remaining caller.
- The 100-year TTL for static clients in Redis is a pragmatic choice that works with the single `ClientRepository` interface and requires no Redis-specific changes.

---

## Implementation

### Phase 1 — Config + seeding + redirect check

1. Extend `OAuthConfig` with `StaticClients []StaticOAuthClient`.
2. Add `staticClientTTL = 100 * 365 * 24 * time.Hour` to `oauth_client_repository.go`.
3. Add an `initOAuthClients` helper to `service.go` (or inline in `NewService`) that seeds static clients via `clients.Store`.
4. Remove the `isLoopbackRedirect(p.RedirectURI)` check from `CreateAuthCode`; keep it in `RegisterClient`.
5. Add tests: static client round-trip with non-loopback redirect URI; DCR loopback restriction still enforced.

### Migration path

None required. Static clients are additive config. Dynamic clients in Redis are unaffected.

---

## Validation

- **Unit tests:** static client registered in config → authorization code flow succeeds with non-loopback redirect URI; DCR still rejects non-loopback URIs; unregistered non-loopback URI still rejected.
- **Integration:** Spaceforce/Twenty completes the "Connect Orbit" flow end-to-end in a Constellation environment.
- **Monitoring:** existing `mcp_oauth_authorize_total{result}` and `mcp_oauth_token_issued_total` metrics cover the new path.

---

## Related Decisions

- [MCP OAuth Authentication Model](2026-06-29-mcp-oauth-authentication.md) — the authorization server surface this amends
- [Personal Access Tokens](2026-04-02-personal-access-tokens.md) — the alternative per-user credential this complements
- [Unified Identity Model](2026-03-05-unified-identity-model.md) — `PrincipalType=HUMAN`, platform token shape

## References

- [Issue #149](https://github.com/clear-route/orbit/issues/149) — Spaceforce / Twenty CRM request
- RFC 7591 — OAuth 2.0 Dynamic Client Registration Protocol
- RFC 8252 — OAuth 2.0 for Native Apps (loopback redirect origin)
- RFC 7636 — Proof Key for Code Exchange (PKCE)
