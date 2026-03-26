/**
 * Application route constants.
 *
 * Imported by route handlers, middleware, and client components to avoid
 * hard-coded path strings scattered across the codebase.
 */

/** All first-class application routes. Used by middleware to identify which
 *  paths require an active session and which are publicly accessible. */
export const APP_ROUTES = {
  home: '/',
  login: '/login',
} as const;

export type AppRoute = (typeof APP_ROUTES)[keyof typeof APP_ROUTES];

/** Public paths that do not require authentication. */
export const PUBLIC_PATHS: ReadonlySet<string> = new Set([
  APP_ROUTES.login,
  // OIDC callback page — receives the code/state from the provider and
  // completes the browser-side token exchange before posting to the BFF.
  '/auth/callback',
  // BFF session endpoint — POST (seal session) and GET (read session) are
  // both public because they are called during and after the auth flow.
  '/api/auth/session',
  // Logout is always reachable so an expired session can still be cleared.
  '/api/auth/logout',
]);

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
