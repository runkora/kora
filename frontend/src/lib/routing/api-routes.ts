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
 *   import { ApiRoute } from '@orbit/lib/routing';
 *
 *   // Without parameter
 *   fetch(ApiRoute.logout.POST(), { method: 'POST' });
 *
 *   // With body
 *   fetch(ApiRoute.session.POST(), { method: 'POST', body: JSON.stringify({ id_token }) });
 */

// ── Base segments ─────────────────────────────────────────────────────────────

const AUTH_BASE = '/api/auth' as const;
const API_BASE = '/api' as const;

// ── Route registry ────────────────────────────────────────────────────────────

export const ApiRoute = {
  /**
   * `/api/health`
   *
   * Reachable without a session — used by monitoring and load-balancer health
   * checks that arrive through the Istio gateway.
   */
  health: {
    GET: (): `${typeof API_BASE}/health` => `${API_BASE}/health`,
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
   * `/api/auth/session`
   *
   * POST — called by the OIDC callback page with `{ id_token }` to exchange
   *        the provider id_token for an Orbit session cookie.
   *
   * GET  — returns the public (non-sensitive) portion of the current session
   *        for client components that need to know who is logged in.
   */
  session: {
    POST: (): `${typeof AUTH_BASE}/session` => `${AUTH_BASE}/session`,
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
  | ReturnType<(typeof ApiRoute)['health']['GET']>
  | ReturnType<(typeof ApiRoute)['logout']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['GET']>;
