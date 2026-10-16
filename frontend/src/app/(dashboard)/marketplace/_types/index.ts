import type { Marketplace, SkillSummary } from '@orbit/shared/api';

/**
 * Sort order of a Skill list, by Skill name. The skills-service does not sort
 * by name, so the server service sorts before it cuts the page.
 */
export type SkillSort = 'name-asc' | 'name-desc';

/** Rows-per-page options accepted by the browse controls and BFF request parser. */
export const PAGE_SIZES = [12, 24, 48] as const;

export const DEFAULT_PAGE_SIZE = 24;

/** Sort options accepted by the browse controls and BFF request parser. */
export const SORT_OPTIONS: readonly SkillSort[] = ['name-asc', 'name-desc'];

/** The default sort order: A to Z by Skill name. */
export const DEFAULT_SORT: SkillSort = 'name-asc';

/** A search shorter than this counts as no search. */
export const MIN_SEARCH_LENGTH = 3;

/** Browse state kept in memory and sent as query values on BFF API requests. */
export interface MarketplaceBrowseParams {
  /** The marketplaceSlug of the Marketplace. Empty means "use the default Marketplace". */
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
   * the Marketplace slug and page. These describe the request independently
   * of the browser's address.
   */
  requested: MarketplaceBrowseParams;
  /** The browse values that the server used, after validation. */
  browse: MarketplaceBrowseParams;
  skills: SkillSummary[];
  total: number;
}

/** What a successful browse request found. */
export type ResultState = 'populated' | 'configured_but_empty' | 'no_matches';
