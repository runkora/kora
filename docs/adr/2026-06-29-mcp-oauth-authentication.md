# MCP OAuth Authentication Model

<!-- Filename: 2026-06-29-mcp-oauth-authentication.md -->

**Status:** Accepted
**Date:** 2026-06-29
**Deciders:** Justin Wilkin, Orbit Core Team
**Technical Story:** [MCP / Tool Gateway PRD](../prd/mcp-tool-gateway.md)

---

## Context

### Background

The [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) has two front doors. The agent door is solved: an agent connects with its platform token, injected by the identity-proxy sidecar. The **human** door is not. A human points a desktop/IDE MCP client (Claude Desktop, Cursor, VS Code) at the gateway, and that client authenticates using the **MCP authorization specification** — a profile of OAuth 2.1. This is a new authentication model for Orbit, with its own spec, which is why it is its own ADR rather than a paragraph in the gateway's.

### The gap

Orbit today is an OIDC **client (relying party)**, not an authorization server. It exposes JWKS (for verifying Orbit-issued tokens), a Device Authorization endpoint, a `/session` endpoint (issues a platform token from an upstream OIDC id_token via the `oidc_id_token` grant), `/exchange` (SPIFFE SVID), and `/revoke`. It does **not** expose `/authorize`, OAuth/OIDC **server** metadata, dynamic client registration, or token introspection.

The MCP authorization spec expects the opposite shape. A spec-compliant MCP client, on hitting a protected MCP server, does:

1. receives `401` with `WWW-Authenticate` pointing at **protected-resource metadata** (RFC 9728);
2. fetches that metadata from the **resource server** (the gateway) to discover the **authorization server**;
3. fetches **authorization-server metadata** (RFC 8414);
4. (optionally) **dynamically registers** as a client (RFC 7591);
5. runs **OAuth 2.1 authorization-code + PKCE**, obtains an access token;
6. calls the gateway with that token.

Real MCP clients implement this flow; they will not perform a bespoke "paste your Dex token into a custom `/session` endpoint" handshake. So supporting them is not a pure reuse of the existing relying-party machinery — there is a genuine new surface.

### Problem Statement

Decide how Orbit satisfies the MCP authorization spec for human MCP clients, reusing as much of the existing token/identity machinery as possible, while keeping the resulting credential a first-class Orbit principal that the gateway's PEP authorises exactly like any other.

### Goals

- A human MCP client authenticates to the gateway via the standard MCP OAuth flow, no Orbit-specific client code.
- The issued access token is an Orbit platform token (or verifiable as one) carrying a HUMAN principal the PEP authorises on existing grants.
- Reuse token signing, JWKS, claims, JTI revocation, and per-tenant OIDC federation.
- Tenant-correct: the human authenticates against *their* tenant's identity provider; the issued token carries that tenant.

### Non-Goals

- The gateway, PEP, and proxy — the [gateway ADR](2026-06-29-mcp-tool-gateway.md).
- Agent authentication (injected platform token) — already solved.
- A general-purpose, externally-consumable Orbit OAuth provider beyond what the MCP spec requires. We build the MCP profile, not a public IdP.

---

## Decision Drivers

- **Spec compliance is non-negotiable** — the value is that off-the-shelf MCP clients just work.
- **Reuse the identity core.** Token signing (RS256), JWKS, `PlatformClaims`, JTI blocklist revocation, and the `oidc_id_token` handoff already exist and are tested.
- **Don't reinvent IdP federation.** Orbit already does per-tenant OIDC discovery and id_token validation; the human's actual login should ride that, not a second user store.
- **Consistency with the platform token model.** The human's token should be an ordinary Orbit platform token like every agent and PAT token — same signing, same verification, authorisation by scopes — not a new token shape.

---

## Considered Options

### Option A — Orbit identity service becomes a minimal OAuth 2.1 authorization server, federating to the tenant IdP (chosen)

Orbit's identity service grows the MCP-required authorization-server surface — `/authorize`, `/token`, AS metadata (RFC 8414), protected-resource metadata served by the gateway (RFC 9728), and dynamic client registration (RFC 7591) — but does **not** become a user directory. When a client hits `/authorize`, Orbit runs its existing OIDC relying-party flow against the **tenant's** configured identity provider (the same per-tenant OIDC it already uses for browser/device login), then issues its own **platform token** as the OAuth access token — an ordinary `aud=orbit-platform` token like every other Orbit token, `PrincipalType=HUMAN`, revocable via the existing JTI blocklist.

- **Pros:** fully spec-compliant (real clients work); reuses token signing, JWKS, claims, JTI revocation, and per-tenant OIDC federation; the access token is already a first-class Orbit principal the PEP understands; one identity authority.
- **Cons:** genuinely new endpoints (`/authorize`, `/token`, metadata, DCR) and the security surface of an authorization server (redirect-URI validation, PKCE enforcement, consent). This is the real cost the PRD flagged.

### Option B — Front Dex as the authorization server

Point the gateway's protected-resource metadata at Dex; Dex is the AS and issues tokens; the gateway validates Dex tokens.

- **Pros:** less net-new Orbit code.
- **Cons:** Dex is the *local dev* IdP, not the per-tenant production IdP — Orbit's production identity is per-tenant OIDC federation that lives in the identity service, not Dex. Dex does not do dynamic client registration. The token would be a Dex token, not an Orbit principal, forcing a second exchange anyway. Rejected: it fragments the identity authority and still doesn't satisfy DCR.

### Option C — Token handoff via a new `/session` grant (non-standard)

The MCP client somehow obtains a tenant-IdP token and posts it to a new `mcp_oauth_token` grant on `/session`, reusing the `oidc_id_token` pattern.

- **Pros:** smallest Orbit change; maximal reuse.
- **Cons:** **not the MCP authorization flow.** Real MCP clients do discovery + authorization-code + PKCE against an AS; they will not perform this bespoke handshake. It only works for a custom client we write — which defeats the goal. Rejected as MVP human auth; the `oidc_id_token` machinery is still reused *internally* by Option A's `/token` step.

### Sub-decision — Dynamic Client Registration

The MCP spec has moved DCR from required toward optional, allowing alternatives (pre-registered public clients, client-id metadata documents). **MVP ships DCR (RFC 7591) for public clients with PKCE** — it is *in* the first cut, because the major desktop MCP clients expect to self-register — with a configuration flag to require pre-registration in stricter deployments. The only thing deferred to implementation is pinning the exact MCP spec revision the endpoints conform to, not whether DCR ships.

---

## Decision

| # | Decision |
|---|----------|
| 1 | Orbit's identity service becomes a **minimal OAuth 2.1 authorization server** for the MCP profile (Option A) — not a user directory. |
| 2 | `/authorize` federates to the **tenant's existing OIDC provider**; the human logs in where they already do. |
| 3 | The issued **access token is an ordinary Orbit platform token** (`aud=orbit-platform`), `PrincipalType=HUMAN`, revocable via the existing JTI blocklist — no new token shape. |
| 4 | The **gateway** (resource server) serves protected-resource metadata (RFC 9728); the **identity service** serves AS metadata (RFC 8414) and DCR (RFC 7591). |
| 5 | Reuse token signing, JWKS verification, `PlatformClaims`, and the `oidc_id_token` validation path internally. |

### Rationale

Option A is the only choice that makes off-the-shelf MCP clients work *and* keeps one identity authority. The cost — a real authorization-server surface — is unavoidable for spec compliance, but it is built *on* the existing identity core (signing, JWKS, claims, JTI, per-tenant OIDC), so it is additive, not a parallel stack. The access token a human ends up with is the same kind of platform token an agent carries, so the gateway's PEP needs no special case: one spine, two doors.

---

## Architecture

### Flow (spec-compliant, federating to the tenant IdP)

```
 MCP client (Claude Desktop)        tool-gateway (RS)        identity service (AS)        tenant OIDC IdP
        │  POST /mcp (no token) ───────▶ │                                                     
        │  ◀─ 401 + WWW-Authenticate ─── │ (points to PRM)                                     
        │  GET /.well-known/oauth-protected-resource ▶ │                                       
        │  ◀──────── AS = identity svc ─ │                                                     
        │  GET /.well-known/oauth-authorization-server ──────────▶ │                           
        │  POST /register (DCR, PKCE) ───────────────────────────▶ │                           
        │  GET /authorize?…PKCE… ────────────────────────────────▶ │  ── OIDC auth-code ─────▶ │
        │                                                          │  ◀── id_token ─────────── │
        │  ◀──────────────── redirect w/ code ───────────────────  │ (validates id_token,      
        │  POST /token (code+verifier) ──────────────────────────▶ │  mints platform token,    
        │  ◀──────── access_token (ordinary Orbit platform JWT) ─  │  HUMAN, tenant=acme)       
        │  POST /mcp (Bearer access_token) ─▶ │  validate (JWKS, standard verifier) → claims → PEP
```

### Token shape

The access token is a `PlatformClaims` JWT, RS256-signed by the existing signing key, verifiable at the existing JWKS endpoint:

```jsonc
{
  "aud": "orbit-platform",          // ordinary platform token (same as agent/PAT tokens)
  "orbit": {
    "principal_type": "HUMAN",
    "tenant_id": "acme",
    "identity_source": "FEDERATED", // logged in via the tenant's OIDC IdP
    "session_id": "…",              // JTI-revocable via existing blocklist
    "display_name": "jane@acme.com",
    "claims": { "mcp_client_id": "<DCR client id>" }
  }
}
```

The gateway validates it with the existing `token.Verifier`, gets `*PlatformClaims` in context, and authorises with the unchanged PEP. No new principal *type* is strictly required — `HUMAN` is correct; the `mcp_client_id` claim records which registered client was used.

The token is a standard `aud=orbit-platform` platform token — the same shape `signToken` already produces and the same audience every Orbit verifier already accepts. So both doors (agent and human) present the same audience and the gateway uses the existing verifier with **no signing or verification changes**. Authorisation is by the human's scopes, exactly as it is for agents and PATs. Per-resource audience binding (RFC 8707 resource indicators) is a possible future hardening, deferred — see *Future Work*.

### New surface vs reuse

| Concern | Mechanism | Status |
|---|---|---|
| Protected-resource metadata (RFC 9728) | gateway `GET /.well-known/oauth-protected-resource` | **new** (small) |
| AS metadata (RFC 8414) | identity `GET /.well-known/oauth-authorization-server` | **new** (small) |
| Dynamic client registration (RFC 7591) | identity `POST /register` (public clients + PKCE) | **new** |
| `/authorize` + `/token` (OAuth 2.1 + PKCE) | identity service | **new** (the real surface) |
| Login (who is this human) | per-tenant OIDC federation | **reused** |
| id_token → platform token | `oidc_id_token` handoff path | **reused** |
| Token signing / JWKS / verification | `internal/platform/token`, identity `signToken` | **reused** (no change — ordinary `aud=orbit-platform` token) |
| Revocation | JTI blocklist (Redis) | **reused** |
| Principal in the gateway | `PlatformClaims` + `AuthMiddleware` | **reused** |

### Tenant correctness & isolation

`/authorize` resolves the tenant (home-realm discovery / the client's declared tenant, reusing existing tenant routing) and federates to *that* tenant's IdP. The issued token is stamped with that `tenant_id`; the gateway authorises only against that tenant's (and global) grants. A human can never obtain a token for another tenant because `/authorize` only knows how to federate to the tenants they can authenticate against.

---

## Amendment 2026-07-04 — SSO session reuse and the `/authorize` tier

Clarifies **where** the browser-facing `/authorize` endpoint lives and how it avoids re-authenticating a human who is already signed into Orbit. This does not change the decision (Option A) — it pins down the tier split the original wrote loosely.

### `/authorize` is a **BFF (frontend) route**, not the identity service

The authorization step is browser-facing, and Orbit's browser session lives in the **Next.js BFF's sealed iron-session cookie** (holds `session.user` + the platform token; populated by `POST /api/auth/session`). A *backend* identity-service endpoint cannot read that cookie, so it could never reuse the human's existing Orbit session. Therefore:

- `/authorize` is served by the **BFF**, which reads the iron-session directly.
- The AS metadata's `authorization_endpoint` (RFC 8414) points at the **BFF** `/authorize`, not the identity service.

### SSO: reuse the existing Orbit session; federate only on cold start

`/authorize` checks the iron-session (`isSessionValid`):

- **Valid session** → the human is already authenticated to Orbit → the BFF has identity issue a short-lived authorization code bound to `{human identity, PKCE challenge, redirect_uri}` and redirects back to the client. **Silent — no IdP redirect.**
- **No session** → drive the existing browser login flow (federate to the tenant's OIDC IdP), return to `/authorize`, then issue the code.

Two layers of SSO therefore reduce prompts: the **tenant IdP's own** session already makes the cold-start redirect silent when the human has an active org session; the **Orbit-session** check additionally skips the redirect *entirely* once they have signed into Orbit. "Always redirect to the org IdP" is never "always re-login."

### `/token` and `/register` stay on the **identity service** (back-channel)

The MCP client exchanges `{code, PKCE verifier}` at `/token`; identity validates against its **code store** (Redis, alongside the JTI blocklist) and mints the ordinary `aud=orbit-platform` HUMAN token via `signToken`. No session is needed here — the code already carries the human the BFF authenticated. DCR (`/register`), AS metadata, and JWKS also stay on identity.

**Tier split:** the **BFF** owns the session-aware browser step (`/authorize` — reads the iron-session, decides silent-code vs federate); the **identity service** owns the code store, token mint, DCR, and signing (`/token`, `/register`, metadata, JWKS). The thin BFF `/authorize` asks identity to mint the code after its session check.

### Host derivation (implementation note)

Discovery-metadata URLs (PRM, AS metadata, the 401 challenge) are **derived from the request host** (ingress `X-Forwarded-*` / `Host`) rather than a configured host, so one binary is correct at any exposure host. Because the host is client-influenceable, those responses are served `Cache-Control: no-store` (never shared-cached, to prevent cross-tenant metadata poisoning), and the resolved host **must be validated against an allowlist of trusted public hostnames before these endpoints are publicly exposed** — deferred to the public MCP-gateway exposure ADR, where the real hosts are decided.

---

## Amendment 2026-07-04 — Refresh tokens for durable MCP connections

### The gap

The first cut of `/token` (`authorization_code` grant) returned only a short-lived access token — `aud=orbit-platform`, lifetime `accessTokenTtlMinutes` (15 min in production). No `refresh_token` was issued and `grant_type=refresh_token` was not accepted. So when the access token expired, an MCP client had **no way to refresh** — it had to re-run the entire authorization-code + consent flow. At a 15-minute access-token lifetime that means re-consent every 15 minutes, which is unusable for a long-lived desktop MCP connection.

### Decision

The MCP `/token` endpoint issues a **refresh token** on the authorization-code exchange and accepts **`grant_type=refresh_token`** to rotate it. This is not new machinery: the human session flow (device grant, browser SSO) has always issued and rotated refresh tokens, and the MCP exchange already mints a full human session under the hood. The change is simply to **stop discarding the session's refresh token** and to route the refresh grant through the existing rotation path:

- **Issue** — `ExchangeAuthCode` builds the session via `createSession` (which mints an opaque refresh token, stores only its hash, and returns it) instead of `buildAndPersistSession` (access-token only), and returns `refresh_token` + `refresh_token_expires_in` in the response.
- **Refresh** — a thin `RefreshOAuthToken` wraps the existing `RefreshSession`, which validates the presented refresh token (hash lookup, not-revoked, not-expired), **rotates** it (revokes the used token and issues a new one), rebuilds the principal from `session.Claims` — so the human's **roles and `mcp_client_id` survive every refresh** — and mints a fresh access + refresh token pair.

`grant_types_supported` (AS metadata) and the DCR response's `grant_types` advertise `refresh_token` so spec-compliant clients discover it.

### Security posture

- **Rotation is mandatory.** Every refresh revokes the used token (one-time use), matching the OAuth 2.1 requirement for public clients. Because the client is public (no secret; PKCE is the proof), the refresh token itself is the bearer credential — rotation is the protection against replay, and a stolen-then-reused token is detected as an already-revoked token.
- **Lifetimes.** Access tokens stay short (`accessTokenTtlMinutes`); refresh tokens use the existing `refreshTokenTtlDays`. The short access-token lifetime plus JTI revocation is unchanged — refresh only removes the re-consent, not the revocation guarantees.
- **Revocation still works.** Refresh tokens are session-bound and the session is revocable, so a human revoking a connection kills both the access path (JTI/session) and the refresh path.
- **No new token model.** Still `aud=orbit-platform`; the [per-resource audience future work](#future-work--per-resource-audiences) is unchanged and orthogonal.

### Deliberate non-goal

The refresh grant is **not bound to `client_id`**. A public client cannot authenticate (no secret), so per RFC 6749 the refresh token is the credential and rotation is the sanctioned protection. Binding the session's `mcp_client_id` to the presenting client on refresh is a reasonable defense-in-depth hardening but is deferred — it does not change the trust model, only narrows misuse of an already-leaked token, which rotation already surfaces.

---

## Consequences

### Positive

- Off-the-shelf MCP clients authenticate with no Orbit-specific code.
- One identity authority; the human's token is an ordinary Orbit principal the PEP already understands.
- Builds on signing/JWKS/JTI/OIDC-federation — additive, not a parallel identity stack.

### Negative

- A real authorization-server surface (`/authorize`, `/token`, metadata, DCR) with its attendant security obligations: strict redirect-URI allow-listing, mandatory PKCE, consent handling, DCR abuse controls. This is the largest net-new build of the three ADRs.
- Orbit crosses from "OIDC client only" to "also an OAuth AS (for the MCP profile)" — a deliberate but notable expansion of the identity service's role.

### Neutral

- DCR scope is configurable (open self-registration vs pre-registration) per deployment posture.
- Spec is still evolving; the implementation pins a revision and isolates the AS surface so it can track changes.

---

## Implementation

- **Phase 1 — Metadata + resource server.** ✅ **Done.** Gateway PRM endpoint (RFC 9728) and `401 + WWW-Authenticate`; identity AS metadata (RFC 8414). URLs derived from the request host (see the 2026-07-04 amendment); `no-store`. Wires discovery end-to-end.
- **Phase 2 — Authorization-code + PKCE.** **`/authorize` is a BFF route** that reuses the existing Orbit session (iron-session) and issues the code silently, federating to the tenant IdP via the existing OIDC path only on cold start; **`/token` on the identity service** validates the code + PKCE and mints an ordinary platform token via the reused id_token→token path; JTI revocation. The AS metadata's `authorization_endpoint` points at the BFF. See the 2026-07-04 amendment.
- **Phase 3 — Dynamic client registration.** `POST /register` for public PKCE clients, with redirect-URI allow-listing and the pre-registration flag.
- **Phase 4 — Hardening.** ✅ **Consent screen done (2026-07-04):** `authorization_endpoint` is now an explicit approval page (`/mcp/authorize`, under the centered `(auth)` chrome). After the SSO session check it shows the requesting client (name via DCR metadata), the loopback destination, and an "acts as you — bounded by your permissions" disclosure, then requires an **Approve** before any code is minted; **Deny** returns `access_denied` to the client. No more silent issuance. **Deferred:** DCR abuse controls (rate-limiting `/register`) — explicitly parked; issuance audit events; and conformance testing against real MCP clients (Claude Desktop / Cursor), which is gated on the public-exposure ADR.

Exit: a stock MCP client completes discovery → registration → authorization → token, and calls the gateway with an Orbit platform token that the PEP authorises on the human's grants.

---

## Validation

- **Success metrics:** a stock MCP client authenticates with zero Orbit-specific configuration beyond the gateway URL; issued tokens are JTI-revocable; no cross-tenant token issuance in tests.
- **Monitoring:** `mcp_oauth_authorize_total{result}`, `mcp_oauth_token_issued_total`, DCR registrations, token revocations, conformance-suite pass rate.

---

## Future Work — per-resource audiences

This ADR deliberately issues the human's MCP token as an ordinary `aud=orbit-platform` platform token, identical to agent and PAT tokens. That keeps one token model across the platform and means *no* signing or verification changes. The trade-off is that, like every Orbit token today, it is not bound to a single resource server — a leaked token is bounded by its *scopes*, not by its audience.

If Orbit later wants true per-resource token binding (OAuth 2.1 / RFC 8707 Resource Indicators) — so a token minted for the MCP gateway cannot be replayed against the inference gateway or the main API — that is a **platform-wide change to the token model**, not an MCP concern, and should be its **own ADR**. It would introduce a per-request audience in `signToken`, an audience registry, and multi-audience verifiers across every service, and would let each resource server (the MCP gateway included) require its own audience. Until that exists, MCP rides the existing uniform-audience model rather than inventing a one-off audience for itself.

**This same work hardens the gateway→upstream caller-identity hop.** The [Gateway ADR](2026-06-29-mcp-tool-gateway.md) forwards the caller's full-power `aud=orbit-platform` token to `callerIdentity: forward` upstreams (e.g. the Orbit MCP) as raw passthrough in MVP. Per-resource audiences plus **RFC 8693 token exchange** would let the gateway swap that for a narrowed `aud=<server>` *delegated* token before forwarding — so even a trusted first-party upstream never holds a replayable full-Orbit credential. Passthrough-now / exchange-later is one coherent thread: the future audience ADR should cover this upstream-hop case alongside the resource-server case.

---

## Related Decisions

- [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) — the resource server this authenticates to.
- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) — the other foundation under the gateway.
- [OIDC Provider Abstraction and Local Identity Provider](2026-03-16-oidc-provider-abstraction-and-local-identity-provider.md) — the per-tenant OIDC federation reused for login.
- [Browser Auth Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — the authorization-code + id_token-handoff precedent.
- [Personal Access Tokens](2026-04-02-personal-access-tokens.md) — the existing long-lived human-credential model and JTI revocation.
- [Unified Identity Model](2026-03-05-unified-identity-model.md) — `PlatformClaims`, principal types, `PrincipalType=HUMAN`.

## References

- MCP authorization specification (OAuth 2.1 profile) — pinned revision in implementation.
- RFC 9728 (Protected Resource Metadata), RFC 8414 (Authorization Server Metadata), RFC 7591 (Dynamic Client Registration), RFC 7636 (PKCE), RFC 8628 (Device Authorization Grant).
- `internal/identity/service.go` (`signToken`, `ExchangeIDToken`, device/session), `internal/platform/token` (claims, verifier), `internal/identity/pat_service.go` (JTI revocation precedent).
