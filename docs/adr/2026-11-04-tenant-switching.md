# Tenant Switching

<!-- Filename: 2026-11-04-tenant-switching.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-11-04
**Deciders:** Justin Wilkin
**Technical Story:** [Tenant Switching PRD](../prd/tenant-switching.md). Amends [2026-10-26: Tenant Claim Binding and Shared Email Domains](2026-10-26-tenant-claim-binding-and-shared-email-domains.md).

---

## Context

[Tenant Claim Binding and Shared Email Domains](2026-10-26-tenant-claim-binding-and-shared-email-domains.md) lets several tenants share an email domain and binds membership to an ID token claim through `spec.oidc.claimBinding`. A person whose claims match several tenants cannot sign in. `bindTenant` in `backend/internal/identity/service.go` returns `ErrMultipleTenantsMatch`, and `handlers.go` maps it to `409 multiple_tenants_match` with the tenant IDs in the message. Browser sign-in (`ExchangeIDToken`) and the device flow (`PollDeviceAuth`) both call `bindTenant`.

The rest of the platform follows the tenant in the token. Downstream services read `kora.tenant_id`. Sessions, profiles, preferences and PATs are keyed by user and tenant. `RefreshSession` re-mints from the stored session row and does not re-check the binding. `handleCreateOAuthCode` in `handlers_oauth.go` mints MCP authorization codes for the tenant of the BFF session. The tenant registry carries a `displayName` per tenant, and `tenantEntry` in `tenant_registry.go` reads it, but identity does not keep it.

The browser completes PKCE in `frontend/src/app/auth/callback/page.tsx` and posts the ID token to `frontend/src/app/api/auth/session/route.ts`. The BFF seals the kora tokens into an iron-session cookie defined in `frontend/src/server/auth-config.ts`. `scripts/auth-device.sh` already takes `--tenant-id`.

This decision touches **Who is this agent?** The tenant in a human session token, and in every MCP token minted from that session, is chosen from the tenants that fresh verified claims match. For a bound tenant, a tenant hint from the browser or the CLI selects among those matches and never adds one. An unbound tenant keeps issuer-only membership. It touches **How does it connect?** An MCP client acts in the tenant the person sees on the consent page. **What can it access?** is unchanged: policy keeps following `kora.tenant_id`.

### Problem Statement

Let a person who matches several tenants sign in, pick a tenant, and switch tenants later, with membership proven by the identity provider on every switch.

### Goals

- Sign-in succeeds for a person who matches several tenants.
- A signed-in person switches tenant from the dashboard, the CLI and the MCP consent page.
- A switch issues a session only for a tenant that fresh claims match.

### Non-Goals

- Membership stored or managed in kora.
- Several live sessions in one browser.
- Switching for agent, service or PAT principals.

---

## Decision Drivers

- The identity provider is the authority on membership. A stored list must not grant a tenant.
- The switcher needs a list to render without a round trip to the identity provider.

---

## Considered Options

### Option 1 (rejected): Stored eligible set only

Identity records the tenants a token matches at sign-in. A switch mints a session for any tenant in that set without contacting the identity provider. A person removed from a group after sign-in could still switch into that tenant until the session ends. The set would grant membership.

### Option 2 (rejected): Silent re-authentication only

Each switch proves membership with a fresh `prompt=none` sign-in. Nothing records the matching tenants. The switcher has no list to render without a sign-in round trip each time it opens.

### Option 3 (chosen): Stored set for display and silent re-authentication to authorise

Identity returns the matching tenants at sign-in, and the BFF keeps them for display. A switch runs a silent identity provider sign-in with the target tenant as a hint. Identity issues the session only when the fresh claims match the target, and the same sign-in returns a fresh set.

---

## Decision

**Chosen Option:** Option 3, stored set for display and silent re-authentication to authorise.

### Rules

- **Eligible set:** The eligible set of a verified ID token is the set of tenants that `bindTenant` matches for it. These are the bound tenants on the issuer and client of the `tenant_id` tenant whose `emailDomains` include the token's email domain and whose `claimBinding` matches the claims. For a token without an `email` claim, the set holds at most the `tenant_id` tenant. For an unbound `tenant_id` tenant, the set is that tenant. The eligible set is display data and grants nothing.
- **Display names:** Identity reads `displayName` from the tenant registry and keeps it with the tenant's OIDC state.
- **Last-used tenant:** The BFF sets a browser cookie with the tenant ID on every successful sign-in and switch. The cookie is a hint and grants nothing.

### Exchange Request

The `oidc_id_token` exchange at `POST /identity/session` and the device poll accept these fields:

- `tenant_id` selects the tenant whose issuer and client verify the ID token. Its meaning is unchanged.
- `tenant_hint` is optional and names the tenant the person wants among several matches.
- `replaces_refresh_token` is optional and applies to the `oidc_id_token` exchange only. Its presence marks the exchange as a switch, and it carries the refresh token of the session the switch replaces. The device poll ignores it.

### Binding

- No match returns `403` as today.
- One match issues the session for that tenant whatever the hint names.
- With several matches and the hint among them, identity issues the session for the hinted tenant.
- With several matches and no hint, or a hint outside them, identity returns `409 multiple_tenants_match`.
- A hint that names an unknown tenant is ignored and is not an error.

The `409` body carries the matches in its details:

```json
{
  "code": "multiple_tenants_match",
  "message": "the account matches several tenants",
  "details": {
    "tenants": [
      { "id": "kora-tenant-example", "display_name": "Example Platform" },
      { "id": "kora-tenant-example-data", "display_name": "Example Data" }
    ]
  }
}
```

The session response for a federated sign-in carries the eligible set as `tenants: [{id, display_name}]`. The BFF keeps the list in the sealed session cookie. No database column stores it.

### Sign-in

1.  The callback page completes PKCE and posts the ID token to the BFF. `tenant_id` comes from discovery through the PKCE state. `tenant_hint` is the last-used cookie, and it is absent when there is no cookie.
2.  When identity returns a session, the BFF seals it with the eligible set and sets the last-used cookie.
3.  When identity returns `409 multiple_tenants_match`, the BFF passes the matches to the callback page, and the page shows a picker with the display names. A first sign-in sends no hint and reaches the picker whenever several tenants match.
4.  The person picks a tenant. The page re-posts the same ID token with the same `tenant_id` and the chosen tenant as `tenant_hint`. There is no second identity provider redirect.

### Switch

1.  The person chooses a tenant from the eligible set in the dashboard switcher.
2.  The browser makes a full-page redirect to the identity provider with `prompt=none` and `login_hint` set to the person's email. Every tenant in the eligible set shares the current tenant's issuer and client, and the redirect uses them. The PKCE state carries the target tenant and the return path.
3.  With the new ID token, the BFF calls the exchange with the current tenant as `tenant_id`, the target as `tenant_hint` and the session's refresh token as `replaces_refresh_token`.
4.  Identity runs two checks before any write. The bound tenant must equal the hint. `replaces_refresh_token` must be an unexpired, unrevoked refresh token of an unrevoked session that belongs to the same principal subject and issuer. When either check fails, identity returns `403 tenant_switch_denied` and creates and revokes nothing. Its details carry the fresh eligible set as `tenants: [{id, display_name}]`. The set is empty when nothing matches.
5.  When both checks pass, identity creates the new session first. It then revokes the replaced session and all its refresh tokens and blocklists the replaced access token. It writes the switch log entry last.
6.  After a switch succeeds, the BFF swaps in the new session and returns to the path. On `403 tenant_switch_denied` it keeps the current session, shows that the switch was denied, and stores the eligible set from the details.
7.  When the identity provider answers `login_required`, `interaction_required`, `consent_required` or `account_selection_required`, the browser starts an interactive sign-in whose state carries the same target tenant and return path.

### Device Flow

`scripts/auth-device.sh` sends `--tenant-id` as `tenant_id` and as `tenant_hint` on the device poll. On `409 multiple_tenants_match` the script prints the matching tenants with their display names and asks for a re-run with `--tenant-id`.

### MCP Consent

The consent page under `frontend/src/app/(auth)/mcp/authorize/` shows the display name of the active tenant. When the eligible set has more than one tenant, the page offers a switch that runs the switch flow with the consent URL as the return path. `handleCreateOAuthCode` keeps minting codes for the tenant of the BFF session. The consent CSRF token in `frontend/src/server/mcp-consent.ts` binds the tenant shown on the page as well as the email. A decision submitted after the session tenant changed fails the token check.

### Audit

Identity writes one structured log entry for each successful switch inside the exchange. The entry carries the principal subject, the from-tenant of the replaced session and the to-tenant.

---

## Consequences

### Positive

- A person in several tenants signs in and works in each without signing out.
- A hint from the browser, the CLI or the consent page cannot add a bound tenant the fresh claims do not match.
- An MCP client's tenant is visible before consent.

### Negative

- Every switch needs a live identity provider session or an interactive sign-in.
- The eligible set in the cookie is as old as the last sign-in or switch. A tenant the person joined later appears only after the next sign-in or switch.
- A browser cannot hold two tenants at once. A switch in one tab moves every tab.
- A last-used tenant the person no longer matches costs one picker step at sign-in.
- Two concurrent switches with the same refresh token can both succeed. Each new session comes from fresh claims for the same person.

### Neutral

- Refresh still re-mints from the stored session without re-checking the binding.
- MCP tokens and PATs issued for a tenant stay valid after the person switches away from it.

---

## Amends 2026-10-26

[2026-10-26](2026-10-26-tenant-claim-binding-and-shared-email-domains.md) keeps Option 1 and its binding, discovery and overlap rules. This decision changes the following parts:

- **Non-goal "A tenant picker or switcher":** The picker and the switcher exist.
- **Sign-in rule "Several matches fail with an error that lists the tenants":** The binding rule in this decision replaces it. A `tenant_hint` chooses among several matches.
- **Negative consequence "A person in the groups of several tenants cannot sign in until the picker exists":** The person signs in to the last-used tenant or picks one.

---

## Related Decisions

- [home-realm-discovery-and-email-based-tenant-routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) - Email domain routing
- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) - Browser PKCE and the BFF session cookie
- [mcp-oauth-authentication](2026-06-29-mcp-oauth-authentication.md) - MCP authorization codes and consent

**Last Updated:** 2026-11-04
