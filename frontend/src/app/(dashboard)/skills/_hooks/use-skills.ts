'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Marketplace, SkillSummary } from '@runkora/shared/api';
import { APP_ROUTES } from '@runkora/lib/routing';
import { usePreferences } from '../../_preferences/preferences-context';
import { fetchSkills } from '../_services/skills-api';
import {
  PAGE_SIZE,
  browseQuery,
  skillKey,
  toggleFavouriteKey,
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
  const { preferences, updatePreferences, flush } = usePreferences();
  const favouriteKeys = preferences.skills.favouriteSkills;
  // Browser Back restores the props of the first render while the address holds later browse
  // values. The address wins: its page loads on mount.
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
  // Null until the user types; until then the box shows the search the URL restored.
  const [typed, setTyped] = useState<string | null>(null);
  const searchText = typed ?? browse.search;
  const inFlight = useRef<AbortController | null>(null);

  const fetchPage = useCallback((next: SkillsBrowse, ready: Promise<void> = Promise.resolve()) => {
    // A newer request aborts this one: only the latest page reaches the screen.
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;

    void ready
      .then(() => (controller.signal.aborted ? null : fetchSkills(next, controller.signal)))
      .then((result) => {
        if (!result || controller.signal.aborted) return;
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
      window.history.replaceState(null, '', `${APP_ROUTES.skills.root}${browseQuery(next)}`);
      setBrowse(next);
      setStatus('loading');
      setError(null);
      // The BFF reads the stored favourites. A favourite added a moment ago is saved first.
      fetchPage(next, next.favourites ? flush() : undefined);
    },
    [fetchPage, flush],
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
    (key: string) => updatePreferences((prev) => toggleFavouriteKey(prev, key)),
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

  const favourites = useMemo(() => new Set(favouriteKeys), [favouriteKeys]);
  // A skill unfavourited in the Favourites view leaves it at once, as the design shows.
  const shown = browse.favourites ? skills.filter((s) => favourites.has(skillKey(s))) : skills;

  return {
    marketplaces: initial.marketplaces,
    browse,
    skills: shown,
    offset,
    total: total - (skills.length - shown.length),
    failures,
    status,
    error,
    favourites,
    toggleFavourite,
    searchText,
    setSearchText: setTyped,
    clearSearch,
    selectChip,
    goToPage,
    retry,
  };
}
