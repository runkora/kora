# PR #17 — Browser Authentication Flow & BFF Session Architecture: Review

**PR:** https://github.com/clear-route/orbit/pull/17  
**Author:** QjStout  
**Base branch:** `main` (which now includes PR #18 — `feat/multitenant-oidc`)  
**Reviewer:** Forge  
**Date:** 2026-06-14  

---

## Executive Summary

PR #17 implements a solid, well-architected browser OIDC flow backed by a BFF (Backend-for-Frontend) session pattern. The design decisions — PKCE in the browser, tokens sealed in an HttpOnly iron-session cookie, the `oidc_id_token` grant type on the backend — are correct and align with ADR-0023. The frontend work is largely production-quality.

**However, the PR has a hard merge-blocker:** it was written against the old single-tenant identity architecture. PR #18 (`feat/multitenant-oidc`) was merged first and fundamentally restructured the backend. The Go backend additions in this PR — `ExchangeIDToken`, `requireOIDC()`, `s.cfg.OIDC.ClientID`, `s.oidcProvider` — **will not compile** against the current `main`. Beyond compilation, the multi-tenant model introduces design mismatches that affect the frontend too.

This review separates critical blockers from significant issues and minor polish items, then provides concrete guidance on what needs to change.

---

## What This PR Implements

### Backend (Go)

| File | Change |
|------|--------|
| `backend/internal/identity/service.go` | Adds `ExchangeIDToken(ctx, SessionRequest)` method; `resolveDisplayName` helper; `AuthorizationEndpoint` field in `oidcProviderMeta`; fallback population of `AuthorizationEndpoint` in `discoverTenantOIDC` |
| `backend/internal/identity/handlers.go` | Adds `case "oidc_id_token"` to `handleSession`; adds `GET /health` route + `handleHealth`; adds `ErrIDTokenInvalid` case to `handleSessionError` |
| `backend/internal/identity/models.go` | Adds `IDToken string` field to `SessionRequest`; adds `ErrIDTokenInvalid` sentinel |
| `config/dex/config.yaml` | Adds `web.allowedOrigins: [http://localhost:3000]`; adds `redirectURIs: ["http://localhost:3000/auth/callback"]` to the local static client |

### Frontend (Next.js)

| Area | What was added |
|------|----------------|
| OIDC client | `src/lib/oidc-client.ts` — lazy `UserManager` singleton wrapping `oidc-client-ts` |
| Auth callback | `src/app/auth/callback/page.tsx` — Client Component completing PKCE, POSTing `id_token` to BFF |
| BFF session route | `src/app/api/auth/session/route.ts` — POST exchanges `id_token` with Go backend; GET returns non-sensitive session info |
| BFF logout route | `src/app/api/auth/logout/route.ts` — POST revokes tokens via Go backend then destroys iron-session |
| Login page | `src/app/(auth)/login/page.tsx` + `_components/sign-in-button.tsx` + `_components/already-signed-in.tsx` |
| Session infrastructure | `src/server/auth-config.ts` — `SessionPayload`, `SessionUser`, cookie config; `src/server/session.ts` — `getSession()`, `isSessionValid()`, `isSessionExpiringSoon()` |
| Config | `next.config.ts` — inlines `ORBIT_OIDC_ISSUER_URL` / `ORBIT_OIDC_CLIENT_ID` into browser bundle at build time |
| Routing | `src/lib/routing/routes.ts`, `src/lib/routing/api-routes.ts` — centralised route constants |
| Infrastructure | `tsconfig.json` path alias restructured to `@orbit/*`; Radix UI + `clsx` + `tailwind-merge` + `iron-session` + `oidc-client-ts` dependencies added |
| K8s | Frontend deployment, service, and gateway manifests in `k8s/frontend/`; Makefile targets for Docker build/push/deploy |

### Architecture Decision Record

`docs/adr/0023-browser-auth-flow-and-bff-session-architecture.md` — Status: Accepted. Covers the PKCE browser flow, BFF session pattern, iron-session cookie design, Edge Runtime middleware constraint, and the `oidc_id_token` grant type. Well written.

---

## Critical (Merge Blockers)

### C-1: `ExchangeIDToken` is written against the removed single-tenant API — will not compile

**The core problem.** PR #18 (already merged) eliminated the single-tenant OIDC model entirely:

- `s.cfg.OIDC` (`OIDCConfig` struct) — **removed**
- `s.oidcProvider` (`*gooidc.Provider` field on `Service`) — **removed**
- `requireOIDC()` method — **removed**

PR #17's `ExchangeIDToken` uses all three of these:

```go
// Will not compile on current main:
if err := s.requireOIDC(); err != nil { ... }           // method does not exist
idToken, err := s.oidcProvider.Verifier(&gooidc.Config{ // field does not exist
    ClientID: s.cfg.OIDC.ClientID,                      // field does not exist
}).Verify(ctx, req.IDToken)
```

**Required fix — rewrite `ExchangeIDToken` to use the multi-tenant `tenantOIDC` helper:**

```go
func (s *Service) ExchangeIDToken(ctx context.Context, req SessionRequest) (*SessionResponse, error) {
    ctx, span := s.tracer.Start(ctx, "identity.ExchangeIDToken",
        trace.WithAttributes(
            attribute.String("grant_type", "oidc_id_token"),
            attribute.String("tenant_id", req.TenantID),
        ),
    )
    defer span.End()

    ts, err := s.tenantOIDC(req.TenantID)
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, ErrOIDCNotConfigured.Error())
        return nil, fmt.Errorf("tenant OIDC state for %q: %w", req.TenantID, err)
    }

    idToken, err := ts.provider.Verifier(&gooidc.Config{
        ClientID: ts.cfg.ClientID,
    }).Verify(ctx, req.IDToken)
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, ErrIDTokenInvalid.Error())
        return nil, fmt.Errorf("%w: %w", ErrIDTokenInvalid, err)
    }

    var claims struct {
        Sub               string `json:"sub"`
        Email             string `json:"email"`
        Name              string `json:"name"`
        PreferredUsername string `json:"preferred_username"`
    }
    if err := idToken.Claims(&claims); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, "claims extraction failed")
        return nil, fmt.Errorf("extract id_token claims: %w", err)
    }

    span.SetAttributes(
        attribute.String("principal.id", claims.Sub),
        attribute.String("principal.type", string(PrincipalTypeHuman)),
        attribute.String("identity.source", string(IdentitySourceFederated)),
    )

    principal := Principal{
        ID:          claims.Sub,
        DisplayName: resolveDisplayName(claims.Name, claims.PreferredUsername, claims.Email, claims.Sub),
        Type:        PrincipalTypeHuman,
        Source:      IdentitySourceFederated,
        TenantID:    req.TenantID,
    }
    return s.createSession(ctx, principal)
}
```

---

### C-2: `discoverTenantOIDC` hard-rejects providers without `device_authorization_endpoint` — blocks web-only IdPs

Current `service.go` on `main` (lines 908–910):

```go
if meta.DeviceAuthorizationEndpoint == "" {
    return nil, fmt.Errorf("oidc provider for tenant %q does not advertise device_authorization_endpoint", tenantID)
}
```

This is a **startup-fatal error**. Any tenant configured with a provider that doesn't support the Device Authorization Grant (e.g. Google Workspace, Azure AD with certain configurations, Auth0 in some modes) will prevent the service from starting at all — even when that tenant only needs the browser PKCE flow that this PR introduces.

PR #17 adds `AuthorizationEndpoint` to `oidcProviderMeta` and populates it, but does not touch this validation gate. As the multi-tenant OIDC registry grows to include IdPs that support only the browser flow, this check will make them unregisterable.

**Required fix — demote `DeviceAuthorizationEndpoint` to a soft warning:**

```go
if meta.AuthorizationEndpoint == "" {
    // Every compliant OIDC provider must have an authorization_endpoint.
    return nil, fmt.Errorf("oidc provider for tenant %q does not advertise authorization_endpoint", tenantID)
}
if meta.DeviceAuthorizationEndpoint == "" {
    // Soft: device flow unsupported. Log but allow — tenant may be browser-only.
    s.logger.Info(ctx, "tenant OIDC provider does not support Device Authorization Grant — browser flow only",
        logging.Field{Key: "tenant_id", Value: tenantID},
        logging.Field{Key: "issuer", Value: cfg.IssuerURL},
    )
}
```

The device-flow handler can then return a clear per-request error when a device-flow request arrives for a browser-only tenant, rather than refusing to start the service.

---

### C-3: `fmt.Errorf("%w: %s", ErrIDTokenInvalid, err.Error())` violates the project error-wrapping convention

From the project conventions and the `noFmtErrorfStringErr` AST lint rule:

> **Wrap errors with `%w`**: Always use `fmt.Errorf("context: %w", err)` — never `%s` for error wrapping.

`make check` will fail on this line.

**Required fix:**

```go
// Wrong (current PR):
return nil, fmt.Errorf("%w: %s", ErrIDTokenInvalid, err.Error())

// Correct:
return nil, fmt.Errorf("%w: %w", ErrIDTokenInvalid, err)
```

---

### C-4: Frontend hardcodes `tenant_id: 'default'` — incompatible with multi-tenant model

`frontend/src/app/api/auth/session/route.ts`, POST handler:

```typescript
body: JSON.stringify({
    grant_type: 'oidc_id_token',
    id_token: idToken,
    tenant_id: 'default',   // ← hardcoded
}),
```

In the multi-tenant world (PR #18), `tenant_id` is a real routing key used by the backend to look up `s.oidcTenants[tenantID]`. Hardcoding `'default'` means:

1. The service will reject all requests unless a tenant named `'default'` exists in `config/tenants.yaml`.
2. There is no mechanism to support users from multiple tenants signing in via the browser.

**Required fix — propagate tenant ID through the OIDC `state` parameter (recommended approach):**

When initiating the OIDC redirect, include `tenantId` in the `state` object passed to `signinRedirect()`. After `signinRedirectCallback()` completes in the callback page, read `user.state.tenantId` and forward it to the BFF session endpoint.

```typescript
// sign-in-button.tsx — when initiating login
getUserManager().signinRedirect({
    state: { next: nextPath, tenantId: currentTenantId }
});

// callback/page.tsx — after signinRedirectCallback() resolves
const tenantId = (user.state as { tenantId?: string })?.tenantId ?? 'default';
// Forward tenantId to the BFF:
await fetch('/api/auth/session', {
    body: JSON.stringify({ id_token: user.id_token, tenant_id: tenantId })
});
```

The `state` object is round-tripped opaquely by the IdP, making it the correct vector for carrying application context through the auth redirect.

---

### C-5: `next.config.ts` bakes a single `ORBIT_OIDC_ISSUER_URL` / `ORBIT_OIDC_CLIENT_ID` into the browser bundle at build time

```typescript
env: {
    ORBIT_OIDC_ISSUER_URL: process.env.ORBIT_OIDC_ISSUER_URL ?? '',
    ORBIT_OIDC_CLIENT_ID:  process.env.ORBIT_OIDC_CLIENT_ID  ?? '',
},
```

In the multi-tenant model, there is no single OIDC issuer or client ID — each tenant has its own (`TenantOIDCConfig.IssuerURL`, `TenantOIDCConfig.ClientID`). Baking these at build time means:

1. You can only support one IdP per deployment, defeating multi-tenancy entirely.
2. Rotating or changing an IdP requires a full frontend rebuild and redeploy.
3. The env var names `ORBIT_OIDC_ISSUER_URL` / `ORBIT_OIDC_CLIENT_ID` no longer exist anywhere in the backend config (removed in PR #18) — there is no authoritative source for these values in production.

**Required fix — expose a runtime tenant OIDC config discovery endpoint on the backend:**

The backend already stores `ts.meta.AuthorizationEndpoint` and `ts.cfg.ClientID` per tenant in `tenantOIDCState`. Add a thin, unauthenticated handler:

```
GET /identity/providers/{tenantId}
→ 200 { "issuer_url": "...", "client_id": "...", "authorization_endpoint": "..." }
→ 404 { "error": "tenant_not_found" }
```

Client IDs and issuer URLs are public information in PKCE flows (there is no client secret). The frontend fetches this at runtime before constructing the `UserManager`, rather than reading build-time constants. Remove the `env` block from `next.config.ts` entirely.

---

## Significant Issues

### S-1: `SessionRequest` doc comment not updated for the new grant type

```go
// SessionRequest is the body for POST /identity/session.
// Supports two grant types:
//   - "urn:ietf:params:oauth:grant-type:device_code" — device flow poll
//   - "refresh_token" — refresh an existing human session
```

PR #17 adds a third grant type but the comment still says "two". The `IDToken` field also has no doc comment.

**Fix:**
```go
// SessionRequest is the body for POST /identity/session.
// Supports three grant types:
//   - "urn:ietf:params:oauth:grant-type:device_code" — device flow poll
//   - "refresh_token"                                — refresh an existing human session
//   - "oidc_id_token"                               — browser PKCE flow (BFF-mediated)
type SessionRequest struct {
    GrantType    string `json:"grant_type"`
    TenantID     string `json:"tenant_id"`
    DeviceCode   string `json:"device_code,omitempty"`
    RefreshToken string `json:"refresh_token,omitempty"`
    // IDToken is the OIDC id_token produced by browser PKCE completion.
    // Required when GrantType is "oidc_id_token".
    IDToken string `json:"id_token,omitempty"`
}
```

---

### S-2: `oidc-client.ts` is a single-tenant singleton — cannot serve multiple IdPs

```typescript
export function getUserManager(): UserManager {
    if (!_userManager) {
        _userManager = new UserManager({
            authority: process.env.ORBIT_OIDC_ISSUER_URL,
            client_id: process.env.ORBIT_OIDC_CLIENT_ID,
            ...
        });
    }
    return _userManager;
}
```

This singleton caches one `UserManager` for the lifetime of the browser tab. When a second tenant with a different IdP is onboarded, all users — regardless of tenant — are routed to the first IdP. This is a consequence of C-5 but requires its own fix: the function signature and internal cache must be keyed by tenant.

**Fix:** Refactor to a per-tenant factory that fetches provider config from the discovery endpoint described in C-5:

```typescript
const _managers: Map<string, UserManager> = new Map();

export async function getUserManagerForTenant(tenantId: string): Promise<UserManager> {
    if (!_managers.has(tenantId)) {
        const config = await fetchTenantOIDCConfig(tenantId); // calls GET /identity/providers/{tenantId}
        _managers.set(tenantId, new UserManager({
            authority:    config.issuer_url,
            client_id:    config.client_id,
            redirect_uri: `${window.location.origin}/auth/callback`,
            response_type: 'code',
            scope: 'openid profile email',
            automaticSilentRenew: false,
            loadUserInfo: false,
        }));
    }
    return _managers.get(tenantId)!;
}
```

---

### S-3: `decodeJwtPayload` does not validate payload shape after `JSON.parse`

```typescript
function decodeJwtPayload(token: string): OrbitJwtPayload {
    // ...
    return JSON.parse(json) as OrbitJwtPayload;
}
```

`JSON.parse` followed by an unchecked `as` cast returns an `any` at runtime. If the backend emits a malformed token, this silently produces `undefined` for fields the caller depends on — particularly `exp`, which drives cookie expiry calculation. The function should assert the minimum required shape:

```typescript
const payload = JSON.parse(json);
if (
    typeof payload !== 'object' ||
    payload === null ||
    typeof payload.sub !== 'string' ||
    typeof payload.exp !== 'number'
) {
    throw new Error('JWT payload is missing required claims (sub, exp)');
}
return payload as OrbitJwtPayload;
```

---

### S-4: Logout route swallows token revocation failures entirely

```typescript
await Promise.allSettled([
    revokeToken(session.refreshToken, 'refresh_token'),
    revokeToken(session.accessToken,  'access_token'),
]);
// session.destroy() always proceeds
```

`Promise.allSettled` is the right choice here (best-effort revocation before cookie destruction is correct per RFC 7009). However, rejected promises are completely discarded. If the backend is unreachable during logout, the user is logged out of the UI but both tokens remain valid on the server for their full TTL.

**Fix:** Log rejected revocations as server-side warnings:

```typescript
const results = await Promise.allSettled([
    revokeToken(session.refreshToken, 'refresh_token'),
    revokeToken(session.accessToken,  'access_token'),
]);
results.forEach((r, i) => {
    if (r.status === 'rejected') {
        console.warn(`[orbit/logout] token revocation ${i} failed:`, r.reason);
    }
});
```

This is server-side Next.js code — `console.warn` goes to the server log, not the browser console.

---

### S-5: `AuthorizationEndpoint` is stored but never read — dead code until C-5 is resolved

PR #17 adds `AuthorizationEndpoint string` to `oidcProviderMeta` and populates it from the discovery document, but no handler reads or returns it. It only becomes meaningful when a tenant OIDC config discovery endpoint (C-5) is implemented. Either implement that endpoint in this PR (preferred, since the field is pointless without it) or explicitly document in the PR description that this is infrastructure for a follow-up.

---

## Minor Issues

### M-1: Health handler returns no version or readiness signal

```go
api.JSON(w, http.StatusOK, map[string]string{
    "status":  "healthy",
    "service": "orbit-identity",
})
```

Acceptable for a basic liveness probe. Consider adding a `version` field (via build-time ldflags) and an `oidc_tenants_loaded` boolean so load balancer health checks can distinguish "process running" from "process running and fully initialised with at least one OIDC tenant".

---

### M-2: `iron-session` `maxAge` (7 days) is misaligned with backend `RefreshTokenTTLDays` (30 days default)

`auth-config.ts`:
```typescript
maxAge: 60 * 60 * 24 * 7, // 7 days
```

`config.go` backend default:
```go
cfg.Token.RefreshTokenTTLDays = 30
```

The iron-session cookie expires 7 days after login while the backend refresh token stays valid for 30 days. The user gets forcibly logged out of the UI while their refresh token is still valid on the server. Conversely, if the backend TTL is later reduced below 7 days, the cookie outlives the refresh token and every silent refresh attempt will fail until it naturally expires.

The right fix is not to pick a matching hardcoded constant — it is to have the backend tell the frontend exactly how long the refresh token is valid, and have the cookie mirror that exactly.

**Backend — add `refresh_token_expires_in` to `SessionResponse` in `models.go`:**

```go
type SessionResponse struct {
    AccessToken           string `json:"access_token"`
    TokenType             string `json:"token_type"`
    ExpiresIn             int    `json:"expires_in"`                         // access token TTL, seconds
    RefreshToken          string `json:"refresh_token,omitempty"`
    RefreshTokenExpiresIn int    `json:"refresh_token_expires_in,omitempty"` // refresh token TTL, seconds
    IssuedAt              int64  `json:"issued_at"`
}
```

Populate it in `buildAndPersistSession` where the refresh token TTL is already known:

```go
RefreshTokenExpiresIn: int(s.cfg.Token.RefreshTokenTTLDays * 24 * 60 * 60),
```

**Frontend — update `BackendSessionResponse` in `auth-config.ts` to include the new field:**

```typescript
export interface BackendSessionResponse {
    access_token:             string;
    token_type:               string;
    expires_in:               number;
    refresh_token:            string;
    refresh_token_expires_in: number; // add this
    issued_at:                number;
}
```

Then in the BFF session POST handler, replace the hardcoded `getSessionOptions()` `maxAge` with the value from the backend response:

```typescript
await session.save({
    ...getSessionOptions(),
    cookieOptions: {
        // Cookie lifetime tracks the refresh token exactly.
        // If the operator changes ORBIT_REFRESH_TOKEN_TTL_DAYS on the backend,
        // the cookie follows automatically — no second constant to keep in sync.
        maxAge: backendData.refresh_token_expires_in,
    },
});
```

---

### M-3: `already-signed-in.tsx` `setTimeout` has no cleanup

```typescript
useEffect(() => {
    setTimeout(() => router.replace('/'), REDIRECT_DELAY_MS);
}, [router]);
```

If the component unmounts before the timeout fires (e.g. user navigates away during the 3-second window), React will attempt a state update on an unmounted component and log a warning.

**Fix:**
```typescript
useEffect(() => {
    const timer = setTimeout(() => router.replace('/'), REDIRECT_DELAY_MS);
    return () => clearTimeout(timer);
}, [router]);
```

---

### M-4: `NEXT_PUBLIC_OIDC_REDIRECT_URI` in `.env.example` is misleading

`NEXT_PUBLIC_*` variables are statically inlined at `pnpm build` time. Listing this in `.env.example` implies it can be set at runtime (like a server-side env var), which it cannot. Since the redirect URI path (`/auth/callback`) is an application constant, not an operator-configurable value, it should either:

- Be hardcoded in `oidc-client.ts` as `` `${window.location.origin}/auth/callback` `` (the origin is always correct at runtime), or
- Be clearly documented in `.env.example` as a build-time variable that must be set before `pnpm build`, with a note that it defaults to the current origin.

---

## What the PR Does Well

- **Token-never-in-browser** is correctly implemented. `access_token` and `refresh_token` live only in the sealed HttpOnly iron-session cookie. The BFF GET endpoint deliberately excludes them.
- **PKCE S256** is correctly configured in `oidc-client.ts`. `automaticSilentRenew: false` and `loadUserInfo: false` are correct for the BFF model — the BFF handles refresh, not the client-side library.
- **Open redirect protection** via `isSafeRedirectPath` in the callback page — good defensive practice.
- **`isSessionExpiringSoon`** (5-minute threshold) and **`isSessionValid`** (30s clock drift tolerance) are well-considered session helpers.
- **`resolveDisplayName`** fallback chain (`name → preferred_username → email → sub`) is clean and RFC-aligned.
- **BFF route isolation** — `session/route.ts` and `logout/route.ts` are correctly marked `server-only` and stay tightly focused.
- **Centralised route constants** in `lib/routing/routes.ts` / `api-routes.ts` — no magic strings in components.
- **ADR-0023** is thorough and explains the trade-offs clearly. The Edge Runtime constraint for `middleware.ts`, the iron-session cookie rationale, and the PKCE flow are all well-documented.
- **Dex `allowedOrigins`** is the correct fix for cross-origin OIDC discovery — browsers require CORS on the discovery document and token endpoints when the IdP and app run on different origins.

---

## Files Reviewed

| File | Status |
|------|--------|
| `backend/internal/identity/service.go` | ❌ Critical — uses removed single-tenant API (`requireOIDC`, `s.oidcProvider`, `s.cfg.OIDC`) |
| `backend/internal/identity/handlers.go` | ⚠️ Depends on broken `ExchangeIDToken`; otherwise correct |
| `backend/internal/identity/models.go` | ⚠️ Minor — doc comment incomplete; `IDToken` field undocumented |
| `config/dex/config.yaml` | ✅ Correct |
| `frontend/src/app/api/auth/session/route.ts` | ❌ Critical — hardcoded `tenant_id: 'default'` |
| `frontend/src/app/api/auth/logout/route.ts` | ⚠️ Significant — revocation failures silently discarded |
| `frontend/src/app/auth/callback/page.tsx` | ✅ Good — needs tenant ID propagation (C-4) |
| `frontend/src/app/(auth)/login/page.tsx` | ✅ Good |
| `frontend/src/app/(auth)/login/_components/already-signed-in.tsx` | ⚠️ Minor — missing `clearTimeout` cleanup |
| `frontend/src/app/(auth)/login/_components/sign-in-button.tsx` | ✅ Good |
| `frontend/src/server/auth-config.ts` | ✅ Good |
| `frontend/src/server/session.ts` | ✅ Good |
| `frontend/src/lib/oidc-client.ts` | ❌ Critical — single-tenant singleton; build-time constants |
| `frontend/next.config.ts` | ❌ Critical — bakes single-tenant OIDC constants at build time |
| `docs/adr/0023-browser-auth-flow-and-bff-session-architecture.md` | ✅ Well written, Accepted |

---

## Merge Checklist

- [ ] **C-1** Rewrite `ExchangeIDToken` to use `s.tenantOIDC(req.TenantID)` instead of `s.oidcProvider` / `s.cfg.OIDC` / `s.requireOIDC()`
- [ ] **C-2** Demote `DeviceAuthorizationEndpoint` absence from startup-fatal error to a logged warning in `discoverTenantOIDC`; add hard validation for `AuthorizationEndpoint` instead
- [ ] **C-3** Fix `fmt.Errorf("%w: %s", ErrIDTokenInvalid, err.Error())` → `fmt.Errorf("%w: %w", ErrIDTokenInvalid, err)`
- [ ] **C-4** Replace hardcoded `tenant_id: 'default'` in BFF session route; propagate tenant ID through the OIDC `state` parameter
- [ ] **C-5** Remove build-time `ORBIT_OIDC_ISSUER_URL` / `ORBIT_OIDC_CLIENT_ID` from `next.config.ts`; add `GET /identity/providers/{tenantId}` endpoint on the backend; update `getUserManager` to fetch config dynamically
- [ ] **S-1** Update `SessionRequest` doc comment for three grant types; add `IDToken` field doc comment
- [ ] **S-2** Refactor `getUserManager()` singleton to a per-tenant factory keyed by `tenantId`
- [ ] **S-3** Add shape validation to `decodeJwtPayload` after `JSON.parse`
- [ ] **S-4** Log failed token revocations in the logout route instead of silently discarding them
- [ ] **S-5** Either implement `GET /identity/providers/{tenantId}` in this PR (using `AuthorizationEndpoint`) or remove the field and defer to a follow-up
- [ ] **M-2** Add `refresh_token_expires_in` to `SessionResponse` on the backend; derive iron-session cookie `maxAge` from it in the BFF instead of hardcoding 7 days
- [ ] **M-3** Add `clearTimeout` cleanup to `already-signed-in.tsx` `useEffect`
- [ ] **M-4** Clarify `.env.example` for `NEXT_PUBLIC_OIDC_REDIRECT_URI` or replace with a runtime origin-relative URI
- [ ] `make check` passes with zero issues after all Go changes
- [ ] `pnpm build` completes without TypeScript errors after all frontend changes
