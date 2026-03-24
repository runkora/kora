# 0023. Browser-Based Authentication Flow and BFF Session Architecture

**Status:** Accepted
**Date:** 2026-03-24
**Deciders:** Engineering Team
**Technical Story:** [PRD — Browser-Based Authentication Flow](../prd/browser-auth-flow.md)

---

## Context

Orbit's web UI is currently unauthenticated. Any user who can reach the URL has full access to every page. The Go identity service (`ADR-0017`) implements only the OAuth 2.0 Device Authorization Grant (RFC 8628), which was explicitly designed for headless CLI use and is unsuitable for a browser session model. ADR-0017 listed "supporting OAuth 2.0 Authorization Code Flow" as a deferred non-goal pending a frontend.

The frontend now exists (`ADR-0019`) and a browser-native authentication mechanism is required. Three architectural questions must be answered before implementation can begin:

1. **Who owns the OIDC Authorization Code Flow with PKCE** — the Next.js BFF or the Go identity service?
2. **Where is PKCE state stored** during the OAuth round-trip between authorization redirect and callback?
3. **How is the BFF session represented and persisted**, and which library implements it?

### Background

The Next.js frontend operates as a Backend-for-Frontend (BFF). Its API routes (`/api/auth/*`) run server-side in Node.js or the Edge Runtime. Its middleware runs exclusively in the Edge Runtime — a V8-based environment that exposes the Web Crypto API but does not include Node.js built-in modules (`node:crypto`, `node:buffer`, etc.). Any library imported by `middleware.ts` must be Edge Runtime compatible.

The Go identity service already owns all session, token issuance, and revocation logic. The JTI blocklist runs in Redis. The existing `POST /identity/session` route accepts a `refresh_token` grant type.

### Problem Statement

Design the smallest, most coherent architecture that:

- Enables browser-native login via OIDC Authorization Code Flow with PKCE
- Keeps all OAuth tokens (`access_token`, `refresh_token`, `id_token`) server-side — the browser never receives them
- Protects all non-public routes via Next.js `middleware.ts`
- Fits within the Edge Runtime constraint for middleware
- Preserves the existing architecture principle that the Go identity service is the single source of truth for all identity, session, and credential operations

### Goals

- Human operators can log in to the Orbit web UI via their configured OIDC provider
- An encrypted, HttpOnly, server-side BFF session represents the logged-in state — opaque to browser JavaScript
- All non-public routes redirect unauthenticated users to `/login`
- Logout revokes the backend token and clears the session cookie
- The flow works against the local OIDC provider (`make dev-up`) with no additional setup
- PKCE verifier and OAuth state are generated and validated in a single trusted process

### Non-Goals

- Replacing or removing the Device Authorization Grant — it remains for CLI use
- Multi-tenant tenant-picker UI
- Social login (Google, GitHub) — these are OIDC provider configuration concerns
- SAML federation
- Admin-initiated session revocation (force-logout other users)
- Mobile or native app authentication

---

## Decision Drivers

* **Security**: OAuth tokens must never reach the browser. PKCE verifier must be generated and validated in a single trusted process.
* **Architectural coherence**: The Go identity service is already the authoritative owner of all OIDC, session, and credential logic. Duplicating any of this in the BFF increases the attack surface and maintenance cost.
* **Edge Runtime compatibility**: `middleware.ts` runs in the V8 Edge Runtime. Any session decryption library it uses must rely solely on the Web Crypto API — no Node.js built-ins.
* **Operational simplicity**: The BFF has no external state store today. Adding one for session management requires new infrastructure and introduces a point of failure.
* **Provider agnosticism**: The BFF must not be coupled to a specific OIDC provider. Provider details belong in the Go backend.

---

## Considered Options

### Question 1: Authorization Code Flow ownership

#### Option 1A: Go backend owns the full flow (BFF is a thin proxy)

**Description:** The Go identity service exposes two new endpoints: `POST /identity/login` (generates PKCE pair and state, returns the fully-constructed provider authorization URL) and an `authorization_code` grant type in `POST /identity/session` (validates state, exchanges code with OIDC provider, issues Orbit platform JWT). The BFF calls the Go backend for both operations — it never communicates with the OIDC provider directly.

**Pros:**
- All OIDC-sensitive logic stays within the single trusted identity service
- PKCE verifier never crosses a process boundary — generated and consumed in the same Go service
- Future OIDC provider changes require updates only in the Go backend, not in Next.js
- Consistent with the existing architecture: the BFF is already a thin proxy to the Go backend for all identity operations
- OIDC client credentials and provider discovery config remain in the Go backend only

**Cons:**
- Adds two new endpoints to the Go identity service (implementation cost)
- BFF cannot implement OIDC directly — it depends on the backend being up for auth initiation

#### Option 1B: BFF talks directly to the OIDC provider; calls backend only for token exchange

**Description:** The Next.js BFF implements the full OIDC Authorization Code Flow: generates PKCE, builds the authorization URL, redirects the browser, handles the callback, and calls the provider's token endpoint. On success, it calls `POST /identity/session` with a new `authorization_code` grant type to exchange the raw OIDC tokens for an Orbit platform JWT.

**Pros:**
- No new Go backend endpoints required for the redirect/callback steps
- BFF can use mature OIDC libraries (`openid-client`)
- Faster to prototype

**Cons:**
- Splits OIDC logic across two services: provider interaction in the BFF, session issuance in the backend
- OIDC client credentials and provider discovery config must be available to the Next.js process, expanding its secret surface
- Any future OIDC provider change requires updates to both the BFF and the Go backend
- PKCE verifier crosses a process boundary (BFF generates, backend may re-verify)
- Violates the architectural principle that the identity service owns all identity operations

#### Option 1C: BFF handles everything end-to-end (bypasses Go backend for auth)

**Description:** The Next.js BFF performs the full OIDC Authorization Code Flow and issues its own session — the Go backend is not involved in login at all. The BFF would need to implement PKCE, state management, token validation, and session issuance independently.

**Pros:**
- Completely decoupled from the Go backend for the auth path

**Cons:**
- Duplicates the entire identity layer in a second service
- The BFF would need to issue its own JWTs or use opaque tokens — incompatible with the existing Orbit platform JWT model
- Every downstream API call that requires an Orbit platform JWT would still need the backend involved
- Highest implementation and maintenance cost; violates all architectural coherence goals

---

### Question 2: PKCE state and verifier storage

#### Option 2A: Redis in the Go backend (keyed by opaque state value, TTL 15 minutes)

**Description:** The Go identity service generates the PKCE code pair and opaque `state` value, stores `{ "verifier": "<base64url>" }` in Redis under the `state` key with a 15-minute TTL. On callback, the backend retrieves and deletes the verifier in a single atomic operation. Redis is already deployed for the JTI blocklist.

**Pros:**
- No new infrastructure — Redis already present and operated by the identity service
- Verifier never leaves the Go process
- Atomic read-delete prevents replay attacks on the state parameter
- Natural fit with Option 1A: since the backend owns the flow, it also owns the PKCE state machine

**Cons:**
- Redis unavailability during auth initiation or callback causes auth to fail
- Redis is a shared dependency; PKCE keys must use a distinct namespace to avoid collisions

#### Option 2B: BFF short-lived HttpOnly cookie (signed, encrypted)

**Description:** The BFF generates the PKCE pair, stores the verifier in a short-lived signed HttpOnly cookie set before the OIDC redirect, and reads it back on callback.

**Pros:**
- No server-side store — scales horizontally without shared state
- Failure mode is predictable (cookie absent → auth restart)

**Cons:**
- PKCE verifier must exist in the BFF process — incompatible with Option 1A where the backend owns PKCE generation
- Requires the BFF to hold raw PKCE material, even if encrypted for transit
- Adds a second encrypted cookie to manage alongside the session cookie

#### Option 2C: Encrypted state parameter (JWE)

**Description:** The code verifier is encrypted and embedded directly in the `state` parameter value as a JWE token. No external store is needed.

**Pros:**
- Stateless — no Redis dependency on the auth path
- Scales horizontally; no shared external state

**Cons:**
- JWE encryption/decryption on every auth initiation and callback adds complexity
- State token size grows significantly (JWE overhead)
- Requires the Go backend to hold a signing/encryption key for state tokens in addition to existing JWT keys
- Medium implementation complexity compared to Option 2A

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
- No opinion about authentication strategy — purely a cookie sealing primitive, which is correct here because the Go backend owns the auth logic
- `BFF_SESSION_SECRET` (already wired in `src/server/config.ts`) maps directly to `iron-session`'s password option

**Cons:**
- Adds one npm dependency
- Sealed cookie format is `iron-session`-specific — if the library is abandoned or its format changes, existing sessions become invalid on upgrade

#### Option 3B: Custom `jose`-based JWE cookie

**Description:** Use the `jose` library (already used or referenced for JWT validation) to encrypt the session payload as a compact JWE (`A256GCMKW` + `A256GCM`) and store it as a cookie value. Implement seal/unseal helpers in `src/server/session.ts`.

**Pros:**
- Full control over encryption algorithm and payload shape
- `jose` uses Web Crypto API — Edge Runtime compatible
- No dependency on a session-specific library; the `jose` library may already be present

**Cons:**
- More implementation code to write and maintain (`src/server/session.ts` must implement key derivation, encryption, decryption, and cookie lifecycle)
- JWE compact serialization is larger than `iron-session`'s sealed format, increasing cookie size
- Cookie key rotation and versioning must be implemented manually

#### Option 3C: Auth.js (NextAuth.js v5)

**Description:** Use Auth.js, the leading Next.js authentication framework. It supports OIDC providers natively, manages sessions, and provides middleware helpers.

**Pros:**
- Comprehensive solution covering provider configuration, callbacks, session management, and middleware
- Large ecosystem and active maintenance

**Cons:**
- Auth.js is opinionated about owning the OIDC flow — it expects to talk to the OIDC provider directly (Option 1B or 1C)
- Conflicts with the decision that the Go backend owns the OIDC flow: Auth.js cannot be used as a pure session layer while delegating all auth logic to the Go backend
- Introduces significant framework concepts (providers, adapters, callbacks) that are unnecessary when the BFF is a thin proxy
- Risk of future coupling between Auth.js OIDC assumptions and Go backend behaviour

---

## Decision

**Q1 — Flow ownership:** Option 1A — The Go backend owns the full Authorization Code Flow with PKCE.

**Q2 — PKCE state storage:** Option 2A — Redis in the Go backend, keyed by opaque `state` value, TTL 15 minutes.

**Q3 — Session library:** Option 3A — `iron-session` v8+.

### Rationale

**Q1:** Option 1A is the only choice consistent with the existing architectural principle that the Go identity service is the single authoritative owner of all OIDC, session, and credential operations. It minimises the secret surface of the Next.js process (no OIDC client credentials in the BFF), ensures the PKCE verifier never crosses a process boundary, and means future OIDC provider changes require no frontend changes. Options 1B and 1C both split identity logic across two services — a maintenance and security liability.

**Q2:** Option 2A is the natural complement to Q1. Since the Go backend owns the flow, it also owns all intermediate auth state. Redis is already present and operated by the identity service. The atomic read-delete on callback prevents state replay attacks. Option 2B requires the BFF to hold PKCE material, which conflicts with Q1. Option 2C is stateless but adds JWE complexity for no operational benefit given Redis is already available.

**Q3:** `iron-session` v8 is the minimal, purpose-built solution for exactly what is needed: a sealed, tamper-evident session cookie backed by the Web Crypto API. It is Edge Runtime compatible, has no opinion about authentication strategy (correct here, since the Go backend owns auth), and maps directly onto the existing `BFF_SESSION_SECRET` env var. Auth.js (Option 3C) is disqualified because it is designed to own the OIDC flow, which conflicts with Q1. A custom `jose` implementation (Option 3B) is viable but adds unnecessary maintenance overhead when `iron-session` solves the problem with a proven, audited implementation.

---

## Consequences

### Positive

- All OAuth tokens stay server-side — the browser never receives `access_token`, `refresh_token`, or `id_token` at any point
- PKCE verifier is generated and validated in a single Go process — no cross-process exposure
- Future OIDC provider changes (Entra, Okta, etc.) require updates only in the Go backend; the Next.js BFF is provider-agnostic
- The BFF session cookie is self-contained — horizontal scaling of the Next.js layer requires no sticky sessions or shared session store
- Middleware operates entirely within the Edge Runtime constraint — no Node.js built-in dependencies

### Negative

- Two new Go backend endpoints must be implemented before any frontend auth code can be end-to-end tested: `POST /identity/login` and the `authorization_code` grant type in `POST /identity/session`
- Redis availability is on the critical path for auth initiation and callback — a Redis outage during the PKCE round-trip will cause login to fail
- `iron-session`'s sealed cookie format is library-specific; a major version bump that changes the format will invalidate all active sessions and require a coordinated deployment

### Neutral

- The Go identity service gains two new endpoints, consistent with its existing role as the identity authority
- `iron-session` is added to the frontend `package.json` as a direct dependency
- `config/dex/config.yaml` requires a new `redirectURIs` entry for local development (the `orbit-local` static client must register `http://localhost:3000/api/auth/callback`)

---

## Implementation

### New Go Backend Endpoints

**`POST /identity/login`**
- Request body: `{ "redirect_uri": "<callback URL>" }`
- Generates a PKCE code pair: verifier is 32–96 random bytes (RFC 7636 §4.1), base64url-encoded; challenge is `BASE64URL(SHA-256(verifier))`
- Generates an opaque `state` token: 32 bytes from `crypto/rand`, base64url-encoded (256-bit entropy)
- Stores `{ "verifier": "<base64url PKCE verifier>" }` in Redis keyed by `state`, TTL 15 minutes
- The `redirect_uri` is config-driven; the value in the request body must match the backend's configured value exactly
- Returns: `{ "authorization_url": "<fully-constructed provider /authorize URL>" }`

**`authorization_code` grant type in `POST /identity/session`**
- Request body: `{ "grant_type": "authorization_code", "code": "<auth code>", "state": "<opaque state>" }`
- Looks up `state` in Redis to retrieve the PKCE verifier; missing or expired state → `400`
- The `redirect_uri` used in the token exchange is config-driven — not accepted from the caller
- Calls the OIDC provider's token endpoint with `code` + `code_verifier`
- Deletes the Redis `state` key after a single successful exchange (prevents replay)
- Issues an Orbit platform JWT and returns the standard `SessionResponse` shape: `{ access_token, refresh_token, expires_in, issued_at }`

### New Frontend Files

| File | Purpose |
|------|---------|
| `middleware.ts` | Edge-compatible session check on every request; redirects to `/login?next=<path>` if no valid session; performs inline token refresh if within expiry threshold |
| `src/server/session.ts` | `iron-session` seal/unseal helpers; defines `SessionPayload` type; never imported from client components |
| `src/server/auth-config.ts` | `iron-session` configuration (`cookieName`, `password`, `cookieOptions`) |
| `src/app/login/page.tsx` | Login page with a single "Sign in" button that POSTs to `/api/auth/login` |
| `src/app/api/auth/login/route.ts` | Calls `POST /identity/login`, stores `next` in a short-lived HttpOnly cookie, returns 302 to authorization URL |
| `src/app/api/auth/callback/route.ts` | Validates `state`, calls `POST /identity/session` with `authorization_code` grant, seals session cookie, clears `next` cookie, redirects to `next` or `/` |
| `src/app/api/auth/session/route.ts` | Returns non-sensitive session data (`user`, `expiresAt`) for client components; returns 401 if no session |
| `src/app/api/auth/logout/route.ts` | Calls `POST /identity/revoke`, clears session cookie, redirects to `/login` |

### `next` Parameter Handling

The `next` destination is stored in a short-lived, separate HttpOnly cookie by the BFF before the OIDC redirect — it is not stored in Redis and is not part of the session payload. On callback, the BFF reads, validates, and clears this cookie. Validation rule: `next` must be a relative URL whose pathname is present in `APP_ROUTES`; any absolute URL, URL with a host component, or unrecognised path is silently discarded and the default `/` is used.

### Session Cookie Attributes

```
HttpOnly; Secure; SameSite=Lax; Path=/
```

`Secure` is conditional on `NODE_ENV=production` to allow `http://localhost` in local development. `SameSite=Lax` provides CSRF protection for navigation requests; CSRF on `POST /api/auth/logout` is accepted risk for this phase (forcing a sign-out has no data exfiltration consequence).

### Token Refresh

Middleware checks `expiresAt` from the session payload. If the access token is within a configurable threshold of expiry (default: 5 minutes), middleware calls `POST /identity/session` with `grant_type=refresh_token` before forwarding the request, then updates the session cookie with the new tokens. This adds one backend round-trip per refresh event — acceptable for a low-traffic control plane.

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `BFF_SESSION_SECRET` | Minimum 32-character secret for `iron-session` cookie sealing | Yes |
| `IDENTITY_SERVICE_URL` | Base URL of the Go identity service | Yes |

Both variables are already defined in `src/server/config.ts`. No new env vars are required.

### Local Development Config Change

Add the following to `config/dex/config.yaml` under the `orbit-local` static client:

```yaml
redirectURIs:
  - 'http://localhost:3000/api/auth/callback'
```

This change ships alongside the Phase 1 implementation.

### Migration Path

There is no existing browser session to migrate. The Device Authorization Grant is unaffected — the Go backend continues to handle it independently. The new `POST /identity/login` endpoint and `authorization_code` grant type are additive.

---

## Validation

### Success Metrics

- A user visiting any protected route without a session cookie is redirected to `/login`
- A user completing the full OIDC login flow (local provider: `dev@orbit.local` / `password`) lands on their original destination
- The browser's DevTools network panel shows no `access_token` or `refresh_token` values in any response body, cookie, or `Location` header at any point in the flow
- `POST /api/auth/logout` results in a cleared session cookie and a backend `POST /identity/revoke` call, confirmed via Go backend logs
- An expired session (simulated by advancing `expiresAt` in the sealed cookie) redirects to `/login` on the next request
- `middleware.ts` passes the Next.js Edge Runtime compatibility check (`next build` with no Edge Runtime warnings)

### Monitoring

- Structured log entries for all auth transitions (login initiation, callback success/failure, token refresh, logout) — no token values in any log field
- Redis key TTL and access patterns observable via existing Redis monitoring
- Go backend spans for `POST /identity/login` and `authorization_code` grant type emitted to the OpenTelemetry pipeline

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — Unified identity model; defines the `Principal`, session, and JWT issuance model that the browser flow builds on
- [ADR-0017](0017-oidc-provider-abstraction-and-local-identity-provider.md) — Introduced Device Authorization Grant and explicitly deferred Authorization Code Flow; this ADR delivers that deferred goal
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

The PKCE state TTL of 15 minutes reflects a conservative upper bound on realistic login interaction time. If a backend constant is introduced for this value, it should be referenced here and in the `POST /identity/login` implementation.

**Last Updated:** 2026-03-25
