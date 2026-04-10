# 35. Inference Usage Logs

**Status:** Proposed  
**Date:** 2025-07-15  
**Deciders:** Platform Engineering  
**Technical Story:** [Inference Logs PRD](../prd/inference-logs.md) — defines the requirement for per-request usage log visibility with per-PAT attribution in the Model Gateway UI.

---

## Context

### Background

The Inference Gateway (ADR-0023) records one `inference_usage` row per request via `PostgresInferenceUsageRepository.Create`. The write path is fully implemented across all inference paths (OpenAI chat streaming/non-streaming, Anthropic Messages streaming/non-streaming, and the failed-inference error path). The `inference_usage` table captures: timestamp, model, provider, routing strategy, prompt/completion/total token counts, estimated cost (USD), latency (ms), HTTP status, error code, retry count, and principal attribution (`tenant_id`, `principal_id`, `principal_type`).

The PAT system (ADR-0033) allows any authenticated human user to create a long-lived, named `PrincipalType=PAT` credential for use with local inference tooling. PATs are RS256-signed JWTs that pass through the same `token.Verifier` as all other platform tokens. At signing time, `CreatePAT` populates `OrbitClaims.SessionID` with the PAT record UUID — making every request from that PAT traceable to a single named key.

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

Add a nullable `token_id UUID` column to `inference_usage`. Extend the inferencegw-local `principal` struct (in `internal/inferencegw/service.go`) with a `TokenID string` field, populated from `claims.Orbit.SessionID` in `principalFromClaims`. Write `token_id` on every `Create` call (null for non-PAT principals). Add a `TokenID *string` filter field to `InferenceUsageFilter`. Expose reading via a new `GET /v1/usage` handler registered in `controller.go`.

`token.Principal` — the platform-wide abstraction in `internal/platform/token/claims.go` — is not modified.

**Pros:**
- Single nullable column; schema change is minimal and non-breaking
- `claims.Orbit.SessionID` already carries the PAT record UUID at signing time (confirmed in ADR-0033 implementation) — no JWT schema change required
- Keeps `token.Principal` clean and PAT-agnostic
- Gateway-local `principal` struct already carries service-specific fields (`PrincipalType`, `IdentitySource`) — `TokenID` is consistent with that pattern
- Null for non-PAT principals is semantically correct and requires no special handling at write time

**Cons:**
- Adds a field to a private struct that is only meaningful for PAT-authenticated callers; empty string / null for all other principal types requires a code comment to be non-confusing

### Option 2: Add an explicit `pat_id` field to `OrbitClaims`

Add `PATID string \`json:"pat_id,omitempty"\`` to `OrbitClaims` in `internal/platform/token/claims.go`. Populated only when `PrincipalType=PAT`. All other token types leave it empty. The inference gateway reads `claims.Orbit.PATID` directly.

**Pros:**
- Semantically explicit — the field name makes the intent clear without a comment
- Avoids dual-use interpretation of `SessionID`

**Cons:**
- Modifies a shared platform type touched by every service binary
- All existing JWT consumers must be updated to handle a new field (even if they only ignore it)
- `OrbitClaims.SessionID` already carries the PAT record UUID reliably — adding `PATID` is strictly redundant
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

**Chosen Option: Option 1 — `token_id` column on `inference_usage` + inferencegw-local `principal.TokenID` extracted from `claims.Orbit.SessionID`.**

### Rationale

`claims.Orbit.SessionID` holds the PAT record UUID for the entire lifetime of a PAT — it is set once at signing and never changes. For a non-refreshable credential like a PAT, this means it is a stable, reliable correlation key for every log row produced by that key. No JWT schema change is required.

Keeping `token.Principal` clean is non-negotiable. It is a platform-wide abstraction consumed by every service binary. The inferencegw-local `principal` struct is already the correct place for service-domain details — `PrincipalType`, `IdentitySource`, and now `TokenID` all belong there, not on the shared platform type.

Option 2 (explicit `pat_id` on `OrbitClaims`) would be semantically cleaner but creates platform-wide churn for a field whose value is already available under a different name. Option 3 (JTI as `token_id`) creates a split key space that complicates log-to-key correlation queries.

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

New column on `inference_usage`:

```sql
-- +goose Up
ALTER TABLE inference_usage
    ADD COLUMN token_id UUID NULL;

CREATE INDEX idx_inference_usage_principal
    ON inference_usage (tenant_id, principal_id, created_at DESC);

CREATE INDEX idx_inference_usage_token
    ON inference_usage (tenant_id, token_id, created_at DESC)
    WHERE token_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_inference_usage_token;
DROP INDEX IF EXISTS idx_inference_usage_principal;
ALTER TABLE inference_usage DROP COLUMN IF EXISTS token_id;
```

Two indexes:
- `(tenant_id, principal_id, created_at DESC)` — primary query pattern (all logs for a user)
- `(tenant_id, token_id, created_at DESC) WHERE token_id IS NOT NULL` — PAT-filtered query pattern; partial index excludes historical null rows from the index entirely

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

Extend `principalFromClaims` to extract `SessionID`:

```go
func principalFromClaims(ctx context.Context) (principal, error) {
    claims, ok := token.ClaimsFromContext(ctx)
    if !ok || claims == nil {
        return principal{}, fmt.Errorf("no verified claims in context: AuthMiddleware may not have run")
    }
    return principal{
        TenantID:       claims.Orbit.TenantID,
        PrincipalID:    claims.Subject,
        PrincipalType:  string(claims.Orbit.PrincipalType),
        IdentitySource: string(claims.Orbit.IdentitySource),
        TokenID:        claims.Orbit.SessionID, // PAT record UUID; "" for non-PAT tokens
    }, nil
}
```

Update all five `InferenceUsageRecord` construction sites to populate `TokenID`:

```go
record := InferenceUsageRecord{
    // ... existing fields ...
    TokenID: nullableUUID(p.TokenID), // pgtype.UUID; null if empty string
}
```

### 3. Read Path: Filter, Service Method, Handler, Route

**`InferenceUsageFilter`** — add optional `TokenID` field:

```go
type InferenceUsageFilter struct {
    TenantID    string
    PrincipalID string
    TokenID     *string   // ← new; nil means no token filter
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

**Route registration** — in `controller.go`:

```go
mux.Handle("GET /v1/usage", chain(h.listUsage))
```

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

- [ADR-0023](0023-inference-gateway-architecture.md) — Inference Gateway architecture; defines `InferenceUsageRecord`, `PostgresInferenceUsageRepository`, and the write path this ADR extends
- [ADR-0033](0033-personal-access-tokens.md) — PAT system; defines `user_pat_tokens`, `OrbitClaims.SessionID`, and `PrincipalType=PAT`
- [ADR-0008](0008-unified-identity-model.md) — Unified identity model; defines `token.Principal` and the constraint that it must remain actor-type-agnostic
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Policy enforcement topology; user-scoped isolation is enforced at the application layer (service method), not at the policy engine layer

---

## References

- [Inference Logs PRD](../prd/inference-logs.md)
- [ADR-0033 — Personal Access Tokens](0033-personal-access-tokens.md)

---

## Notes

The `token_id` column intentionally has no hard foreign key constraint to `user_pat_tokens.id`. A hard FK with `ON DELETE SET NULL` would silently nullify log attribution when a PAT is deleted; `ON DELETE CASCADE` would destroy log history. The current approach — soft reference via UUID, PAT revocation sets `revoked_at` but preserves the record — means log rows always retain their attribution and the UI can display the PAT display name and revocation state by joining on `user_pat_tokens.id` at read time.

**Last Updated:** 2025-07-15
