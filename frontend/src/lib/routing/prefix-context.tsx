'use client';

import { createContext, useContext, type ReactNode } from 'react';
import { useRouter } from 'next/navigation';

import { setClientDevEnvPrefix } from './devenv-prefix';

const PrefixContext = createContext('');

/**
 * Makes the server-observed dev-environment path prefix (X-Forwarded-Prefix,
 * read in the root layout via getDevEnvPrefix) available to client code two
 * ways: React Context (usePrefix()) for components, and a plain module
 * mirror (getClientDevEnvPrefix, devenv-prefix.ts) for api-routes.ts's plain
 * functions. Calling setClientDevEnvPrefix directly in the render body (not
 * an effect) means the mirror is populated before hydration completes, with
 * no window where a client-triggered fetch could read a stale/empty value.
 */
export function PrefixProvider({ prefix, children }: { prefix: string; children: ReactNode }) {
  setClientDevEnvPrefix(prefix);
  return <PrefixContext.Provider value={prefix}>{children}</PrefixContext.Provider>;
}

/** The current dev-environment path prefix (e.g. "/abc123"), or "" outside one. */
export function usePrefix(): string {
  return useContext(PrefixContext);
}

/**
 * Prefixes href only when it's an absolute path ("/foo"). Query-only
 * ("?page=2") and other relative targets resolve against the current
 * (already-prefixed) pathname, so prefixing them again would double up.
 */
function withPrefix(prefix: string, href: string): string {
  return href.startsWith('/') ? prefix + href : href;
}

/**
 * useRouter() wrapper that prefixes every push/replace target — the
 * mechanical replacement for every `router.push('/foo')` /
 * `router.replace('/foo')` call site outside a dev environment, `usePrefix()`
 * returns "" so this is byte-identical to the plain router.
 */
export function usePrefixedRouter(): ReturnType<typeof useRouter> {
  const router = useRouter();
  const prefix = usePrefix();
  return {
    ...router,
    push: (href: string, options?: Parameters<typeof router.push>[1]) =>
      router.push(withPrefix(prefix, href), options),
    replace: (href: string, options?: Parameters<typeof router.replace>[1]) =>
      router.replace(withPrefix(prefix, href), options),
  };
}
