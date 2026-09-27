import type { Marketplace, PackageOverview, SkillSort } from '@orbit/shared/api';

/** Rows-per-page options. A `pageSize` in the URL must be one of these values. */
export const PAGE_SIZES = [12, 24, 48] as const;

export const DEFAULT_PAGE_SIZE = 24;

/** Sort options. A `sort` in the URL must be one of these values. */
export const SORT_OPTIONS: readonly SkillSort[] = ['name-asc', 'name-desc'];

/** The sort order when the URL has no `sort`: A to Z by skill name. */
export const DEFAULT_SORT: SkillSort = 'name-asc';

/** A search shorter than this counts as no search. */
export const MIN_SEARCH_LENGTH = 3;

/** The browse values that the Marketplace overview keeps in the URL. */
export interface MarketplaceBrowseParams {
  /** Marketplace name. Empty means "use the default Marketplace". */
  marketplace: string;
  /** Search text. Empty means "no search". */
  query: string;
  /** Page number, from 1. */
  page: number;
  pageSize: number;
  /** Sort order of the skills, by skill name. */
  sort: SkillSort;
}

/** One loaded result page of the Marketplace overview. */
export interface MarketplaceOverviewData {
  marketplaces: Marketplace[];
  /**
   * The browse values that the request asked for, before the server corrected
   * the Marketplace name and the page. The hook compares them with the URL.
   */
  requested: MarketplaceBrowseParams;
  /** The browse values that the server used, after validation. */
  browse: MarketplaceBrowseParams;
  skills: PackageOverview[];
  total: number;
}

/** What a successful browse request found. */
export type ResultState = 'populated' | 'configured_but_empty' | 'no_matches';
