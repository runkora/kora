/**
 * routes.ts
 *
 * Single source of truth for every internal route in the Orbit app.
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
 *   import { type AppRoute } from '@core/routing/routes';
 */

export const APP_ROUTES = ['/', '/login', '/dashboard', '/chat'] as const;

/**
 * Every valid internal route in the Orbit app.
 *
 * This is a compile-time exhaustive union — passing a string that is not
 * in `APP_ROUTES` to an `AppLink` is a TypeScript error.
 */
export type AppRoute = (typeof APP_ROUTES)[number];
