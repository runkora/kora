'use client';

// ── useMarketplace hook ───────────────────────────────────────────────────────
//
// Owns all state for the Marketplace overview. The RSC page loads the first
// result page, and the hook starts from that data. Each later browse change
// (search, Marketplace, sort order, page, rows per page) goes through the BFF:
//
//   1. Abort the request in flight and start a new request.
//   2. Keep the current results on screen. The view shows a progress notice.
//   3. Ignore a response that is not from the newest request.
//   4. On success, show the new results and write the browse values to the
//      URL. On error, keep the current results and URL, and show a warning.
//
// A page change adds a browser history entry (pushState), so Back and Forward
// move between pages. Other changes replace the current entry (replaceState).
//
// Back from another page, for example a Skill page, can show an old server
// render from the Next.js cache. On mount, the hook compares the URL with the
// values of the server render. If they differ, it loads the results for the URL.

import { useCallback, useEffect, useRef, useState } from 'react';
import type { Marketplace, PackageOverview, SkillSort } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';
import { fetchMarketplaceOverview } from '../_services/marketplace-api';
import { buildBrowseUrl, parseBrowseParams, toSearchQuery } from '../_utils/browse-params';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

/**
 * How a request changes the browser history:
 * - `push`: a page change. On success, add a history entry.
 * - `replace`: another browse change. On success, replace the current entry.
 * - `popstate`: Back or Forward already changed the URL. The search box shows
 *   the search text of the URL. On error, restore the URL of the results on
 *   screen.
 */
type HistoryMode = 'push' | 'replace' | 'popstate';

interface BrowseRequest {
  browse: MarketplaceBrowseParams;
  history: HistoryMode;
}

/**
 * The complete state and actions interface for the Marketplace overview.
 * Components use only this interface, through useMarketplaceContext().
 */
export interface MarketplaceStateShape {
  // ── Server state
  marketplaces: Marketplace[];
  /** The browse values of the results on screen. */
  browse: MarketplaceBrowseParams;
  skills: PackageOverview[];
  total: number;
  status: LoadStatus;
  error: string | null;

  // ── Local state
  /** The text in the search box. It can differ from `browse.query` while the user types. */
  searchText: string;

  // ── Actions
  /** Sets the search box text. The search starts 300 ms after the last change. */
  setSearchText(text: string): void;
  clearSearch(): void;
  selectMarketplace(name: string): void;
  goToPage(page: number): void;
  setPageSize(pageSize: number): void;
  /** Sets the sort order of the skills and goes back to page 1. */
  setSort(sort: SkillSort): void;
  /** Sends the failed request again. */
  retry(): void;
}

const SEARCH_DEBOUNCE_MS = 300;

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * `onMarketplacePicked` is called once after a successful load that shows the
 * Marketplace that the user picked with selectMarketplace(). It is not called
 * for any other change. The provider uses it to save the pick.
 */
export function useMarketplace(
  initialData: MarketplaceOverviewData,
  initialError: string | null = null,
  onMarketplacePicked?: (name: string) => void,
): MarketplaceStateShape {
  const [overview, setOverview] = useState<MarketplaceOverviewData>(initialData);
  const [status, setStatus] = useState<LoadStatus>(() => toInitialStatus(initialError));
  const [error, setError] = useState<string | null>(initialError);
  const [searchText, setSearchTextState] = useState<string>(initialData.browse.query);

  // Timers and event listeners read these refs, because they can run after a
  // later render has changed the state.

  // The browse values of the results on screen.
  const committedRef = useRef<MarketplaceBrowseParams>(initialData.browse);
  const hasSuccessfulOverviewRef = useRef(initialError === null);
  // The request in flight, or null.
  const pendingRef = useRef<BrowseRequest | null>(null);
  // The request that failed last, for retry(), or null.
  const failedRef = useRef<BrowseRequest | null>(toInitialFailedRequest(initialData, initialError));
  // The controller of the request in flight. A newer request aborts it.
  const controllerRef = useRef<AbortController | null>(null);
  // Increases by 1 for each request. A response from an older request is ignored.
  const requestIdRef = useRef(0);
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The Marketplace that the user picked, until a successful load shows it, or null.
  const pickedMarketplaceRef = useRef<string | null>(null);
  // The latest onMarketplacePicked, so that load() stays stable.
  const onMarketplacePickedRef = useRef(onMarketplacePicked);

  useEffect(() => {
    onMarketplacePickedRef.current = onMarketplacePicked;
  }, [onMarketplacePicked]);

  const load = useCallback(async (request: BrowseRequest) => {
    // 1. Abort the request in flight and start a new request.
    if (controllerRef.current) {
      controllerRef.current.abort();
    }
    const controller = new AbortController();
    controllerRef.current = controller;
    requestIdRef.current += 1;
    const requestId = requestIdRef.current;
    pendingRef.current = request;

    // 2. Keep the current results on screen.
    setStatus('loading');
    if (request.history === 'popstate') {
      // The request follows the URL. Show the search text of the URL.
      setSearchTextState(request.browse.query);
    }

    const result = await fetchMarketplaceOverview(request.browse, controller.signal);

    // 3. Ignore a response that is not from the newest request.
    if (result.cancelled || requestId !== requestIdRef.current) {
      return;
    }
    pendingRef.current = null;

    if (result.overview === null) {
      // 4a. Error: keep the current results and URL.
      failedRef.current = request;
      if (result.partialOverview && !hasSuccessfulOverviewRef.current) {
        // The list loaded, even though this Marketplace's Skills failed.
        // Keep the selector available so another Marketplace can be picked.
        committedRef.current = result.partialOverview.browse;
        setOverview(result.partialOverview);
      }
      setError(result.error);
      setStatus('error');
      if (request.history === 'popstate') {
        // Back or Forward changed the URL. Restore the URL of the results on screen.
        window.history.replaceState(null, '', browseUrl(committedRef.current));
        setSearchTextState(committedRef.current.query);
      }
      return;
    }

    // 4b. Success: show the new results and write the browse values to the URL.
    const next = result.overview;
    failedRef.current = null;
    committedRef.current = next.browse;
    hasSuccessfulOverviewRef.current = true;
    setOverview(next);
    setError(null);
    setStatus('success');
    if (request.history === 'push') {
      window.history.pushState(null, '', browseUrl(next.browse));
    } else {
      window.history.replaceState(null, '', browseUrl(next.browse));
    }
    if (
      pickedMarketplaceRef.current !== null &&
      next.browse.marketplace === pickedMarketplaceRef.current
    ) {
      // The results show the Marketplace that the user picked. Report it once.
      pickedMarketplaceRef.current = null;
      const onPicked = onMarketplacePickedRef.current;
      if (onPicked) {
        onPicked(next.browse.marketplace);
      }
    }
  }, []);

  /** The browse values that a new change starts from. */
  const baseBrowse = useCallback((): MarketplaceBrowseParams => {
    if (pendingRef.current) {
      // A request is in flight. Start from the newest intent of the user.
      return pendingRef.current.browse;
    }
    // No request is in flight. Start from the results on screen.
    return committedRef.current;
  }, []);

  // On mount, make sure that the results on screen belong to the URL.
  useEffect(() => {
    const urlBrowse = parseBrowseParams(new URLSearchParams(window.location.search));
    if (!isRenderForUrl(initialData, urlBrowse)) {
      // Back from another page, for example a Skill page, restored an old
      // server render from the Next.js cache. The URL is correct, but the
      // results on screen are not. Load the results for the URL.
      // The load starts in a timer callback, because an effect must not change
      // the state of its component synchronously (react-hooks/set-state-in-effect).
      const timer = setTimeout(() => {
        void load({ browse: urlBrowse, history: 'popstate' });
      }, 0);
      return () => clearTimeout(timer);
    }
    if (initialError !== null) {
      // The first load failed. Keep the URL, so that a reload tries it again.
      return;
    }
    // Write the validated browse values to the URL. This removes invalid
    // values, such as `?page=-2`, from the address bar.
    const url = browseUrl(committedRef.current);
    if (url !== window.location.pathname + window.location.search) {
      window.history.replaceState(null, '', url);
    }
  }, [initialData, initialError, load]);

  // Back and Forward: load the result page of the restored URL.
  useEffect(() => {
    function handlePopState() {
      // History navigation supersedes active requests and typed text whose
      // debounce deadline has not arrived yet.
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
        searchTimerRef.current = null;
      }
      if (controllerRef.current) {
        controllerRef.current.abort();
      }
      requestIdRef.current += 1;
      pendingRef.current = null;
      pickedMarketplaceRef.current = null;
      if (window.location.pathname !== APP_ROUTES.marketplace.overview) {
        // The restored URL is another page. Next.js handles it.
        return;
      }
      const browse = parseBrowseParams(new URLSearchParams(window.location.search));
      if (sameBrowse(browse, committedRef.current)) {
        // Forward can restore the visible results before Back finishes.
        setSearchTextState(browse.query);
        if (hasSuccessfulOverviewRef.current) {
          failedRef.current = null;
          setError(null);
          setStatus('success');
        } else {
          setStatus('error');
        }
        return;
      }
      void load({ browse, history: 'popstate' });
    }
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, [load]);

  // On unmount, stop the search timer and the request in flight.
  useEffect(() => {
    return () => {
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }
      if (controllerRef.current) {
        controllerRef.current.abort();
      }
    };
  }, []);

  const setSearchText = useCallback(
    (text: string) => {
      setSearchTextState(text);
      if (searchTimerRef.current) {
        clearTimeout(searchTimerRef.current);
      }
      searchTimerRef.current = setTimeout(() => {
        const base = baseBrowse();
        const query = toSearchQuery(text);
        if (query === base.query) {
          // The search did not change, for example when the user added a space.
          return;
        }
        void load({ browse: { ...base, query, page: 1 }, history: 'replace' });
      }, SEARCH_DEBOUNCE_MS);
    },
    [baseBrowse, load],
  );

  const clearSearch = useCallback(() => {
    if (searchTimerRef.current) {
      clearTimeout(searchTimerRef.current);
    }
    setSearchTextState('');
    void load({ browse: { ...baseBrowse(), query: '', page: 1 }, history: 'replace' });
  }, [baseBrowse, load]);

  const selectMarketplace = useCallback(
    (name: string) => {
      // A later request can replace this one, for example a search. The pick
      // counts when a successful load shows this Marketplace.
      pickedMarketplaceRef.current = name;
      void load({ browse: { ...baseBrowse(), marketplace: name, page: 1 }, history: 'replace' });
    },
    [baseBrowse, load],
  );

  const goToPage = useCallback(
    (page: number) => {
      void load({ browse: { ...baseBrowse(), page }, history: 'push' });
    },
    [baseBrowse, load],
  );

  const setPageSize = useCallback(
    (pageSize: number) => {
      void load({ browse: { ...baseBrowse(), pageSize, page: 1 }, history: 'replace' });
    },
    [baseBrowse, load],
  );

  const setSort = useCallback(
    (sort: SkillSort) => {
      void load({ browse: { ...baseBrowse(), sort, page: 1 }, history: 'replace' });
    },
    [baseBrowse, load],
  );

  const retry = useCallback(() => {
    if (failedRef.current) {
      void load(failedRef.current);
    }
  }, [load]);

  return {
    marketplaces: overview.marketplaces,
    browse: overview.browse,
    skills: overview.skills,
    total: overview.total,
    status,
    error,
    searchText,
    setSearchText,
    clearSearch,
    selectMarketplace,
    goToPage,
    setPageSize,
    setSort,
    retry,
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function toInitialStatus(initialError: string | null): LoadStatus {
  if (initialError !== null) {
    // The RSC load failed.
    return 'error';
  }
  // The RSC loaded the first result page.
  return 'success';
}

function toInitialFailedRequest(
  initialData: MarketplaceOverviewData,
  initialError: string | null,
): BrowseRequest | null {
  if (initialError !== null) {
    // The RSC load failed. retry() sends the same browse values through the BFF.
    return { browse: initialData.browse, history: 'replace' };
  }
  // Nothing failed.
  return null;
}

/**
 * Tells whether a server render belongs to the browse values in the URL. It
 * does when the URL has the values that the request asked for, or the values
 * that the server used. The second case occurs after the hook wrote the
 * validated values to the URL.
 */
function isRenderForUrl(
  data: MarketplaceOverviewData,
  urlBrowse: MarketplaceBrowseParams,
): boolean {
  return sameBrowse(urlBrowse, data.requested) || sameBrowse(urlBrowse, data.browse);
}

/** The overview URL for the browse values. Other query keys stay unchanged. */
function browseUrl(browse: MarketplaceBrowseParams): string {
  return buildBrowseUrl(window.location.pathname, window.location.search, browse);
}

function sameBrowse(a: MarketplaceBrowseParams, b: MarketplaceBrowseParams): boolean {
  return (
    a.marketplace === b.marketplace &&
    a.query === b.query &&
    a.page === b.page &&
    a.pageSize === b.pageSize &&
    a.sort === b.sort
  );
}
