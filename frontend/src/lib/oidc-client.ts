/**
 * OIDC client singleton — browser-side only.
 *
 * Wraps `oidc-client-ts` UserManager with the project's provider configuration
 * sourced from `NEXT_PUBLIC_*` environment variables. The module is safe to
 * import from Client Components and callback pages; it must NOT be imported
 * from Server Components, Route Handlers, or middleware (those paths are
 * server-only and have no window/storage context).
 *
 * Configuration:
 *   ORBIT_OIDC_ISSUER_URL          — e.g. http://dex.orbit.local:5556/dex
 *   ORBIT_OIDC_CLIENT_ID           — e.g. orbit-local
 *   NEXT_PUBLIC_OIDC_REDIRECT_URI  — e.g. http://localhost:3000/auth/callback
 *
 * ORBIT_OIDC_ISSUER_URL and ORBIT_OIDC_CLIENT_ID are shared with the backend
 * and forwarded to the browser bundle via the `env` block in next.config.ts.
 * NEXT_PUBLIC_OIDC_REDIRECT_URI remains frontend-specific because the redirect
 * path differs from the backend's ORBIT_OIDC_REDIRECT_URI.
 *
 * The client is a public client (no secret). PKCE (S256) is used automatically
 * by oidc-client-ts for all authorization code flows.
 */
import { UserManager, type UserManagerSettings } from 'oidc-client-ts';

function buildSettings(): UserManagerSettings {
  const authority = process.env.ORBIT_OIDC_ISSUER_URL;
  const client_id = process.env.ORBIT_OIDC_CLIENT_ID;
  const redirect_uri = process.env.NEXT_PUBLIC_OIDC_REDIRECT_URI;

  if (!authority || !client_id || !redirect_uri) {
    throw new Error(
      'Missing OIDC configuration. Ensure ORBIT_OIDC_ISSUER_URL, ' +
        'ORBIT_OIDC_CLIENT_ID, and NEXT_PUBLIC_OIDC_REDIRECT_URI are set.',
    );
  }

  return {
    authority,
    client_id,
    redirect_uri,
    // Request openid + profile for name/preferred_username claims, email for the
    // email claim. The identity provider must expose these in the id_token.
    scope: 'openid profile email',
    // Store PKCE verifier and state in sessionStorage so they survive the
    // provider redirect but are cleared when the tab is closed.
    userStore: undefined, // default: sessionStorage via WebStorageStateStore
    // Do not automatically renew tokens via silent refresh — the BFF handles
    // token refresh using the Orbit opaque refresh token.
    automaticSilentRenew: false,
    // We only consume the id_token (handed to the BFF); the access token
    // issued by Dex is intentionally discarded.
    loadUserInfo: false,
  };
}

// Lazily initialised so that the module can be imported at the top level in
// Client Components without throwing on the server (where window is absent).
let _userManager: UserManager | null = null;

export function getUserManager(): UserManager {
  if (!_userManager) {
    _userManager = new UserManager(buildSettings());
  }
  return _userManager;
}
