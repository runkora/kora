'use client';

/**
 * @tier 3 — triggers-scoped. A delivery history paged from the API: the first page on mount
 * and whenever the query changes, older pages on request. Filtering happens on the server, so a
 * status filter covers the whole window rather than the rows already loaded.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listDeliveryPage } from '../_services/triggers-api';
import { toDeliveryRow } from '../_types';
import type { DeliveryRow } from '../_types';
import { rangeStart } from '../_types/stats';
import type { StatsRange } from '../_types/stats';

/** Deliveries asked for on each page. */
export const DELIVERY_PAGE_SIZE = 50;

/** What to list: one trigger's, one agent's triggers' or the tenant's deliveries, optionally by status. */
export interface DeliveryQuery {
  triggerId?: string;
  agentTypeId?: string;
  /** A `status` filter value; omitted or `all` for every status. */
  status?: string;
  /** Bounds every page to deliveries received in this range. */
  window?: StatsRange;
}

export interface TriggerDeliveries {
  deliveries: DeliveryRow[];
  /** The first page has not landed. */
  loading: boolean;
  /** An older page is on its way. */
  loadingMore: boolean;
  /** There are older deliveries to fetch. */
  hasMore: boolean;
  error: string | null;
  loadOlder: () => void;
  retry: () => void;
}

interface State {
  /** What these rows answer; a different query starts over. */
  key: string;
  rows: DeliveryRow[];
  /** The API's cursor for the next page; null on the last page. */
  cursor: string | null;
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
}

const EMPTY = { rows: [], cursor: null, loading: true, loadingMore: false, error: null };

/** `all` is no status filter. */
function normalize(query: DeliveryQuery | null): DeliveryQuery {
  return { ...query, status: query?.status === 'all' ? undefined : query?.status };
}

/** What the rows answer; empty when nothing is open, which fetches nothing. */
function queryKey(query: DeliveryQuery | null, q: DeliveryQuery): string {
  if (query === null || q.triggerId === '' || q.agentTypeId === '') return '';
  return `${q.triggerId ?? ''}/${q.agentTypeId ?? ''}/${q.status ?? ''}/${q.window ?? ''}`;
}

/** `query` null, or naming an empty trigger or agent id, fetches nothing. */
export function useTriggerDeliveries(query: DeliveryQuery | null): TriggerDeliveries {
  const normalized = normalize(query);
  const { triggerId, agentTypeId, status, window: range } = normalized;
  const key = queryKey(query, normalized);
  const [state, setState] = useState<State>({ key, ...EMPTY });
  const [attempt, setAttempt] = useState(0);
  // Bumped whenever the list starts over, so a response already on its way is dropped. `busy`
  // keeps a second next-page request from starting before the first has landed.
  const epoch = useRef(0);
  const busy = useRef(false);
  // The range's start, shared by every page of one query.
  const after = useRef<string | undefined>(undefined);
  const current = useMemo<State>(
    () => (state.key === key ? state : { key, ...EMPTY }),
    [state, key],
  );

  const fetchPage = useCallback(
    (cursor?: string) =>
      listDeliveryPage({
        ...(triggerId !== undefined && { triggerId }),
        ...(agentTypeId !== undefined && { agentTypeId }),
        ...(status !== undefined && { status }),
        ...(after.current !== undefined && { receivedAfter: after.current }),
        limit: DELIVERY_PAGE_SIZE,
        ...(cursor !== undefined && { cursor }),
      }),
    [triggerId, agentTypeId, status],
  );

  useEffect(() => {
    if (key === '') return;
    const mine = ++epoch.current;
    busy.current = false;
    after.current = range === undefined ? undefined : rangeStart(range);
    void fetchPage().then(({ deliveries, nextCursor, error }) => {
      if (epoch.current !== mine) return;
      setState({
        key,
        rows: deliveries.map(toDeliveryRow),
        cursor: nextCursor,
        loading: false,
        loadingMore: false,
        error,
      });
    });
    return () => {
      epoch.current += 1;
    };
  }, [key, attempt, fetchPage, range]);

  const loadOlder = useCallback(() => {
    if (busy.current || current.loading || current.cursor === null) return;
    busy.current = true;
    const mine = epoch.current;
    setState({ ...current, loadingMore: true, error: null });
    void fetchPage(current.cursor).then(({ deliveries, nextCursor, error }) => {
      if (epoch.current !== mine) return;
      busy.current = false;
      setState((prev) =>
        prev.key !== key
          ? prev
          : {
              ...prev,
              rows: [...prev.rows, ...deliveries.map(toDeliveryRow)],
              // A failed page keeps its cursor for the retry.
              cursor: error === null ? nextCursor : prev.cursor,
              loadingMore: false,
              error,
            },
      );
    });
  }, [current, fetchPage, key]);

  const retry = useCallback(() => {
    setState({ key, ...EMPTY });
    setAttempt((n) => n + 1);
  }, [key]);

  return {
    deliveries: current.rows,
    loading: current.loading,
    loadingMore: current.loadingMore,
    hasMore: current.cursor !== null,
    error: current.error,
    loadOlder,
    retry,
  };
}
