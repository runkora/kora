# OAuth2 Web-Redirect Clients — Product Requirements Document

**Status:** Approved
**Created:** 2026-07-28
**Last Updated:** 2026-07-28
**Owner:** Justin Wilkin
**Stakeholders:** Orbit Core Team, Constellation / Spaceforce team (Twenty CRM PoC)

---

## Executive Summary

Orbit's OAuth2 authorization server currently restricts redirect URIs to loopback addresses (`127.0.0.1`, `localhost`, `::1`) — a deliberate constraint tuned for desktop/IDE MCP clients (RFC 8252). Hosted web applications like Twenty CRM (running in Constellation) need to authenticate individual users against Orbit and obtain per-user tokens for budget and policy attribution. This PRD defines the minimal extension — static pre-registered clients with non-loopback redirect URIs — that unlocks this without expanding Orbit's OAuth surface into a general-purpose public IdP.

---

## Problem Statement

### Current State

Orbit ships a minimal OAuth 2.1 authorization server for the MCP profile ([mcp-oauth-authentication ADR](../adr/2026-06-29-mcp-oauth-authentication.md)). Its design is deliberately narrowed to desktop/IDE MCP clients (Claude Desktop, Cursor, VS Code) that use the RFC 8252 loopback redirect pattern:

- `oauth_service.go`'s `CreateAuthCode` rejects any `redirect_uri` that is not `127.0.0.1`, `localhost`, or `::1`.
- Dynamic client registration (DCR, RFC 7591) only accepts loopback redirect URIs.
- There is no mechanism to pre-register a client with a hosted (non-loopback) callback URL.

### User Pain Points

- **Spaceforce / Twenty CRM:** wants a "Connect Orbit" flow in Twenty's settings so users can log in with their Orbit identity and have AI usage attributed to them personally (budget + policy tracking), rather than a single shared PAT. The standard PKCE authorization-code flow works end-to-end — the only blocker is the redirect URI restriction.
- **Any future Constellation-hosted app:** any tenant-hosted web application that wants per-user Orbit authentication faces the same wall.
- **Workaround cost:** until this lands, Spaceforce ships with self-service PAT paste — per-user attribution works, but the UX is poor and the token lifecycle is manual.

### Business Impact

- Orbit tokens issued via the OAuth flow are already first-class platform tokens (`aud=orbit-platform`, `PrincipalType=HUMAN`) accepted at the inference gateway without any additional steps — there is no implementation gap on the token side. The single blocker is the redirect URI restriction.
- Failure to support hosted-app OAuth forces every multi-tenant integration (not just Spaceforce) back to PAT-paste, which is both poor UX and operationally harder to govern (PATs have manual lifecycle vs. OAuth with automated refresh and revocation).
- The MCP OAuth ADR called out a general-purpose public IdP as a **non-goal** — this feature stays within that constraint by using *pre-registered* clients rather than opening DCR to arbitrary redirect URIs.

---

## Goals and Objectives

### Primary Goals

1. Allow Orbit platform administrators to statically pre-register OAuth clients with non-loopback redirect URIs in the identity service's YAML configuration.
2. Enable PKCE authorization-code flows from hosted web applications to complete successfully when the redirect URI is pre-registered.
3. Keep the change minimal and backwards-compatible: DCR for loopback-only MCP clients is unchanged; the loopback restriction for dynamic registrations is unchanged.

### Success Metrics

- Spaceforce/Twenty can complete a PKCE authorization-code flow with a real hosted callback URL and obtain an Orbit platform token per user.
- Dynamic client registration continues to reject non-loopback redirect URIs.
- Pre-registered clients bypass the loopback check and are served from a permanent (no-TTL) in-process store.
- Zero changes to token shape, scopes, or the inference gateway.

### Non-Goals

- Turning Orbit into a general-purpose OAuth IdP with a self-service client registration UI.
- Supporting confidential clients (client secrets) — all clients remain public (PKCE only).
- Relaxing the DCR loopback restriction for dynamically-registered clients.
- Custom scopes per pre-registered client (uses the same scope model as all other OAuth flows).

---

## User Stories

### US-1 — Hosted app authenticates users with Orbit

**As a** Constellation app developer,
**I want to** register my hosted web app's callback URL as a pre-registered Orbit OAuth client,
**So that** users can log in with Orbit and have their AI usage attributed to their individual identity.

**Acceptance Criteria:**
- [ ] A platform admin adds a static client entry to the identity service YAML (`oauth.staticClients`) with a `client_id`, `client_name`, and one or more `redirect_uris` (may be non-loopback HTTPS URLs).
- [ ] The authorization code flow (`/mcp/authorize` → `POST /identity/oauth/token`) completes with a non-loopback `redirect_uri` that matches a static registration.
- [ ] The issued token is an ordinary `aud=orbit-platform` HUMAN token, accepted at the inference gateway.
- [ ] The static client has no TTL — it persists as long as the config is present.

### US-2 — DCR behaviour is unchanged for MCP desktop clients

**As a** desktop MCP client developer,
**I want** the existing dynamic client registration to continue working exactly as before,
**So that** existing MCP client integrations (Claude Desktop, Cursor) are not broken.

**Acceptance Criteria:**
- [ ] `POST /identity/oauth/register` continues to accept loopback redirect URIs and reject non-loopback ones.
- [ ] Dynamically-registered clients still expire after 30 days.
- [ ] A dynamically-registered client with a loopback URI can still complete the full OAuth flow.

---

## Proposed Solution

### High-Level Approach

Add an `oauth.staticClients` list to the identity service YAML configuration. Each entry declares a pre-registered public client: a stable `client_id`, an optional `client_name`, and one or more `redirect_uris` (which may be arbitrary HTTPS URLs, not just loopback). On service startup these clients are loaded from config and seeded into the in-memory client store alongside the Redis-backed dynamic registrations.

The authorization-code flow (`CreateAuthCode`) gains a single additional check: if the client is pre-registered, skip the loopback restriction. The loopback restriction on DCR (`RegisterClient`) is unchanged.

### Key Components

- **Config extension (`OAuthConfig`):** new `StaticClients []StaticOAuthClient` field in `identity/config.go`; loaded from YAML and applied via a seed step in `NewService`.
- **Seeded client store:** the existing `ClientRepository` interface gains a `Seed` method (or the `NewService` constructor calls `Store` at startup with a zero TTL / permanent entry); static clients are stored in-process so they never expire.
- **Relaxed redirect check in `CreateAuthCode`:** after validating the client exists and `AllowsRedirect` passes, bypass the `isLoopbackRedirect` check for any registered client — the prior `AllowsRedirect` call already enforces that only registered URIs are accepted.
- **No change to DCR:** `RegisterClient` retains its loopback-only validation for dynamic registrations.

### User Experience

1. Platform admin adds to `identity/config/base.yaml` (or an env-specific overlay) under `oauth.staticClients`:
   ```yaml
   oauth:
     staticClients:
       - clientId: spaceforce-twenty
         clientName: "Spaceforce / Twenty CRM"
         redirectUris:
           - "https://twenty.constellation.example.com/api/auth/callback/orbit"
   ```
2. Identity service restarts; the static client is available immediately.
3. Twenty initiates the PKCE flow: discovers AS metadata, sends the user to `/mcp/authorize` with `client_id=spaceforce-twenty`, the hosted `redirect_uri`, and a PKCE challenge. User approves on the consent screen. `/identity/oauth/token` exchanges the code for an Orbit platform token.
4. Twenty stores and refreshes the token; AI usage is attributed to the individual user.

---

## Scope

### In Scope

- `OAuthConfig.StaticClients` YAML field and config loading.
- Seeding static clients into the `ClientRepository` at startup.
- Removing the loopback URI restriction for clients whose redirect URI was explicitly pre-registered (static or dynamic) — the `AllowsRedirect` check is the enforcement; `isLoopbackRedirect` becomes a DCR-only guard.
- Tests for the new path (static client registration + non-loopback code flow) and regression tests confirming DCR loopback-only behaviour is unchanged.
- ADR amendment to `mcp-oauth-authentication.md`.

### Out of Scope

- A UI for managing pre-registered clients.
- Confidential clients or client secrets.
- Per-client scope restrictions.
- Token lifetimes configurable per client.
- Relaxing DCR to accept non-loopback URIs.

---

## Requirements

### Functional Requirements

**P0 (MVP):**
- [ ] FR-1: `identity/config/base.yaml` accepts an `oauth.staticClients` list; each entry has `clientId`, `clientName` (optional), and `redirectUris`.
- [ ] FR-2: Static clients are available immediately after service startup, with no TTL.
- [ ] FR-3: `CreateAuthCode` accepts a non-loopback `redirect_uri` if and only if the client is pre-registered and `AllowsRedirect` passes.
- [ ] FR-4: `RegisterClient` (DCR) continues to reject non-loopback redirect URIs.
- [ ] FR-5: Static clients are visible to `GetClient` (used by the consent screen).
- [ ] FR-6: The full PKCE authorization-code + refresh-token flow works end-to-end for a static client with a non-loopback redirect URI.

**P1 (Fast-follow):**
- [ ] FR-7: Operator documentation (or Helm value) for registering static clients in a deployment.

### Non-Functional Requirements

**Security:**
- The `AllowsRedirect` exact-match check is the security gate for pre-registered non-loopback clients — only URIs explicitly listed in config are permitted. Prefix/wildcard matching is not allowed.
- Static `client_id` values must be treated as non-secret identifiers; no client secret is issued (PKCE is the proof).

**Backwards compatibility:**
- No existing DCR behaviour changes.
- No change to token shape, scopes, or the inference gateway.
- Dynamic clients registered via Redis continue to work exactly as before.

---

## Dependencies

| Dependency | Status | Note |
|---|---|---|
| MCP OAuth Authentication ADR | Implemented | Base authorization server surface this extends |
| Identity service config loader | Implemented | `config.Load[Config]` / `base.yaml` pattern reused |
| `ClientRepository` interface | Implemented | Extended with seed/static support |
| Redis dynamic client store | Implemented | Unchanged |

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Static `client_id` collision with a dynamically-registered client | Medium | Static clients are seeded first; DCR generates random IDs unlikely to collide. Document that static IDs should be human-readable slugs, not random. |
| Config-driven `redirectUris` is an exact-match list — wildcard or prefix leakage | High | `AllowsRedirect` uses `slices.Contains` (exact match only). `isLoopbackRedirect` remains the DCR guard. No prefix/wildcard matching anywhere. |
| Over-broad static client registration by ops mistake | Medium | Operational: review of `redirectUris` is part of the PR process for config changes, same as any security-sensitive config. |

---

## Alternatives Considered

### Alternative 1: Relax DCR to accept non-loopback URIs

Open dynamic client registration to arbitrary redirect URIs, removing the loopback restriction entirely.

**Why not chosen:**
- DCR clients have a 30-day TTL and are designed for ephemeral MCP client sessions. Opening them to arbitrary redirect URIs means any caller can self-register a hosted callback URL with no admin review.
- This is the pattern Orbit deliberately rejected in the MCP OAuth ADR's Non-Goals section ("We build the MCP profile, not a public IdP").

### Alternative 2: A per-tenant database table for registered clients

Store static clients in PostgreSQL instead of YAML config.

**Why not chosen:**
- Adds a migration, a new repository, and a management API for a small initial surface (likely 1–2 clients per deployment). The config-driven approach is simpler and aligns with how Orbit manages other operator-level concerns (tenants, OIDC providers).
- Can be promoted to a DB-backed store in a follow-up if the number of clients grows.

### Alternative 3: Accept a PAT-paste workaround permanently

Ship Spaceforce with PAT-paste and never add OAuth for hosted apps.

**Why not chosen:**
- PATs have manual lifecycle (the user must rotate them, paste them again, and revoke them manually). OAuth with refresh tokens is strictly better for attributing per-user usage at scale.
- This feature unlocks a class of integration (Constellation-hosted apps doing per-user Orbit login) that PAT-paste cannot serve well long-term.

---

## Related Documents

- [MCP OAuth Authentication ADR](../adr/2026-06-29-mcp-oauth-authentication.md) — the authorization server surface this extends
- [MCP / Tool Gateway PRD](mcp-tool-gateway.md) — the tool-access use case this feeds
- [Personal Access Tokens](personal-access-tokens.md) — the existing long-lived human credential this complements
