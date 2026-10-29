'use client';

/**
 * @tier 3 — triggers-scoped. One trigger's delivery history, loaded when its modal opens and
 * paged back on request. The first page is fetched in the background, so the modal opens at once
 * and the list shows a placeholder until it lands.
 *
 * A scheduled or manual trigger's deliveries are its own, so they are asked for by trigger. A
 * webhook's are not: a delivery fans out to every trigger on the source, and the API's trigger
 * filter only matches the one a delivery is recorded against. So a webhook's history is read
 * from its source, and the deliveries that reached this trigger are picked out of it. Pages are
 * then fewer than the page size, and the history ends when the source runs out, not the trigger.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listDeliveryPage } from '../_services/triggers-api';
import { toDeliveryRow } from '../_types';
import type { DeliveryRow } from '../_types';

/** Deliveries asked for on open and on each "Load older". */
export const DELIVERY_PAGE_SIZE = 50;

export interface TriggerDeliveries {
  deliveries: DeliveryRow[];
  /** The first page has not landed. */
  loading: boolean;
  /** An older page is on its way. */
  loadingMore: boolean;
  /** There may be older deliveries to fetch. */
  hasMore: boolean;
  error: string | null;
  loadOlder: () => void;
  retry: () => void;
}

interface State {
  /** What these rows answer; a different trigger starts over. */
  key: string;
  rows: DeliveryRow[];
  /** The oldest `receivedAt` fetched, shown or not: the next page starts before it. */
  cursor: string | undefined;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  error: string | null;
}

const EMPTY = {
  rows: [],
  cursor: undefined,
  loading: true,
  loadingMore: false,
  hasMore: false,
  error: null,
};

/** `triggerId` empty means no trigger is open: nothing is fetched. */
export function useTriggerDeliveries(triggerId: string, sourceId: string): TriggerDeliveries {
  const key = `${triggerId}/${sourceId}`;
  const [state, setState] = useState<State>({ key, ...EMPTY });
  const [attempt, setAttempt] = useState(0);
  // Bumped whenever what is shown starts over (another trigger, a retry), so a response that was
  // already on its way when that happened is recognised as stale and dropped. `busy` keeps a
  // second "Load older" from starting before the first has landed.
  const epoch = useRef(0);
  const busy = useRef(false);
  // A different trigger's rows are not this one's, so they read as not loaded yet.
  const current = useMemo<State>(
    () => (state.key === key ? state : { key, ...EMPTY }),
    [state, key],
  );

  const fetchPage = useCallback(
    (before?: string) =>
      listDeliveryPage({
        ...(sourceId !== '' ? { sourceId } : { triggerId }),
        limit: DELIVERY_PAGE_SIZE,
        ...(before !== undefined && { before }),
      }),
    [triggerId, sourceId],
  );

  /** The deliveries on a page that reached this trigger. */
  const mineOf = useCallback(
    (page: DeliveryRow[]) =>
      sourceId === ''
        ? page
        : page.filter(
            (d) => d.triggerId === triggerId || d.runs.some((r) => r.triggerId === triggerId),
          ),
    [triggerId, sourceId],
  );

  useEffect(() => {
    if (triggerId === '') return;
    const mine = ++epoch.current;
    busy.current = false;
    void fetchPage().then(({ deliveries, error }) => {
      if (epoch.current !== mine) return;
      const page = deliveries.map(toDeliveryRow);
      setState({
        key,
        rows: mineOf(page),
        cursor: page[page.length - 1]?.receivedAt,
        loading: false,
        loadingMore: false,
        hasMore: error === null && deliveries.length >= DELIVERY_PAGE_SIZE,
        error,
      });
    });
    return () => {
      epoch.current += 1;
    };
  }, [key, triggerId, attempt, fetchPage, mineOf]);

  const loadOlder = useCallback(() => {
    if (busy.current || current.loading || !current.hasMore) return;
    busy.current = true;
    const mine = epoch.current;
    setState({ ...current, loadingMore: true, error: null });
    void fetchPage(current.cursor).then(({ deliveries, error }) => {
      if (epoch.current !== mine) return;
      busy.current = false;
      const page = deliveries.map(toDeliveryRow);
      setState((prev) =>
        prev.key !== key
          ? prev
          : {
              ...prev,
              rows: [...prev.rows, ...mineOf(page)],
              cursor: page[page.length - 1]?.receivedAt ?? prev.cursor,
              loadingMore: false,
              // A failed page says nothing about whether there is more, so "Load older" stays
              // and doubles as the retry.
              hasMore: error === null ? deliveries.length >= DELIVERY_PAGE_SIZE : prev.hasMore,
              error,
            },
      );
    });
  }, [current, fetchPage, key, mineOf]);

  const retry = useCallback(() => {
    setState({ key, ...EMPTY });
    setAttempt((n) => n + 1);
  }, [key]);

  return {
    deliveries: current.rows,
    loading: current.loading,
    loadingMore: current.loadingMore,
    hasMore: current.hasMore,
    error: current.error,
    loadOlder,
    retry,
  };
}
