/**
 * api-routes.ts
 *
 * Single source of truth for every BFF (Backend-for-Frontend) API route in
 * the Orbit app.
 *
 * Each entry is keyed by route name, then by HTTP method. Methods are
 * functions so that routes with optional query parameters can be built
 * type-safely at the call site rather than via string concatenation.
 *
 * Add a new entry here when adding a new BFF route handler under
 * `src/app/api/`. Removing an entry will produce a compile error at every
 * call site that references the deleted route.
 *
 * @example
 *   import { ApiRoute } from '@core/routing/api-routes';
 *
 *   // With optional parameter
 *   <form action={ApiRoute.login.POST({ next: '/dashboard' })}>
 *
 *   // Without parameter
 *   <form action={ApiRoute.logout.POST()}>
 */

// ── Auth routes ───────────────────────────────────────────────────────────────

const AUTH_BASE = '/api/auth' as const;

export const ApiRoute = {
  /**
   * `/api/auth/login`
   *
   * Initiates the OIDC Authorization Code flow with PKCE. The BFF calls the
   * Go identity service, stores a short-lived `orbit_next` cookie, and issues
   * a 302 redirect to the provider's authorization endpoint.
   */
  login: {
    /**
     * @param params.next - Optional post-login redirect path. Must be a safe
     *   relative path; absolute URLs are rejected server-side.
     */
    POST: (params?: { next?: string }): string => {
      if (params?.next) {
        return `${AUTH_BASE}/login?next=${encodeURIComponent(params.next)}`;
      }
      return `${AUTH_BASE}/login`;
    },
  },

  /**
   * `/api/auth/logout`
   *
   * Revokes the Orbit tokens server-side (best-effort) and destroys the
   * iron-session cookie, then redirects to `/login`.
   */
  logout: {
    POST: (): `${typeof AUTH_BASE}/logout` => `${AUTH_BASE}/logout`,
  },

  /**
   * `/api/auth/callback`
   *
   * OIDC Authorization Code callback. Called by the identity provider after
   * the user authenticates — not typically constructed manually.
   */
  callback: {
    GET: (): `${typeof AUTH_BASE}/callback` => `${AUTH_BASE}/callback`,
  },

  /**
   * `/api/auth/session`
   *
   * Returns the public (non-sensitive) portion of the current session for
   * client components that need to know who is logged in.
   */
  session: {
    GET: (): `${typeof AUTH_BASE}/session` => `${AUTH_BASE}/session`,
  },
} as const;

/**
 * The union of all BFF API route path strings producible by `ApiRoute`.
 *
 * Use this type when you need to accept or store an API route value outside
 * of a direct `ApiRoute.*.*()` call.
 */
export type ApiRoutePath =
  | ReturnType<(typeof ApiRoute)['login']['POST']>
  | ReturnType<(typeof ApiRoute)['logout']['POST']>
  | ReturnType<(typeof ApiRoute)['callback']['GET']>
  | ReturnType<(typeof ApiRoute)['session']['GET']>;
