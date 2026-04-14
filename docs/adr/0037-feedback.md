# ADR-0037 Implementation Review

**Reviewer:** Claude (automated review)
**Date:** 2026-04-14
**Branch:** `feat/policy-frontend`

---

## Overall Assessment

The implementation is faithful to the ADR. The end-to-end flow — Rego rule, Go engine/handler, BFF session enrichment, proxy-based refresh, React context, sidebar filtering — is all present and consistent with the design doc. The code is clean, well-commented, and follows existing patterns in the codebase. Below are findings grouped by severity.

---

## Issues

### 1. Duplicate session fetch — sidebar + permissions provider (Medium)

`PermissionsProvider` (`permissions-provider.tsx:36-48`) fetches `GET /api/auth/session` on mount to read `permissions.scopes`. `DashboardSidebar` (`dashboard-sidebar.tsx:27-37`) independently fetches the same endpoint on mount to read `user.email` and `user.name`. This means every dashboard page load makes two identical requests to the session endpoint.

**Recommendation:** Either lift the user data into the `PermissionsProvider` context (it already gets the full session response) and have the sidebar consume it from context, or consolidate into a single `SessionProvider` that exposes both user identity and permissions. This eliminates a redundant network call and a potential race where the sidebar shows "Loading..." while permissions have already resolved (or vice versa).

### 2. `evaluatedAt` from the ADR response is ignored (Low)

The policy gateway returns `evaluated_at` in the `ScopesResponse`, but `fetchPermissions` in `route.ts:124` and `tryRefreshScopes` in `proxy.ts:91` both discard it and use `Date.now()` instead. The ADR design (lines 130-131, 168) shows `evaluatedAt` in the session, and the code does store it — but it's always the BFF's clock, not the policy gateway's evaluation timestamp.

This is a minor semantic inconsistency. In practice the two clocks are close enough that it doesn't matter today, but if you ever need to debug staleness ("when did OPA actually evaluate this?"), the BFF timestamp won't answer that question. Consider using `data.evaluated_at` from the response when available, falling back to `Date.now()`.

### 3. Scope gating is only applied to one nav entry (Low)

Only `Policy Engine` in `SECURITY_NAV` has `requiredScope: 'platform:grants:read'`. All other nav entries — including Model Gateway pages (which presumably require inference scopes) and Identity — have no scope gate. The ADR mentions (line 88): "the sidebar needs to know visibility for ~10 nav entries". The infrastructure is in place but the actual scope assignments are minimal.

This is likely intentional for an incremental rollout (many pages are still `wip: true`), but worth noting that the permission model won't provide real value until more entries are gated. Consider adding a follow-up task to assign `requiredScope` to the remaining nav entries as pages come online.

### 4. No page-level guard implemented yet (Low)

The ADR describes server-component page guards (lines 248-256) where a layout checks scopes and renders a `<ForbiddenPage />` for unauthorized users. No layout or page in the diff implements this pattern. The sidebar hides entries, but a user who navigates directly to a URL (bookmark, shared link) would still see the page content — the backend would reject API calls, but the page shell would render.

This is an advisory-only concern (the backend is authoritative), but it's called out in the ADR as part of the design. Consider implementing the guard for at least the policy admin pages in this PR, or noting it as a follow-up.

### 5. No `hasScope`/`hasAnyScope` usage in actual components (Low)

`lib/permissions.ts` exports `hasScope`, `hasAnyScope`, and `hasAllScopes`, but no component in the diff uses them. The sidebar does `scopes.includes(entry.requiredScope)` directly. The helpers are fine to have, but they're dead code today. The ADR shows them being used (lines 273-279), so this is presumably staged for future component work.

### 6. `PermissionsProvider` renders children before scopes load (Low)

`PermissionsProvider` renders children immediately with `scopes: [], loaded: false`. The sidebar checks `scopes.includes(...)`, which will be false for everything during the loading window — all scope-gated entries will flash hidden then appear once the fetch completes. For the current single gated entry this is barely noticeable, but as more entries get gated it could cause visible layout shift.

**Options:**
- Show a skeleton/spinner while `loaded === false`
- Provide initial scopes from the server component (the session is available server-side) to avoid the client fetch entirely — the `PermissionsProvider` could accept `initialScopes` as a prop from the layout's `getSession()` call

### 7. Bundle parsed twice for scopes query (Nit)

In `engine.go:155-166`, the bundle bytes are parsed a second time (`bundle.NewReader`) because "OPA's `rego.New` consumes the parsed bundle." The comment explains this, and it only happens on bundle load (not per-request), so the performance impact is negligible. But it's worth confirming this is a real OPA constraint and not just a reference issue — if `rego.New` doesn't actually mutate the `bundle.Bundle` struct, you could reuse `b` directly.

---

## Positives

- **Fail-closed on login, fail-open on refresh** — the asymmetric failure strategy in `route.ts:106-131` (empty scopes on login failure) vs `proxy.ts:170-172` (preserve existing scopes on refresh failure) is exactly right. New sessions start locked down; established sessions degrade gracefully.

- **Rego rule is clean and correct** — `principal_all_scopes` (`authz.rego:337-349`) collects scopes from both grants and role mappings using partial set rules, which naturally deduplicates. It reuses the same `_principal_type`/`_principal_id`/`_principal_roles` normalization as the allow rules, so there's zero divergence between what the allow rules check and what the scopes endpoint reports.

- **Thorough Rego tests** — Six test cases (`authz_test.go:310-404`) covering agent grants, no grants, role-only, grants+role union, custom roles, and wrong-principal isolation. The `compileAndPrepareScopesQuery` helper mirrors the real engine's query compilation path.

- **Auth middleware on scopes endpoint** — The `/scopes` route (`controller.go:43-45`) correctly uses `HandleWithMiddleware` with `AuthMiddleware` rather than trusting a body-supplied token like `/check` and `/query` do. This matches the ADR design where the principal comes from the `Authorization` header.

- **AbortSignal.timeout on all fetch calls** — Both `fetchPermissions` (5s) and `tryRefreshScopes` (5s) have timeouts, preventing a slow/hung policy gateway from blocking page loads or token refreshes.

- **Session cookie growth is bounded** — The scope set is `string[]` sealed into the iron-session cookie. For typical Orbit deployments (10-30 scopes), this adds well under 1KB to the cookie, consistent with the ADR's analysis.

---

## Checklist vs. ADR Implementation Table

| ADR Item | Status | Notes |
|---|---|---|
| `handlers_scopes.go` — GET /v1/policy/scopes | Done | Clean handler, nil-coalesces to `[]string{}` |
| `lib/permissions.ts` — hasScope, hasAnyScope | Done | Also adds `hasAllScopes` (not in ADR, fine) |
| `permissions-context.tsx` → `permissions-provider.tsx` | Done | File renamed from ADR's `contexts/` to `_components/`, acceptable |
| `authz.rego` — `principal_all_scopes` rule | Done | Correct partial set rule |
| `controller.go` — mount /scopes route | Done | With auth middleware |
| `models.go` — ScopesResponse type | Done | |
| `auth-config.ts` — SessionPayload + permissions | Done | |
| `route.ts` — fetch scopes on session creation | Done | Fail-closed |
| `proxy.ts` — refresh scopes on token renewal | Done | Fail-open (preserve existing) |
| `nav-entries.ts` — requiredScope field | Done | Only one entry uses it |
| `dashboard-sidebar.tsx` — filter by permission | Done | |

---

## Summary

The implementation is solid and ready for review. The main actionable item is the duplicate session fetch (#1). The remaining items are either incremental rollout gaps (expected) or minor improvements that can land in follow-up work.
