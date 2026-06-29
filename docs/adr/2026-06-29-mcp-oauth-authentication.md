# MCP OAuth Authentication Model

<!-- Filename: 2026-06-29-mcp-oauth-authentication.md -->

**Status:** Proposed
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
5. runs **OAuth 2.1 authorization-code + PKCE**, obtains an access token whose audience is the gateway;
6. calls the gateway with that token.

Real MCP clients implement this flow; they will not perform a bespoke "paste your Dex token into a custom `/session` endpoint" handshake. So supporting them is not a pure reuse of the existing relying-party machinery — there is a genuine new surface.

### Problem Statement

Decide how Orbit satisfies the MCP authorization spec for human MCP clients, reusing as much of the existing token/identity machinery as possible, while keeping the resulting credential a first-class Orbit principal that the gateway's PEP authorises exactly like any other.

### Goals

- A human MCP client authenticates to the gateway via the standard MCP OAuth flow, no Orbit-specific client code.
- The issued access token is an Orbit platform token (or verifiable as one) carrying a HUMAN principal the PEP authorises on existing grants.
- Reuse token signing, JWKS, claims, JTI revocation, and per-tenant OIDC federation.
- Tenant-correct: the human authenticates against *their* tenant's identity provider; the token is audience-bound to the gateway.

### Non-Goals

- The gateway, PEP, and proxy — the [gateway ADR](2026-06-29-mcp-tool-gateway.md).
- Agent authentication (injected platform token) — already solved.
- A general-purpose, externally-consumable Orbit OAuth provider beyond what the MCP spec requires. We build the MCP profile, not a public IdP.

---

## Decision Drivers

- **Spec compliance is non-negotiable** — the value is that off-the-shelf MCP clients just work.
- **Reuse the identity core.** Token signing (RS256), JWKS, `PlatformClaims`, JTI blocklist revocation, and the `oidc_id_token` handoff already exist and are tested.
- **Don't reinvent IdP federation.** Orbit already does per-tenant OIDC discovery and id_token validation; the human's actual login should ride that, not a second user store.
- **Audience binding.** The gateway is a resource server; tokens must be bound to it, not general-purpose platform tokens.

---

## Considered Options

### Option A — Orbit identity service becomes a minimal OAuth 2.1 authorization server, federating to the tenant IdP (chosen)

Orbit's identity service grows the MCP-required authorization-server surface — `/authorize`, `/token`, AS metadata (RFC 8414), protected-resource metadata served by the gateway (RFC 9728), and dynamic client registration (RFC 7591) — but does **not** become a user directory. When a client hits `/authorize`, Orbit runs its existing OIDC relying-party flow against the **tenant's** configured identity provider (the same per-tenant OIDC it already uses for browser/device login), then issues its own **platform token** as the OAuth access token, audience-bound to the gateway, `PrincipalType=HUMAN`, revocable via the existing JTI blocklist.

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

The MCP spec has moved DCR from required toward optional, allowing alternatives (pre-registered public clients, client-id metadata documents). MVP implements **DCR (RFC 7591) for public clients with PKCE**, because the major desktop MCP clients expect to self-register, with a configuration flag to require pre-registration in stricter deployments. Exact MVP surface pinned in implementation against the chosen spec revision.

---

## Decision

| # | Decision |
|---|----------|
| 1 | Orbit's identity service becomes a **minimal OAuth 2.1 authorization server** for the MCP profile (Option A) — not a user directory. |
| 2 | `/authorize` federates to the **tenant's existing OIDC provider**; the human logs in where they already do. |
| 3 | The issued **access token is an Orbit platform token**, `PrincipalType=HUMAN`, **audience-bound to the gateway**, revocable via the existing JTI blocklist. |
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
        │  ◀──────── access_token (Orbit platform JWT, aud=gw) ──  │  aud=tool-gateway, HUMAN) 
        │  POST /mcp (Bearer access_token) ─▶ │  validate (JWKS) → claims → PEP                  
```

### Token shape

The access token is a `PlatformClaims` JWT, RS256-signed by the existing signing key, verifiable at the existing JWKS endpoint:

```jsonc
{
  "aud": "tool-gateway",            // audience-bound to the resource server
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

### New surface vs reuse

| Concern | Mechanism | Status |
|---|---|---|
| Protected-resource metadata (RFC 9728) | gateway `GET /.well-known/oauth-protected-resource` | **new** (small) |
| AS metadata (RFC 8414) | identity `GET /.well-known/oauth-authorization-server` | **new** (small) |
| Dynamic client registration (RFC 7591) | identity `POST /register` (public clients + PKCE) | **new** |
| `/authorize` + `/token` (OAuth 2.1 + PKCE) | identity service | **new** (the real surface) |
| Login (who is this human) | per-tenant OIDC federation | **reused** |
| id_token → platform token | `oidc_id_token` handoff path | **reused** |
| Token signing / JWKS / verification | `internal/platform/token`, identity `signToken` | **reused** |
| Revocation | JTI blocklist (Redis) | **reused** |
| Principal in the gateway | `PlatformClaims` + `AuthMiddleware` | **reused** |

### Tenant correctness & isolation

`/authorize` resolves the tenant (home-realm discovery / the client's declared tenant, reusing existing tenant routing) and federates to *that* tenant's IdP. The issued token is stamped with that `tenant_id`; the gateway authorises only against that tenant's (and global) grants. A human can never obtain a token for another tenant because `/authorize` only knows how to federate to the tenants they can authenticate against.

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

- **Phase 1 — Metadata + resource server.** Gateway PRM endpoint and `401 + WWW-Authenticate`; identity AS metadata. Wires discovery end-to-end with a pre-registered test client.
- **Phase 2 — Authorization-code + PKCE.** `/authorize` (federating to the tenant IdP via the existing OIDC path) and `/token` (minting an audience-bound platform token via the reused id_token→token path); JTI revocation.
- **Phase 3 — Dynamic client registration.** `POST /register` for public PKCE clients, with redirect-URI allow-listing and the pre-registration flag.
- **Phase 4 — Hardening.** Consent, DCR abuse controls, audit of token issuance, and conformance testing against real MCP clients (Claude Desktop / Cursor).

Exit: a stock MCP client completes discovery → registration → authorization → token, and calls the gateway with an Orbit platform token that the PEP authorises on the human's grants.

---

## Validation

- **Success metrics:** a stock MCP client authenticates with zero Orbit-specific configuration beyond the gateway URL; issued tokens are audience-bound and JTI-revocable; no cross-tenant token issuance in tests.
- **Monitoring:** `mcp_oauth_authorize_total{result}`, `mcp_oauth_token_issued_total`, DCR registrations, token revocations, conformance-suite pass rate.

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
