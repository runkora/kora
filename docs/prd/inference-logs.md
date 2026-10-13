# Inference Logs - Product Requirements Document

**Status:** Implemented
**Created:** 2025-04-09
**Last Updated:** 2025-04-10
**Owner:** TBD
**Stakeholders:** TBD

---

## Executive Summary

Users of the kora Model Gateway authenticate their requests using Personal Access Tokens (PATs). Today, inference requests are recorded in the backend but there is no way for a user to view those logs. This feature surfaces per-request inference logs in the Model Gateway UI, attributed to the PAT that made the request, giving users full visibility into their API usage.

---

## Problem Statement

### Current State

The kora Inference Gateway already writes a usage record (`inference_usage`) for every inference request — capturing model, provider, token counts, estimated cost, latency, and status. However:

- There is no HTTP endpoint to retrieve these records.
- Usage records currently attribute requests to the **user ID** only (`principal_id = userID`). There is no column identifying *which* PAT was used, making per-key attribution impossible.
- The Model Gateway UI has no Logs page wired to real data.
- The API Keys page in the Model Gateway UI does not link to logs for a specific key.

### User Pain Points

- Users cannot audit what requests have been made through their API keys.
- Users cannot tell which of their keys is responsible for a spike in usage or cost.
- Users cannot debug a failed inference request without access to error codes, latency, or retry counts.
- Users have no way to verify a newly created API key is functioning before deploying it.

### Business Impact

- Lack of per-key attribution reduces trust in the platform — users cannot confidently manage multiple keys across different agents or environments.
- Without observable usage, users have no feedback loop to optimise model selection or catch runaway agent costs early.
- The absence of a logs view is a gap relative to comparable API platforms (OpenAI, Anthropic) that users expect by default.

---

## Goals and Objectives

### Primary Goals

1. Surface inference logs in the Model Gateway UI so users can see every request made through their account.
2. Attribute each inference log row to the specific PAT that was used, identified by the key's friendly display name.
3. Allow users to filter logs by API key on the Logs page, and navigate to a pre-filtered view directly from the API Keys page.
4. Ensure users can only ever see logs they own — tenant and user isolation must be enforced at the API layer, not just the UI.

### Success Metrics

- A user can open the Logs page and see all inference requests made under their account within seconds.
- A user can select a specific API key from the Logs page filter and see only requests attributed to that key.
- A user can click "View Logs" on a key row on the API Keys page and arrive at the Logs page with the correct filter pre-applied.
- Logs endpoint enforces user-scoped isolation: querying another user's logs returns zero results, not an error that leaks information.
- PAT attribution is correct: each log row maps to exactly one PAT record, identified by its friendly name.

### Non-Goals

- Admin / cross-user log viewing (explicitly deferred; see Future Considerations).
- Prompt and completion body logging (content is not stored; only metadata is in scope).
- Real-time / streaming log delivery (polling the logs endpoint is sufficient for v1).
- Log export or download.
- Usage aggregation dashboards or cost graphs (possible future feature).
- Log retention policy enforcement (deferred; no deletion or archiving in this release).

---

## User Stories

### Primary User Stories

**As a** Model Gateway user,  
**I want to** view a chronological list of all inference requests made with my API keys,  
**So that** I can audit my usage and catch unexpected activity.

**Acceptance Criteria:**
- [ ] The Logs page displays inference log rows ordered newest-first.
- [ ] Each row shows: timestamp, model, provider, status, token counts, estimated cost, latency, and the friendly name of the API key used.
- [ ] The page is scoped to the authenticated user — no other user's logs are visible.

---

**As a** Model Gateway user,  
**I want to** filter the Logs page by a specific API key using its friendly name,  
**So that** I can isolate usage for a particular agent, environment, or project.

**Acceptance Criteria:**
- [ ] A filter control on the Logs page lists all of the user's active API keys by display name.
- [ ] Selecting a key filters the log list to only rows attributed to that key.
- [ ] Clearing the filter restores the full unfiltered view.

---

**As a** Model Gateway user,  
**I want to** click "View Logs" on an API key row on the API Keys page,  
**So that** I can jump directly to a pre-filtered log view for that key without manual navigation.

**Acceptance Criteria:**
- [ ] The API Keys page has a "View Logs" action on each key row.
- [ ] Clicking it navigates to the Logs page with the correct key filter pre-applied (e.g., via query parameter).
- [ ] The filter is reflected in the URL so the link is shareable.

### Secondary User Stories

**As a** Model Gateway user,  
**I want to** see the status and error code for failed inference requests in the logs,  
**So that** I can debug why a request failed without needing access to server logs.

---

## Proposed Solution

### High-Level Approach

> **Note:** Full implementation detail is deferred to the ADR. This section describes the conceptual approach only.

**Backend attribution fix:** The JWT issued for a PAT currently sets `sub = userID`. The PAT record's own UUID is already embedded in the JWT as `orbit.session_id` (it is passed as the `sessionID` argument to `signToken` at creation time). A new `token_id` column is added to `inference_usage`; when `principalFromClaims` extracts a PAT-type principal, it reads `claims.Orbit.SessionID` as the PAT record ID and populates `token_id`. For non-PAT principals, `token_id` is null.

**New HTTP endpoint:** A `GET /v1/usage` (or equivalent) endpoint is added to the `inferencegw` service. It calls `InferenceUsageRepository.List` with a filter that always scopes by `TenantID` + `PrincipalID` (the user ID from the verified token) and optionally by `token_id` when a PAT filter is requested. The response includes enough PAT metadata (PAT ID, display name) for the frontend to render the filter UI.

**Frontend — Logs page:** The Logs page at `model-gateway/logs` fetches the usage list and renders a table. A filter dropdown is populated by fetching the user's PAT list (already available via `GET /identity/tokens`). The selected key ID is passed as a query parameter (`?key=<pat-id>`) to both the API and the URL, enabling deep-linking from the API Keys page.

**Frontend — API Keys page wiring:** A "View Logs" action is added to each row on the API Keys page that navigates to `/model-gateway/logs?key=<pat-id>`.

### User Experience

#### User Flow — Browsing All Logs

1. User navigates to Model Gateway → Logs.
2. Page loads with all inference logs for the user, newest first.
3. Each row shows: key name, model, provider, status badge, token counts, estimated cost, latency, and timestamp.
4. User can scroll / paginate through results.

#### User Flow — Filtering by Key

1. User opens the "API Key" filter dropdown on the Logs page.
2. Dropdown lists all the user's active PATs by display name.
3. User selects a key — table filters to only that key's logs.
4. URL updates to `?key=<pat-id>` — link is shareable.

#### User Flow — Jump from API Keys Page

1. User is on the API Keys page reviewing their keys.
2. User clicks the "View Logs" action on a specific key row.
3. User is navigated to `/model-gateway/logs?key=<pat-id>` — logs are pre-filtered.

### Key Components

- **`token_id` column** on `inference_usage` table: stores the PAT record UUID when a request is authenticated via a PAT.
- **`GET /v1/usage` endpoint** on the inference gateway: lists usage records scoped to the authenticated user, with optional PAT filter.
- **Logs page** (`model-gateway/logs`): renders the usage table and filter UI.
- **API Keys page** (`model-gateway/keys`): adds "View Logs" navigation action per key row.

---

## Scope

### In Scope

- New `token_id` column on `inference_usage` (migration + backfill strategy to be decided in ADR).
- PAT ID extraction in `principalFromClaims` for PAT-type principals.
- HTTP handler to list inference usage records, scoped to authenticated user.
- Frontend Logs page with pagination and API key filter.
- "View Logs" deep-link action on the API Keys page.
- User-scoped isolation (own logs only).

### Out of Scope

- Admin cross-user log viewing.
- Prompt/completion body capture.
- Aggregated usage dashboards.
- Log retention / expiry.
- PAT regeneration (separate feature).
- Real-time log streaming.

### Future Considerations

- **Admin log view:** A user with sufficient privileges could view logs for any user within their tenant. The schema and API design in this feature should not preclude this — the `InferenceUsageFilter` already supports arbitrary `TenantID` + `PrincipalID` combinations, so an admin endpoint would simply lift the `PrincipalID` restriction while enforcing `TenantID`. No rework needed.
- **Usage aggregation / cost dashboards:** The `inference_usage` table already has the required fields; a separate aggregation layer can be built on top.
- **Log export:** CSV/JSON download of the filtered log view.
- **PAT regeneration:** If PATs gain a regenerate/rotate capability, the new PAT would receive a new `id` and JTI, and historical logs under the old PAT ID would remain attributed to it correctly.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Each inference request made with a PAT is attributed to that PAT's record UUID in `inference_usage.token_id`.
- [ ] A new HTTP endpoint returns a user-scoped, paginated list of inference usage records.
- [ ] The endpoint enforces that only the authenticated user's records are returned.
- [ ] The endpoint supports optional filtering by `token_id` (PAT record UUID).
- [ ] The Logs page displays inference records for the authenticated user.
- [ ] The Logs page supports filtering by API key using its friendly display name.
- [ ] The selected key filter is reflected in the URL as a query parameter.
- [ ] The API Keys page includes a "View Logs" action per key that deep-links to the pre-filtered Logs page.

**Should Have (P1):**
- [ ] The Logs page displays: timestamp, key display name, key status (active / revoked), model, provider, status, prompt tokens, completion tokens, estimated cost (USD), and latency (ms).
  > **Note:** `GET /v1/usage` returns `token_id` (the PAT record UUID), not a resolved display name. Key display name and revocation status are resolved on the frontend by cross-referencing the PAT list from the existing `GET /identity/tokens` endpoint — no backend join is required.
- [ ] Failed requests show the error code alongside status.
- [ ] The logs list is paginated (offset or cursor — to be decided in ADR).
- [ ] The filter dropdown on the Logs page is populated from the live PAT list, not hardcoded.

**Nice to Have (P2):**
- [ ] Time range filter (from/to) on the Logs page.
- [x] Status filter (success / failed / all) on the Logs page — already implemented in `LogsTab`.
- [ ] Provider filter on the Logs page.

### Non-Functional Requirements

**Security:**
- The usage list endpoint must enforce `TenantID` + `PrincipalID` (user ID) scoping on every query — these must come from the verified JWT claims, never from user-supplied request parameters.
- The `token_id` filter requires no pre-validation ownership check. The SQL filter `WHERE tenant_id = $1 AND principal_id = $2 AND token_id = $3` is self-sufficient: `$1` and `$2` are sourced exclusively from the verified JWT claims of the requesting user, so a user supplying an arbitrary UUID as `$3` can only ever receive their own rows — a non-matching UUID returns 0 results, indistinguishable from a valid UUID with no logs.

**Performance:**
- The `inference_usage` table should have an index on `(tenant_id, principal_id, created_at DESC)` to support the primary query pattern efficiently.
- An additional index on `(tenant_id, token_id, created_at DESC)` for PAT-filtered queries.

**Observability:**
- The new list endpoint emits a structured log entry and OTel span per request, including `tenant_id`, `principal_id`, and whether a `token_id` filter was applied.

---

## Dependencies

### Internal Dependencies

- **PAT system** (`internal/identity`): PAT CRUD is implemented and stable. The PAT record UUID is already embedded in PAT-issued JWTs as `orbit.session_id`.
- **`InferenceUsageRepository`** (`internal/inferencegw`): `Create` and `List` are implemented. `List` already supports `PrincipalID` and `TenantID` filters. A new `TokenID` filter field is needed.
- **Model Gateway frontend**: Keys page at `frontend/src/app/(dashboard)/model-gateway/keys/page.tsx` is fully implemented (RSC, server-side PAT fetch, revoke/create modals). Logs page at `frontend/src/app/(dashboard)/model-gateway/logs/page.tsx` is merged with a complete table UI and mock data — server-side fetching and key-filter dropdown remain to be wired up.
- **Database migration tooling** (`goose`): Required for adding `token_id` column.

### External Dependencies

- None.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Historical `inference_usage` rows have no `token_id` (pre-dates this feature) | Medium | High | `token_id` is nullable; historical rows display "Unknown key" or are excluded from key-specific filter results. |
| `claims.Orbit.SessionID` semantics — using it as PAT record ID may confuse future session-type disambiguation | Medium | Low | Resolved in OQ-1: `claims.Orbit.SessionID` is used as-is. Usage is documented in code via a comment on the local `principal.TokenID` field. No JWT schema change required for this feature. |
| PAT list and log list are separate API calls — filter dropdown may show stale/revoked keys | Low | Medium | Revoked PATs can still have historical logs; show revoked keys in the filter with a visual indicator. |
| Large log volumes causing slow page loads | Medium | Low | Enforce server-side pagination; add database indexes as listed in NFRs. |

---

## Open Questions

1. **`token_id` attribution mechanism:** Should we read `claims.Orbit.SessionID` (PAT record UUID, already present in JWT) or add an explicit `pat_id` field to `OrbitClaims`? An explicit field is cleaner but requires a token schema change.
   - **Decision:** Use `claims.Orbit.SessionID` as-is — no JWT schema change required. The PAT record UUID is already reliably stored there at signing time. The value is extracted in `principalFromClaims` into the **inferencegw-local `principal` struct** only (a new `TokenID string` field). `token.Principal` — the platform-wide identity abstraction shared across all service binaries — is **not modified**: it is agnostic to PAT-specific semantics and must remain so. The local `principal` struct is a domain-specific value object scoped to the inference gateway; adding `TokenID` there is appropriate. For non-PAT principals (OIDC-federated humans, SPIFFE-attested agents) `TokenID` is the empty string and `inference_usage.token_id` is null.
   - **Status:** Closed

2. **Backfill strategy for `token_id`:** Historical rows without `token_id` — do we attempt a best-effort backfill using session records, or simply accept nulls?
   - **Decision:** No backfill. The raw JWT is never persisted, so there is no reliable source of truth to retroactively correlate historical usage rows to a PAT record UUID. The `token_id` column is nullable; historical rows will display a placeholder (e.g., "Unknown key") in the UI and will be excluded from key-specific filter results. This is clearly communicated in the UI rather than hidden.
   - **Status:** Closed

3. **Pagination style:** Offset-based (simple, current `List` implementation) vs. cursor-based (better for high-volume, append-only data). Given expected v1 volumes, offset is likely sufficient.
   - **Decision:** Offset-based pagination for v1. The existing `InferenceUsageRepository.List` implementation already uses limit/offset. At expected early volumes the page-drift problem (new rows inserted between page fetches shifting results) is acceptable. The implementation must not preclude migrating to cursor-based pagination later — no offset value should be hardcoded in the URL scheme or frontend.
   - **Status:** Closed

4. **Revoked PAT display in filter:** Should revoked PATs appear in the API key filter on the Logs page (since they may have historical logs), and if so how are they visually distinguished?
   - **Decision:** Revoked PATs must appear in the filter dropdown if they have associated log rows — hiding them would make historical logs for revoked keys unreachable. In the filter dropdown, active and revoked keys are separated into groups. In the log table itself, key revocation status is surfaced as a dedicated **Key Status** column (not a badge appended to the display name cell) — keeping the display name clean and the status independently scannable. The existing `PAT.RevokedAt` field is sufficient to derive status.
   - **Status:** Closed

5. **Logs page schema:** ✅ Resolved. Logs page is merged at `frontend/src/app/(dashboard)/model-gateway/logs/page.tsx`. The `LogEntry` interface in `logs-tab.tsx` confirms the required fields: `id` (request ID), `time` (timestamp string), `model`, `status` (HTTP status int), `latency` (ms), `tokensIn`, `tokensOut`, `cost` (USD float), `keyId` (PAT record UUID), `keyName` (friendly display name). This maps cleanly to `InferenceUsageRecord` once `token_id` attribution is added. The status-filter (2xx / 4xx/5xx / all) is already built. Remaining work: key-filter dropdown, `?key=<pat-id>` query param handling, server-side data fetch.
   - **Status:** Closed

6. **PAT regeneration:** There is currently no PAT regeneration capability. If added in future, the new PAT receives a new `id` and JTI — historical logs under the old PAT ID remain correctly attributed. No design change needed.
   - **Status:** Closed (no action required for this feature; tracked in Future Considerations)

---

## Timeline and Milestones

### Phases

**Phase 1: Backend attribution + API**
- Add `token_id` column to `inference_usage` (migration)
- Extract PAT record UUID from claims in `principalFromClaims` and populate `token_id` on write
- Add `TokenID` filter to `InferenceUsageFilter` and `InferenceUsageRepository.List`
- Implement `GET /v1/usage` endpoint with user-scoped enforcement

**Phase 2: Frontend**
- Implement Logs page with table and API key filter dropdown
- Wire API Keys page "View Logs" deep-link action

### Key Milestones

- [x] PRD Approved
- [x] ADR Created — [inference-usage-logs](../adr/2026-04-17-inference-usage-logs.md)
- [x] Implementation Started — Phase 1 complete (backend attribution + API)
- [x] Phase 2 Complete — frontend logs page, key filter, deep-link from API Keys page
- [ ] Alpha/Beta
- [ ] Launch

---

## Appendix

### Related Documents

- [inference-usage-logs](../adr/2026-04-17-inference-usage-logs.md) — Inference Logs architecture decision
- [repository-pattern-and-code-organization](../adr/2026-02-17-repository-pattern-and-code-organization.md) — Repository pattern and code organization

### Glossary

- **Principal:** The unified identity abstraction for all actors in kora. Every inference request, policy decision, and audit event is attributed to a Principal. A Principal has a `Type` (HUMAN, AGENT, SERVICE, SYSTEM, PAT), a stable `ID`, and a `TenantID`.
- **PAT (Personal Access Token):** A long-lived, user-issued API key used to authenticate programmatic requests to the kora Inference Gateway. PATs are prefixed `orb_` and are never stored in plaintext — only a SHA-256 hash is persisted alongside the PAT record UUID, user ID, display name, and expiry.
- **JWT (JSON Web Token):** The signed token format used for all kora platform tokens. PATs are issued as RS256-signed JWTs carrying `PlatformClaims`. The raw JWT is what a caller presents in the `Authorization: Bearer` or `x-api-key` header.
- **JTI (JWT ID):** The unique identifier embedded in a JWT's `jti` registered claim. For PATs, the JTI is a UUID generated at creation time and stored on the PAT record. It is used to revoke in-flight tokens via a Redis blocklist. The JTI is distinct from the PAT record UUID.
- **`inference_usage`:** The Postgres table that records one row per inference request processed by the kora Inference Gateway. Fields include: model, provider, token counts, estimated cost (USD), latency (ms), status, and principal attribution.
- **`token_id`:** A proposed new column on `inference_usage` that stores the PAT record UUID when a request is authenticated via a PAT. Null for non-PAT principals (human sessions, agents). Enables per-key log attribution and filtering.
- **`orbit.session_id` claim:** The `OrbitClaims.SessionID` field embedded in every platform JWT. For PAT-issued tokens, this value is the PAT record UUID (set at signing time). For human sessions, it is the session record UUID.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
