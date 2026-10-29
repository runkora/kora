/**
 * routes.ts
 *
 * Single source of truth for every internal page route in the kora app.
 *
 * Add a new entry here when adding a new page. Removing an entry here
 * will produce a compile error at every `AppLink` call site that
 * references the deleted route — dead links cannot be silently introduced.
 *
 * Import the `AppRoute` type when you need to accept or store a route
 * value outside of an `AppLink` (e.g. in a `redirect()` call or a
 * nav config array).
 *
 * @example
 *   import { APP_ROUTES, type AppRoute } from '@orbit/lib/routing';
 */

import { ApiRoute } from './api-routes';

// ── Page routes ───────────────────────────────────────────────────────────────

export const APP_ROUTES = {
  home: '/',
  login: '/login',
  chat: '/chat',
  /**
   * OIDC authorization code callback — registered as the `redirect_uri` for
   * the browser-side OIDC client. Not navigated to directly; listed here so
   * it can be referenced in `PUBLIC_PATHS` without a bare string literal.
   */
  authCallback: '/auth/callback',

  // ── Control Plane ────────────────────────────────────────────────────────
  agents: {
    list: '/agents' as const,
    /** Opens the agent builder modal over the agents list (?agent=new). */
    new: '/agents?agent=new' as const,
    detail: (slug: string) => `/agents/${slug}` as const,
    run: (slug: string, runId: string) => `/agents/${slug}/runs/${runId}` as const,
  },
  connections: '/connections',
  environments: '/environments',
  triggers: '/triggers',
  mcpGateway: {
    servers: '/mcp-gateway',
    server: (slug: string) => `/mcp-gateway/${slug}` as const,
  },
  marketplace: {
    overview: '/marketplace',
    browse: (marketplaceSlug: string) =>
      `/marketplace/${encodeURIComponent(marketplaceSlug)}` as const,
    detail: (marketplaceSlug: string, skillName: string) =>
      `/marketplace/${encodeURIComponent(marketplaceSlug)}/${encodeURIComponent(skillName)}` as const,
  },

  // ── Policy ───────────────────────────────────────────────────────────────
  policy: {
    overview: '/policy/overview',
    grants: '/policy/grants',
    roles: '/policy/roles',
    vocabulary: '/policy/vocabulary',
    routes: '/policy/routes',
    evaluate: '/policy/evaluate',
  },
  identity: '/identity',

  // ── Account ─────────────────────────────────────────────────────────────
  profile: '/profile',

  // ── System ───────────────────────────────────────────────────────────────
  storage: '/storage',
  settings: '/settings',

  // ── Model Gateway ─────────────────────────────────────────────────────────
  modelGateway: {
    overview: '/model-gateway/overview',
    models: '/model-gateway/models',
    catalog: '/model-gateway/catalog',
    myUsage: '/model-gateway/my-usage',
    analytics: '/model-gateway/analytics',
    analyticsPrincipal: (id: string) =>
      `/model-gateway/analytics/principals/${encodeURIComponent(id)}` as const,
    budgets: '/model-gateway/budgets',
    logs: '/model-gateway/logs',
    keys: '/model-gateway/keys',
    setup: '/model-gateway/setup',
  },
} as const;

/**
 * Recursively extracts every leaf string value from a nested const object.
 * Used so that `AppRoute` covers both flat and grouped route entries.
 */
type LeafRoutes<T> = T extends string
  ? T
  : T extends Record<string, unknown>
    ? { [K in keyof T]: LeafRoutes<T[K]> }[keyof T]
    : never;

/**
 * Every valid internal page route in the kora app.
 *
 * This is a compile-time exhaustive union — passing a string that is not
 * in `APP_ROUTES` to an `AppLink` is a TypeScript error.
 */
export type AppRoute = LeafRoutes<typeof APP_ROUTES>;

// ── Public paths ──────────────────────────────────────────────────────────────

/**
 * Paths that do not require an authenticated session.
 *
 * Consumed by the Edge middleware (`proxy.ts`) to skip the session check.
 * Both page routes and BFF API routes are included so the middleware lets
 * the auth flow through end-to-end without interruption.
 */
export const PUBLIC_PATHS: ReadonlySet<string> = new Set([
  APP_ROUTES.login,
  // OIDC callback page — receives the code/state from the provider and
  // completes the browser-side token exchange before posting to the BFF.
  APP_ROUTES.authCallback,
  // BFF session endpoint — POST (seal session) and GET (read session) are
  // both public because they are called during and after the auth flow.
  ApiRoute.session.POST(),
  // Logout is always reachable so an expired session can still be cleared.
  ApiRoute.logout.POST(),
  // Health endpoint — must be reachable without a session for monitoring and
  // external load balancer health checks that come through the Istio gateway.
  ApiRoute.health.GET(),
  // Home Realm Discovery — resolves OIDC tenant config from an email address.
  // Must be public because it is called before any session exists (pre-auth).
  // The middleware matches pathname only (no query string), so we register the
  // bare path here rather than a URL with ?email= baked in.
  ApiRoute.discover.PATH(),
  // MCP OAuth consent page + its decision endpoint — both do their own session
  // check (SSO reuse) and cold-start login redirect, preserving the FULL authorize
  // URL (including the OAuth query) in `next`. The middleware must not intercept
  // them, or it would redirect with pathname-only `next` and drop the OAuth params
  // through login.
  '/mcp/authorize',
  '/api/mcp/authorize',
]);

// ── Redirect safety ───────────────────────────────────────────────────────────

/**
 * Returns `true` when `path` is a safe relative redirect destination.
 * Rejects absolute URLs, protocol-relative URLs, and paths that start with
 * `//` to prevent open-redirect attacks.
 */
export function isSafeRedirectPath(path: string): boolean {
  if (!path || !path.startsWith('/')) return false;
  if (path.startsWith('//')) return false;
  try {
    // Reject anything that resolves to a different origin.
    const url = new URL(path, 'http://localhost');
    return url.origin === 'http://localhost';
  } catch {
    return false;
  }
}
