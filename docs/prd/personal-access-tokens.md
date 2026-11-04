# Personal Access Tokens (PATs) - Product Requirements Document

**Status:** Implemented  
**Created:** 2026-04-01  
**Last Updated:** 2026-04-01  
**Owner:** Platform Team  
**Stakeholders:** Platform Engineering, Developer Experience

---

## Executive Summary

kora's inference gateway is valuable beyond deployed agent clusters — developers need to point local coding agents (Claude Code, Cursor, Continue, etc.) directly at kora inference without spinning up a full SPIFFE-attested workload. Personal Access Tokens (PATs) are user-bound, longer-lived tokens scoped exclusively to inference endpoints, enabling this local development workflow securely and without compromising the platform's identity guarantees.

---

## Problem Statement

### Current State

kora currently issues two classes of platform token:

- **Human session tokens** — short-lived (60 min), issued via OIDC device flow or id_token exchange, backed by refresh tokens. Designed for browser/CLI sessions.
- **Agent tokens** — short-lived (60 min), issued via SPIFFE SVID attestation. Designed for workloads running inside kora-managed clusters with SPIRE installed.

Neither type is practical for a developer running a coding agent locally and pointing it at kora inference. Human session tokens expire in 60 minutes and require re-authentication via a browser redirect. Agent tokens require a full SPIFFE/SPIRE attestation chain, which is only available inside the cluster.

### User Pain Points

- Developers cannot use kora inference for local coding agents without re-authenticating every hour.
- There is no stable credential a developer can configure once and leave in their local tool (`.env`, settings file, etc.) without it expiring mid-session.
- Agent tokens require cluster infrastructure that does not exist on a developer laptop.
- Using human session tokens for automated local tools defeats the purpose of short-lived tokens — developers are forced to either script refresh loops or abandon kora inference for direct provider keys.

### Business Impact

- kora's inference gateway is underused during development because the credential story for local tooling is broken.
- Developers bypass kora and use raw provider API keys locally, eliminating all governance, usage attribution, and cost visibility during development.
- Every inference call made with a raw provider key is invisible to the platform — no usage records, no policy enforcement, no auditability.

---

## Goals and Objectives

### Primary Goals

1. Enable any authenticated kora user to create a long-lived token suitable for local development tooling.
2. Ensure these tokens are scoped exclusively to inference endpoints — they cannot access the main kora API.
3. Give users full lifecycle control: create, list, and revoke their own PATs.

### Success Metrics

- A developer can configure a PAT in their local coding agent once and use it for the full 90-day default lifetime without re-authenticating.
- All inference calls made with PATs produce complete usage records attributed to the correct user and tenant.
- A revoked PAT is rejected within the Redis TTL window (< 1 second in practice, bounded by the JTI blocklist propagation already used for session revocation).
- Zero PAT raw values stored in kora's database — only SHA-256 hashes.

### Non-Goals

- Fine-grained scope selection at PAT creation time (e.g., restricting to specific providers or models). This will be addressed when the OPA policy engine is fully integrated ([universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md)).
- PAT rotation/re-roll endpoint. Deferred to a follow-on iteration.
- `last_used_at` tracking. Deferred to a follow-on iteration.
- UI for PAT management. This PRD covers the API layer only.
- Service-account or machine-identity PATs. PATs are bound to human users only.
- PAT federation across tenants.

---

## User Stories

### Primary User Stories

**As a** developer,  
**I want to** create a Personal Access Token from the kora CLI or API,  
**So that** I can configure my local coding agent (Claude Code, Cursor, Continue) to use kora inference without re-authenticating every hour.

**Acceptance Criteria:**
- [ ] Authenticated user can `POST /api/v1/identity/tokens` and receive a raw PAT value exactly once.
- [ ] PAT response includes the token ID, name, expiry timestamp, and the raw token string (shown once, not retrievable again).
- [ ] Default expiry is 90 days from creation; user can specify a custom `expires_in_days` (1–365).
- [ ] PAT is accepted by the inference gateway (`/v1/chat/completions`, `/v1/messages`, `/v1/models`).

---

**As a** developer,  
**I want to** list all my active Personal Access Tokens,  
**So that** I can see what credentials exist and identify tokens I no longer need.

**Acceptance Criteria:**
- [ ] `GET /api/v1/identity/tokens` returns all non-revoked, non-expired PATs for the authenticated user.
- [ ] Response includes ID, name, created_at, expires_at for each token. Raw token value is never returned after creation.
- [ ] Only the requesting user's tokens are returned — no cross-user visibility.

---

**As a** developer,  
**I want to** revoke a Personal Access Token by ID,  
**So that** I can immediately invalidate a token I no longer need or that may have been exposed.

**Acceptance Criteria:**
- [ ] `DELETE /api/v1/identity/tokens/{id}` revokes the PAT immediately.
- [ ] Revoked tokens are rejected by the inference gateway within the Redis JTI blocklist propagation window.
- [ ] Users can only revoke their own tokens; attempting to revoke another user's token returns 404 (not 403, to avoid information leakage).

---

**As a** developer with a PAT,  
**I want** the token to be rejected if I accidentally use it against a non-inference endpoint,  
**So that** the inference-only scope of the token is enforced by the platform, not by convention.

**Acceptance Criteria:**
- [ ] A PAT presented to any main kora API endpoint (`/api/v1/...` excluding inference) returns 403 Forbidden.
- [ ] The inference gateway accepts PATs alongside existing `AGENT` and `HUMAN` session tokens.

---

### Secondary User Stories

**As a** platform operator,  
**I want** all PAT-sourced inference calls to produce usage records attributed to the issuing user,  
**So that** PAT usage is visible in tenant reporting and cost attribution.

**Acceptance Criteria:**
- [ ] `InferenceUsageRecord.PrincipalType` is set to `PAT` for all PAT-originated requests.
- [ ] `InferenceUsageRecord.PrincipalID` is set to the user's subject identifier.

---

## Proposed Solution

### High-Level Approach

Introduce a new `PrincipalType` value (`PAT`) in the platform token layer. Issue PATs as standard RS256-signed JWTs using the existing signing infrastructure, with a configurable TTL (default 90 days). Store only the SHA-256 hash of the raw token in a new `user_pat_tokens` table. Enforce the inference-only constraint via a thin middleware in the inference gateway that allows only `PAT`, `AGENT`, and `HUMAN` token types — and at the main API layer, reject `PAT` tokens.

PATs carry no refresh token. When a PAT expires or is revoked, the user simply creates a new one.

### User Experience

#### Creating a PAT (CLI / direct API)

```bash
# Create a PAT with default 90-day expiry
curl -X POST https://orbit.example.com/api/v1/identity/tokens \
  -H "Authorization: Bearer $KORA_SESSION_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "claude-code-local"}'

# Response (raw token shown exactly once)
{
  "id": "pat_01HX...",
  "name": "claude-code-local",
  "token": "orb_...",   # ← shown once, store it now
  "expires_at": "2025-04-25T10:00:00Z",
  "created_at": "2026-04-01T10:00:00Z"
}
```

#### Using a PAT with a coding agent

```bash
# Claude Code — set ANTHROPIC_BASE_URL + API key
export ANTHROPIC_BASE_URL=https://orbit.example.com/v1
export ANTHROPIC_API_KEY=orb_...

# OpenAI-dialect agents
export OPENAI_BASE_URL=https://orbit.example.com/v1
export OPENAI_API_KEY=orb_...
```

The `x-api-key` middleware in the inference gateway already normalises the key to `Authorization: Bearer` before verification — no special PAT handling needed in the SDKs.

#### User Flow

1. Developer authenticates to kora via device flow (`make auth-device` or UI).
2. Developer calls `POST /api/v1/identity/tokens` with their session token and a descriptive name.
3. kora issues the PAT, returns the raw value once, and stores only its hash.
4. Developer sets the PAT as the API key in their local coding agent configuration.
5. When the agent calls kora inference, the `x-api-key` middleware promotes it to `Authorization: Bearer`.
6. `AuthMiddleware` verifies the JWT and stores claims (PrincipalType = `PAT`) in context.
7. The inference gateway's PAT guard middleware confirms the principal type is inference-permitted.
8. The request proceeds through policy evaluation and provider routing as normal.

### Key Components

- **`PlatformClaims` extension** — Add `PAT` to the `PrincipalType` enum in `backend/internal/platform/token/claims.go`.
- **`user_pat_tokens` table** — New migration in `backend/cmd/identity/migrations/`. Stores token metadata and SHA-256 hash; no raw token at rest.
- **PAT issuance service method** — `CreatePAT` on the identity `Service` — signs a JWT with `PrincipalType=PAT`, persists the hash row, returns the raw token once.
- **PAT management HTTP handlers** — `POST /api/v1/identity/tokens`, `GET /api/v1/identity/tokens`, `DELETE /api/v1/identity/tokens/{id}` on the identity service.
- **Inference gateway PAT guard** — A thin middleware (`requireInferencePrincipal`) that allows `PAT`, `AGENT`, and `HUMAN` token types at inference routes; rejects all others.
- **Main API PAT rejection** — The main kora API auth flow checks for `PrincipalType=PAT` and returns 403.
- **Revocation** — Reuses the existing JTI blocklist (Redis) and `revoked_at` column pattern.

---

## Scope

### In Scope

- `PAT` principal type addition to the platform token layer
- `user_pat_tokens` database table and repository
- PAT issuance with configurable TTL (default 90 days, max 365 days)
- PAT management API: create, list, revoke
- Inference-only enforcement middleware in the inference gateway
- Main API rejection of `PAT`-typed tokens
- Usage attribution: `PrincipalType=PAT` in inference usage records

### Out of Scope

- PAT management UI (deferred — API-first)
- Fine-grained scope selection at creation time (deferred to OPA integration, [universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md))
- `last_used_at` tracking (deferred to follow-on iteration)
- PAT rotation / re-roll endpoint (deferred to follow-on iteration)
- Admin-level PAT management (listing/revoking other users' PATs)
- PAT issuance for non-human principals (service accounts, agents)
- PAT usage in the identity service itself (PATs are inference-only)

### Future Considerations

- **Scope selection at creation** — Once OPA policy is integrated ([universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md)), users will be able to declare which providers or model families a PAT is permitted to call.
- **`last_used_at` tracking** — Update a `last_used_at` timestamp on the PAT row on each successful inference request.
- **Rotate / re-roll** — `POST /api/v1/identity/tokens/{id}/rotate` — atomically revoke the old PAT and issue a new one with the same name and TTL.
- **Admin visibility** — Tenant admins listing and revoking PATs across their tenant.
- **PAT management UI** — A settings screen in the kora web UI.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Any authenticated human user can create a PAT via `POST /api/v1/identity/tokens`
- [ ] PAT names are user-supplied strings (1–128 chars) for identification purposes
- [ ] PAT expiry defaults to 90 days; user may specify `expires_in_days` (1–365)
- [ ] Raw PAT value is returned exactly once at creation and never retrievable again
- [ ] Only the SHA-256 hash of the raw token is persisted in the database
- [ ] `GET /api/v1/identity/tokens` returns all active (non-revoked, non-expired) PATs for the requesting user
- [ ] `DELETE /api/v1/identity/tokens/{id}` immediately revokes a PAT
- [ ] Revoked PATs are added to the Redis JTI blocklist for fast rejection
- [ ] PATs are accepted by the inference gateway with full usage attribution
- [ ] PATs are rejected by the main kora API (non-inference endpoints) with 403
- [ ] All inference calls using PATs produce `InferenceUsageRecord` rows with `principal_type = 'PAT'`

**Should Have (P1):**
- [ ] Attempting to revoke another user's PAT returns 404 (not 403)
- [ ] Expired PATs are excluded from the list response (filtered at query time)
- [ ] PAT creation validates that `name` is non-empty and within length limits
- [ ] Token format has the `orb_` prefix for visual identification and secret scanning compatibility

**Nice to Have (P2):**
- [ ] `expires_in_days` validation returns a descriptive error when out of range
- [ ] PAT list response includes a count of total active tokens

### Non-Functional Requirements

**Performance:**
- PAT creation and revocation: < 100ms p95 (database-backed, no external calls)
- PAT verification at inference: no additional latency beyond existing JWT verification (PAT is a standard JWT — same `Verifier` path)

**Security:**
- Raw PAT value is never stored; only SHA-256 hash persisted in `user_pat_tokens`
- PAT JWTs are signed RS256 using the same signing key as all other platform tokens
- JTI is unique per PAT; revocation via Redis JTI blocklist takes effect immediately
- PATs carry `PrincipalType=PAT` in `KoraClaims` — scope enforcement is type-driven and cannot be spoofed without access to the signing key
- Users can only see and revoke their own PATs

**Reliability:**
- PAT verification path is identical to existing JWT verification — no new failure modes at the hot path
- PAT issuance is synchronous and database-backed; no external dependencies beyond Postgres

**Observability:**
- PAT creation and revocation emit structured log events with `tenant_id`, `principal_id`, `token_id`
- All inference usage attributed to PATs surfaces `principal_type=PAT` in usage records and OTel spans

---

## Dependencies

### Internal Dependencies

- **Identity service** (`backend/internal/identity/`) — Provides signing key, session infrastructure, and JTI blocklist. PAT issuance builds on the same `signToken` path. ✅ Exists.
- **Platform token package** (`backend/internal/platform/token/`) — `PrincipalType` enum must be extended with `PAT`. ✅ Exists, small addition.
- **JTI blocklist** (`JTIBlocklistRepository` / Redis) — Used for fast PAT revocation. ✅ Exists.
- **Inference gateway** (`backend/internal/inferencegw/`) — Needs a `requireInferencePrincipal` guard middleware. ✅ Exists, small addition.

### External Dependencies

- None. PAT issuance, storage, and verification are entirely within the kora platform.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| PAT raw value exposed in logs | High | Low | Enforce that raw token is never logged; only token ID and hash appear in structured logs. Code review gate. |
| User creates a PAT and loses the value | Low | Medium | Document clearly in API response that the token is shown once. Revoke-and-recreate is the recovery path. |
| PAT used on main API endpoints (privilege escalation) | Medium | Low | `PrincipalType=PAT` check in main API auth path; returns 403. Belt-and-suspenders with inference gateway allow-list. |
| 365-day PATs create long exposure windows | Medium | Medium | Default is 90 days. Future: scope restriction per PAT (OPA integration). |
| Token prefix `orb_` collides with future token types | Low | Low | Reserve `orb_` as the platform-wide credential prefix; document convention in ADR. All kora-issued programmatic credentials use this prefix. |

---

## Alternatives Considered

### Alternative 1: Extend human session TTL

**Description:** Simply allow human session tokens to be issued with a longer TTL (days/weeks) via a new grant parameter.

**Why not chosen:**
- Human sessions carry refresh token semantics and OIDC session linkage that are semantically wrong for static developer credentials.
- Long-lived human sessions complicate session revocation (revoking a session also kills a long-lived credential that may be shared across tools).
- No clear way to scope long-lived human sessions to inference only without adding scope infrastructure to the session layer.

### Alternative 2: Add a `Scope` field to `KoraClaims`

**Description:** Add `Scope []string` to `KoraClaims` (e.g., `["inference:*"]`). Enforcement checks the scope field at each endpoint.

**Why not chosen:**
- Scope selection at PAT creation is explicitly deferred to the OPA policy integration phase. Introducing a scope field now without a policy engine to enforce it is premature.
- `PrincipalType=PAT` achieves the same inference-only constraint today with zero additional struct fields and zero stringly-typed scope strings.
- Scope can be layered on top of `PrincipalType` later without breaking changes to PAT-holding clients.

### Alternative 3: Reuse `identity_sessions` table

**Description:** Store PATs as rows in the existing `identity_sessions` table with `principal_type='PAT'`.

**Why not chosen:**
- The sessions table carries OIDC session semantics (`identity_source`, refresh token FK relationships) that don't apply to PATs.
- PAT-specific fields (`name`) don't belong in the sessions table.
- A dedicated table keeps the data model clean and makes PAT-specific queries (list by user, check hash) efficient and unambiguous.

---

## Open Questions

1. **Max PAT count per user:** Should there be a cap on how many active PATs a single user can hold?
   - **Status:** Resolved — no cap for now; revisit when admin tooling exists.

2. **Tenant-level PAT disable:** Should tenant admins be able to disable PAT issuance for their tenant?
   - **Status:** Deferred — not in scope for this iteration. Will be addressed alongside admin tooling.

---

## Timeline and Milestones

### Phases

**Phase 1: Core PAT infrastructure** (current)
- `PrincipalType=PAT` token layer extension
- `user_pat_tokens` table and repository
- PAT issuance, list, revoke API
- Inference gateway enforcement middleware
- Main API PAT rejection

**Phase 2: Management enhancements** (follow-on)
- `last_used_at` tracking
- Rotate / re-roll endpoint
- Admin visibility (tenant-scoped PAT listing)

**Phase 3: Scope integration** (future — depends on [universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md) OPA integration)
- Fine-grained scope selection at PAT creation
- Per-PAT model/provider allow-lists enforced by OPA

### Key Milestones

- [x] PRD Approved: 2026-04-01
- [ ] ADR Created: 2026-04-01
- [ ] Implementation Started: TBD
- [ ] Alpha / internal testing: TBD
- [ ] Launch: TBD

---

## Appendix

### Related Documents

- [unified-identity-model](../adr/2026-03-05-unified-identity-model.md) - Unified identity model (PrincipalType foundations)
- [universal-gateway-policy-enforcement](../adr/2026-03-05-universal-gateway-policy-enforcement.md) - OPA policy enforcement (future scope integration)
- [inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md) - Inference gateway architecture
- [personal-access-tokens](../adr/2026-04-02-personal-access-tokens.md) - PAT technical architecture decision

### Glossary

- **PAT** — Personal Access Token. A user-bound, longer-lived, inference-scoped platform token.
- **JTI** — JWT ID. A unique identifier per token used for revocation tracking.
- **JTI Blocklist** — Redis-backed store of revoked JTI values, checked by the token verifier on every request.
- **PrincipalType** — The discriminator field in `KoraClaims` that identifies the class of identity making a request (`HUMAN`, `AGENT`, `SERVICE`, `SYSTEM`, `PAT`).

---

**Approval Signatures:**

- [x] Product Owner: Platform Team — 2026-04-01
- [x] Technical Lead: Platform Engineering — 2026-04-01
- [ ] Security Review: _________________
