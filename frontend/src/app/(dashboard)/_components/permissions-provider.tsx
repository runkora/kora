'use client';

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';

import { ApiRoute } from '@orbit/lib/routing/api-routes';

// ── Types ────────────────────────────────────────────────────────────────────

interface PermissionsContextValue {
  /** Complete set of scopes the current principal holds. */
  scopes: string[];
  /** Whether the permissions have been loaded from the session. */
  loaded: boolean;
}

// ── Context ──────────────────────────────────────────────────────────────────

const PermissionsContext = createContext<PermissionsContextValue>({
  scopes: [],
  loaded: false,
});

// ── Provider ─────────────────────────────────────────────────────────────────

/**
 * Fetches permissions from the BFF session endpoint and provides them to
 * all child components via context. Renders children immediately (scopes
 * arrive asynchronously — components that depend on them check `loaded`).
 */
export function PermissionsProvider({ children }: { children: ReactNode }) {
  const [value, setValue] = useState<PermissionsContextValue>({
    scopes: [],
    loaded: false,
  });

  useEffect(() => {
    fetch(ApiRoute.session.GET())
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        setValue({
          scopes: data?.permissions?.scopes ?? [],
          loaded: true,
        });
      })
      .catch(() => {
        setValue({ scopes: [], loaded: true });
      });
  }, []);

  return <PermissionsContext.Provider value={value}>{children}</PermissionsContext.Provider>;
}

// ── Hook ─────────────────────────────────────────────────────────────────────

/** Returns the current principal's permission set. */
export function usePermissions(): PermissionsContextValue {
  return useContext(PermissionsContext);
}
