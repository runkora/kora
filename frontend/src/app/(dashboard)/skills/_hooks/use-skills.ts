'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import type { Marketplace, SkillSummary } from '@runkora/shared/api';
import { APP_ROUTES } from '@runkora/lib/routing';
import { usePreferences } from '../../_preferences/preferences-context';
import { fetchSkills } from '../_services/skills-api';
import {
  PAGE_SIZE,
  browseQuery,
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
  /** The offset of the skills on screen, which lag `browse` while a page loads. */
  offset: number;
  total: number;
  failures: SkillsOverview['failures'];
  status: LoadStatus;
  error: SkillsLoadError | null;
  /** The user's favourites as `marketplace:path` keys, from the preferences document. */
  favourites: Set<string>;
  toggleFavourite(key: string): void;
  /** The search box text. It becomes the search 300 ms after the last change. */
  searchText: string;
  setSearchText(text: string): void;
  clearSearch(): void;
  /** A Marketplace slug, `''` for every Marketplace, or null for the Favourites view. */
  selectChip(marketplace: string | null): void;
  goToPage(page: number): void;
  retry(): void;
}

const SEARCH_DEBOUNCE_MS = 300;
const BROWSE_KEYS = ['marketplace', 'favourites', 'search', 'page'];

/** The overview URL for the browse values, keeping any other query keys such as `agent`. */
function browseUrl(browse: SkillsBrowse): string {
  const query = new URLSearchParams(window.location.search);
  BROWSE_KEYS.forEach((key) => query.delete(key));
  new URLSearchParams(browseQuery(browse)).forEach((value, key) => query.set(key, value));
  const qs = query.toString();
  return `${APP_ROUTES.skills.root}${qs ? `?${qs}` : ''}`;
}

/** The browse values in the address bar, or null during the server render and off the overview. */
function browseFromLocation(): SkillsBrowse | null {
  if (typeof window === 'undefined' || window.location.pathname !== APP_ROUTES.skills.root) {
    return null;
  }
  return toBrowse(Object.fromEntries(new URLSearchParams(window.location.search)));
}

function sameBrowse(a: SkillsBrowse, b: SkillsBrowse): boolean {
  return (
    a.marketplace === b.marketplace &&
    a.favourites === b.favourites &&
    a.search === b.search &&
    a.page === b.page
  );
}

/**
 * The RSC page loads the first page from the URL. Each later change writes the URL with
 * replaceState and fetches that one page through the BFF.
 */
export function useSkills(
  initial: SkillsOverview,
  initialError: SkillsLoadError | null,
): SkillsStateShape {
  const { preferences, updatePreferences } = usePreferences();
  const favouriteKeys = preferences.skills.favouriteSkills;
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
  const [offset, setOffset] = useState((initial.browse.page - 1) * PAGE_SIZE);
  const [failures, setFailures] = useState(restored ? [] : initial.failures);
  const [status, setStatus] = useState<LoadStatus>(
    restored ? 'loading' : initialError ? 'error' : 'success',
  );
  const [error, setError] = useState(restored ? null : initialError);
  // Null until the user types, so the box follows the search the URL restores.
  const [typed, setTyped] = useState<string | null>(null);
  const searchText = typed ?? browse.search;
  const inFlight = useRef<AbortController | null>(null);
  // Read at request time, so toggling a favourite does not change the fetch callbacks.
  const keys = useRef(favouriteKeys);
  useEffect(() => {
    keys.current = favouriteKeys;
  }, [favouriteKeys]);

  const fetchPage = useCallback((next: SkillsBrowse) => {
    // A newer request aborts this one, so only the latest page reaches the screen.
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;

    void fetchSkills(next, keys.current, controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      setError(result.error);
      setSkills(result.data?.skills ?? []);
      setTotal(result.data?.total ?? 0);
      setOffset(result.data?.offset ?? 0);
      setFailures(result.data?.failures ?? []);
      setStatus(result.error ? 'error' : 'success');
    });
  }, []);

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

  const toggleFavourite = useCallback(
    (key: string) =>
      updatePreferences((prev) => {
        const keys = prev.skills.favouriteSkills;
        const next = keys.includes(key) ? keys.filter((k) => k !== key) : [...keys, key];
        return { ...prev, skills: { favouriteSkills: next } };
      }),
    [updatePreferences],
  );

  const clearSearch = useCallback(() => {
    setTyped(null);
    load({ ...browse, search: '', page: 1 });
  }, [browse, load]);

  const selectChip = useCallback(
    (marketplace: string | null) =>
      load({
        ...browse,
        marketplace: marketplace ?? '',
        favourites: marketplace === null,
        page: 1,
      }),
    [browse, load],
  );

  const goToPage = useCallback((page: number) => load({ ...browse, page }), [browse, load]);

  const retry = useCallback(() => load(browse), [browse, load]);

  return {
    marketplaces: initial.marketplaces,
    browse,
    skills,
    offset,
    total,
    failures,
    status,
    error,
    favourites: new Set(favouriteKeys),
    toggleFavourite,
    searchText,
    setSearchText: setTyped,
    clearSearch,
    selectChip,
    goToPage,
    retry,
  };
}
