# Personal Access Tokens for Inference

**Status:** Accepted  
**Date:** 2026-04-02
**Deciders:** Platform Engineering  
**Technical Story:** Enable developers to use kora inference from local coding agents (Claude Code, Cursor, Continue) without re-authenticating every 60 minutes.

---

## Context

### Background

kora's platform token model today has two classes of JWT:

| Token Class | PrincipalType | Issued Via | TTL | Refresh |
|---|---|---|---|---|
| Human session | `HUMAN` | OIDC device flow or id_token exchange | 60 min | Yes (30 days) |
| Agent session | `AGENT` | SPIFFE SVID attestation (`POST /exchange`) | 60 min | No |

The identity service is the sole RSA private key holder. All tokens are RS256-signed JWTs whose `KoraClaims` contain `SessionID`, `PrincipalType`, `TenantID`, `IdentitySource`, `DisplayName`, and a freeform `Claims map[string]string`. There is no scope or audience-restriction field in `KoraClaims` today.

The inference gateway (`backend/internal/inferencegw/`) already handles both human and agent tokens via a uniform middleware chain:

```
xAPIKeyMiddleware        (x-api-key → Authorization: Bearer)
  → AuthMiddleware       (verify JWT, store PlatformClaims in ctx)
  → handler              (extract principal, evaluate policy, route)
```

> Note: the middleware chain above reflects the current state pre-PAT. The PAT implementation adds `orbCredentialMiddleware` between `xAPIKeyMiddleware` and `AuthMiddleware`. No additional handler-level guard is added — principal-type enforcement at the inference gateway is deferred to the OPA policy layer ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)).

The `xAPIKeyMiddleware` step is particularly relevant: Claude Code and other Anthropic-SDK tools send their credential in the `x-api-key` header, which is normalised before verification. Any valid platform token already works in this header.

### Problem Statement

Developers running coding agents locally need a stable, longer-lived credential to point those agents at kora inference. Neither existing token class is suitable:

- Human session tokens expire after 60 minutes and require an interactive browser redirect to renew.
- Agent tokens require a SPIFFE/SPIRE attestation chain that only exists inside kora-managed clusters — it cannot be obtained on a developer laptop.

The result is that developers bypass kora and use raw provider API keys locally, which eliminates all usage attribution, cost visibility, and policy enforcement during the development phase.

### Goals

- Allow any authenticated human user to create a long-lived token that works at inference endpoints.
- Scope those tokens to inference only — they must not grant access to the main kora management API.
- Give users self-service lifecycle control: create, list, revoke.
- Preserve kora's security properties: only hashed token values at rest, JTI-based revocation, full usage attribution.

### Non-Goals

- Fine-grained scope selection per PAT (deferred to OPA integration, [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)).
- `last_used_at` tracking and rotation/re-roll (deferred to follow-on iteration).
- PAT management UI (API-first; UI is a follow-on).
- Admin-level cross-user PAT management.
- Non-human PATs (service accounts, agents).

---

## Decision Drivers

* **Minimal structural footprint** — The new token type should fit the existing JWT shape without adding new fields to `KoraClaims` that all token types must carry.
* **Enforcement co-location** — Scope restriction (inference-only) should be enforced at the enforcement boundary (inference gateway), not at the signing layer.
* **Consistent verification path** — PATs must verify through the same `token.Verifier` as all other platform tokens. No special-cased verification logic.
* **Security parity** — Raw token values never stored; SHA-256 hash only. JTI blocklist revocation. Identical signing key and algorithm.
* **Developer ergonomics** — A PAT must work with the `x-api-key` header that Claude Code and other Anthropic-SDK tools use out of the box. No SDK configuration beyond setting the base URL and key.

---

## Considered Options

### Option 1: New `PrincipalType` value — `PAT`

Add `PAT` to the existing `PrincipalType` enum in `backend/internal/platform/token/claims.go`. The token is otherwise structurally identical to existing platform JWTs. Inference-only enforcement is achieved by:

1. The inference gateway allowing `PAT`, `AGENT`, and `HUMAN` in a `requireInferencePrincipal` middleware.
2. The main kora API rejecting `PAT`-typed tokens at its auth layer with 403.

PATs are persisted in a new `user_pat_tokens` table (name, token_hash, expires_at, revoked_at). No refresh token; revoke-and-recreate is the recovery path.

**Pros:**
- Zero new fields in `KoraClaims` — no struct changes ripple to existing consumers.
- Enforcement is type-driven and follows the existing `PrincipalType` dispatch pattern already used throughout the stack (policy evaluator, usage records, OTel spans).
- PAT-originated inference calls attribute correctly to the issuing user with no additional plumbing.
- `PrincipalType=PAT` is self-documenting in usage records, logs, and traces.
- Fully composable with future scope fields — a `Scope` field can be added to `KoraClaims` later without breaking PAT-holding clients.

**Cons:**
- `PrincipalType` carries dual semantics: "what kind of identity" and (implicitly) "what scope". This is a mild conceptual overload, acceptable now, to be resolved cleanly when scope is added.

### Option 2: Add `Scope []string` to `KoraClaims`

Extend `KoraClaims` with a `Scope []string` field. PATs carry `scope: ["inference:*"]`. The inference gateway validates the scope; other APIs reject tokens without the required scope for their operation.

**Pros:**
- Explicit, extensible scope model from day one.
- Forward-compatible with fine-grained model/provider scoping.

**Cons:**
- Adds a new field to the shared `platform/token` claims struct that all existing token consumers must handle (even if they ignore it).
- Scope enforcement without a policy engine is effectively a static string check — it provides structure without real policy flexibility. This work is premature until OPA is integrated ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)).
- Scope strings are stringly-typed; mistakes are silent. A `PrincipalType` enum value is validated at compile time.
- The inference gateway's `AllowAllPolicyEvaluator` is still in place — adding a scope field without replacing it gives a false sense of enforcement depth.

### Option 3: Extended TTL on human session tokens

Allow `POST /session` to issue human session tokens with a configurable TTL (hours to days) via a new grant parameter.

**Pros:**
- No new token type; no new table.
- Minimal code change.

**Cons:**
- Long-lived human sessions carry OIDC session semantics (refresh token linkage, `identity_source=FEDERATED`) that are semantically wrong for a static developer credential.
- Cannot be scoped to inference-only without adding scope infrastructure to the entire session layer.
- Complicates session revocation: revoking a long-lived session that a developer has embedded in three tools has broader blast radius than revoking a named PAT.
- Human session refresh tokens are tied to the session row — a long-lived session creates a long-lived refresh window, undermining the rotation model.

### Option 4: Reuse `identity_sessions` table

Store PATs as rows in the existing `identity_sessions` table with `principal_type='PAT'`.

**Pros:**
- Revocation infrastructure (JTI blocklist, `revoked_at`) reused without new table.
- Fewer migrations.

**Cons:**
- The sessions table carries OIDC-session semantics (`identity_source`, refresh token FK) that are semantically wrong for PATs.
- No `name` column for user-facing management — would require adding a nullable column that applies to no other session type.
- PAT-specific queries (list by user, check hash for verification workflows) are muddled with session data.
- The refresh token FK and cascade-delete behaviour is an unexpected footgun for a token type that has no refresh lifecycle.

---

## Decision

**Chosen Option: Option 1 — New `PrincipalType` value `PAT` with a dedicated `user_pat_tokens` table.**

### Rationale

Option 1 is the minimal-footprint, idiomatic choice for this codebase. `PrincipalType` is already the central discriminator for token-type dispatch in every layer of the stack: the policy evaluator, usage records, OTel attribute emission, and session repository queries all key off it. Adding `PAT` slots cleanly into this established pattern.

The "dual semantics" concern from the cons list is a real but manageable trade-off. `PrincipalType` has always carried implicit scope meaning — `AGENT` tokens are expected to come from workloads, `HUMAN` tokens from interactive sessions. `PAT` is consistent with this convention. When OPA policy integration lands ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)), proper scope fields on `KoraClaims` will supersede the implicit convention, and `PrincipalType=PAT` will remain a useful identity signal alongside explicit scopes.

Option 2 is architecturally cleaner in the long run but is premature without the policy engine. Options 3 and 4 reuse existing constructs in ways that fight their semantics.

---

## Consequences

### Positive

- Developers get a practical, self-service credential for local inference tooling immediately.
- All PAT-originated inference calls are fully attributed to the issuing user and tenant — no dark usage.
- Security properties are unchanged: same signing key, same verification path, same JTI blocklist revocation.
- The `PrincipalType=PAT` discriminator is immediately useful in policy evaluation once OPA replaces `AllowAllPolicyEvaluator`.
- The `user_pat_tokens` table is purpose-built — clean schema, clean queries, no semantic baggage.

### Negative

- `PrincipalType` carries implicit scope meaning until OPA integration lands. Teams adding new API surfaces must remember to check for `PAT` and reject it if appropriate.
- One new database table and migration to maintain.

### Neutral

- PAT raw values are shown once and not stored, consistent with the `agent_tokens` and `identity_refresh_tokens` patterns. This is a deliberate UX trade-off (revoke-and-recreate on loss) that matches industry convention (GitHub PATs, Stripe API keys).
- PATs have no refresh lifecycle. This is intentional — they are credentials, not sessions.

---

## Implementation

### New `PrincipalType` Constant

```go
// backend/internal/platform/token/claims.go
const (
    PrincipalTypeHuman   PrincipalType = "HUMAN"
    PrincipalTypeAgent   PrincipalType = "AGENT"
    PrincipalTypeService PrincipalType = "SERVICE"
    PrincipalTypeSystem  PrincipalType = "SYSTEM"
    PrincipalTypePAT     PrincipalType = "PAT"   // ← new
)
```

No other changes to `KoraClaims` or `PlatformClaims`.

### Database Migration

New table in `backend/cmd/identity/migrations/`:

```sql
-- +goose Up
CREATE TABLE user_pat_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL,
    user_id     UUID        NOT NULL,        -- principal_id of the issuing human user
    name        TEXT        NOT NULL,        -- user-assigned label (1–128 chars)
    token_hash  TEXT        NOT NULL UNIQUE, -- SHA-256 hex of the raw JWT
    jti         TEXT        NOT NULL UNIQUE, -- JWT ID, used for JTI blocklist revocation
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX user_pat_tokens_user_id_idx    ON user_pat_tokens (user_id);
CREATE INDEX user_pat_tokens_tenant_id_idx  ON user_pat_tokens (tenant_id);

-- +goose Down
DROP TABLE user_pat_tokens;
```

### Identity Service — New Methods

Three new methods on `identity.Service`:

```go
// CreatePAT issues a new PAT for the authenticated user.
// Returns the raw JWT string (caller must present it to the user — it is not stored).
func (s *Service) CreatePAT(ctx context.Context, req CreatePATRequest) (CreatePATResponse, error)

// ListPATs returns all active (non-revoked, non-expired) PATs for the given user.
func (s *Service) ListPATs(ctx context.Context, tenantID, userID string) ([]PAT, error)

// RevokePAT revokes a PAT by ID, scoped to the owning user (returns ErrNotFound for other users' tokens).
func (s *Service) RevokePAT(ctx context.Context, tenantID, userID, patID string) error
```

`CreatePAT` internally calls `signToken` with `PrincipalType=PAT` and a configurable expiry (default 90 days, max 365 days). The `IdentitySource` is `PLATFORM`. The raw JWT is SHA-256 hashed; only the hash is persisted. The raw value is returned once in `CreatePATResponse.Token`.

### HTTP Handlers — New Routes

Three new routes registered on the identity service's controller:

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/identity/tokens` | Human session token | Create a new PAT |
| `GET` | `/api/v1/identity/tokens` | Human session token | List caller's active PATs |
| `DELETE` | `/api/v1/identity/tokens/{id}` | Human session token | Revoke a PAT by ID |

**Request / Response shapes:**

```
POST /api/v1/identity/tokens
{
  "name": "claude-code-local",         // required, 1–128 chars
  "expires_in_days": 90                // optional, 1–365, default 90
}

→ 201 Created
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "claude-code-local",
  "token": "eyJhbGci...",              // raw JWT — shown once
  "expires_at": "2025-04-25T10:00:00Z",
  "created_at": "2026-04-01T10:00:00Z"
}

GET /api/v1/identity/tokens
→ 200 OK
{
  "tokens": [
    {
      "id": "550e8400-...",
      "name": "claude-code-local",
      "expires_at": "2025-04-25T10:00:00Z",
      "created_at": "2026-04-01T10:00:00Z"
    }
  ]
}
// NOTE: raw token value never returned after creation

DELETE /api/v1/identity/tokens/{id}
→ 204 No Content
```

**Guard:** All three routes require the caller's token to have `PrincipalType != PAT`. PATs cannot be used to manage other PATs (preventing escalation via a compromised PAT).

### Token Format

PAT raw values are returned as `orb_<jwt>` — the raw RS256-signed JWT prefixed with `orb_`. The prefix serves two purposes: secret scanning tools can detect leaked PATs by pattern, and developers can identify the credential type at a glance in `.env` files and shell history.

### Prefix Stripping Middleware

The `orb_` prefix is stripped by a dedicated `orbCredentialMiddleware` in `backend/internal/inferencegw/middleware.go`. This is separate from `xAPIKeyMiddleware` — each middleware has a single responsibility and they are composed together in the inference gateway's middleware chain:

```
xAPIKeyMiddleware          (x-api-key → Authorization: Bearer)
  → orbCredentialMiddleware (strips orb_ prefix from Authorization header value)
  → AuthMiddleware          (verifies JWT, stores claims in context)
  → handler
```

`orbCredentialMiddleware` operates solely on the `Authorization` header, stripping the `orb_` prefix from the bearer value if present:

```go
// orbCredentialMiddleware strips the orb_ prefix from the Authorization header
// bearer value. This allows PATs (returned as orb_<jwt>) to be used directly
// as x-api-key values or Authorization: Bearer credentials without client-side
// prefix handling.
func orbCredentialMiddleware() mw.Middleware
```

Separating the concerns means:
- `xAPIKeyMiddleware` stays focused on header promotion and can be used on any route that needs it.
- `orbCredentialMiddleware` can be composed independently on any route that accepts `orb_`-prefixed credentials.
- Both cases are handled — `x-api-key: orb_<jwt>` (via xAPIKey then orbCredential) and `Authorization: Bearer orb_<jwt>` (via orbCredential alone).

### Inference Gateway — No New Principal Guard

No new principal-type guard middleware is added to the inference gateway. Any token that passes `AuthMiddleware` (valid JWT signature, not expired, not on the JTI blocklist) is permitted to reach inference handlers regardless of `PrincipalType`.

Fine-grained enforcement — which principal types may call which providers or models — is the responsibility of the policy layer ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)). Adding a static allowlist middleware now would duplicate logic that the policy engine will own and would need to be removed or reconciled when OPA integration lands. The `PrincipalType=PAT` discriminator is already available in the request context for the policy engine to key off.

The only enforcement boundary added in this ADR is at the main kora API (see below), which prevents PATs from accessing management endpoints entirely.

### Main kora API — PAT Rejection

The main API (`cmd/orbit` or equivalent) adds a check after `AuthMiddleware` verifies the token. If `claims.Kora.PrincipalType == token.PrincipalTypePAT`, the request is rejected with 403 before reaching any handler. This can be implemented as a thin middleware applied to the main API's route group, or as a guard inside the auth middleware itself keyed on configuration.

The preferred approach is a thin middleware (symmetric with the inference gateway pattern), so rejection logic is co-located with the service boundary it protects.

### `TokenConfig` — New TTL Field

```go
// backend/internal/identity/config.go
type TokenConfig struct {
    AccessTokenTTLMinutes   int    // existing
    RefreshTokenTTLDays     int    // existing
    PATDefaultTTLDays       int    // new — default 90
    PATMaxTTLDays           int    // new — default 365
    Audience                string // existing
}
```

Env var overrides: `ORBIT_PAT_DEFAULT_TTL_DAYS`, `ORBIT_PAT_MAX_TTL_DAYS`.

### Repository Interface

```go
// backend/internal/identity/ (new file: pat_repository.go)

type PAT struct {
    ID        string
    TenantID  string
    UserID    string
    Name      string
    JTI       string
    ExpiresAt time.Time
    CreatedAt time.Time
    RevokedAt *time.Time
}

type CreatePATRequest struct {
    TenantID  string
    UserID    string
    Name      string
    JTI       string
    TokenHash string
    ExpiresAt time.Time
}

type PATRepository interface {
    Create(ctx context.Context, req CreatePATRequest) (PAT, error)
    ListByUser(ctx context.Context, tenantID, userID string) ([]PAT, error)
    GetByID(ctx context.Context, tenantID, userID, id string) (PAT, error)
    Revoke(ctx context.Context, tenantID, userID, id string) error
}
```

### Revocation Flow

PAT revocation (`DELETE /api/v1/identity/tokens/{id}`) follows the same pattern as session revocation:

1. Load the PAT row (scoped to `user_id` — returns `ErrNotFound` for other users' tokens).
2. Set `revoked_at = now()` in `user_pat_tokens`.
3. Add the PAT's `jti` to the Redis JTI blocklist with TTL = `expires_at - now()`.

On the next inference request with the revoked PAT, `AuthMiddleware` → `verifier.Verify` checks the JTI blocklist (existing path) and returns 401 before the request reaches any handler.

### Migration Path

No migration of existing tokens is required. The change is purely additive:
- New `PrincipalType` constant (backward-compatible enum extension).
- New database table (no changes to existing tables).
- New HTTP routes (no changes to existing routes).
- New inference gateway middleware (applied to existing route group with no behaviour change for existing token types).

Existing `HUMAN` and `AGENT` tokens continue to work at all endpoints they currently work at. The main API PAT-rejection middleware has no effect on any existing token type.

---

## Validation

### Success Metrics

- PAT-authenticated inference requests produce `InferenceUsageRecord` rows with `principal_type='PAT'` and `principal_id` matching the issuing user.
- Revoked PATs are rejected within one Redis round-trip after revocation completes.
- No raw PAT values appear in application logs, database rows, or OTel span attributes.
- `make check` passes with zero issues after implementation.

### Monitoring

- Structured log events on PAT create (`pat.created`) and revoke (`pat.revoked`) with `tenant_id`, `user_id`, `pat_id`.
- `principal_type=PAT` in OTel span attributes for all PAT-authenticated inference requests.
- Inference usage records filterable by `principal_type='PAT'` for tenant-level PAT usage dashboards.

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) - Unified identity model — defines `PrincipalType` and `IdentitySource` foundations
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) - OPA policy enforcement — future home of fine-grained PAT scope enforcement
- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) - Inference gateway architecture — defines the middleware chain PAT enforcement integrates into
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) - SPIFFE/SPIRE attestation — the workload identity model that PATs complement for local dev

---

## References

- [PRD: Personal Access Tokens](../prd/personal-access-tokens.md)
- [GitHub Personal Access Tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) — industry reference for PAT UX conventions
- [RFC 7519 — JSON Web Tokens](https://datatracker.ietf.org/doc/html/rfc7519)

---

## Notes

**Token prefix decision:** PATs are returned as `orb_<jwt>` from the API. The `orb_` prefix enables secret scanning (tools can match the pattern to detect leaked PATs in source or logs) and gives developers a clear visual identifier in `.env` files. Prefix stripping is handled by a dedicated `orbCredentialMiddleware` — separate from `xAPIKeyMiddleware` so each middleware has a single responsibility and they can be composed independently. No stripping logic is needed in SDK or CLI consumers. The `PrincipalType=PAT` claim inside the JWT remains the canonical runtime self-identifier.

**Future scope integration:** When OPA policy integration lands ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)), `PrincipalType=PAT` becomes a first-class input to the policy engine. Scope fields on `KoraClaims` (if added then) will refine what a PAT is allowed to do within the inference domain. The current design does not foreclose that path.

**PAT guard on PAT management endpoints:** PATs cannot be used to create, list, or revoke other PATs. This prevents a compromised PAT from being used to extend its own lifetime or pivot to new credentials. The guard is enforced at the handler level by checking `PrincipalType != PAT` before processing any management request.

**Last Updated:** 2026-04-01
