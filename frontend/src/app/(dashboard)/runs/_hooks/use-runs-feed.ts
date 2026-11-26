'use client';

/**
 * @tier 3 — runs-scoped. The run list for the filters: the live stream, opened before the first
 * page so no change between them is lost, then pages on request. Changing filters starts over.
 */

import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import type { RunStreamMessage, RunWithAttribution } from '@runkora/shared/api';
import {
  RUN_CREATED,
  emptyFeed,
  feedReducer,
  type FeedAction,
  type UnkeyedFeedAction,
} from '../_lib/runs-feed';
import { fetchRunsPage, openRunsStream } from '../_services/runs-api';
import { RUNS_PAGE_SIZE, runFilterParams, type RunFilters } from '../_types';

/** The first wait before reopening a stream the server closed. It doubles on each failure. */
const REOPEN_MS = 5_000;
const REOPEN_MAX_MS = 60_000;

export interface RunsFeed {
  /** The runs on the page shown. */
  runs: RunWithAttribution[];
  /** The first page has not landed and no rows are loaded. */
  loading: boolean;
  /** The first page failed. */
  error: string | null;
  retry: () => void;
  /** The page shown, from 0. */
  pageIndex: number;
  hasNewer: boolean;
  hasOlder: boolean;
  newer: () => void;
  older: () => void;
  pageLoading: boolean;
  pageError: string | null;
  /** New matching runs that arrived while a later page was shown. */
  freshCount: number;
  /** Loads the first page, which holds the new runs. */
  showFresh: () => void;
  /** The stream dropped and is retrying. */
  reconnecting: boolean;
  /** Runs created since the page opened, which rows mark as New. */
  liveIds: ReadonlySet<string>;
}

export function useRunsFeed(filters: RunFilters): RunsFeed {
  // A string: a new filters object with the same values changes nothing.
  const key = runFilterParams(filters).toString();
  const [state, dispatch] = useReducer(feedReducer, key, emptyFeed);
  const feed = state.key === key ? state : emptyFeed(key);
  // Each first-page load and page fetch answers only if no newer load has started.
  const epoch = useRef(0);
  const [reconnecting, setReconnecting] = useState(false);
  const [liveIds, setLiveIds] = useState<ReadonlySet<string>>(() => new Set());

  const act = useCallback(
    (action: UnkeyedFeedAction) => dispatch({ ...action, key } as FeedAction),
    [key],
  );

  const loadFirst = useCallback(() => {
    const mine = ++epoch.current;
    const query = new URLSearchParams(key);
    query.set('limit', String(RUNS_PAGE_SIZE));
    void fetchRunsPage(query).then(({ page, error }) => {
      if (epoch.current !== mine) return;
      act(
        page
          ? { type: 'first', page }
          : { type: 'firstFailed', error: error ?? 'Failed to load runs.' },
      );
    });
  }, [key, act]);

  const reload = useCallback(() => {
    act({ type: 'reload' });
    loadFirst();
  }, [act, loadFirst]);

  useEffect(() => {
    let source: EventSource | null = null;
    let timer: number | undefined;
    let failures = 0;
    let lastId: string | null = null;
    let loaded = false;
    // A stream that opens with no position to resume from may have missed changes.
    let reloadOnOpen = false;
    const loadOnce = () => {
      if (loaded) return;
      loaded = true;
      loadFirst();
    };
    const open = () => {
      const query = new URLSearchParams(key);
      if (lastId) query.set('after', lastId);
      const current = openRunsStream(query);
      source = current;
      // The first page is read once the stream is subscribed, so no change falls between them.
      current.onopen = () => {
        failures = 0;
        setReconnecting(false);
        if (!loaded) loadOnce();
        else if (reloadOnOpen) {
          reloadOnOpen = false;
          reload();
        }
      };
      current.addEventListener('run', (event) => {
        const { data, lastEventId } = event as MessageEvent<string>;
        if (lastEventId) lastId = lastEventId;
        const message = parseMessage(data);
        if (!message) return;
        act({ type: 'message', message });
        if (message.type === RUN_CREATED && message.matches)
          setLiveIds((ids) => new Set(ids).add(message.run.id));
      });
      // The stream cannot resume from where it was. A fresh stream without a position replaces
      // it, so the browser does not reconnect with the same expired id, and the first page
      // is read again once it opens.
      current.addEventListener('reset', () => {
        current.close();
        lastId = null;
        reloadOnOpen = true;
        open();
      });
      current.onerror = () => {
        setReconnecting(true);
        // The list still loads when the stream cannot open, whether the browser retries or not.
        loadOnce();
        if (lastId === null) reloadOnOpen = true;
        // A closed stream does not retry by itself: the server refused it or the session ended.
        if (current.readyState !== EventSource.CLOSED) return;
        current.close();
        const wait = Math.min(REOPEN_MS * 2 ** failures, REOPEN_MAX_MS);
        failures += 1;
        timer = window.setTimeout(open, wait);
      };
    };
    open();
    return () => {
      source?.close();
      window.clearTimeout(timer);
      epoch.current += 1;
      setReconnecting(false);
    };
  }, [key, act, loadFirst, reload]);

  const older = useCallback(() => {
    if (feed.pageLoading || feed.loading) return;
    if (feed.index + 1 < feed.pages.length) {
      act({ type: 'goTo', index: feed.index + 1 });
      return;
    }
    const cursor = feed.pages[feed.index]?.nextCursor;
    if (!cursor) return;
    const mine = epoch.current;
    act({ type: 'pageLoading' });
    const query = new URLSearchParams(key);
    query.set('limit', String(RUNS_PAGE_SIZE));
    query.set('cursor', cursor);
    void fetchRunsPage(query).then(({ page, error }) => {
      if (epoch.current !== mine) return;
      act(
        page
          ? { type: 'page', page }
          : { type: 'pageFailed', error: error ?? 'Failed to load runs.' },
      );
    });
  }, [feed, key, act]);

  // Back on the first page, runs held by the bar load with it, so they show at the top.
  const newer = useCallback(() => {
    act({ type: 'goTo', index: feed.index - 1 });
    if (feed.index === 1 && feed.fresh.length > 0) reload();
  }, [feed.index, feed.fresh.length, act, reload]);

  const shown = feed.pages[feed.index];
  const runs = useMemo(() => shown?.runs ?? [], [shown]);
  return {
    runs,
    loading: feed.loading && feed.pages.length === 0,
    error: feed.error,
    retry: reload,
    pageIndex: feed.index,
    hasNewer: feed.index > 0,
    hasOlder: feed.index + 1 < feed.pages.length || Boolean(shown?.nextCursor),
    newer,
    older,
    pageLoading: feed.pageLoading,
    pageError: feed.pageError,
    freshCount: feed.fresh.length,
    showFresh: reload,
    reconnecting,
    liveIds,
  };
}

/** A message the page can draw, or null; a malformed one is dropped rather than breaking the list. */
function parseMessage(data: string): RunStreamMessage | null {
  try {
    const message = JSON.parse(data) as RunStreamMessage;
    const run = message.run;
    const valid =
      typeof message.type === 'string' &&
      typeof message.matches === 'boolean' &&
      typeof run?.id === 'string' &&
      typeof run.agent_type_id === 'string' &&
      typeof run.state === 'string';
    return valid ? message : null;
  } catch {
    return null;
  }
}
