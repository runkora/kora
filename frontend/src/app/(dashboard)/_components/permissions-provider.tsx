'use client';

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';

import { ApiRoute } from '@orbit/lib/routing/api-routes';

// ── Types ────────────────────────────────────────────────────────────────────

interface SessionUser {
  sub: string;
  email: string;
  name: string;
}

interface SessionContextValue {
  /** Complete set of scopes the current principal holds. */
  scopes: string[];
  /** Authenticated user identity (email, name, sub). */
  user: SessionUser | null;
  /** Whether the session has been loaded from the BFF. */
  loaded: boolean;
}

// ── Context ──────────────────────────────────────────────────────────────────

const SessionContext = createContext<SessionContextValue>({
  scopes: [],
  user: null,
  loaded: false,
});

// ── Provider ─────────────────────────────────────────────────────────────────

/**
 * Fetches the full session (user identity + permissions) from the BFF and
 * provides both to all child components via a single context. This avoids
 * duplicate session fetches from multiple consumers.
 *
 * Renders children immediately — components that depend on session data
 * check `loaded` to handle the async loading window.
 */
export function PermissionsProvider({ children }: { children: ReactNode }) {
  const [value, setValue] = useState<SessionContextValue>({
    scopes: [],
    user: null,
    loaded: false,
  });

  useEffect(() => {
    fetch(ApiRoute.session.GET())
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        setValue({
          scopes: data?.permissions?.scopes ?? [],
          user: data?.user ?? null,
          loaded: true,
        });
      })
      .catch(() => {
        setValue({ scopes: [], user: null, loaded: true });
      });
  }, []);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

// ── Hooks ────────────────────────────────────────────────────────────────────

/** Returns the current principal's permission set and user identity. */
export function useSession(): SessionContextValue {
  return useContext(SessionContext);
}

/** Returns only the permission scopes and loaded state. */
export function usePermissions(): { scopes: string[]; loaded: boolean } {
  const { scopes, loaded } = useContext(SessionContext);
  return { scopes, loaded };
}
