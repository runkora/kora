# Inference Usage Logs

**Status:** Accepted  
**Date:** 2026-04-17
**Deciders:** Platform Engineering  
**Technical Story:** [Inference Logs PRD](../prd/inference-logs.md) — defines the requirement for per-request usage log visibility with per-PAT attribution in the Model Gateway UI.

---

## Context

### Background

The Inference Gateway ([inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md)) records one `inference_usage` row per request via `PostgresInferenceUsageRepository.Create`. The write path is fully implemented across all inference paths (OpenAI chat streaming/non-streaming, Anthropic Messages streaming/non-streaming, and the failed-inference error path). The `inference_usage` table captures: timestamp, model, provider, routing strategy, prompt/completion/total token counts, estimated cost (USD), latency (ms), HTTP status, error code, retry count, and principal attribution (`tenant_id`, `principal_id`, `principal_type`).

The PAT system ([personal-access-tokens](2026-04-02-personal-access-tokens.md)) allows any authenticated human user to create a long-lived, named `PrincipalType=PAT` credential for use with local inference tooling. PATs are RS256-signed JWTs that pass through the same `token.Verifier` as all other platform tokens. At signing time, `CreatePAT` populates `KoraClaims.SessionID` with the PAT record UUID — making every request from that PAT traceable to a single named key.

The Model Gateway frontend has a Logs page at `frontend/src/app/(dashboard)/model-gateway/logs/page.tsx` with a fully implemented table UI and status filter, currently backed by mock data. The Keys page at `frontend/src/app/(dashboard)/model-gateway/keys/page.tsx` is server-side fetched and fully functional.

### Problem Statement

Three gaps prevent usage logs from being surfaced to users:

1. **No `token_id` on `inference_usage`**: The table records `principal_id` (the user's stable UUID) but not which named PAT was used. Users cannot distinguish between requests made by "My Claude Code key" and "My Cursor key" — both appear as the same `principal_id`.
2. **No HTTP read surface**: `InferenceUsageRepository.List` exists with full filter support at the repository layer but has no service method, no HTTP handler, and no registered route. The data is written but unreachable.
3. **Frontend is unconnected**: The Logs page renders mock data. No server-side fetch, no real API call, no key-filter wiring.

### Goals

- Surface per-request inference logs to users in the Model Gateway UI
- Attribute each log row to the specific named PAT that made the request
- Allow users to filter logs by named PAT, including keys that have been revoked
- Enforce strict user and tenant isolation: a user must never see another user's logs
- Leave the path open for an admin view of cross-user logs without requiring structural rework

### Non-Goals

- Logging inference request or response content (messages, prompts, completions) — metadata only
- Real-time / push-based log streaming to the frontend
- Cross-user log aggregation or admin view (v1; architecturally unblocked)
- Cursor-based pagination (v1; offset-based is sufficient and must not preclude future migration)
- Backfilling historical `inference_usage` rows with `token_id` values
- Provider-level or model-level log filtering (tracked as P2 in the PRD)

---

## Decision Drivers

* **Minimal schema footprint**: Add only what is needed — one nullable column on an existing table. No new tables.
* **Security isolation**: User and tenant scoping must come from verified JWT claims, never from caller-supplied parameters. The SQL predicate must be self-sufficient.
* **No `token.Principal` contamination**: `token.Principal` is a platform-wide identity abstraction for all actor types. PAT-specific fields must not be added to it.
* **Consistent with existing patterns**: The new endpoint must follow the same Controller/Service/Repository pattern as all other inference gateway routes.
* **Forward compatibility**: User-scoped isolation must be implemented at a layer that can be bypassed (with appropriate authorization) for a future admin view — not hardcoded in the repository.

---

## Considered Options

### Option 1: Add `token_id` column + inferencegw-local `principal.TokenID` field

Add a nullable `token_id TEXT` column to `inference_usage` (all ID columns in this schema use `TEXT`, not `UUID`). Extend the inferencegw-local `principal` struct (in `internal/inferencegw/service.go`) with a `TokenID string` field, populated conditionally in `principalFromClaims`: `claims.Kora.SessionID` is extracted only when `PrincipalType == token.PrincipalTypePAT` — for all other principal types `TokenID` is the empty string and `token_id` is written as `NULL`. Write `token_id` on every `Create` call. Add a `TokenID *string` filter field to `InferenceUsageFilter`. Expose reading via a new `GET /v1/usage` handler registered in `controller.go`.

`token.Principal` — the platform-wide abstraction in `internal/platform/token/claims.go` — is not modified.

**Pros:**
- Single nullable column; schema change is minimal and non-breaking
- `claims.Kora.SessionID` already carries the PAT record UUID at signing time (confirmed in [personal-access-tokens](2026-04-02-personal-access-tokens.md) implementation) — no JWT schema change required
- Keeps `token.Principal` clean and PAT-agnostic
- Gateway-local `principal` struct already carries service-specific fields (`PrincipalType`, `IdentitySource`) — `TokenID` is consistent with that pattern
- Null for non-PAT principals is semantically correct and requires no special handling at write time

**Cons:**
- Adds a field to a private struct that is only meaningful for PAT-authenticated callers; empty string / null for all other principal types requires a code comment to be non-confusing

### Option 2: Add an explicit `pat_id` field to `KoraClaims`

Add `PATID string \`json:"pat_id,omitempty"\`` to `KoraClaims` in `internal/platform/token/claims.go`. Populated only when `PrincipalType=PAT`. All other token types leave it empty. The inference gateway reads `claims.Kora.PATID` directly.

**Pros:**
- Semantically explicit — the field name makes the intent clear without a comment
- Avoids dual-use interpretation of `SessionID`

**Cons:**
- Modifies a shared platform type touched by every service binary
- All existing JWT consumers must be updated to handle a new field (even if they only ignore it)
- `KoraClaims.SessionID` already carries the PAT record UUID reliably — adding `PATID` is strictly redundant
- Sets a precedent for adding principal-type-specific fields to the shared claims struct, which will accumulate over time

### Option 3: JTI as `token_id`

Use `claims.RegisteredClaims.ID` (the JTI) as `token_id` rather than the PAT record UUID (`SessionID`). The JTI is already present in every JWT.

**Pros:**
- Already in `RegisteredClaims` — no new field access required
- JTI is unique per token issuance

**Cons:**
- JTI is not the PAT record UUID. Correlating a log entry back to a named PAT requires a join through `user_pat_tokens` on a different column (`jti`), which is not how the table is primarily indexed
- The `user_pat_tokens` table uses `id` (PAT record UUID) as its primary key and foreign key target everywhere. Using JTI as the log correlation key creates a split key space for the same concept
- JTI is the revocation key (Redis blocklist) — treating it as the usage log correlation key conflates two distinct concerns

---

## Decision

**Chosen Option: Option 1 — `token_id` column on `inference_usage` + inferencegw-local `principal.TokenID` extracted from `claims.Kora.SessionID`.**

### Rationale

`claims.Kora.SessionID` holds the PAT record UUID for the entire lifetime of a PAT — it is set once at signing and never changes. For a non-refreshable credential like a PAT, this means it is a stable, reliable correlation key for every log row produced by that key. No JWT schema change is required.

Keeping `token.Principal` clean is non-negotiable. It is a platform-wide abstraction consumed by every service binary. The inferencegw-local `principal` struct is already the correct place for service-domain details — `PrincipalType`, `IdentitySource`, and now `TokenID` all belong there, not on the shared platform type.

Option 2 (explicit `pat_id` on `KoraClaims`) would be semantically cleaner but creates platform-wide churn for a field whose value is already available under a different name. Option 3 (JTI as `token_id`) creates a split key space that complicates log-to-key correlation queries.

---

## Consequences

### Positive

- Users can see which named PAT made each inference request in the Logs page
- Historical logs for revoked PATs remain accessible — `token_id` is a nullable record UUID, not a live credential reference
- The SQL predicate `WHERE tenant_id = $1 AND principal_id = $2` (both from verified JWT claims) enforces user scoping without any additional validation logic
- Adding an optional `AND token_id = $3` to that predicate is self-sufficient for PAT-filtered queries — a caller supplying an arbitrary UUID for `$3` can only ever receive their own rows, returning 0 results if the UUID doesn't match any of their logs
- `token.Principal` is not modified — no ripple effect to Identity, agents, or any other service binary

### Negative

- `principal.TokenID` is an empty string for all non-PAT callers — requires a code comment to be non-confusing to future contributors
- One new nullable column adds marginal schema complexity

### Neutral

- Historical `inference_usage` rows pre-dating this migration have `token_id = NULL`. These are displayed in the UI with a "Unknown key" placeholder and are excluded from key-specific filter results. This is communicated clearly in the UI rather than hidden.
- The `inference_usage.token_id` column stores the PAT record UUID from `user_pat_tokens.id`, creating an implicit foreign-key relationship. This is intentionally not a hard FK constraint — `token_id` must remain non-null even after a PAT is revoked, and ON DELETE CASCADE semantics would destroy log history.

---

## Implementation

### 1. Database Migration

Two changes to `inference_usage`. All ID columns in this schema use `TEXT`, not `UUID` — `token_id` follows that convention.

```sql
-- +goose Up

-- 1. Extend the principal_type CHECK constraint to include PAT.
--    The original constraint was created unnamed; Postgres auto-names it
--    inference_usage_principal_type_check.
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_principal_type_check;

ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_principal_type_check
    CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM', 'PAT'));

-- 2. Drop the now-redundant single-column principal_id index.
--    The new compound index (tenant_id, principal_id, created_at DESC) below
--    is a strict superset; retaining both wastes storage and write overhead.
DROP INDEX IF EXISTS idx_inference_usage_principal_id;

-- 3. Add token_id for PAT attribution.
ALTER TABLE inference_usage
    ADD COLUMN token_id TEXT NULL;

CREATE INDEX idx_inference_usage_principal
    ON inference_usage (tenant_id, principal_id, created_at DESC);

CREATE INDEX idx_inference_usage_token
    ON inference_usage (tenant_id, token_id, created_at DESC)
    WHERE token_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_inference_usage_token;
DROP INDEX IF EXISTS idx_inference_usage_principal;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS token_id;
-- Restore the original single-column index that the Up block dropped.
CREATE INDEX IF NOT EXISTS idx_inference_usage_principal_id
    ON inference_usage (principal_id, created_at DESC);
ALTER TABLE inference_usage
    DROP CONSTRAINT inference_usage_principal_type_check;
ALTER TABLE inference_usage
    ADD CONSTRAINT inference_usage_principal_type_check
    CHECK (principal_type IN ('AGENT', 'HUMAN', 'SERVICE', 'SYSTEM'));
```

Two new indexes (one existing `idx_inference_usage_tenant_id` is preserved):
- `(tenant_id, principal_id, created_at DESC)` — primary query pattern (all logs for a user)
- `(tenant_id, token_id, created_at DESC) WHERE token_id IS NOT NULL` — PAT-filtered query pattern; partial index excludes historical null rows entirely

### 2. Write Path: `principal.TokenID` and `principalFromClaims`

Extend the inferencegw-local `principal` struct:

```go
// internal/inferencegw/service.go
type principal struct {
    TenantID       string
    PrincipalID    string
    PrincipalType  string
    IdentitySource string
    // TokenID is the PAT record UUID for PrincipalType=PAT callers.
    // Empty string (written as NULL) for all other principal types.
    TokenID        string
}
```

Extend `principalFromClaims` to extract `SessionID` conditionally. Two JWT fields are relevant here and must not be confused:

- **`claims.Kora.SessionID`** (`KoraClaims.SessionID`, JSON `orbit.session_id`) — the PAT record UUID for PAT-issued tokens; the session record UUID for OIDC-issued tokens. This is the correlation key for log attribution.
- **`claims.RegisteredClaims.ID`** (the JTI, JSON `jti`) — a per-issuance UUID used exclusively for revocation via the Redis blocklist. It is NOT the PAT record UUID and must not be used as `token_id`.

`PrincipalType` is a direct passthrough — `'PAT'` is now a valid value in the CHECK constraint:

```go
func principalFromClaims(ctx context.Context) (principal, error) {
    claims, ok := token.ClaimsFromContext(ctx)
    if !ok || claims == nil {
        return principal{}, fmt.Errorf("no verified claims in context: AuthMiddleware may not have run")
    }

    // KoraClaims.SessionID is set for ALL token types: for OIDC human sessions
    // it holds the session record UUID; for PATs it holds the PAT record UUID.
    // Only extract it as TokenID for PAT principals — for all other types TokenID
    // must be empty so inference_usage.token_id is written as NULL.
    var tokenID string
    if claims.Kora.PrincipalType == token.PrincipalTypePAT {
        tokenID = claims.Kora.SessionID
    }

    return principal{
        TenantID:       claims.Kora.TenantID,
        PrincipalID:    claims.Subject,
        PrincipalType:  string(claims.Kora.PrincipalType),
        IdentitySource: string(claims.Kora.IdentitySource),
        TokenID:        tokenID,
    }, nil
}
```

Update all five `InferenceUsageRecord` construction sites to populate `TokenID`. All IDs in this schema are `TEXT`, so use a nullable string helper:

```go
record := InferenceUsageRecord{
    // ... existing fields ...
    TokenID: nullableText(p.TokenID), // *string; nil when TokenID is "" (non-PAT callers)
}
```

### 3. Repository Changes Checklist

The following changes are required to the repository and model layer before the read path can be wired up:

> **Pre-implementation:** The new migration's `DROP CONSTRAINT inference_usage_principal_type_check` depends on Postgres auto-naming the original unnamed `principal_type` CHECK constraint in `20260328000000_create_inference_usage.sql`. Verify the constraint exists under that name (`\d inference_usage` in psql) before running the new migration. If the auto-generated name differs, update the new migration's `DROP CONSTRAINT` clause to match. The original migration has been intentionally left unchanged.

- [ ] Add `TokenID *string` field to `InferenceUsageFilter` in `models.go`
- [ ] Add `token_id` to the `InferenceUsageRecord` struct in `models.go` (type `*string`, nullable)
- [ ] Update the 17-column INSERT in `PostgresInferenceUsageRepository.Create` to include `token_id` (17th → 18th column; use `nullableText(record.TokenID)`)
- [ ] Add `token_id` predicate to `buildFilter` in `repository.go` (append when `f.TokenID != nil`)
- [ ] Extend `PostgresInferenceUsageRepository.List` to return `([]InferenceUsageRecord, int, error)` — add `COUNT(*) OVER()` window function to the SELECT

### 4. Read Path: Filter, Service Method, Handler, Route

**`InferenceUsageFilter`** — add optional `TokenID` field:

```go
type InferenceUsageFilter struct {
    TenantID    string
    PrincipalID string
    TokenID     *string   // ← new; nil means no token filter; TEXT to match schema column type
    // ... existing fields (provider, status, time range, pagination) ...
}
```

**`buildFilter`** — append predicate when set:

```go
if f.TokenID != nil {
    conditions = append(conditions, fmt.Sprintf("token_id = $%d", argIdx))
    args = append(args, *f.TokenID)
    argIdx++
}
```

**Service method** — `ListUsage`:

```go
func (s *Service) ListUsage(ctx context.Context, p principal, tokenID *string, page, pageSize int) ([]InferenceUsageRecord, error) {
    filter := InferenceUsageFilter{
        TenantID:    p.TenantID,
        PrincipalID: p.PrincipalID,
        TokenID:     tokenID,
        Limit:       pageSize,
        Offset:      (page - 1) * pageSize,
    }
    return s.usageRepo.List(ctx, filter)
}
```

`TenantID` and `PrincipalID` are always sourced from the verified JWT `principal` — they are never passed through from request parameters.

**Handler** — `GET /v1/usage`:

```go
func (h *Handler) listUsage(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    p, err := principalFromClaims(ctx)
    if err != nil {
        api.ErrorResponse(ctx, w, h.logger, http.StatusUnauthorized, "unauthorized", err)
        return
    }

    tokenID := queryParamPtr(r, "key")    // optional ?key=<pat-uuid>
    page     := queryParamInt(r, "page", 1)
    pageSize := queryParamInt(r, "page_size", 50)

    records, err := h.service.ListUsage(ctx, p, tokenID, page, pageSize)
    if err != nil {
        api.ErrorResponse(ctx, w, h.logger, http.StatusInternalServerError, "internal_error", err)
        return
    }
    api.JSON(ctx, w, h.logger, http.StatusOK, records)
}
```

**Response envelope:** The handler returns a flat JSON object with a `data` array and a `total` count:

```go
type listUsageResponse struct {
    Data  []InferenceUsageRecord `json:"data"`
    Total int                    `json:"total"`
}
```

A total count is required for the frontend to render page navigation ("Page 1 of N"). `InferenceUsageRepository.List` is extended to return `([]InferenceUsageRecord, int, error)` — the `int` is a `COUNT(*)` over the same filter without the `LIMIT`/`OFFSET` clauses (a single query using `COUNT(*) OVER()` window function avoids a second round-trip).

**Route registration** — in `controller.go`:

```go
mux.Handle("GET /v1/usage", chain(h.listUsage))
```

`chain` is the same middleware wrapper applied to all inference gateway routes. It runs JWT verification and claims injection before the handler executes — `principalFromClaims` in the handler body is therefore guaranteed to find valid, server-verified claims in the request context.

### 4. Frontend

**`_services/usage-server.ts`** — new server-side fetch service modelled after `_services/tokens-server.ts`. Calls `GET /v1/usage` with optional `?key=` and pagination params.

**`_types/index.ts`** — add `InferenceUsageRecord` type aligned to the API response shape.

**`logs/page.tsx`** — convert from mock data to server-side fetch using the new service. Accept an optional `searchParams.key` to pre-populate the key filter (deep-link target from the Keys page).

**`logs-tab.tsx`** — accept `records: InferenceUsageRecord[]` and `keys: PATSummary[]` as props (replacing the current zero-prop, mock-data implementation). Add a **Key Status** column to the table — a dedicated column displaying `Active` or `Revoked` per row, independent of the key display name cell.

**`keys/page.tsx`** — add a "View Logs" action per PAT row that navigates to `/model-gateway/logs?key=<pat-id>`.

### Migration Path

The `token_id` column is nullable. All existing rows retain their current values (NULL). No backfill is performed. The UI displays "Unknown key" for rows with `token_id = NULL` and excludes them from key-specific filter results.

---

## Validation

### Success Metrics

- A request authenticated via PAT produces an `inference_usage` row with `token_id` matching the PAT record UUID
- A request authenticated via a human OIDC session produces an `inference_usage` row with `token_id = NULL`
- `GET /v1/usage` returns only rows belonging to the authenticated user's `principal_id` — no cross-user data leakage
- `GET /v1/usage?key=<arbitrary-uuid>` returns 0 results when the UUID does not match any of the caller's own PAT IDs (not an error, not another user's data)
- The Logs page renders real data for both PAT-authenticated and session-authenticated users
- Navigating to the Logs page from the Keys page with a specific key pre-selects that key in the filter

### Monitoring

- OTel span emitted per `GET /v1/usage` request with attributes: `tenant_id`, `principal_id`, `token_id_filter_applied` (bool)
- Structured log entry per request at INFO level with the same attributes

---

## Related Decisions

- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference Gateway architecture; defines `InferenceUsageRecord`, `PostgresInferenceUsageRepository`, and the write path this ADR extends
- [personal-access-tokens](2026-04-02-personal-access-tokens.md) — PAT system; defines `user_pat_tokens`, `KoraClaims.SessionID`, and `PrincipalType=PAT`
- [unified-identity-model](2026-03-05-unified-identity-model.md) — Unified identity model; defines `token.Principal` and the constraint that it must remain actor-type-agnostic
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Policy enforcement topology; user-scoped isolation is enforced at the application layer (service method), not at the policy engine layer

---

## References

- [Inference Logs PRD](../prd/inference-logs.md)
- [personal-access-tokens](2026-04-02-personal-access-tokens.md)

---

## Notes

The `token_id` column intentionally has no hard foreign key constraint to `user_pat_tokens.id`. A hard FK with `ON DELETE SET NULL` would silently nullify log attribution when a PAT is deleted; `ON DELETE CASCADE` would destroy log history. The current approach — soft reference via UUID, PAT revocation sets `revoked_at` but preserves the record — means log rows always retain their attribution and the UI can display the PAT display name and revocation state by joining on `user_pat_tokens.id` at read time.

**Last Updated:** 2026-04-10
