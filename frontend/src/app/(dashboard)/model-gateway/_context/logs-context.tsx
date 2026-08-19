'use client';

// ── LogsContext ───────────────────────────────────────────────────────────────
//
// Provides the LogsStateShape to any component inside the logs page tree
// without prop-drilling. The RSC page passes initial data to `LogsProvider`
// which bootstraps `useLogs` synchronously — no useEffect hydration flash.

import { createContext, useContext } from 'react';
import { useLogs } from '../_hooks/use-logs';
import type { LogsStateShape, LogsVariant } from '../_hooks/use-logs';
import type { InferenceUsageRecord, PrincipalIdentityMap } from '../_types';
import type { PAT } from '@orbit/shared/api';

// ── Context ───────────────────────────────────────────────────────────────────

const LogsContext = createContext<LogsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface LogsProviderProps {
  /** Surface this provider drives — self-scoped 'me' (default) or admin 'all'. */
  variant?: LogsVariant;
  initialRecords: InferenceUsageRecord[];
  initialHasNextPage: boolean;
  initialNextCursor: string | null;
  initialPats: PAT[];
  initialIdentities?: PrincipalIdentityMap;
  initialError: string | null;
  /** Fetch page 1 on mount instead of trusting seeded data (admin 'all'). */
  fetchOnMount?: boolean;
  children: React.ReactNode;
}

export function LogsProvider({
  variant = 'me',
  initialRecords,
  initialHasNextPage,
  initialNextCursor,
  initialPats,
  initialIdentities,
  initialError,
  fetchOnMount,
  children,
}: LogsProviderProps) {
  const state = useLogs({
    variant,
    initialRecords,
    initialHasNextPage,
    initialNextCursor,
    initialPats,
    initialIdentities,
    initialError,
    fetchOnMount,
  });
  return <LogsContext.Provider value={state}>{children}</LogsContext.Provider>;
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

/**
 * useLogsContext
 *
 * Must be called inside a `<LogsProvider>` subtree. Throws a clear error
 * if called outside to prevent silent null-access bugs.
 */
export function useLogsContext(): LogsStateShape {
  const ctx = useContext(LogsContext);
  if (!ctx) {
    throw new Error('useLogsContext must be used inside <LogsProvider>');
  }
  return ctx;
}
