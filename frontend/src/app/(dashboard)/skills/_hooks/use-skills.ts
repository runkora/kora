'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import type { Marketplace, SkillSummary } from '@runkora/shared/api';
import { APP_ROUTES } from '@runkora/lib/routing';
import { usePreferences } from '../../_preferences/preferences-context';
import { fetchSkillsPage } from '../_services/skills-api';
import {
  toBrowse,
  toSearch,
  type SkillsBrowse,
  type SkillsLoadError,
  type SkillsOverview,
} from '../_types';

export type LoadStatus = 'loading' | 'success' | 'error';

export interface SkillsStateShape {
  marketplaces: Marketplace[];
  /** The browse values of the latest request. The URL holds the same values. */
  browse: SkillsBrowse;
  skills: SkillSummary[];
  total: number;
  pageSize: number;
  status: LoadStatus;
  error: SkillsLoadError | null;
  /** The search box text. It becomes the search 300 ms after the last change. */
  searchText: string;
  setSearchText(text: string): void;
  clearSearch(): void;
  selectMarketplace(slug: string): void;
  goToPage(page: number): void;
  retry(): void;
}

const SEARCH_DEBOUNCE_MS = 300;

/** The overview URL for the browse values, keeping any other query keys such as `agent`. */
function browseUrl(browse: SkillsBrowse): string {
  const query = new URLSearchParams(window.location.search);
  query.delete('search');
  query.delete('page');
  if (browse.search) query.set('search', browse.search);
  if (browse.page > 1) query.set('page', String(browse.page));
  const qs = query.toString();
  return `${APP_ROUTES.skills.marketplace(browse.marketplace)}${qs ? `?${qs}` : ''}`;
}

/** The browse values in the address bar, or null during the server render. */
function browseFromLocation(): SkillsBrowse | null {
  if (typeof window === 'undefined') return null;
  const query = new URLSearchParams(window.location.search);
  const marketplace = decodeURIComponent(window.location.pathname.split('/')[2] ?? '');
  return toBrowse(marketplace, query.get('search'), query.get('page'));
}

function sameBrowse(a: SkillsBrowse, b: SkillsBrowse): boolean {
  return a.marketplace === b.marketplace && a.search === b.search && a.page === b.page;
}

/**
 * The RSC page loads the first page from the URL. Each later change writes the URL with
 * replaceState and fetches that one page through the BFF.
 */
export function useSkills(
  initial: SkillsOverview,
  initialError: SkillsLoadError | null,
): SkillsStateShape {
  const { preferences } = usePreferences();
  const pageSize = preferences.tables.defaultPageSize;
  // Browser Back restores this page with the props of its first render while the address
  // holds later browse values, so the address wins and its page loads on mount.
  const [restored] = useState(() => {
    const fromLocation = browseFromLocation();
    return fromLocation &&
      initial.marketplaces.length > 0 &&
      !sameBrowse(fromLocation, initial.browse)
      ? fromLocation
      : null;
  });
  const [browse, setBrowse] = useState(restored ?? initial.browse);
  const [skills, setSkills] = useState(restored ? [] : initial.skills);
  const [total, setTotal] = useState(restored ? 0 : initial.total);
  const [status, setStatus] = useState<LoadStatus>(
    restored ? 'loading' : initialError ? 'error' : 'success',
  );
  const [error, setError] = useState(restored ? null : initialError);
  // Null until the user types, so the box follows the search the URL restores.
  const [typed, setTyped] = useState<string | null>(null);
  const searchText = typed ?? browse.search;
  const inFlight = useRef<AbortController | null>(null);

  const fetchPage = useCallback(
    (next: SkillsBrowse) => {
      // A newer request aborts this one, so only the latest page reaches the screen.
      inFlight.current?.abort();
      const controller = new AbortController();
      inFlight.current = controller;

      void fetchSkillsPage(next, pageSize, controller.signal).then((result) => {
        if (controller.signal.aborted) return;
        setError(result.error);
        setSkills(result.data?.skills ?? []);
        setTotal(result.data?.total ?? 0);
        setStatus(result.error ? 'error' : 'success');
      });
    },
    [pageSize],
  );

  const load = useCallback(
    (next: SkillsBrowse) => {
      window.history.replaceState(null, '', browseUrl(next));
      setBrowse(next);
      setStatus('loading');
      setError(null);
      fetchPage(next);
    },
    [fetchPage],
  );

  useEffect(() => {
    if (restored) fetchPage(restored);
    const requests = inFlight;
    return () => requests.current?.abort();
  }, [restored, fetchPage]);

  useEffect(() => {
    const search = toSearch(typed);
    if (typed === null || search === browse.search) return;
    const timer = setTimeout(() => load({ ...browse, search, page: 1 }), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [typed, browse, load]);

  const clearSearch = useCallback(() => {
    setTyped(null);
    load({ ...browse, search: '', page: 1 });
  }, [browse, load]);

  const selectMarketplace = useCallback(
    (marketplace: string) => load({ ...browse, marketplace, page: 1 }),
    [browse, load],
  );

  const goToPage = useCallback((page: number) => load({ ...browse, page }), [browse, load]);

  const retry = useCallback(() => load(browse), [browse, load]);

  return {
    marketplaces: initial.marketplaces,
    browse,
    skills,
    total,
    pageSize,
    status,
    error,
    searchText,
    setSearchText: setTyped,
    clearSearch,
    selectMarketplace,
    goToPage,
    retry,
  };
}
