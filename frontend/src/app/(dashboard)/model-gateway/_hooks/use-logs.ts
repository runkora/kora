'use client';

// ── useLogs hook ──────────────────────────────────────────────────────────────
//
// Owns all state for the model-gateway Logs page. The shape is expressed as an
// explicit interface (`LogsStateShape`) that doubles as the upgrade contract:
// when this feature graduates to Zustand, the store's state + actions must
// satisfy this same interface so callers need zero changes. Grow it additively.
//
// One hook backs two surfaces, selected by `variant`:
//   - 'me'  → self-scoped GET /v1/usage (RSC-seeded, no identity map)
//   - 'all' → admin tenant-wide GET /v1/usage/all (lazy, principal-enriched)
//
// Pagination uses cursor-based navigation. The hook maintains an internal
// cursor stack: advancing pushes the current `nextCursor` onto the stack;
// going back pops it. Changing the key or principal filter resets the stack.

import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchAllUsage, fetchUsage } from '../_services/usage-api';
import type { InferenceUsageRecord, PrincipalIdentityMap } from '../_types';
import type { PAT } from '@orbit/shared/api';

// ── State shape ───────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export type LogsVariant = 'me' | 'all';

/**
 * Rolling time window for the log query. 'all' clears the bound; the others map
 * to a `from` timestamp of now minus the window (open-ended to the present).
 */
export type LogsTimeRange = 'all' | '24h' | '7d' | '30d';

/** Converts a rolling time range to an ISO `from` timestamp, or undefined for
 *  'all' (no lower bound). Evaluated at fetch time so the window is relative to
 *  the moment the query fires, not to when the filter was last changed. */
function timeRangeToFrom(range: LogsTimeRange): string | undefined {
  if (range === 'all') return undefined;
  const ms = range === '24h' ? 86_400_000 : range === '7d' ? 604_800_000 : 2_592_000_000;
  return new Date(Date.now() - ms).toISOString();
}

/**
 * Outcome of a single fetch attempt.
 *   - 'applied' → results were written to state (this fetch is the latest).
 *   - 'error'   → the fetch failed and error state was written.
 *   - 'stale'   → a newer fetch superseded this one; results were discarded and
 *                 no state was touched. Callers must NOT roll back on 'stale'
 *                 (the newer fetch now owns the cursor stack and status).
 */
type FetchOutcome = 'applied' | 'error' | 'stale';

export interface LogsStateShape {
  // ── data ────────────────────────────────────────────────────────────────────
  /** Which surface this instance drives — self-scoped or admin tenant-wide. */
  variant: LogsVariant;
  records: InferenceUsageRecord[];
  hasNextPage: boolean;
  nextCursor: string | null;
  hasPrevPage: boolean;
  pats: PAT[];
  /** Principal → identity attribution (admin 'all' variant only; `{}` for 'me'). */
  identities: PrincipalIdentityMap;
  /** Active principal narrowing filter, or null when unfiltered ('all' only). */
  principalFilterId: string | null;
  /** Active status filter (exact `status` value), or null when unfiltered. */
  statusFilter: string | null;
  /** Active model filter (raw `model_id` value), or null when unfiltered. */
  modelFilter: string | null;
  /** Active rolling time window. Defaults to 'all' (matches the unfiltered seed). */
  timeRange: LogsTimeRange;
  status: LoadStatus;
  error: string | null;

  // ── actions ─────────────────────────────────────────────────────────────────
  /**
   * Re-fetch usage records from the BFF. Pass `key` to filter by a specific
   * API key (PAT UUID). Preserves the active principal filter. Resets pagination.
   */
  refresh: (key?: string) => Promise<void>;
  /**
   * Set the principal narrowing filter (admin 'all' variant only). Pass null to
   * clear it. Preserves the active key filter. Resets pagination. No-op for 'me'.
   */
  setPrincipalFilter: (principalId: string | null, principalType: string | null) => Promise<void>;
  /** Set the status filter (pass null to clear). Preserves other filters. Resets pagination. */
  setStatusFilter: (status: string | null) => Promise<void>;
  /** Set the model filter by raw model_id (pass null to clear). Preserves other filters. Resets pagination. */
  setModelFilter: (modelId: string | null) => Promise<void>;
  /** Set the rolling time window. Preserves other filters. Resets pagination. */
  setTimeRange: (range: LogsTimeRange) => Promise<void>;
  /** Advance to the next page. No-op if `hasNextPage` is false. */
  goNext: () => Promise<void>;
  /** Go back to the previous page. No-op if `hasPrevPage` is false. */
  goPrev: () => Promise<void>;
}

// ── Config ────────────────────────────────────────────────────────────────────

export interface UseLogsConfig {
  variant: LogsVariant;
  initialRecords: InferenceUsageRecord[];
  initialHasNextPage: boolean;
  initialNextCursor: string | null;
  initialPats: PAT[];
  initialIdentities?: PrincipalIdentityMap;
  initialError?: string | null;
  /**
   * Fetch page 1 on mount instead of trusting seeded data. Used by the 'all'
   * variant, which has no RSC seed (scopes are only known client-side).
   */
  fetchOnMount?: boolean;
}

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * useLogs
 *
 * Manages the full state for one Logs surface. For the 'me' variant, initial
 * data comes from the RSC parent so the client renders immediately without a
 * loading flash. For the 'all' variant, `fetchOnMount` triggers a client-side
 * admin fetch once the tab mounts.
 */
export function useLogs(config: UseLogsConfig): LogsStateShape {
  const {
    variant,
    initialRecords,
    initialHasNextPage,
    initialNextCursor,
    initialPats,
    initialIdentities,
    initialError = null,
    fetchOnMount = false,
  } = config;

  const [records, setRecords] = useState<InferenceUsageRecord[]>(initialRecords);
  const [hasNextPage, setHasNextPage] = useState<boolean>(initialHasNextPage);
  const [nextCursor, setNextCursor] = useState<string | null>(initialNextCursor);
  const [pats, setPats] = useState<PAT[]>(initialPats);
  const [identities, setIdentities] = useState<PrincipalIdentityMap>(initialIdentities ?? {});
  const [principalFilterId, setPrincipalFilterId] = useState<string | null>(null);
  const [statusFilter, setStatusFilterState] = useState<string | null>(null);
  const [modelFilter, setModelFilterState] = useState<string | null>(null);
  const [timeRange, setTimeRangeState] = useState<LogsTimeRange>('all');
  const [status, setStatus] = useState<LoadStatus>(
    initialError
      ? 'error'
      : fetchOnMount
        ? 'loading'
        : initialRecords.length > 0
          ? 'success'
          : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  // Cursor stack for previous-page navigation. Each entry is the nextCursor
  // value used to reach the current page — popping it goes back one page.
  const cursorStack = useRef<string[]>([]);
  const [hasPrevPage, setHasPrevPage] = useState<boolean>(false);

  // Active filters — tracked in refs so goNext/goPrev/refresh/setPrincipalFilter
  // all pass a consistent filter set to load() without re-render churn.
  const activeKey = useRef<string | undefined>(undefined);
  const activePrincipalId = useRef<string | undefined>(undefined);
  const activePrincipalType = useRef<string | undefined>(undefined);
  const activeStatus = useRef<string | undefined>(undefined);
  const activeModel = useRef<string | undefined>(undefined);
  // Stored as the range token; the ISO `from` bound is derived per-fetch so the
  // window is always relative to the query moment (see timeRangeToFrom).
  const activeTimeRange = useRef<LogsTimeRange>('all');

  // Monotonic fetch generation. Each runFetch bumps it and captures its own id;
  // when it resolves, if the ref has moved on, a newer fetch has superseded it
  // and this one's results are discarded. This is what prevents a slow
  // page-turn from overwriting the state of a filter change fired after it —
  // e.g. click Next, then pick a Principal chip before Next resolves.
  const fetchGen = useRef(0);

  const runFetch = useCallback(
    async (cursor: string | undefined): Promise<FetchOutcome> => {
      // Assumes the caller has already moved status to 'loading' (or that the
      // initial state is 'loading', as on the fetchOnMount path). All setState
      // calls here happen after an await, so this is safe to invoke from an
      // effect without tripping react-hooks/set-state-in-effect.
      const gen = ++fetchGen.current;
      const from = timeRangeToFrom(activeTimeRange.current);
      if (variant === 'all') {
        const result = await fetchAllUsage({
          key: activeKey.current,
          cursor,
          principalId: activePrincipalId.current,
          principalType: activePrincipalType.current,
          status: activeStatus.current,
          model: activeModel.current,
          from,
        });
        // A newer fetch started while this was in flight — drop these results
        // untouched so we never regress to superseded state.
        if (gen !== fetchGen.current) return 'stale';
        if (result.error) {
          setError(result.error);
          setStatus('error');
          return 'error';
        }
        setRecords(result.records);
        setHasNextPage(result.hasNextPage);
        setNextCursor(result.nextCursor);
        setPats(result.pats);
        setIdentities(result.identities);
        setStatus('success');
        return 'applied';
      }

      const result = await fetchUsage({
        key: activeKey.current,
        cursor,
        status: activeStatus.current,
        model: activeModel.current,
        from,
      });
      if (gen !== fetchGen.current) return 'stale';
      if (result.error) {
        setError(result.error);
        setStatus('error');
        return 'error';
      }
      setRecords(result.records);
      setHasNextPage(result.hasNextPage);
      setNextCursor(result.nextCursor);
      setPats(result.pats);
      setStatus('success');
      return 'applied';
    },
    [variant],
  );

  const load = useCallback(
    async (cursor: string | undefined): Promise<FetchOutcome> => {
      setStatus('loading');
      setError(null);
      return runFetch(cursor);
    },
    [runFetch],
  );

  const refresh = useCallback(
    async (key?: string) => {
      activeKey.current = key;
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(undefined);
    },
    [load],
  );

  const setPrincipalFilter = useCallback(
    async (principalId: string | null, principalType: string | null) => {
      if (variant !== 'all') return;
      activePrincipalId.current = principalId ?? undefined;
      activePrincipalType.current = principalType ?? undefined;
      setPrincipalFilterId(principalId);
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(undefined);
    },
    [variant, load],
  );

  // status / model / time apply to both variants — the self-scoped GET /v1/usage
  // accepts the same status/model_id/from params as the admin endpoint. Each
  // setter mirrors setPrincipalFilter: stamp the ref, mirror to state for the
  // controls, reset pagination, refetch.
  const setStatusFilter = useCallback(
    async (next: string | null) => {
      activeStatus.current = next ?? undefined;
      setStatusFilterState(next);
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(undefined);
    },
    [load],
  );

  const setModelFilter = useCallback(
    async (next: string | null) => {
      activeModel.current = next ?? undefined;
      setModelFilterState(next);
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(undefined);
    },
    [load],
  );

  const setTimeRange = useCallback(
    async (next: LogsTimeRange) => {
      activeTimeRange.current = next;
      setTimeRangeState(next);
      cursorStack.current = [];
      setHasPrevPage(false);
      await load(undefined);
    },
    [load],
  );

  const goNext = useCallback(async () => {
    if (!nextCursor) return;
    const prevStack = [...cursorStack.current];
    cursorStack.current.push(nextCursor);
    setHasPrevPage(true);
    const outcome = await load(nextCursor);
    // Roll back only on a genuine failure. On 'stale', a newer load has already
    // reset the cursor stack and owns state — rolling back would clobber it.
    if (outcome === 'error') {
      cursorStack.current = prevStack;
      setHasPrevPage(prevStack.length > 0);
    }
  }, [nextCursor, load]);

  const goPrev = useCallback(async () => {
    if (cursorStack.current.length === 0) return;
    cursorStack.current.pop();
    const prevCursor = cursorStack.current[cursorStack.current.length - 1];
    setHasPrevPage(cursorStack.current.length > 0);
    await load(prevCursor);
  }, [load]);

  // Lazy first load for surfaces with no RSC seed (the admin 'all' variant).
  // Initial status is already 'loading' when fetchOnMount is set, and runFetch
  // only calls setState after awaiting — so no synchronous setState in-effect.
  const didMountFetch = useRef(false);
  useEffect(() => {
    if (fetchOnMount && !didMountFetch.current) {
      didMountFetch.current = true;
      void runFetch(undefined);
    }
  }, [fetchOnMount, runFetch]);

  return {
    variant,
    records,
    hasNextPage,
    nextCursor,
    hasPrevPage,
    pats,
    identities,
    principalFilterId,
    statusFilter,
    modelFilter,
    timeRange,
    status,
    error,
    refresh,
    setPrincipalFilter,
    setStatusFilter,
    setModelFilter,
    setTimeRange,
    goNext,
    goPrev,
  };
}
