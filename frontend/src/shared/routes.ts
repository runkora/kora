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
  // BFF auth routes are always public — they initiate or complete the flow.
  '/api/auth/login',
  '/api/auth/callback',
  '/api/auth/logout',
  '/api/auth/session',
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
