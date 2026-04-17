# 41. PAT Scope Assignment via Explicit Grants

**Status:** Accepted
**Date:** 2026-04-17
**Deciders:** Platform Engineering
**Technical Story:** PATs (ADR-0033) were issued with `PrincipalType=PAT` but no scope control — any valid PAT could reach any inference endpoint. This ADR defines how PATs acquire, validate, and update their permissions within the platform's policy model (ADR-0035).

---

## Context

### Background

ADR-0033 introduced PATs as long-lived credentials for developer tooling. At the time, fine-grained scope control was explicitly deferred:

> *"Fine-grained scope selection per PAT (deferred to OPA integration, ADR-0011)."*

ADR-0035 subsequently delivered the OPA policy model: a grant system mapping `(principal_type, principal_id) → [scopes]` per tenant, compiled into per-tenant OPA bundles and evaluated at every request. Human principals receive scopes from two sources: OIDC role claims (via `policy_role_scopes` mappings) and explicit grants. Agent and service principals use explicit grants only.

PATs were not wired into this model. They carry `PrincipalType=PAT` in their JWT claims but no roles and no grants — meaning the OPA evaluator has nothing to evaluate against and PATs effectively have access to any scope the gateway accepts.

### Problem Statement

PATs must participate in the same policy model as every other principal type. Specifically:

1. Each PAT should have an explicit, user-defined set of scopes it is permitted to use.
2. Those scopes must be a subset of the scopes the issuing user holds at the time of assignment.
3. The scope set must be manageable after creation (users may gain access to new things and want to extend an existing PAT).
4. The model must work identically in local development (Dex + GLAuth) and production (enterprise OIDC providers).

### Goals

- Give each PAT an explicit, auditable set of permitted scopes.
- Bound PAT scopes to the issuing user's current effective access at assignment time.
- Allow scope updates after creation for both additions (user gained new access) and removals.
- Reuse the existing grant infrastructure with zero new principal-type-specific policy logic.
- Work without any dependency on SCIM, a persistent user-role store, or a specific IdP.

### Non-Goals

- Automatic propagation of role changes to existing PAT grants (see Decision section for rationale).
- Admin-level cross-user PAT grant management.
- Group or team-based PAT permissions.
- SCIM integration (separate initiative, dependent on enterprise IdP availability).

---

## Decision Drivers

* **IdP independence** — The solution must work with Dex + GLAuth locally and any OIDC provider in production. Any approach requiring SCIM push or a persistent role store synchronised from the IdP is ruled out.
* **Consistency with existing policy model** — PAT authorization should follow the same grant-based evaluation path as every other principal type, with no special-cased OPA rules.
* **Least-privilege** — A PAT must not be able to do more than the user who created it currently can, at the moment of grant assignment.
* **Auditability** — Every PAT's permitted scopes must be inspectable without decoding a JWT or querying an IdP.
* **Operational simplicity** — Scope management (create, update, revoke) must be achievable via the existing policy and identity service APIs without new infrastructure.

---

## Considered Options

### Option 1: OIDC Role Inheritance

Allow PATs to inherit the issuing user's OIDC roles, either by embedding them in the JWT at creation time or by resolving them from the bundle at request time.

**Pros:**
- No explicit scope selection required — PAT access mirrors the user automatically.
- No new data or UI.

**Cons:**
- OIDC roles are ephemeral JWT claims. There is no persistent record of "user X currently has role Y" in the platform. The only source of truth is the user's live OIDC session JWT, which is not available at PAT request time.
- Role changes (granted or revoked) cannot be detected because the platform has no role-change event stream. A PAT issued when a user had `tenant-admin` would retain that access indefinitely even after the user lost the role, with no mechanism to detect or act on the change.
- Long-lived tokens with inherited roles are an industry anti-pattern: they silently expand or contract in ways the token holder cannot predict.

### Option 2: SCIM-Backed Role Materialisation

Implement SCIM 2.0 endpoints. The IdP pushes user and group changes into a platform `user_role_assignments` table. PAT authorization resolves the issuing user's current roles from this table at request time via the OPA bundle.

**Pros:**
- Role changes propagate to PATs in real-time.
- Single source of truth for user roles, usable by both interactive sessions and PATs.

**Cons:**
- Dex and GLAuth (the local development stack) do not support SCIM. The model cannot be tested or used locally without swapping the IdP or building a SCIM simulator.
- SCIM is a significant engineering investment: ~8 protocol-conformant endpoints, SCIM 2.0's non-trivial PATCH dialect, and per-IdP behaviour variations (Entra vs Okta vs others).
- Introduces hard coupling to IdP provisioning capability that is not universally available.

### Option 3: Roles-on-Login Snapshot

On each OIDC login, upsert the user's roles (extracted from the session JWT) into a `user_role_assignments` table. PAT authorization reads from this table.

**Pros:**
- Works with Dex + GLAuth — no SCIM required.
- Role changes propagate on next login.

**Cons:**
- Requires a `users` table (does not currently exist — the platform has no user directory; users are identified solely by their OIDC `sub` claim).
- Roles are stale between logins. A user whose role was revoked may still have PAT access until they next authenticate interactively — a window that could be weeks if the user only uses PATs.
- Creates a hidden dependency between interactive login frequency and PAT security posture.

### Option 4: Explicit Grants (Chosen)

At PAT creation time, the user selects a set of scopes from the platform vocabulary. The identity service validates that the requested scopes are a subset of the user's current effective scopes (via the in-process policy evaluator), then creates a `policy_grant` row with `principal_type=PAT` and `principal_id=<pat_id>`. The OPA evaluator resolves PAT access identically to any other grant-backed principal. Scopes can be updated after creation subject to the same validation.

**Pros:**
- Works with any OIDC provider, including Dex + GLAuth locally. No IdP capabilities required beyond token issuance.
- Reuses the entire existing grant infrastructure — no new OPA rules, no new bundle data sources, no new tables.
- Each PAT's permissions are explicit, auditable, and stable. Access does not change silently due to upstream role changes.
- Consistent with industry practice: GitHub fine-grained PATs, AWS IAM user access keys, and Stripe API keys all use explicit permission grants for long-lived machine credentials.
- PAT revocation deletes the grant atomically, eliminating any residual access.

**Cons:**
- User must explicitly manage PAT scopes. If a user gains new access they want a PAT to use, they must update the PAT manually.
- If a user loses access to a scope, PATs that hold that scope continue to hold it until the PAT is revoked or the scope is manually removed. This is addressed operationally (see Consequences).

---

## Decision

**Chosen Option: Option 4 — Explicit grants, selected at PAT creation time, updatable after creation.**

### Rationale

Options 1–3 all require either a persistent user-role store or IdP capabilities that are not available in the local development environment. Option 4 requires neither. The local stack and any production IdP are treated identically.

The "stale access on role loss" concern raised by Options 1–3 is real but is not unique to this design. SSH keys, API keys, and service account credentials in every major platform exhibit the same property: they hold access independently of the issuing identity's current role state. The mitigation is operational hygiene — PAT revocation on user offboarding — which is already required for the PAT lifecycle in general and is consistent with how the rest of the industry handles long-lived machine credentials.

The explicit grant model has a meaningful security advantage: the scope of each PAT is known, fixed, and inspectable. There are no silent expansions or contractions due to role changes. An auditor or operator can determine exactly what a given PAT can do without consulting the IdP.

---

## Consequences

### Positive

- PAT authorization participates in the full OPA policy evaluation path with no special cases.
- `PAT` grants are visible in the policy administration dashboard alongside `HUMAN`, `AGENT`, and `SERVICE` grants.
- Revoking a PAT atomically removes its grant — no residual access window.
- Scope update is a standard grant replace operation consistent with the existing no-update pattern (delete + recreate).

### Negative

- Users must explicitly update PAT scopes when their access changes. There is no automatic propagation.
- If a user loses a role, their PAT scopes derived from that role persist until explicitly removed or the PAT is revoked. Offboarding procedures must include PAT revocation.

### Neutral

- The effective-scopes endpoint introduced here is a new read path for the policy service, useful beyond PAT management (e.g., scope selector UI for other future features).
- `PAT` becomes the fourth valid `principal_type` value in `policy_grants`, alongside `HUMAN`, `AGENT`, and `SERVICE`.

---

## Implementation

### JWT Claim: PAT Identity

ADR-0033 sets the JWT `sub` claim to the **issuing user's OIDC subject** to preserve usage attribution. This means `sub` cannot be used as the grant `principal_id` for PATs — the OPA grant lookup `(principal_type=PAT, principal_id=<pat_id>)` requires the PAT's own UUID, not the user's.

The PAT UUID is carried in a new `pat_id` field added to `OrbitClaims`:

```go
// backend/internal/platform/token/claims.go
type OrbitClaims struct {
    SessionID      string            `json:"session_id,omitempty"`
    PrincipalType  PrincipalType     `json:"principal_type"`
    TenantID       string            `json:"tenant_id"`
    IdentitySource IdentitySource    `json:"identity_source"`
    DisplayName    string            `json:"display_name,omitempty"`
    Claims         map[string]string `json:"claims,omitempty"`
    PatID          string            `json:"pat_id,omitempty"` // ← new; set only when PrincipalType=PAT
}
```

At issuance, `PatID` is set to the UUID from `user_pat_tokens.id`. For all other principal types it is omitted.

The policy evaluator derives `_principal_id` for PAT principals from `PatID`, not `sub`:

```rego
_principal_id := claims.orbit.pat_id if {
    claims.orbit.principal_type == "PAT"
} else := claims.sub
```

This preserves `sub = issuing user's OIDC subject` for attribution (usage records, logs, OTel spans) while giving OPA a stable, per-PAT identity for grant lookup.

### Policy Grants: Add PAT Principal Type

The `policy_grants` table and the grants repository's `principal_type` validation currently accept `HUMAN`, `AGENT`, and `SERVICE`. Add `PAT` as a valid value.

No schema migration is required — `principal_type` is a `TEXT` column. A check constraint update or application-level enum extension is sufficient.

### Effective Scopes Endpoint

A new read endpoint on the policy service resolves the calling principal's full set of effective scopes:

```
GET /api/v1/policy/effective-scopes
Authorization: Bearer <human session token>

→ 200 OK
{
  "scopes": [
    "inference:invoke",
    "inference:list",
    "platform:grants:read"
  ]
}
```

Resolution logic (mirrors OPA evaluation for the calling principal):

1. Resolve `principal_roles` from the caller's JWT `Claims["roles"]`.
2. For each role, expand via `policy_role_scopes` for the tenant.
3. Union with any explicit `HUMAN` grants for the caller's `sub`.
4. Return the deduplicated, sorted scope list.

This endpoint is used by the PAT creation and update UI to populate the scope selector. It is also used server-side during PAT create/update validation.

### Identity Service: PAT Creation Changes

`POST /api/v1/identity/tokens` gains a `scopes` field:

```
POST /api/v1/identity/tokens
{
  "name": "claude-code-local",
  "expires_in_days": 90,
  "scopes": ["inference:invoke", "inference:list"]
}
```

The identity service handler:

1. Resolves the caller's effective scopes using the in-process policy evaluator (same library used by the inference gateway — `internal/platform/policy`).
2. Validates each requested scope exists in the tenant vocabulary.
3. Validates each requested scope is covered by the caller's effective scopes. Requests containing out-of-bounds scopes are rejected with `400 Bad Request` and a descriptive error identifying the offending scopes.
4. Creates the PAT record in `user_pat_tokens`.
5. Creates a `policy_grant` row: `(tenant_id, principal_type=PAT, principal_id=<pat_id>, scopes=[...])`.

Steps 4 and 5 are executed in a single database transaction. If the grant creation fails, the PAT record is rolled back.

The PAT `id` (UUID) is used as `principal_id` in the grant, not the issuing user's `sub`. This ensures each PAT has its own independently revocable grant.

### Identity Service: PAT Scope Update

A new endpoint allows updating scopes on an existing, non-revoked PAT:

```
PATCH /api/v1/identity/tokens/{id}/scopes
Authorization: Bearer <human session token>

{
  "scopes": ["inference:invoke"]
}
```

The handler first verifies that the PAT identified by `{id}` belongs to the calling user (`pat.user_id == caller.sub`). A PAT owned by a different user within the same tenant returns `404 Not Found` — the same response as a non-existent PAT, to avoid confirming that the ID exists.

Validation is otherwise identical to creation: the new scope set must be a subset of the caller's current effective scopes at update time. The full scope set is replaced (not merged) — the caller must send the complete desired scope list.

The grant is updated via delete + recreate within a transaction, consistent with the existing grants no-update pattern.

**Scope reduction** (removing scopes the user still holds) is always permitted without re-validation — a user can always narrow a PAT's access.

**Scope addition** is validated: the user must currently hold each scope being added. If a scope was previously held (and thus previously grantable) but the user's role has since been revoked, adding that scope to a PAT will fail at update time with `403 Forbidden`.

### Identity Service: PAT Revocation Changes

`DELETE /api/v1/identity/tokens/{id}` gains a step:

1. *(existing)* Load PAT, set `revoked_at`, add JTI to Redis blocklist.
2. *(new)* Delete the `policy_grant` for `(tenant_id, principal_type=PAT, principal_id=<pat_id>)`.

Steps 1 and 2 are best-effort ordered: JTI blocklisting is the primary revocation mechanism (takes effect immediately). Grant deletion is a cleanup step — a blocklisted JTI will be rejected before OPA is evaluated regardless.

### List Response: Include Scopes

`GET /api/v1/identity/tokens` response gains a `scopes` field per token:

```json
{
  "tokens": [
    {
      "id": "550e8400-...",
      "name": "claude-code-local",
      "scopes": ["inference:invoke", "inference:list"],
      "expires_at": "2026-07-16T10:00:00Z",
      "created_at": "2026-04-17T10:00:00Z"
    }
  ]
}
```

Scopes are read from the grant, not from the JWT (the JWT does not carry scopes). If no grant exists for a PAT (e.g., it was created before this ADR landed), `scopes` is returned as an empty array.

### OPA Policy: `authz.rego`

The existing grant-based allow rule in `authz.rego` already resolves grants by `principal_type` + `principal_id`. No rule changes are required provided `PAT` is accepted as a valid `principal_type` value in the grants data.

The rule that restricts role-based scope resolution to `HUMAN` principals is unchanged:

```rego
principal_all_scopes contains scope if {
    _principal_type == "HUMAN"          # ← PAT does not enter this path
    some role in _principal_roles
    some scope in data.roles.role_scopes[role]
}
```

PATs resolve scopes only via the grant path:

```rego
principal_all_scopes contains scope if {
    some grant in data.grants.grants
    grant.principal_type == _principal_type
    grant.principal_id   == _principal_id
    some scope in grant.scopes
}
```

This is the same path used by `AGENT` and `SERVICE` principals today. No new Rego is required.

### Frontend: PAT Creation UI

The PAT creation form gains a scope selector step:

1. On mount, fetch `GET /api/v1/policy/effective-scopes` to get the user's current scopes.
2. Render scopes grouped by resource type (from vocabulary metadata).
3. User selects desired subset. At minimum one scope must be selected.
4. Submit includes `scopes: [...]` in the creation payload.

The PAT list view gains:
- A "Permissions" column showing the current scope set (truncated with expand).
- An "Edit permissions" action that opens an update modal with the same scope selector, pre-checked with current scopes.

### Migration Path

Existing PATs created before this ADR have no grants. They will be denied by the OPA evaluator on every request once this change is deployed, since `principal_has_required_scope` will find no matching grant.

Two options for handling existing PATs:

1. **Revoke-and-recreate (preferred):** Existing PATs are treated as legacy credentials. Users are notified and prompted to create new PATs with explicit scopes. A grace period (e.g., 30 days) is provided before enforcement is enabled.
2. **Backfill inference scopes:** A migration script creates `(PAT, pat_id) → [inference:invoke, inference:list]` grants for all existing non-revoked PATs, preserving their original access level. Users can then adjust scopes via the new UI.

Option 2 is lower friction for existing users and is the recommended path. The migration script is idempotent and can be run at deployment time.

---

## Validation

### Success Metrics

- All PAT-authenticated requests are evaluated against a grant in OPA; no PAT reaches a handler without an explicit grant covering the required scope.
- `GET /api/v1/identity/tokens` returns accurate scope lists matching the live grants.
- Scope updates are reflected in OPA evaluation within one bundle poll interval (default 30s).
- PAT revocation deletes the associated grant; subsequent requests with the revoked token receive 401 (JTI blocklist) before OPA is evaluated.

### Monitoring

- `pat.scopes_updated` structured log event on scope change with `tenant_id`, `user_id`, `pat_id`, `old_scopes`, `new_scopes`.
- 403 responses on PAT-authenticated requests surface as `principal_type=PAT` in OTel span attributes, filterable in the observability stack.

---

## Related Decisions

- [ADR-0033](0033-personal-access-tokens.md) — PAT issuance, storage, and revocation model that this ADR extends
- [ADR-0035](0035-policy-gateway-and-authorisation-model.md) — Grant-based policy model and OPA bundle structure
- [ADR-0036](0036-dynamic-route-scope-registration.md) — Route-to-scope mapping that defines the vocabulary PATs draw from
- [ADR-0038](0038-policy-administration-dashboard.md) — Policy dashboard that will surface PAT grants alongside other principal grants

---

## References

- [GitHub Fine-Grained PATs](https://github.blog/security/application-security/introducing-fine-grained-personal-access-tokens-for-github/) — industry reference for explicit-grant PAT model
- [RFC 7519 — JSON Web Tokens](https://datatracker.ietf.org/doc/html/rfc7519)

---

## Notes

**Why not check the issuing user's current access at request time?** A request-time intersection check (token scopes ∩ user's live scopes) would give stronger guarantees — PATs lose access immediately when the user loses a role. However, the user's live scopes are only available via their OIDC session JWT, which is not present during a PAT-authenticated request. There is no persistent role store to query. The only correct implementation would be SCIM (Option 2), which is ruled out by the IdP-independence requirement.

**Why does scope loss not force-remove PAT grants?** Role changes are invisible to the platform — OIDC roles arrive only in session JWTs, and there is no role-change event stream. The platform cannot act on events it cannot observe. Operational mitigation (PAT revocation on offboarding) is the correct layer for this concern, consistent with industry practice for all long-lived machine credentials.

**Last Updated:** 2026-04-17
