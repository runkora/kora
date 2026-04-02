# 0030. Browser-Based Authentication Flow and BFF Session Architecture

**Status:** Accepted
**Date:** 2026-03-24
**Deciders:** Engineering Team
**Technical Story:** [PRD — Browser-Based Authentication Flow](../prd/browser-auth-flow.md)

---

## Context

Orbit's web UI is currently unauthenticated. Any user who can reach the URL has full access to every page. The Go identity service (`ADR-0017`) implements only the OAuth 2.0 Device Authorization Grant (RFC 8628), which was designed for headless CLI use and is unsuitable for a browser session model. ADR-0017 listed "supporting OAuth 2.0 Authorization Code Flow" as a deferred non-goal pending a frontend.

The frontend now exists (`ADR-0019`) and a browser-native authentication mechanism is required. Three architectural questions must be answered before implementation can begin:

1. **Who executes the Authorization Code Flow with PKCE** — a browser-side OIDC client or the Go identity service?
2. **How does the browser's OIDC token become an Orbit platform session** — what is the handoff mechanism?
3. **How is the BFF session represented and persisted**, and which library implements it?

### Background

The Next.js frontend operates as a Backend-for-Frontend (BFF). Its API routes (`/api/auth/*`) run server-side in Node.js or the Edge Runtime. Its middleware runs exclusively in the Edge Runtime — a V8-based environment that exposes the Web Crypto API but does not include Node.js built-in modules (`node:crypto`, `node:buffer`, etc.). Any library imported by `middleware.ts` must be Edge Runtime compatible.

The Go identity service already owns all session, token issuance, and revocation logic. The JTI blocklist runs in Redis. The existing `POST /identity/session` route accepts a `refresh_token` grant type.

The OIDC provider (Dex in local development) is a standard OIDC-compliant provider. It supports Authorization Code Flow with PKCE and exposes a JWKS endpoint for public key distribution.

### Problem Statement

Design the smallest, most coherent architecture that:

- Enables browser-native login via OIDC Authorization Code Flow with PKCE
- Results in an Orbit platform JWT sealed into an encrypted HttpOnly cookie — the browser holds no Orbit token in readable form
- Protects all non-public routes via Next.js `middleware.ts`
- Fits within the Edge Runtime constraint for middleware
- Adds the minimum possible surface to the Go identity service
- Uses purpose-built, well-maintained browser tooling for the OIDC flow

### Goals

- Human operators can log in to the Orbit web UI via their configured OIDC provider
- An encrypted, HttpOnly BFF session cookie represents the logged-in state — opaque to browser JavaScript for Orbit token material
- All non-public routes redirect unauthenticated users to `/login`
- Logout revokes the backend token and clears the session cookie
- The flow works against the local OIDC provider (`make dev-up`) with no additional setup

### Non-Goals

- Replacing or removing the Device Authorization Grant — it remains for CLI use
- Multi-tenant tenant-picker UI
- Social login (Google, GitHub) — these are OIDC provider configuration concerns
- SAML federation
- Admin-initiated session revocation (force-logout other users)
- Mobile or native app authentication

---

## Decision Drivers

* **Security**: Orbit `access_token` and `refresh_token` must never reach browser JavaScript. The OIDC `id_token` may be held transiently in browser memory during the callback flow; it must be discarded immediately after the session handoff.
* **Minimalism**: The Go identity service should grow by the smallest possible surface. It already validates JWTs — validating an externally-presented `id_token` is a natural, minimal extension.
* **Tooling alignment**: Browser OIDC clients are mature, well-audited, and purpose-built for the Authorization Code Flow with PKCE. Reimplementing this in Go adds maintenance overhead for no security gain.
* **Provider agnosticism**: Neither the BFF nor the browser client should be coupled to a specific OIDC provider. The provider is an interchangeable configuration detail.
* **Edge Runtime compatibility**: `middleware.ts` runs in the V8 Edge Runtime. Any session library it uses must rely solely on the Web Crypto API — no Node.js built-ins.
* **Operational simplicity**: Avoiding server-side PKCE state storage eliminates a Redis dependency on the authentication critical path.

---

## Considered Options

### Question 1: Who executes the Authorization Code Flow with PKCE?

#### Option 1A: Frontend OIDC client (browser-side library)

**Description:** A browser-side OIDC client library runs in the Next.js frontend. It generates the PKCE code verifier, stores it in sessionStorage, constructs the authorization URL, redirects the browser, handles the callback, and exchanges the authorization code at the OIDC provider's token endpoint. On receiving the `id_token`, the browser forwards it to the BFF via `POST /api/auth/session`.

**Pros:**
- PKCE implementation is handled by a purpose-built, well-tested OIDC library — no custom implementation required
- No server-side state store (Redis) required for PKCE round-trip — the verifier lives in sessionStorage for the lifetime of the callback
- Code verifier never leaves the browser until it is used for the token exchange — it is not transmitted to any Orbit server
- Aligns with standard practice for SPAs with a BFF: the browser is the OIDC client, the BFF is the session layer
- OIDC provider changes require only environment variable updates — no Go backend changes
- Scales horizontally with no shared state on the auth critical path

**Cons:**
- The `id_token` is held in browser JavaScript memory transiently during the callback flow
- PKCE verifier in sessionStorage is readable by same-origin JavaScript — an XSS attacker could read it during the callback window
- OIDC issuer URL and client ID must be exposed as `NEXT_PUBLIC_*` env vars — these are not secrets, but they are public configuration

#### Option 1B: Go backend owns the full flow (BFF as thin proxy)

**Description:** The Go identity service exposes `POST /identity/login` (generates PKCE pair and opaque state, stores verifier in Redis, returns the fully-constructed authorization URL) and an `authorization_code` grant type in `POST /identity/session` (looks up state in Redis, exchanges code with OIDC provider, issues Orbit platform JWT). The BFF calls the Go backend for both operations — it never communicates with the OIDC provider directly and implements no PKCE logic.

**Pros:**
- PKCE verifier never enters the browser — generated and consumed entirely within the Go process
- OIDC `id_token` never reaches the browser
- No OIDC-specific npm dependency in the frontend

**Cons:**
- Requires two new Go backend endpoints before any frontend auth code can be end-to-end tested
- Requires server-side PKCE state storage (Redis) with a TTL — a new operational dependency on the auth critical path
- Reimplements PKCE logic in Go that browser OIDC libraries already handle correctly
- OIDC client config (issuer URL, client ID) must be available in both the Go backend and the frontend build
- Redis unavailability during login initiation or callback causes auth to fail

#### Option 1C: BFF talks directly to the OIDC provider (Next.js API routes own the flow)

**Description:** The Next.js BFF implements the full OIDC Authorization Code Flow in API routes: generates PKCE, builds the authorization URL, redirects the browser, handles the callback, and calls the provider's token endpoint server-side.

**Pros:**
- `id_token` never reaches the browser
- No browser-side OIDC library needed

**Cons:**
- PKCE state must be stored server-side between redirect and callback (cookie or Redis)
- Splits OIDC logic across two services: provider interaction in the BFF, session issuance in the backend
- OIDC client credentials and provider discovery config must be available in the Next.js process, expanding its secret surface if a `client_secret` is used
- More implementation code in the BFF — the BFF becomes a partial OIDC client rather than a session layer

---

### Question 2: How does the OIDC token become an Orbit platform session?

This question only applies once Option 1A is chosen — the browser has the `id_token` and must exchange it for an Orbit session.

#### Option 2A: New `oidc_id_token` grant type in `POST /identity/session`

**Description:** The BFF receives the `id_token` from the browser and forwards it to the Go identity service via `POST /identity/session` with `grant_type: oidc_id_token`. The identity service validates the `id_token` against the OIDC provider's JWKS, extracts claims, and issues an Orbit platform JWT using the existing `createSession` path.

**Pros:**
- Single new grant type in an existing endpoint — minimal Go backend surface change
- Token validation is centralised in the identity service, consistent with the existing architecture
- The identity service already performs OIDC provider discovery and JWKS fetching — this is a natural extension of that capability
- The BFF remains a thin layer: receive `id_token` from browser, forward to backend, seal result into cookie

**Cons:**
- The `id_token` crosses from browser to BFF to Go backend — it is transmitted over the internal network, which must be trusted
- The BFF must strip the `id_token` from memory after forwarding and not log it

#### Option 2B: BFF validates `id_token` locally and calls an existing Go backend endpoint

**Description:** The BFF uses a JWT validation library (`jose`) to verify the `id_token` signature against the OIDC provider's JWKS, extracts claims, and calls an existing Go backend endpoint to establish a session (e.g., `POST /identity/exchange` with a synthetic payload).

**Pros:**
- No new grant type in the Go backend

**Cons:**
- Duplicates `id_token` validation logic in two places (BFF and backend)
- `POST /identity/exchange` is designed for SPIFFE SVID exchange — repurposing it would be semantically incorrect
- The BFF must perform OIDC JWKS fetching and key rotation handling — complexity that belongs in the identity service
- If the validation logic diverges between BFF and backend, the BFF could issue sessions the backend would not have authorised

---

### Question 3: BFF session library

The BFF session must be represented as an encrypted HttpOnly cookie. The session payload is:

```typescript
interface SessionPayload {
  user: { sub: string; email: string; name: string };
  accessToken: string;
  refreshToken: string;
  expiresAt: number; // Unix seconds
}
```

The library used to seal and unseal this cookie **must be Edge Runtime compatible** — `middleware.ts` imports it to check session validity on every request.

#### Option 3A: `iron-session` (v8+)

**Description:** `iron-session` seals the session payload into a password-protected, tamper-evident cookie using AES-CBC + HMAC-SHA-256, backed entirely by the Web Crypto API. Version 8 was rewritten to be Edge Runtime compatible with no Node.js built-in dependencies.

**Pros:**
- Purpose-built for exactly this use case: sealed, self-contained session cookies
- Edge Runtime compatible (Web Crypto API only) — can be imported by `middleware.ts`
- Minimal API: `getIronSession(req, res, options)` / `session.save()` — no provider concepts or ceremony
- Widely adopted in the Next.js ecosystem; well-maintained
- No opinion about authentication strategy — purely a cookie sealing primitive, which is correct here because the Go backend owns the session issuance logic
- `BFF_SESSION_SECRET` (already wired in `src/server/config.ts`) maps directly to `iron-session`'s password option

**Cons:**
- Adds one npm dependency
- Sealed cookie format is `iron-session`-specific — a major version bump that changes the format will invalidate all active sessions

#### Option 3B: Custom `jose`-based JWE cookie

**Description:** Use the `jose` library to encrypt the session payload as a compact JWE and store it as a cookie value. Implement seal/unseal helpers in `src/server/session.ts`.

**Pros:**
- Full control over encryption algorithm and payload shape
- `jose` uses Web Crypto API — Edge Runtime compatible
- No session-specific library dependency

**Cons:**
- More implementation code to write and maintain (key derivation, encryption, decryption, cookie lifecycle, key rotation)
- JWE compact serialization is larger than `iron-session`'s sealed format, increasing cookie size
- Cookie key rotation and versioning must be implemented manually

#### Option 3C: Auth.js (NextAuth.js v5)

**Description:** Use Auth.js as the integrated Next.js authentication framework. It supports OIDC providers natively and manages sessions.

**Pros:**
- Comprehensive solution covering provider configuration, callbacks, session management, and middleware
- Large ecosystem and active maintenance

**Cons:**
- Auth.js is designed to own the OIDC flow end-to-end — it expects to talk to the OIDC provider directly, which conflicts with using a browser-side OIDC client for the flow
- Cannot be used purely as a session layer without taking on its full OIDC provider machinery
- Introduces significant framework concepts (providers, adapters, callbacks) that are unnecessary here
- Risk of future coupling between Auth.js OIDC assumptions and Go backend behaviour

---

## Decision

**Q1 — Flow ownership:** Option 1A — A browser-side frontend OIDC client executes the Authorization Code Flow with PKCE.

**Q2 — Token handoff:** Option 2A — New `oidc_id_token` grant type in `POST /identity/session`.

**Q3 — Session library:** Option 3A — `iron-session` v8+.

**Q4 — Browser OIDC client library:** Option 4A — `oidc-client-ts`.

### Rationale

**Q1:** A frontend OIDC client is the standard, purpose-built mechanism for Authorization Code Flow with PKCE in a browser application. Browser OIDC libraries are well-tested, handle PKCE verifier generation and storage correctly, and are maintained by the OIDC community. Option 1B (backend-owned flow) reimplements this in Go and adds a Redis dependency on the auth critical path for no security benefit that is not already achieved by the `id_token` handoff approach. Option 1C splits OIDC logic into the BFF, which becomes a partial OIDC client rather than a clean session layer.

**Q2:** A single new grant type in an existing endpoint is the minimal, coherent extension to the Go identity service. The identity service already performs OIDC discovery and JWKS fetching — validating an externally-presented `id_token` is a natural, small addition to that capability. Option 2B duplicates validation logic in the BFF and requires repurposing an endpoint designed for SPIFFE SVID exchange.

**Q3:** `iron-session` v8 is the minimal, purpose-built solution for sealed, tamper-evident session cookies backed by the Web Crypto API. It is Edge Runtime compatible, has no opinion about authentication strategy (correct here, since the Go backend owns session issuance), and maps directly onto the existing `BFF_SESSION_SECRET` env var. Auth.js (Option 3C) is disqualified because it is designed to own the OIDC flow. A custom `jose` implementation (Option 3B) is viable but adds unnecessary implementation and maintenance overhead.

**Q4:** `oidc-client-ts` is the de facto standard browser OIDC client in the TypeScript ecosystem — framework-agnostic, actively maintained, and the direct continuation of the widely-deployed `oidc-client` library. Being framework-agnostic is the right property here: the critical path through the `/auth/callback` page requires direct access to `UserManager` to extract the `id_token` for the BFF handoff step, and neither Option 4B nor 4C meaningfully simplifies that interaction. Option 4B (`react-oidc-context`) adds a React wrapper over `oidc-client-ts` but both packages would be required, adding dependency weight for no functional gain in the callback path. Option 4C (`@axa-fr/react-oidc`) is more opinionated about the callback lifecycle, which conflicts with the BFF-driven handoff. Option 4D (NextAuth) is disqualified at the architecture level — it runs the OIDC flow server-side, reversing the Q1 decision, and is designed to own the session layer, displacing `iron-session`.

---

## Consequences

### Positive

- PKCE is handled by a purpose-built browser OIDC library — no custom implementation to maintain
- No Redis dependency on the auth critical path — PKCE state lives in sessionStorage for the callback window only
- The Go identity service grows by one grant type in one existing endpoint — minimal surface change
- Future OIDC provider changes require only environment variable updates — no Go backend or BFF code changes
- The BFF session cookie is self-contained — horizontal scaling of the Next.js layer requires no sticky sessions or shared session store
- Middleware operates entirely within the Edge Runtime constraint

### Negative

- The OIDC `id_token` is held in browser JavaScript memory transiently during the callback flow — this is an accepted, bounded exposure window
- PKCE verifier in sessionStorage is readable by same-origin JavaScript; an XSS attack during the callback window could read it — mitigated by the short TTL and standard XSS defences (CSP)
- OIDC issuer URL and client ID are public configuration exposed in `NEXT_PUBLIC_*` env vars — these are not secrets, but they are visible in the browser
- `iron-session`'s sealed cookie format is library-specific — a major version bump that changes the format will invalidate all active sessions and require a coordinated deployment

### Neutral

- The Go identity service gains one new grant type (`oidc_id_token`) in `POST /identity/session`, consistent with its existing role as the identity authority
- `iron-session` is added to the frontend `package.json` as a direct dependency
- `oidc-client-ts` is added to the frontend `package.json` as a direct dependency
- `config/dex/config.yaml` requires a new `redirectURIs` entry for local development (the `orbit-local` static client must register `http://localhost:3000/auth/callback`)
- OIDC issuer URL and client ID are added as `NEXT_PUBLIC_*` environment variables

---

## Implementation

### New Go Backend Addition

**`oidc_id_token` grant type in `POST /identity/session`**

- Request body: `{ "grant_type": "oidc_id_token", "id_token": "<raw OIDC JWT>" }`
- Validates the `id_token` signature using the OIDC provider's JWKS (fetched via the already-initialised `go-oidc` provider)
- Validates standard claims: issuer, audience (`client_id`), expiry
- Extracts identity claims: `sub`, `email`, `name`, `preferred_username`
- Calls the existing `createSession` path to issue an Orbit platform JWT and refresh token
- Returns the standard `SessionResponse` shape: `{ access_token, refresh_token, expires_in, issued_at }`
- No Redis dependency — there is no PKCE state to store or retrieve

### New Frontend Files

| File | Purpose |
|------|---------|
| `middleware.ts` | Edge-compatible session check on every request; redirects to `/login?next=<path>` if no valid session; performs inline token refresh if within expiry threshold |
| `src/lib/oidc-client.ts` | OIDC client configuration — issuer, client ID, redirect URI, scopes; initialised from `NEXT_PUBLIC_*` env vars |
| `src/server/session.ts` | `iron-session` seal/unseal helpers; defines `SessionPayload` type; never imported from client components |
| `src/server/auth-config.ts` | `iron-session` configuration (`cookieName`, `password`, `cookieOptions`) |
| `src/app/login/page.tsx` | Login page with a single "Sign in" button that calls `signinRedirect()` on the OIDC client, encoding the post-login destination in the OIDC `state` parameter |
| `src/app/auth/callback/page.tsx` | Client-rendered callback page; invokes `signinRedirectCallback()` on the OIDC client to complete the token exchange, then POSTs the `id_token` to `/api/auth/session` |
| `src/app/api/auth/session/route.ts` | Accepts `{ id_token }` from the callback page; forwards to Go identity service with `oidc_id_token` grant; seals returned Orbit tokens into session cookie; redirects to `state.next` or `/` |
| `src/app/api/auth/session/route.ts` (GET) | Returns non-sensitive session data (`user`, `expiresAt`) for client components; returns 401 if no session |
| `src/app/api/auth/logout/route.ts` | Calls `POST /identity/revoke`, clears session cookie, redirects to `/login` |

### Post-Login Redirect Destination

The post-login destination (`next`) is encoded in the OIDC `state` parameter by the OIDC client at the time of the authorization redirect. The OIDC client carries this value through the provider round-trip and makes it available to the callback handler. On callback, the BFF reads `state.next`, validates it as a relative path present in `APP_ROUTES`, and uses it as the final redirect destination. Any value that is absolute, contains a host, or is not in `APP_ROUTES` is silently discarded and the default `/` is used.

### OIDC Client Configuration

The OIDC client is initialised with values from `NEXT_PUBLIC_*` environment variables:

| Variable | Description |
|----------|-------------|
| `NEXT_PUBLIC_OIDC_ISSUER_URL` | OIDC provider issuer URL (e.g., `http://dex.orbit.test:5556/dex`) |
| `NEXT_PUBLIC_OIDC_CLIENT_ID` | OIDC client ID registered with the provider (public client — no secret) |
| `NEXT_PUBLIC_OIDC_REDIRECT_URI` | Callback URL (e.g., `http://localhost:3000/auth/callback`) |

No `client_secret` is used — the frontend OIDC client is a public client. PKCE provides code interception protection.

### Session Cookie Attributes

```
HttpOnly; Secure; SameSite=Lax; Path=/
```

`Secure` is conditional on `NODE_ENV=production` to allow `http://localhost` in local development. `SameSite=Lax` provides CSRF protection for navigation requests; CSRF on `POST /api/auth/logout` is accepted risk for this phase (forcing a sign-out has no data exfiltration consequence).

### Token Refresh

Middleware checks `expiresAt` from the session payload. If the access token is within a configurable threshold of expiry (default: 5 minutes), middleware calls `POST /identity/session` with `grant_type: refresh_token` before forwarding the request, then updates the session cookie with the new tokens. This adds one backend round-trip per refresh event — acceptable for a low-traffic control plane.

### BFF Session Secret

| Variable | Description | Required |
|----------|-------------|----------|
| `BFF_SESSION_SECRET` | Minimum 32-character secret for `iron-session` cookie sealing | Yes |

Already defined in `src/server/config.ts`. No new env var required.

### Local Development Config Change

Add the following to `config/dex/config.yaml` under the `orbit-local` static client:

```yaml
redirectURIs:
  - 'http://localhost:3000/auth/callback'
```

This change ships alongside the Phase 1 implementation.

### Migration Path

There is no existing browser session to migrate. The Device Authorization Grant is unaffected — the Go backend continues to handle it independently. The new `oidc_id_token` grant type is additive and does not alter any existing grant type behaviour.

---

## Validation

### Success Metrics

- A user visiting any protected route without a session cookie is redirected to `/login`
- A user completing the full OIDC login flow (local provider: `dev@orbit.test` / `password`) lands on their original destination
- The browser's DevTools network panel shows no `access_token` or `refresh_token` values in any response body or cookie value that is readable by JavaScript
- `POST /api/auth/logout` results in a cleared session cookie and a backend `POST /identity/revoke` call, confirmed via Go backend logs
- An expired session redirects to `/login` on the next request
- `middleware.ts` passes the Next.js Edge Runtime compatibility check (`next build` with no Edge Runtime warnings)

### Monitoring

- Structured log entries for all auth transitions (callback success/failure, session creation, token refresh, logout) — no token values in any log field
- Go backend spans for the `oidc_id_token` grant type emitted to the OpenTelemetry pipeline
- OIDC JWKS fetch errors surfaced as structured log entries in the Go backend

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — Unified identity model; defines the `Principal`, session, and JWT issuance model that the browser flow builds on
- [ADR-0017](0017-oidc-provider-abstraction-and-local-identity-provider.md) — Introduced Device Authorization Grant and explicitly deferred Authorization Code Flow; this ADR delivers on that deferred goal
- [ADR-0019](0019-frontend-scaffolding-and-toolchain.md) — Established the Next.js BFF architecture and the `APP_ROUTES` pattern
- [ADR-0022](0022-opentelemetry-observability-discipline.md) — Observability discipline; auth route spans and structured logs must comply with its conventions

---

## References

- [PRD — Browser-Based Authentication Flow](../prd/browser-auth-flow.md)
- [RFC 6749 — The OAuth 2.0 Authorization Framework](https://datatracker.ietf.org/doc/html/rfc6749)
- [RFC 7636 — Proof Key for Code Exchange (PKCE)](https://datatracker.ietf.org/doc/html/rfc7636)
- [RFC 8628 — OAuth 2.0 Device Authorization Grant](https://datatracker.ietf.org/doc/html/rfc8628)
- [OIDC Core 1.0 — Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth)
- [iron-session v8 — Edge Runtime compatible sealed session cookies](https://github.com/vvo/iron-session)
- [Next.js Edge Runtime — supported APIs](https://nextjs.org/docs/app/api-reference/edge)

---

## Notes

This ADR supersedes the "deferred" status of Authorization Code Flow in ADR-0017. The Device Authorization Grant defined in ADR-0017 is unaffected and continues to serve CLI use cases.

The `oidc_id_token` grant type is intentionally narrow: it accepts only a signed `id_token` from the configured OIDC provider. It does not accept raw access tokens, userinfo responses, or assertions from any provider not configured in the Go identity service.

**Last Updated:** 2026-03-25
