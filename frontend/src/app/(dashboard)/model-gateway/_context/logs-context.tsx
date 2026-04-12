'use client';

// ── LogsContext ───────────────────────────────────────────────────────────────
//
// Provides the LogsStateShape to any component inside the logs page tree
// without prop-drilling. The RSC page passes initial data to `LogsProvider`
// which bootstraps `useLogs` synchronously — no useEffect hydration flash.

import { createContext, useContext } from 'react';
import { useLogs } from '../_hooks/use-logs';
import type { LogsStateShape } from '../_hooks/use-logs';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

// ── Context ───────────────────────────────────────────────────────────────────

const LogsContext = createContext<LogsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface LogsProviderProps {
  initialRecords: InferenceUsageRecord[];
  initialTotal: number;
  initialPats: PAT[];
  initialError: string | null;
  children: React.ReactNode;
}

export function LogsProvider({
  initialRecords,
  initialTotal,
  initialPats,
  initialError,
  children,
}: LogsProviderProps) {
  const state = useLogs(initialRecords, initialTotal, initialPats, initialError);
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
