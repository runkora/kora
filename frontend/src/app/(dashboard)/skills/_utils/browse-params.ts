// Validates Marketplace browse values for API requests and initial defaults.
// The overview path carries the selected Marketplace; other browse values stay in memory.

import {
  PAGE_SIZE,
  DEFAULT_SORT,
  DEFAULT_SEARCH_SORT,
  MIN_SEARCH_LENGTH,
  SORT_OPTIONS,
  type MarketplaceBrowseParams,
  type ResultState,
  type SkillSort,
} from '../_types';

/** The browse query keys removed from navigation URLs. */
const BROWSE_KEYS = ['marketplace', 'query', 'page', 'pageSize', 'sort'];

/**
 * Reads the browse values from a URL query and validates them. An absent or
 * invalid value becomes its default. The server checks the Marketplace name,
 * because only the server has the Marketplace list.
 */
export function parseBrowseParams(search: URLSearchParams): MarketplaceBrowseParams {
  const query = toSearchQuery(search.get('query'));
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
    query,
    page: toPage(search.get('page')),
    pageSize: PAGE_SIZE,
    sort: toSort(search.get('sort'), query !== ''),
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

function toSort(value: string | null, searching: boolean): SkillSort {
  const option = SORT_OPTIONS.find((sort) => sort === value);
  if (option && (option !== 'relevance' || searching)) {
    // One of the sort options.
    return option;
  }
  // No sort, or a value that is not an option. Use the default sort.
  return searching ? DEFAULT_SEARCH_SORT : DEFAULT_SORT;
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
 * Removes browse values from an existing URL. Other query keys, such as
 * `agent` for the agent builder or Storybook's `id`, stay unchanged.
 */
export function cleanBrowseUrl(pathname: string, currentSearch: string): string {
  const search = new URLSearchParams(currentSearch);
  BROWSE_KEYS.forEach((key) => search.delete(key));

  const queryString = search.toString();
  if (queryString) {
    return `${pathname}?${queryString}`;
  }
  return pathname;
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
