'use client';

// ── useLogs hook ──────────────────────────────────────────────────────────────
//
// Owns all state for the model-gateway Logs page. The shape is expressed as an
// explicit interface (`LogsStateShape`) that doubles as the upgrade contract:
// when this feature graduates to Zustand, the store's state + actions must
// satisfy this same interface so callers need zero changes.
//
// Pagination uses cursor-based navigation. The hook maintains an internal
// cursor stack: advancing pushes the current `nextCursor` onto the stack;
// going back pops it. Changing the key filter resets the stack.

import { useState, useCallback, useRef } from 'react';
import { fetchUsage } from '../_services/usage-api';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@runkora/shared/api';

// ── State shape ───────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface LogsStateShape {
  // ── data ────────────────────────────────────────────────────────────────────
  records: InferenceUsageRecord[];
  hasNextPage: boolean;
  nextCursor: string | null;
  hasPrevPage: boolean;
  pats: PAT[];
  status: LoadStatus;
  error: string | null;

  // ── actions ─────────────────────────────────────────────────────────────────
  /**
   * Re-fetch usage records from the BFF. Pass `key` to filter by a specific
   * API key (PAT UUID). Resets pagination to the first page.
   */
  refresh: (key?: string) => Promise<void>;
  /** Advance to the next page. No-op if `hasNextPage` is false. */
  goNext: () => Promise<void>;
  /** Go back to the previous page. No-op if `hasPrevPage` is false. */
  goPrev: () => Promise<void>;
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
 * user changes the key filter or manually reloads the tab) and resets the
 * cursor stack to page 1.
 */
export function useLogs(
  initialRecords: InferenceUsageRecord[],
  initialHasNextPage: boolean,
  initialNextCursor: string | null,
  initialPats: PAT[],
  initialError: string | null = null,
): LogsStateShape {
  const [records, setRecords] = useState<InferenceUsageRecord[]>(initialRecords);
  const [hasNextPage, setHasNextPage] = useState<boolean>(initialHasNextPage);
  const [nextCursor, setNextCursor] = useState<string | null>(initialNextCursor);
  const [pats, setPats] = useState<PAT[]>(initialPats);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialRecords.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  // Cursor stack for previous-page navigation. Each entry is the nextCursor
  // value used to reach the current page — popping it goes back one page.
  const cursorStack = useRef<string[]>([]);
  const [hasPrevPage, setHasPrevPage] = useState<boolean>(false);

  // Active key filter — tracked so goNext/goPrev can pass it through.
  const activeKey = useRef<string | undefined>(undefined);

  const load = useCallback(
    async (key: string | undefined, cursor: string | undefined): Promise<boolean> => {
      setStatus('loading');
      setError(null);

      const result = await fetchUsage({ key, cursor });

      if (result.error) {
        setError(result.error);
        setStatus('error');
        return false;
      } else {
        setRecords(result.records);
        setHasNextPage(result.hasNextPage);
        setNextCursor(result.nextCursor);
        setPats(result.pats);
        setStatus('success');
        return true;
      }
    },
    [],
  );

  const refresh = useCallback(
    async (key?: string) => {
      activeKey.current = key;
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(key, undefined);
    },
    [load],
  );

  const goNext = useCallback(async () => {
    if (!nextCursor) return;
    const prevStack = [...cursorStack.current];
    cursorStack.current.push(nextCursor);
    setHasPrevPage(true);
    const ok = await load(activeKey.current, nextCursor);
    if (!ok) {
      cursorStack.current = prevStack;
      setHasPrevPage(prevStack.length > 0);
    }
  }, [nextCursor, load]);

  const goPrev = useCallback(async () => {
    if (cursorStack.current.length === 0) return;
    cursorStack.current.pop();
    const prevCursor = cursorStack.current[cursorStack.current.length - 1];
    setHasPrevPage(cursorStack.current.length > 0);
    await load(activeKey.current, prevCursor);
  }, [load]);

  return {
    records,
    hasNextPage,
    nextCursor,
    hasPrevPage,
    pats,
    status,
    error,
    refresh,
    goNext,
    goPrev,
  };
}
