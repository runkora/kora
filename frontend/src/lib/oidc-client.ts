/**
 * OIDC client factory — browser-side only.
 *
 * Replaces the previous env-var singleton with a per-tenant factory function.
 * Tenant OIDC configuration (issuer URL, client ID, scopes) is resolved at
 * runtime via the Home Realm Discovery endpoint (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) and handed to this
 * factory by the login page before initiating the PKCE redirect.
 *
 * The factory is safe to import from Client Components and callback pages.
 * It must NOT be imported from Server Components, Route Handlers, or middleware
 * (those paths are server-only and have no window/storage context).
 *
 * Session handoff:
 *   The login page writes the resolved TenantOidcConfig to sessionStorage under
 *   TENANT_CONFIG_STORAGE_KEY immediately before calling signinRedirect(). The
 *   callback page reads it back to reconstruct the UserManager for the
 *   signinRedirectCallback() call — PKCE verifier and state are already in
 *   sessionStorage written by oidc-client-ts.
 */
import { UserManager, type UserManagerSettings } from 'oidc-client-ts';

// ── Types ─────────────────────────────────────────────────────────────────────

/** The subset of tenant OIDC config needed to drive the browser-side PKCE flow. */
export interface TenantOidcConfig {
  tenantId: string;
  issuerUrl: string;
  clientId: string;
  /** Scopes to request, e.g. ["openid", "profile", "email"]. */
  scopes: string[];
}

// ── Storage key ───────────────────────────────────────────────────────────────

/**
 * sessionStorage key under which the login page persists the resolved tenant
 * OIDC config for the callback page to read back after the provider round-trip.
 * Stored in sessionStorage (not localStorage) so it is scoped to the tab and
 * cleared automatically when the tab is closed.
 */
export const TENANT_CONFIG_STORAGE_KEY = 'orbit_tenant_config';

// ── Factory ───────────────────────────────────────────────────────────────────

/**
 * Constructs a new `UserManager` for the given tenant OIDC configuration.
 *
 * A new instance is created each time — callers are responsible for reuse if
 * needed (in practice the login page creates one to initiate the redirect, and
 * the callback page creates another to complete it; both paths are one-shot).
 */
export function buildUserManager(cfg: TenantOidcConfig): UserManager {
  const redirect_uri =
    typeof window !== 'undefined' ? `${window.location.origin}/auth/callback` : '';

  const settings: UserManagerSettings = {
    authority: cfg.issuerUrl,
    client_id: cfg.clientId,
    redirect_uri,
    scope: cfg.scopes.join(' '),
    // Store PKCE verifier and state in sessionStorage so they survive the
    // provider redirect but are cleared when the tab is closed.
    userStore: undefined, // default: sessionStorage via WebStorageStateStore
    // Token refresh is handled by the BFF using the kora opaque refresh token.
    automaticSilentRenew: false,
    // We only consume the id_token (handed to the BFF); the access token
    // issued by the IdP is intentionally discarded.
    loadUserInfo: false,
  };

  return new UserManager(settings);
}
