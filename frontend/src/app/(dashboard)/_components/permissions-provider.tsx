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
    let timer: ReturnType<typeof setTimeout> | null = null;
    let mounted = true;

    async function loadSession() {
      const res = await fetch(ApiRoute.session.GET());
      const data = res.ok ? await res.json() : null;

      if (!mounted) return;

      setValue({
        scopes: data?.permissions?.scopes ?? [],
        user: data?.user ?? null,
        loaded: true,
      });

      // Schedule a proactive re-fetch 4 minutes before the token expires so
      // the context stays in sync for long-lived sessions. The middleware
      // refresh window is 5 minutes, so the next navigation will have already
      // triggered a server-side refresh by the time this fires.
      if (data?.expiresAt) {
        const secondsUntilExpiry = data.expiresAt - Math.floor(Date.now() / 1000);
        if (secondsUntilExpiry > 5 * 60) {
          timer = setTimeout(loadSession, (secondsUntilExpiry - 4 * 60) * 1000);
        }
      }
    }

    loadSession().catch(() => {
      if (mounted) setValue({ scopes: [], user: null, loaded: true });
    });

    return () => {
      mounted = false;
      if (timer) clearTimeout(timer);
    };
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
