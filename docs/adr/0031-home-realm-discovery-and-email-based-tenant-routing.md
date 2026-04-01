# 0031. Home Realm Discovery and Email-Based Tenant Routing

**Status:** Accepted
**Date:** 2026-03-27
**Deciders:** Engineering Team
**Technical Story:** PR #17 (`feat/browser-auth-flow`) hardcoded `tenant_id: 'default'` and a singleton `UserManager` initialised from build-time env vars — both assumptions are incompatible with the multi-tenant OIDC registry introduced in ADR-0026.

---

## Context

ADR-0030 (Browser-Based Authentication Flow and BFF Session Architecture) defines the mechanics of the PKCE browser login flow: a browser-side `oidc-client-ts` `UserManager` initiates the Authorization Code Flow, the OIDC provider returns an `id_token`, and the BFF exchanges it for an Orbit platform session via `POST /identity/sessions` with `grant_type: oidc_id_token`.

ADR-0026 introduced a multi-tenant OIDC registry: each tenant has its own OIDC issuer URL, client ID, and set of scopes. The identity service validates that the `id_token` issuer matches the OIDC provider registered for the tenant in the registry.

These two ADRs together create a gap: **the frontend must know which tenant to authenticate against before auth has happened**. This is the tenant routing problem. It must be solved at two moments in the flow:

1. **At login initiation** — to construct the `UserManager` with the correct `authority` (issuer URL), `client_id`, and `scope`.
2. **At the BFF session POST** — to pass `tenant_id` to `POST /identity/sessions` so the identity service can route to the correct OIDC provider for `id_token` validation.

At both moments there is no session, no cookie, and no previously verified user identity.

### Background

The initial implementation in PR #17 worked around this problem by:
- Baking `ORBIT_OIDC_ISSUER_URL` and `ORBIT_OIDC_CLIENT_ID` into `next.config.ts` as build-time env vars — making the frontend single-tenant by construction
- Hardcoding `tenant_id: 'default'` in the BFF session route

Both workarounds are fundamentally incompatible with the multi-tenant architecture. The same frontend build must be able to route any user to the correct tenant, and the same running instance must serve users from different tenants.

### Problem Statement

Design a mechanism by which the frontend can determine which tenant a user belongs to — and therefore which OIDC provider to use — before any authentication has occurred, without requiring the user to know or enter a tenant ID.

### Goals

- Any user can log in from the same frontend origin regardless of which tenant they belong to
- Users never need to know or type a tenant ID — they only need their email address
- The OIDC `UserManager` is constructed with per-tenant issuer URL, client ID, and scopes at login time
- The correct `tenant_id` is passed to `POST /identity/sessions` at callback time
- The solution is compatible with the multi-tenant OIDC registry introduced in ADR-0026
- Local development works without modification — the `default` tenant in `config/tenants.yaml` registers `orbit.local` as its email domain

### Non-Goals

- Subdomain-per-tenant routing (considered, deferred — see Options)
- Path-based tenant routing (e.g. `/t/acme/login`)
- Self-service tenant registration
- Mapping users to tenants based on anything other than email domain (e.g. invitation codes, organisation IDs)
- Admin UI for managing email domain assignments

---

## Decision Drivers

* **No tenant ID input from users**: B2B SaaS users should not need to know their tenant ID. Email address is the only identifier a user reliably knows.
* **Multi-tenant from a single origin**: The frontend serves all tenants from the same URL. Build-time baked config is ruled out.
* **ADR-0026 compatibility**: Tenant resolution must align with the OIDC registry — the discovered tenant must be the same tenant the identity service will validate against.
* **Security**: The discovery endpoint exposes only public OIDC configuration (issuer URL, client ID). It must never expose client secrets or internal tenant state.
* **PKCE state threading**: Once a tenant is discovered, its ID must be preserved through the OIDC redirect round-trip so the callback handler can pass it to the BFF session POST.
* **Local dev zero-config**: `make dev-up` + `make dev` must continue to work without extra setup.

---

## Considered Options

### Option A: Subdomain per tenant

**Description:** Each tenant gets its own subdomain (`acme.orbit.io`). Next.js middleware reads the `host` header, extracts the subdomain, and uses it as the tenant identifier throughout the flow.

**Pros:**
- Unambiguous — tenant is known at every point in the flow from the first request
- Works with SSO deep links and bookmarks
- Clean URLs; standard practice for enterprise SaaS (Linear, Notion, Slack workspaces)
- No user input required

**Cons:**
- Requires wildcard DNS and wildcard TLS certificate
- Local development requires per-tenant `/etc/hosts` entries or a local reverse proxy
- Adds infrastructure complexity before the product has tenants to serve
- New tenant requires DNS provisioning, which may be outside the operator's control in self-hosted deployments

### Option B: Path-based tenant

**Description:** Tenant is embedded in the URL path: `orbit.io/t/acme/login`. Extracted from the URL segment by Next.js middleware.

**Pros:**
- Works on any domain with no DNS changes
- Straightforward to implement in Next.js middleware

**Cons:**
- Users must know their tenant slug — does not eliminate the need to know a tenant identifier
- The tenant segment must be threaded through the PKCE `state` parameter to survive the IdP redirect round-trip

### Option C: Query parameter

**Description:** Tenant is passed as a URL query parameter: `orbit.io/login?tenant=acme`. The login page reads `searchParams.tenant` and uses it to look up tenant config.

**Pros:**
- Trivial to implement
- Useful as a deep-link generation mechanism for platform-generated login URLs

**Cons:**
- Users must know their tenant slug
- Lost if the user navigates directly to `/login` without the parameter
- Does not scale to a product where users expect to sign in from a plain URL

### Option D: Email-based Home Realm Discovery (HRD)

**Description:** The login page presents an email input as its first step. On submit, the BFF calls `GET /identity/tenants/discover?email=user@acme.com`. The identity service extracts the email domain, scans the in-memory tenant registry for a tenant whose `emailDomains` list includes that domain, and returns the tenant's public OIDC configuration (tenant ID, issuer URL, client ID, scopes). The frontend constructs the `UserManager` from this response and initiates the PKCE redirect, encoding `tenant_id` in the OIDC `state` parameter. The callback handler reads `tenant_id` from state and passes it to `POST /identity/sessions`.

This is the pattern used by Slack ("find your workspace"), Okta ("Sign in with your company email"), and Auth0 Home Realm Discovery.

**Pros:**
- Users need only their email address — no tenant slug, no subdomain, no URL parameter
- Works from a single origin with no DNS or infrastructure changes
- `tenant_id` travels through the PKCE `state` parameter — it survives the IdP round-trip without any server-side state store
- The discovery endpoint is read-only and stateless — it queries the in-memory registry, adds no database load
- Compatible with the existing multi-tenant OIDC registry in ADR-0026 — `emailDomains` is a natural addition to `TenantOIDCConfig`
- The factory-function pattern for `UserManager` (replacing the singleton) follows naturally from this design

**Cons:**
- Adds one backend endpoint and one frontend BFF proxy route
- Adds one extra round-trip before auth starts (email submit → discover → PKCE redirect)
- Requires `emailDomains` to be configured for each tenant in `config/tenants.yaml` — operators must list the email domains their users will sign in with
- Email domain is a coarse-grained signal — does not prove the user is a member of the tenant (the OIDC `id_token` provides this proof)

---

## Decision

**Chosen Option:** Option D — Email-based Home Realm Discovery.

### Rationale

Options A, B, and C all require users to have prior knowledge of a tenant identifier. In a B2B product, this is an unacceptable UX constraint: users know their email address, not the name their organisation was registered under. Option D eliminates this constraint entirely.

Subdomain routing (Option A) is the right long-term URL strategy for an enterprise product and is not foreclosed by this decision — it is listed as a non-goal for now because it requires infrastructure changes (wildcard DNS, wildcard TLS) that are not justified before the product has paying tenants. When subdomain routing is introduced, the HRD flow can be bypassed: the subdomain unambiguously identifies the tenant, making the email discovery step unnecessary. The two mechanisms are complementary, not competing.

The security properties of Option D are sound: the discovery endpoint returns only public OIDC configuration — issuer URL and client ID — which are not secrets (they appear in every PKCE authorization URL). Email domain matching is not an access control decision; it is a routing hint. The authoritative access control decision is made by the OIDC provider when it issues the `id_token`, and by the identity service when it validates the `id_token` against the tenant's registered issuer and applies `claimBindings` per ADR-0026.

The `tenant_id` in PKCE `state` eliminates any server-side state requirement for the discovery round-trip. `oidc-client-ts` passes the `state` object through the provider redirect and makes it available to the callback handler as `user.state` — no Redis, no cookie, no session.

---

## Consequences

### Positive

- Users log in with only their email address — no organisational knowledge required
- The `oidc-client-ts` singleton is replaced by a factory, making the OIDC client properly multi-tenant
- Build-time baked env vars (`ORBIT_OIDC_ISSUER_URL`, `ORBIT_OIDC_CLIENT_ID`) are removed from `next.config.ts` — the frontend is no longer single-tenant by construction
- The hardcoded `tenant_id: 'default'` in the BFF session route is replaced by a dynamically threaded value
- The discovery endpoint is a natural fit for future subdomain routing: middleware can call it with the subdomain as a tenant hint rather than an email, bypassing the email input step entirely

### Negative

- An additional round-trip (email submit → HRD → PKCE redirect) adds latency to the login initiation step — acceptable for a human-interactive flow
- Operators must configure `emailDomains` for each tenant in `config/tenants.yaml`; misconfiguration (missing domain) means users from that domain see a "not found" error rather than a login redirect
- Email domain is not a cryptographic proof of tenant membership — a user who knows another tenant's email domain could attempt to initiate auth against that tenant's IdP (the OIDC provider will reject them if they have no account there)

### Neutral

- `TenantOIDCConfig` gains an `EmailDomains []string` field in the tenant registry schema
- `frontend/src/lib/oidc-client.ts` is rewritten from a lazy singleton to a `buildUserManager(issuerUrl, clientId, scopes)` factory
- `frontend/src/app/(auth)/login/page.tsx` becomes a two-step component: email input, then SSO redirect
- `frontend/next.config.ts` removes the `ORBIT_OIDC_ISSUER_URL` and `ORBIT_OIDC_CLIENT_ID` env declarations

---

## Implementation

### Backend Changes

#### 1. Schema: add `emailDomains` to `TenantOIDCConfig`

In `backend/internal/identity/tenant_registry.go`, add `EmailDomains []string` to `TenantOIDCConfig`:

```go
type TenantOIDCConfig struct {
    TenantID     string   `yaml:"id"`
    EmailDomains []string `yaml:"emailDomains"`
    IssuerURL    string   `yaml:"issuerURL"`
    ClientID     string   `yaml:"clientID"`
    Scopes       []string `yaml:"scopes"`
    // ... existing fields
}
```

The `config/tenants.yaml` schema becomes:

```yaml
version: "v1"
tenants:
  - id: default
    emailDomains: ["orbit.local"]
    oidc:
      issuerURL: "http://dex.orbit.local:5556/dex"
      clientID: "orbit-local"
      scopes: ["openid", "email", "profile"]
```

#### 2. Service method: `DiscoverTenantByEmail`

```go
type TenantDiscovery struct {
    TenantID  string   `json:"tenant_id"`
    IssuerURL string   `json:"issuer_url"`
    ClientID  string   `json:"client_id"`
    Scopes    []string `json:"scopes"`
}

func (s *Service) DiscoverTenantByEmail(ctx context.Context, email string) (*TenantDiscovery, error)
```

- Extracts the domain part from the email address (the substring after `@`)
- Scans the in-memory tenant registry for a tenant whose `EmailDomains` slice contains the domain (case-insensitive)
- Returns `TenantDiscovery` with the tenant's public OIDC configuration
- Returns `ErrTenantNotFound` if no match — the handler returns HTTP 404

The raw email is never logged. Only the extracted domain may appear in structured log fields at debug level.

#### 3. New endpoint: `GET /identity/tenants/discover`

```
GET /identity/tenants/discover?email=user@acme.com
```

- **Auth required:** No — this endpoint is public. It runs before any session exists.
- **Response (200):**
  ```json
  {
    "tenant_id": "acme",
    "issuer_url": "https://login.acme.com",
    "client_id": "orbit-prod",
    "scopes": ["openid", "email", "profile"]
  }
  ```
- **Response (404):** `{ "error": "tenant_not_found" }` — no tenant registered for this email domain.
- **Response (400):** `{ "error": "invalid_email" }` — malformed input (no `@`, no domain part).
- The response never includes `client_secret`, `claimBindings`, or any other internal tenant configuration.

### Frontend Changes

#### 4. New BFF proxy route: `GET /api/auth/discover`

A Next.js API route that proxies to `GET /identity/tenants/discover?email=`. Avoids exposing the backend URL to the browser and keeps CORS out of the auth critical path.

```typescript
// src/app/api/auth/discover/route.ts
export async function GET(request: Request) {
  const { searchParams } = new URL(request.url)
  const email = searchParams.get('email')
  // validate, proxy to backend, return TenantDiscovery or error
}
```

#### 5. `oidc-client.ts` — replace singleton with factory

```typescript
// src/lib/oidc-client.ts
import { UserManager, UserManagerSettings } from 'oidc-client-ts'

export function buildUserManager(
  issuerUrl: string,
  clientId: string,
  scopes: string[],
): UserManager {
  const settings: UserManagerSettings = {
    authority: issuerUrl,
    client_id: clientId,
    redirect_uri: `${window.location.origin}/auth/callback`,
    scope: scopes.join(' '),
    response_type: 'code',
  }
  return new UserManager(settings)
}
```

No module-level singleton. Each login flow constructs a fresh `UserManager` from the discovered tenant config.

#### 6. Login page — two-step flow

**Step 1 — Email input:**
The login page renders an email input and a "Continue" button. On submit it calls `GET /api/auth/discover?email=` and stores the resolved `TenantDiscovery` in component state.

**Step 2 — SSO redirect:**
A "Sign in with SSO" button calls `buildUserManager(...)` then `signinRedirect()` with a structured `state` object:

```typescript
await userManager.signinRedirect({
  state: {
    tenantId: discovery.tenant_id,
    next: searchParams.get('next') ?? '/',
  },
})
```

The `state` object is serialised by `oidc-client-ts`, sent to the OIDC provider as the `state` parameter, and returned unchanged to the callback URL.

#### 7. Callback page — extract `tenant_id` from PKCE state

The callback page reads the discovered tenant config from `sessionStorage` (written before `signinRedirect()`), constructs a matching `UserManager`, and completes the callback:

```typescript
// src/app/auth/callback/page.tsx
const tenantConfig = JSON.parse(sessionStorage.getItem('orbit_tenant_config') ?? '{}')
const userManager = buildUserManager(tenantConfig.issuerUrl, tenantConfig.clientId, tenantConfig.scopes)
const user = await userManager.signinRedirectCallback()
const { tenantId, next } = user.state as { tenantId: string; next: string }

await fetch('/api/auth/session', {
  method: 'POST',
  body: JSON.stringify({
    id_token: user.id_token,
    tenant_id: tenantId,
  }),
})
```

`sessionStorage` is used because it is cleared on tab close, limiting the exposure window of the tenant config. If `sessionStorage` is unavailable or empty (tab restore, privacy mode), the callback page returns a 400-equivalent error and redirects to `/login`.

#### 8. Remove build-time OIDC env vars from `next.config.ts`

The `ORBIT_OIDC_ISSUER_URL` and `ORBIT_OIDC_CLIENT_ID` entries are removed from `next.config.ts`. These env vars no longer exist in the multi-tenant backend configuration and must not be baked into the build.

### Full Login Flow Sequence

```
Browser                  BFF (Next.js)              Backend
  |                           |                         |
  | enter email               |                         |
  | GET /api/auth/discover?email=                       |
  |-------------------------->|                         |
  |                           | GET /identity/tenants/discover?email=
  |                           |------------------------>|
  |                           |<-- { tenant_id, issuer_url, client_id, scopes }
  |<-- TenantDiscovery -------|                         |
  |                           |                         |
  | write sessionStorage      |                         |
  | signinRedirect(state={tenantId, next})              |
  |                      [IdP: Dex / Okta / etc]        |
  |<-- redirect /auth/callback?code=&state=             |
  |                           |                         |
  | read sessionStorage       |                         |
  | signinRedirectCallback()  |                         |
  | user.state.tenantId       |                         |
  |                           |                         |
  | POST /api/auth/session    |                         |
  |   { id_token, tenant_id } |                         |
  |-------------------------->|                         |
  |                           | POST /identity/sessions |
  |                           |   { grant_type: oidc_id_token, id_token, tenant_id }
  |                           |------------------------>|
  |                           |<-- { access_token, refresh_token, refresh_token_expires_in }
  |<-- Set-Cookie: orbit_session (sealed, HttpOnly)     |
  | redirect to 'next'        |                         |
```

### Migration Path

The existing single-tenant OIDC env vars (`ORBIT_OIDC_ISSUER_URL`, `ORBIT_OIDC_CLIENT_ID`) used by the PR #17 implementation are removed. Operators who referenced these vars must migrate to declaring their OIDC configuration in `config/tenants.yaml` (or the Kubernetes ConfigMap equivalent per ADR-0026) and setting `emailDomains` on their tenant record.

Local development requires adding `emailDomains: ["orbit.local"]` to the `default` tenant entry in `config/tenants.yaml`. The local Dex user (`dev@orbit.local`) will then resolve correctly through the discovery flow.

---

## Validation

### Success Metrics

- A user entering `dev@orbit.local` on the login page is redirected to the Dex OIDC provider and completes login successfully
- A user entering an email with an unknown domain receives a clear "organisation not found" error and is not redirected
- The browser's DevTools network panel shows no `client_secret` or internal tenant config in the `/api/auth/discover` response
- The `tenant_id` received by `POST /identity/sessions` matches the tenant registered for the user's email domain
- `next.config.ts` contains no references to `ORBIT_OIDC_ISSUER_URL` or `ORBIT_OIDC_CLIENT_ID`

### Monitoring

- Structured log entry on each discovery request: domain queried, tenant matched or not, duration — no raw email logged
- HTTP 404 rate on `/identity/tenants/discover` — a spike indicates users entering email addresses for unconfigured domains
- Go backend spans for discovery requests emitted to the OpenTelemetry pipeline

---

## Open Questions

1. **Rate limiting on the discovery endpoint** — The endpoint is public and unauthenticated. A high request rate could be used to enumerate which email domains have tenants registered. Rate limiting at the gateway (Istio, Envoy) is the correct mitigation but is not implemented in this ADR. Tracked as a future hardening item.

2. **Multiple tenants matching the same email domain** — The current design returns the first match. If two tenants register the same email domain (possible in a misconfigured deployment), the behaviour is undefined. The identity service should fail loudly at startup if duplicate email domains are detected across tenants.

3. **sessionStorage availability in the callback page** — If the browser clears sessionStorage between the PKCE redirect and the callback (privacy mode in some browsers, tab restore), the callback page cannot construct the `UserManager`. The fallback (encoding issuer URL and client ID in PKCE `state`) should be implemented defensively alongside the sessionStorage primary path.

---

## Related Decisions

- [ADR-0026](0026-tenant-registration-and-oidc-provider-binding.md) — Defines the multi-tenant OIDC registry that this ADR extends with `emailDomains`
- [ADR-0030](0030-browser-auth-flow-and-bff-session-architecture.md) — Defines the PKCE browser flow and BFF session architecture that this ADR plugs tenant routing into
- [ADR-0008](0008-unified-identity-model.md) — Foundational identity model; multi-tenancy requirement originated here

---

## References

- [Auth0 — Home Realm Discovery](https://auth0.com/docs/authenticate/login/auth0-universal-login/identifier-first)
- [Okta — Identity Engine sign-in with email](https://developer.okta.com/docs/guides/oie-embedded-sdk-use-case-basic-sign-in/nodejs/main/)
- [RFC 7636 — Proof Key for Code Exchange (PKCE)](https://datatracker.ietf.org/doc/html/rfc7636)
- [oidc-client-ts — state parameter handling](https://authts.github.io/oidc-client-ts/classes/UserManager.html#signinRedirect)

---

## Notes

The email domain is a routing hint, not an access control boundary. The OIDC provider is the authoritative source of truth for whether a user is permitted to authenticate against a tenant. The identity service's `claimBindings` (ADR-0026) provide a secondary enforcement layer. The discovery step only determines which door to knock on — the door itself decides whether to open.

**Last Updated:** 2026-03-27
