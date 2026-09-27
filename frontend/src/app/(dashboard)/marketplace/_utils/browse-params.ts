// Reads, validates, and writes the Marketplace browse values in the URL.
// The RSC page, the BFF route, and the client hook all use these functions,
// so the three request paths apply the same rules and defaults.

import type { SkillSort } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';
import {
  DEFAULT_PAGE_SIZE,
  DEFAULT_SORT,
  MIN_SEARCH_LENGTH,
  PAGE_SIZES,
  SORT_OPTIONS,
  type MarketplaceBrowseParams,
  type ResultState,
} from '../_types';

/** The URL keys of the browse values. Other keys in the URL stay unchanged. */
const BROWSE_KEYS = ['marketplace', 'query', 'page', 'pageSize', 'sort'];

/**
 * Reads the browse values from a URL query and validates them. An absent or
 * invalid value becomes its default. The server checks the Marketplace name,
 * because only the server has the Marketplace list.
 */
export function parseBrowseParams(search: URLSearchParams): MarketplaceBrowseParams {
  let marketplace: string;
  const marketplaceFromUrl = search.get('marketplace');
  if (marketplaceFromUrl) {
    // The URL names a Marketplace.
    marketplace = marketplaceFromUrl;
  } else {
    // No Marketplace in the URL. The server uses the first Marketplace.
    marketplace = '';
  }

  return {
    marketplace,
    query: toSearchQuery(search.get('query')),
    page: toPage(search.get('page')),
    pageSize: toPageSize(search.get('pageSize')),
    sort: toSort(search.get('sort')),
  };
}

/**
 * Converts the text in the search box to the search that the server applies.
 * Text shorter than MIN_SEARCH_LENGTH counts as no search.
 */
export function toSearchQuery(text: string | null): string {
  let trimmed: string;
  if (text) {
    // The user typed text. Ignore the spaces at each end.
    trimmed = text.trim();
  } else {
    // No text.
    trimmed = '';
  }

  if (trimmed.length < MIN_SEARCH_LENGTH) {
    // Too short to search. Show all Skills.
    return '';
  }
  return trimmed;
}

function toPage(value: string | null): number {
  const page = Number(value);
  if (Number.isInteger(page) && page >= 1) {
    // A whole page number from 1 upward.
    return page;
  }
  // No page, or not a whole number from 1 upward. Start at page 1.
  return 1;
}

function toPageSize(value: string | null): number {
  const size = Number(value);
  const allowedSizes: readonly number[] = PAGE_SIZES;
  if (allowedSizes.includes(size)) {
    // One of the rows-per-page options.
    return size;
  }
  // No size, or a size that is not an option. Use the default size.
  return DEFAULT_PAGE_SIZE;
}

function toSort(value: string | null): SkillSort {
  const option = SORT_OPTIONS.find((sort) => sort === value);
  if (option) {
    // One of the sort options.
    return option;
  }
  // No sort, or a value that is not an option. Use the default sort.
  return DEFAULT_SORT;
}

/**
 * Converts the `searchParams` object of a Next.js page to URLSearchParams.
 * When a key occurs more than once, the first value is used.
 */
export function recordToSearchParams(
  record: Record<string, string | string[] | undefined>,
): URLSearchParams {
  const search = new URLSearchParams();
  Object.entries(record).forEach(([key, value]) => {
    if (typeof value === 'string') {
      // The key occurs once.
      search.set(key, value);
    } else if (Array.isArray(value) && value.length > 0) {
      // The key occurs more than once. Use the first value.
      search.set(key, value[0]);
    }
  });
  return search;
}

/**
 * Returns the URL of the overview with the given browse values. The other
 * query keys, such as `agent` for the agent builder, stay unchanged. Default
 * values are not written, so that the URL stays short.
 */
export function buildBrowseUrl(
  pathname: string,
  currentSearch: string,
  browse: MarketplaceBrowseParams,
): string {
  const search = new URLSearchParams(currentSearch);
  BROWSE_KEYS.forEach((key) => search.delete(key));

  if (browse.marketplace) {
    search.set('marketplace', browse.marketplace);
  }
  if (browse.query) {
    search.set('query', browse.query);
  }
  if (browse.page !== 1) {
    search.set('page', String(browse.page));
  }
  if (browse.pageSize !== DEFAULT_PAGE_SIZE) {
    search.set('pageSize', String(browse.pageSize));
  }
  if (browse.sort !== DEFAULT_SORT) {
    search.set('sort', browse.sort);
  }

  const queryString = search.toString();
  if (queryString) {
    return `${pathname}?${queryString}`;
  }
  return pathname;
}

/**
 * Returns the URL of a Skill page. The URL carries the browse values of the
 * overview, so that the back link on the Skill page returns to the same
 * results (PRD FR-17). The Skill page uses these values only for that link.
 */
export function buildSkillUrl(slug: string, browse: MarketplaceBrowseParams): string {
  return buildBrowseUrl(APP_ROUTES.marketplace.detail(slug), '', browse);
}

/** Returns the URL of the overview with the given browse values, and no other query keys. */
export function buildOverviewUrl(browse: MarketplaceBrowseParams): string {
  return buildBrowseUrl(APP_ROUTES.marketplace.overview, '', browse);
}

/** Tells what a successful browse request found. */
export function toResultState(total: number, query: string): ResultState {
  if (total > 0) {
    // The request found Skills.
    return 'populated';
  }
  if (query) {
    // The search found no Skills.
    return 'no_matches';
  }
  // The Marketplace has no Skills that the user can view.
  return 'configured_but_empty';
}
