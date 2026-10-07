/**
 * external-urls.ts
 *
 * Single source of truth for every external URL linked from the kora app.
 *
 * Add a new entry here when linking to an external resource. Removing an
 * entry will produce a compile error at every `ExternalLink` call site
 * that references it — broken external links cannot be silently introduced.
 *
 * Import the `ExternalUrl` type when you need to accept or store a URL
 * value outside of an `ExternalLink`.
 *
 * @example
 *   import { type ExternalUrl } from '@core/routing/external-urls';
 */

export const EXTERNAL_URLS = [
  'https://github.com/clear-route/orbit',
  'https://github.com/clear-route/orbit/blob/main/docs/guides/deployment.md#sandbox-images-and-environments',
] as const satisfies readonly (`https://${string}` | `http://${string}`)[];

/**
 * Every external URL that may be linked from the kora app.
 *
 * This is a compile-time exhaustive union — passing a string that is not
 * in `EXTERNAL_URLS` to an `ExternalLink` is a TypeScript error.
 */
export type ExternalUrl = (typeof EXTERNAL_URLS)[number];
