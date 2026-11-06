'use client';

/**
 * @tier 3 — events-scoped. The event feed: the first page for the filters, older pages on
 * request, and the live stream from the newest loaded event. A search shows matching history
 * only, with the stream closed until it is cleared.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { fetchEventPage, openEventStream } from '../_services/events-api';
import { EVENT_PAGE_SIZE, filterParams } from '../_types';
import type { EventFilters, EventPage, EventRecord, KoraEvent, SeenPrincipal } from '../_types';

/** How long to wait before reopening a stream the server closed. */
const REOPEN_MS = 5_000;

/**
 * Live shows events as they arrive, paused holds them back, reconnecting has lost the stream,
 * and search shows matching history only.
 */
export type StreamState = 'live' | 'paused' | 'reconnecting' | 'search';

export interface EventFeed {
  events: KoraEvent[];
  /** The first page has not landed. */
  loading: boolean;
  /** The first page failed. */
  error: string | null;
  /** The last "Load older" failed. Older pages failing leaves the stream open. */
  olderError: string | null;
  retry: () => void;
  hasMore: boolean;
  loadingMore: boolean;
  loadOlder: () => void;
  stream: StreamState;
  setPaused: (paused: boolean) => void;
  /** Events held back while paused. */
  pending: number;
  showPending: () => void;
  /** Events that arrived live since the page opened. */
  freshIds: ReadonlySet<string>;
  /** Every principal and agent the feed has shown, for the filter menus. */
  principals: SeenPrincipal[];
  agents: string[];
}

interface Page {
  /** The filters these rows answer; other filters start over. */
  key: string;
  records: EventRecord[];
  cursor: string | null;
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  olderError: string | null;
}

const EMPTY = {
  records: [],
  cursor: null,
  loading: true,
  loadingMore: false,
  error: null,
  olderError: null,
};

export interface InitialEvents {
  /** The filters the server loaded for. */
  key: string;
  page: EventPage | null;
  error: string | null;
}

export function useEventFeed(filters: EventFilters, initial: InitialEvents): EventFeed {
  // The key is a string, so a new filters object with the same values changes nothing.
  const key = filterParams(filters).toString();
  const params = useMemo(() => new URLSearchParams(key), [key]);
  const searching = filters.query.trim() !== '';

  const [page, setPage] = useState<Page>(() =>
    initial.page === null && initial.error === null
      ? { key: initial.key, ...EMPTY }
      : {
          key: initial.key,
          records: initial.page?.events ?? [],
          cursor: initial.page?.next_cursor ?? null,
          loading: false,
          loadingMore: false,
          error: initial.error,
          olderError: null,
        },
  );
  const [attempt, setAttempt] = useState(0);
  const [paused, setPausedState] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  // Events held back while paused, for the filters they arrived under.
  const [held, setHeld] = useState<{ key: string; records: EventRecord[] }>({ key, records: [] });
  const pending = useMemo(() => (held.key === key ? held.records : []), [held, key]);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());
  const [seen, setSeen] = useState<Map<string, SeenPrincipal>>(() =>
    remember(new Map(), initial.page?.events ?? []),
  );
  const [seenAgents, setSeenAgents] = useState<Set<string>>(() =>
    rememberAgents(new Set(), initial.page?.events ?? []),
  );
  const epoch = useRef(0);
  const busy = useRef(false);
  const current = useMemo<Page>(() => (page.key === key ? page : { key, ...EMPTY }), [page, key]);

  const learn = useCallback((records: EventRecord[]) => {
    if (records.length === 0) return;
    setSeen((m) => remember(new Map(m), records));
    setSeenAgents((s) => rememberAgents(new Set(s), records));
  }, []);

  // The first page for these filters. The server already loaded the page it opened with.
  const skipFirst = useRef(page.key === key && !page.loading);
  useEffect(() => {
    if (skipFirst.current) {
      skipFirst.current = false;
      return;
    }
    const mine = ++epoch.current;
    busy.current = false;
    const query = new URLSearchParams(params);
    query.set('limit', String(EVENT_PAGE_SIZE));
    void fetchEventPage(query).then(({ page: data, error }) => {
      if (epoch.current !== mine) return;
      learn(data?.events ?? []);
      setPage({
        key,
        records: data?.events ?? [],
        cursor: data?.next_cursor ?? null,
        loading: false,
        loadingMore: false,
        error,
        olderError: null,
      });
    });
    return () => {
      epoch.current += 1;
    };
  }, [key, params, attempt, learn]);

  const loadOlder = useCallback(() => {
    if (busy.current || current.loading || current.cursor === null) return;
    busy.current = true;
    const mine = epoch.current;
    setPage({ ...current, loadingMore: true, olderError: null });
    const query = new URLSearchParams(params);
    query.set('limit', String(EVENT_PAGE_SIZE));
    query.set('cursor', current.cursor);
    void fetchEventPage(query).then(({ page: data, error }) => {
      if (epoch.current !== mine) return;
      busy.current = false;
      learn(data?.events ?? []);
      setPage((prev) =>
        prev.key !== key
          ? prev
          : {
              ...prev,
              records: [...prev.records, ...(data?.events ?? [])],
              // A failed page keeps the cursor, so "Load older" doubles as the retry.
              cursor: error === null ? (data?.next_cursor ?? null) : prev.cursor,
              loadingMore: false,
              olderError: error,
            },
      );
    });
  }, [current, params, key, learn]);

  // The newest loaded seq when the first page lands; the stream replays from there.
  const ready = current.key === key && !current.loading && current.error === null;
  const head = current.records[0]?.seq ?? 0;
  const headRef = useRef(head);
  const pausedRef = useRef(paused);
  // Declared before the stream effect, so the stream opens from the current head.
  useEffect(() => {
    headRef.current = head;
    pausedRef.current = paused;
  }, [head, paused]);

  useEffect(() => {
    if (!ready || searching) return;
    let source: EventSource | null = null;
    let timer: number | undefined;
    const open = (after: number) => {
      const query = new URLSearchParams(params);
      query.delete('q');
      if (after > 0) query.set('after', String(after));
      source = openEventStream(query);
      source.onopen = () => setReconnecting(false);
      source.onerror = () => {
        setReconnecting(true);
        // A closed stream does not retry by itself: the server refused it or the session ended.
        if (source?.readyState === EventSource.CLOSED) {
          source.close();
          timer = window.setTimeout(() => open(headRef.current), REOPEN_MS);
        }
      };
      source.onmessage = (message: MessageEvent<string>) => {
        const record = parseRecord(message.data);
        if (record === null) return;
        learn([record]);
        if (pausedRef.current) {
          setHeld((h) =>
            h.key !== key
              ? { key, records: [record] }
              : h.records.some((r) => r.seq === record.seq)
                ? h
                : { key, records: [record, ...h.records] },
          );
          return;
        }
        setPage((prev) =>
          prev.key !== key || prev.records.some((r) => r.seq === record.seq)
            ? prev
            : { ...prev, records: [record, ...prev.records] },
        );
        setFresh((s) => new Set(s).add(record.event.id));
      };
    };
    open(headRef.current);
    return () => {
      source?.close();
      window.clearTimeout(timer);
      setReconnecting(false);
    };
  }, [ready, searching, params, key, learn]);

  const showPending = useCallback(() => {
    setPage((prev) => {
      const known = new Set(prev.records.map((r) => r.seq));
      const add = pending.filter((r) => !known.has(r.seq)).sort((a, b) => b.seq - a.seq);
      return { ...prev, records: [...add, ...prev.records] };
    });
    setFresh((s) => new Set([...s, ...pending.map((r) => r.event.id)]));
    setHeld({ key, records: [] });
  }, [pending, key]);

  const setPaused = useCallback(
    (next: boolean) => {
      setPausedState(next);
      if (!next) showPending();
    },
    [showPending],
  );

  const retry = useCallback(() => {
    setPage({ key, ...EMPTY });
    setAttempt((n) => n + 1);
  }, [key]);

  const events = useMemo(() => current.records.map((r) => r.event), [current.records]);
  const principals = useMemo(
    () => [...seen.values()].sort((a, b) => a.name.localeCompare(b.name)),
    [seen],
  );
  const agents = useMemo(() => [...seenAgents].sort(), [seenAgents]);

  return {
    events,
    loading: current.loading,
    error: current.error,
    olderError: current.olderError,
    retry,
    hasMore: current.cursor !== null,
    loadingMore: current.loadingMore,
    loadOlder,
    stream: searching ? 'search' : reconnecting ? 'reconnecting' : paused ? 'paused' : 'live',
    setPaused,
    pending: searching ? 0 : pending.length,
    showPending,
    freshIds: fresh,
    principals,
    agents,
  };
}

function parseRecord(data: string): EventRecord | null {
  try {
    const record = JSON.parse(data) as EventRecord;
    return typeof record.seq === 'number' && typeof record.event?.id === 'string' ? record : null;
  } catch {
    return null;
  }
}

function remember(map: Map<string, SeenPrincipal>, records: EventRecord[]) {
  for (const { event: e } of records) {
    const name = e.koraprincipalname || map.get(e.koraprincipal)?.name || e.koraprincipal;
    map.set(e.koraprincipal, { subject: e.koraprincipal, name, type: e.koraprincipaltype });
  }
  return map;
}

function rememberAgents(set: Set<string>, records: EventRecord[]) {
  for (const { event: e } of records) if (e.koraagent) set.add(e.koraagent);
  return set;
}
