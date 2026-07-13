/**
 * devenv-prefix.ts
 *
 * A dev environment's app traffic is reachable at a single shared hostname
 * with the environment ID as a path prefix
 * (docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
 * The gateway strips that prefix before forwarding the request to this app
 * and records what it stripped in the X-Forwarded-Prefix request header.
 * Reading it back here lets server-rendered redirects (the login flow in
 * particular) restore the prefix on the URLs they hand back to the browser,
 * instead of assuming the app is served from the root.
 *
 * Only page-navigation redirects use this today (the login flow). It does
 * not make the whole app prefix-aware — ordinary in-app links still resolve
 * root-relative, per the ADR's accepted limitation that the browser URL goes
 * unprefixed again after the first client-side navigation.
 */

export const DEVENV_PREFIX_HEADER = 'x-forwarded-prefix';

/**
 * Returns the environment path prefix (e.g. "/abc123") the gateway stripped
 * from this request, or "" outside a dev environment (production, or a
 * request that reached the app directly).
 */
export function getDevEnvPrefix(headers: { get(name: string): string | null }): string {
  const value = headers.get(DEVENV_PREFIX_HEADER);
  return value && value.startsWith('/') ? value : '';
}
