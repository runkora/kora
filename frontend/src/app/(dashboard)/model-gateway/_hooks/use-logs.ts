'use client';

// ── useLogs hook ──────────────────────────────────────────────────────────────
//
// Owns all state for the model-gateway Logs page. The shape is expressed as an
// explicit interface (`LogsStateShape`) that doubles as the upgrade contract:
// when this feature graduates to Zustand, the store's state + actions must
// satisfy this same interface so callers need zero changes.

import { useState, useCallback } from 'react';
import { fetchUsage } from '../_services/usage-api';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

// ── State shape ───────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface LogsStateShape {
  // ── data ────────────────────────────────────────────────────────────────────
  records: InferenceUsageRecord[];
  total: number;
  pats: PAT[];
  status: LoadStatus;
  error: string | null;

  // ── actions ─────────────────────────────────────────────────────────────────
  /**
   * Re-fetch usage records from the BFF. Pass `key` to filter by a specific
   * API key (PAT UUID). Pass `undefined` or omit to fetch all records.
   */
  refresh: (key?: string) => Promise<void>;
}

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * useLogs
 *
 * Manages the full state for the Logs page. Accepts `initialData` from the
 * RSC parent — the RSC fetches on the server and passes the result down so
 * the client renders immediately without a loading flash.
 *
 * Calling `refresh(key?)` triggers a fresh client-side fetch (e.g. after the
 * user changes the key filter or manually reloads the tab).
 */
export function useLogs(
  initialRecords: InferenceUsageRecord[],
  initialTotal: number,
  initialPats: PAT[],
  initialError: string | null = null,
): LogsStateShape {
  const [records, setRecords] = useState<InferenceUsageRecord[]>(initialRecords);
  const [total, setTotal] = useState<number>(initialTotal);
  const [pats, setPats] = useState<PAT[]>(initialPats);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialRecords.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async (key?: string) => {
    setStatus('loading');
    setError(null);

    const result = await fetchUsage(key);

    if (result.error) {
      setError(result.error);
      setStatus('error');
    } else {
      setRecords(result.records);
      setTotal(result.total);
      setPats(result.pats);
      setStatus('success');
    }
  }, []);

  return { records, total, pats, status, error, refresh };
}
