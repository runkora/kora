'use client';

// ── useMarketplace hook ───────────────────────────────────────────────────────
//
// Owns all state for the Marketplace overview. The RSC page loads the first
// result page, and the hook starts from that data. Each later browse change
// (search, Marketplace, sort order, page) goes through the BFF:
//
//   1. Abort the request in flight and start a new request.
//   2. Keep the current results on screen. The view shows a progress notice.
//   3. Ignore a response that is not from the newest request.
//   4. On success, show the new results. On error, keep the current results
//      and show a warning.
//
// The address identifies the selected Marketplace. Other browse values stay
// in shared React state while navigating to a Skill and back. Returning reloads
// the remembered page; a hard refresh starts from server defaults.

import { startTransition, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { usePathname } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { Marketplace, SkillSummary } from '@orbit/shared/api';
import { MarketplaceBrowseStateContext } from '../_context/marketplace-browse-state';
import { fetchMarketplaceOverview } from '../_services/marketplace-api';
import { cleanBrowseUrl, parseBrowseParams, toSearchQuery } from '../_utils/browse-params';
import {
  PAGE_SIZE,
  DEFAULT_SEARCH_SORT,
  type MarketplaceBrowseParams,
  type MarketplaceOverviewData,
  type SkillSort,
} from '../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

interface BrowseRequest {
  browse: MarketplaceBrowseParams;
}

/**
 * The complete state and actions interface for the Marketplace overview.
 * Components use only this interface, through useMarketplaceContext().
 */
export interface MarketplaceStateShape {
  // ── Server state
  marketplaces: Marketplace[];
  /** The display name of the Marketplace on screen. browse.marketplace is its slug. */
  marketplaceName: string;
  /** The browse values of the results on screen. */
  browse: MarketplaceBrowseParams;
  skills: SkillSummary[];
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
  selectMarketplace(marketplaceSlug: string): void;
  goToPage(page: number): void;
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
  onMarketplacePicked?: (marketplaceSlug: string) => void,
): MarketplaceStateShape {
  const routePathname = usePathname();
  const sharedBrowse = useContext(MarketplaceBrowseStateContext);
  const rememberBrowse = sharedBrowse?.rememberBrowse;
  const [restoredBrowse] = useState<MarketplaceBrowseParams | null>(() => {
    const marketplace = marketplaceFromPath(routePathname) ?? initialData.browse.marketplace;
    if (
      initialError !== null ||
      !initialData.marketplaces.some((entry) => entry.marketplaceSlug === marketplace)
    ) {
      return null;
    }
    const remembered = sharedBrowse?.browseByMarketplace[marketplace]?.browse;
    const loaded = initialData.browse;
    if (
      !remembered ||
      (remembered.marketplace === loaded.marketplace &&
        remembered.query === loaded.query &&
        remembered.page === loaded.page &&
        PAGE_SIZE === loaded.pageSize &&
        remembered.sort === loaded.sort)
    ) {
      return null;
    }
    return { ...remembered, pageSize: PAGE_SIZE };
  });
  const [overview, setOverview] = useState<MarketplaceOverviewData>(() =>
    restoredBrowse
      ? { ...initialData, requested: restoredBrowse, browse: restoredBrowse, skills: [], total: 0 }
      : initialData,
  );
  const [status, setStatus] = useState<LoadStatus>(() =>
    restoredBrowse ? 'loading' : toInitialStatus(initialError),
  );
  const [error, setError] = useState<string | null>(initialError);
  const [searchText, setSearchTextState] = useState<string>(overview.browse.query);

  // Timers and asynchronous requests read these refs, because they can run after a
  // later render has changed the state.

  // The browse values of the results on screen.
  const committedRef = useRef<MarketplaceBrowseParams>(overview.browse);
  const hasSuccessfulOverviewRef = useRef(initialError === null && restoredBrowse === null);
  // The request in flight, or null.
  const pendingRef = useRef<BrowseRequest | null>(null);
  // The request that failed last, for retry(), or null.
  const failedRef = useRef<BrowseRequest | null>(toInitialFailedRequest(initialData, initialError));
  // The controller of the request in flight. A newer request aborts it.
  const controllerRef = useRef<AbortController | null>(null);
  // Increases by 1 for each request. A response from an older request is ignored.
  const requestIdRef = useRef(0);
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Clearing search restores the last alphabetical choice, also after a Skill visit.
  const browseSortRef = useRef<Exclude<SkillSort, 'relevance'>>(
    sharedBrowse?.browseByMarketplace[overview.browse.marketplace]?.browseSort ??
      (overview.browse.sort === 'name-desc' ? 'name-desc' : 'name-asc'),
  );
  // The Marketplace that the user picked, until a successful load shows it, or null.
  const pickedMarketplaceRef = useRef<string | null>(null);
  // The latest onMarketplacePicked, so that load() stays stable.
  const onMarketplacePickedRef = useRef(onMarketplacePicked);

  useEffect(() => {
    onMarketplacePickedRef.current = onMarketplacePicked;
  }, [onMarketplacePicked]);

  const load = useCallback(
    async (request: BrowseRequest) => {
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

      const result = await fetchMarketplaceOverview(
        { ...request.browse, pageSize: PAGE_SIZE },
        controller.signal,
      );

      // 3. Ignore a response that is not from the newest request.
      if (result.cancelled || requestId !== requestIdRef.current) {
        return;
      }
      pendingRef.current = null;

      if (result.overview === null) {
        // 4a. Error: keep the current results.
        failedRef.current = request;
        if (result.partialOverview && !hasSuccessfulOverviewRef.current) {
          // The list loaded, even though this Marketplace's Skills failed.
          // Keep the selector available so another Marketplace can be picked.
          committedRef.current = result.partialOverview.browse;
          setOverview(result.partialOverview);
        }
        setError(result.error);
        setStatus('error');
        return;
      }

      // 4b. Success: show the new results.
      const next = result.overview;
      if (next.browse.marketplace) {
        rememberBrowse?.(next.browse, browseSortRef.current);
      }
      failedRef.current = null;
      committedRef.current = next.browse;
      hasSuccessfulOverviewRef.current = true;
      setOverview(next);
      setError(null);
      setStatus('success');
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
    },
    [rememberBrowse],
  );

  useEffect(() => {
    if (restoredBrowse) {
      startTransition(() => {
        void load({ browse: restoredBrowse });
      });
    }
  }, [restoredBrowse, load]);

  /** The browse values that a new change starts from. */
  const baseBrowse = useCallback((): MarketplaceBrowseParams => {
    if (pendingRef.current) {
      // A request is in flight. Start from the newest intent of the user.
      return pendingRef.current.browse;
    }
    // No request is in flight. Start from the results on screen.
    return committedRef.current;
  }, []);

  // Back can restore the default entry's cached data under a named path.
  // Load that path's Marketplace unless the server already resolved it.
  useEffect(() => {
    const marketplace = marketplaceFromPath(routePathname);
    if (
      marketplace &&
      marketplace !== committedRef.current.marketplace &&
      marketplace !== initialData.requested.marketplace
    ) {
      startTransition(() => {
        void load({ browse: parseBrowseParams(new URLSearchParams({ marketplace })) });
      });
    }
  }, [routePathname, initialData.requested.marketplace, load]);

  // Keep the path aligned with the Marketplace whose results are on screen.
  // Storybook preview paths stay in Storybook; old browse query values are removed.
  useEffect(() => {
    if (
      pendingRef.current &&
      pendingRef.current.browse.marketplace !== overview.browse.marketplace
    ) {
      return;
    }
    const currentPath = window.location.pathname;
    const isOverview =
      currentPath === APP_ROUTES.marketplace.overview || marketplaceFromPath(currentPath) !== null;
    if (!isOverview || (routePathname && routePathname !== currentPath)) return;
    const marketplace = overview.browse.marketplace;
    const pathname = marketplace
      ? APP_ROUTES.marketplace.browse(marketplace)
      : APP_ROUTES.marketplace.overview;
    const url = cleanBrowseUrl(pathname, window.location.search);
    if (url !== window.location.pathname + window.location.search) {
      // Next.js copies its own history state and updates usePathname for this call.
      window.history.replaceState(null, '', url + window.location.hash);
    }
  }, [overview.browse.marketplace, routePathname, status]);

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
        const sort = query ? (base.query ? base.sort : DEFAULT_SEARCH_SORT) : browseSortRef.current;
        void load({ browse: { ...base, query, sort, page: 1 } });
      }, SEARCH_DEBOUNCE_MS);
    },
    [baseBrowse, load],
  );

  const clearSearch = useCallback(() => {
    if (searchTimerRef.current) {
      clearTimeout(searchTimerRef.current);
    }
    setSearchTextState('');
    void load({ browse: { ...baseBrowse(), query: '', sort: browseSortRef.current, page: 1 } });
  }, [baseBrowse, load]);

  const selectMarketplace = useCallback(
    (marketplaceSlug: string) => {
      // A later request can replace this one, for example a search. The pick
      // counts when a successful load shows this Marketplace.
      pickedMarketplaceRef.current = marketplaceSlug;
      void load({ browse: { ...baseBrowse(), marketplace: marketplaceSlug, page: 1 } });
    },
    [baseBrowse, load],
  );

  const goToPage = useCallback(
    (page: number) => {
      void load({ browse: { ...baseBrowse(), page } });
    },
    [baseBrowse, load],
  );

  const setSort = useCallback(
    (sort: SkillSort) => {
      if (sort !== 'relevance') browseSortRef.current = sort;
      void load({ browse: { ...baseBrowse(), sort, page: 1 } });
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
    marketplaceName: toMarketplaceName(overview),
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
    setSort,
    retry,
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function marketplaceFromPath(pathname: string | null): string | null {
  const match = pathname?.match(/^\/marketplace\/([^/]+)\/?$/);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
}

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
    return { browse: initialData.browse };
  }
  // Nothing failed.
  return null;
}

/** Finds the display name of the Marketplace on screen, from its slug. */
function toMarketplaceName(overview: MarketplaceOverviewData): string {
  const shown = overview.marketplaces.find(
    (marketplace) => marketplace.marketplaceSlug === overview.browse.marketplace,
  );
  return shown ? shown.name : '';
}
