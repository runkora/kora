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
 * The root layout (app/layout.tsx) reads the prefix server-side and passes
 * it to PrefixProvider (prefix-context.tsx), which makes it available to
 * client code two ways: React Context (usePrefix()) for components, and the
 * plain module-level mirror below for code that isn't a component/hook.
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

// A plain module-level mirror of the current request's prefix, for the rare
// non-component/non-hook consumers (api-routes.ts's plain route-builder
// functions) that can't call useContext. Set exactly once per page load, by
// PrefixProvider (prefix-context.tsx) synchronously during its own render —
// not in an effect — so it's populated before any client-triggered fetch
// could possibly run. React Context (usePrefix()) is still the source of
// truth for anything that IS a component/hook; this is purely a mirror for
// what can't consume it.
let clientPrefix = '';

/** Called once by PrefixProvider during render. Not for use elsewhere. */
export function setClientDevEnvPrefix(prefix: string): void {
  clientPrefix = prefix;
}

/**
 * Reads the current dev-environment prefix outside of React, for
 * api-routes.ts's plain functions. "" until PrefixProvider has rendered at
 * least once — acceptable because the very first paint is server-rendered
 * HTML (which is inherently already correctly prefixed by Next.js's own
 * routing), and this value only matters for CLIENT-triggered fetches, which
 * by definition happen after hydration (i.e. after PrefixProvider has run).
 */
export function getClientDevEnvPrefix(): string {
  return clientPrefix;
}
