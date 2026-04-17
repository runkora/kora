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

  /**
   * `/api/auth/discover?email=user@example.com`
   *
   * GET — Home Realm Discovery (ADR-0031). Resolves the OIDC tenant config
   *       for a given email address by matching the email domain against
   *       registered tenant email domains.
   *
   * Returns `{ tenant_id, issuer_url, client_id, scopes }` — never the client secret.
   */
  discover: {
    /**
     * The bare path without query parameters — use this in `PUBLIC_PATHS`
     * because the middleware matches `pathname` (no query string) against the
     * set. `GET(email)` cannot be used there since it always includes `?email=`.
     */
    PATH: (): `${typeof AUTH_BASE}/discover` => `${AUTH_BASE}/discover`,
    GET: (email: string): `${typeof AUTH_BASE}/discover?email=${string}` =>
      `${AUTH_BASE}/discover?email=${encodeURIComponent(email)}`,
  },

  /**
   * `/api/models`
   *
   * GET — proxies to the Go inference gateway `GET /v1/models` and returns
   *       the full list of available models. Requires an authenticated session.
   */
  models: {
    GET: (): `${typeof API_BASE}/models` => `${API_BASE}/models`,
  },

  /**
   * `/api/policy/stats`
   *
   * GET — proxies to the policy-service `GET /api/v1/policy/stats`.
   */
  policyStats: {
    GET: (): `${typeof API_BASE}/policy/stats` => `${API_BASE}/policy/stats`,
  },

  /**
   * `/api/policy/grants`
   *
   * GET  — list grants for the caller's tenant.
   * POST — create a new grant.
   */
  policyGrants: {
    GET: (): `${typeof API_BASE}/policy/grants` => `${API_BASE}/policy/grants`,
    POST: (): `${typeof API_BASE}/policy/grants` => `${API_BASE}/policy/grants`,
  },

  /**
   * `/api/policy/grants/[id]`
   *
   * DELETE — revoke a grant by ID.
   */
  policyGrant: {
    DELETE: (id: string): `${typeof API_BASE}/policy/grants/${string}` =>
      `${API_BASE}/policy/grants/${id}`,
  },

  /**
   * `/api/policy/roles`
   *
   * GET — list role→scope mappings for the caller's tenant.
   * PUT — create or replace a role→scope mapping.
   */
  policyRoles: {
    GET: (): `${typeof API_BASE}/policy/roles` => `${API_BASE}/policy/roles`,
    PUT: (): `${typeof API_BASE}/policy/roles` => `${API_BASE}/policy/roles`,
  },

  /**
   * `/api/policy/roles/[role]`
   *
   * DELETE — remove a role→scope mapping.
   */
  policyRole: {
    DELETE: (role: string): `${typeof API_BASE}/policy/roles/${string}` =>
      `${API_BASE}/policy/roles/${encodeURIComponent(role)}`,
  },

  /**
   * `/api/policy/vocabulary`
   *
   * GET — list scope vocabulary with optional usage counts.
   */
  policyVocabulary: {
    GET: (): `${typeof API_BASE}/policy/vocabulary` => `${API_BASE}/policy/vocabulary`,
  },

  /**
   * `/api/policy/routes`
   *
   * GET — list all registered route→scope mappings.
   */
  policyRoutes: {
    GET: (): `${typeof API_BASE}/policy/routes` => `${API_BASE}/policy/routes`,
  },

  /**
   * `/api/policy/evaluate/explain`
   *
   * GET — scope explain for a principal (proxies to policy-gateway).
   */
  policyExplain: {
    GET: (): `${typeof API_BASE}/policy/evaluate/explain` => `${API_BASE}/policy/evaluate/explain`,
  },

  /**
   * `/api/policy/evaluate/simulate`
   *
   * POST — simulate a policy check (proxies to policy-gateway).
   */
  policySimulate: {
    POST: (): `${typeof API_BASE}/policy/evaluate/simulate` =>
      `${API_BASE}/policy/evaluate/simulate`,
  },

  /**
   * `/api/usage`
   *
   * GET — proxies to the Go inference gateway `GET /v1/usage` and returns
   *       inference usage records for the authenticated user. Accepts an
   *       optional `key` query param to filter by a specific API key (PAT UUID)
   *       and an optional `cursor` for cursor-based pagination.
   */
  usage: {
    GET: (params?: { key?: string; cursor?: string }): string => {
      const qs = new URLSearchParams();
      if (params?.key) qs.set('key', params.key);
      if (params?.cursor) qs.set('cursor', params.cursor);
      const q = qs.toString();
      return q ? `${API_BASE}/usage?${q}` : `${API_BASE}/usage`;
    },
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
  | ReturnType<(typeof ApiRoute)['session']['GET']>
  | ReturnType<(typeof ApiRoute)['discover']['PATH']>
  | `${typeof AUTH_BASE}/discover?email=${string}`
  | ReturnType<(typeof ApiRoute)['models']['GET']>
  | ReturnType<(typeof ApiRoute)['policyStats']['GET']>
  | ReturnType<(typeof ApiRoute)['policyGrants']['GET']>
  | ReturnType<(typeof ApiRoute)['policyGrants']['POST']>
  | ReturnType<(typeof ApiRoute)['policyGrant']['DELETE']>
  | ReturnType<(typeof ApiRoute)['policyRoles']['GET']>
  | ReturnType<(typeof ApiRoute)['policyRoles']['PUT']>
  | ReturnType<(typeof ApiRoute)['policyRole']['DELETE']>
  | ReturnType<(typeof ApiRoute)['policyVocabulary']['GET']>
  | ReturnType<(typeof ApiRoute)['policyRoutes']['GET']>
  | ReturnType<(typeof ApiRoute)['policyExplain']['GET']>
  | ReturnType<(typeof ApiRoute)['policySimulate']['POST']>
  | ReturnType<(typeof ApiRoute)['usage']['GET']>;
