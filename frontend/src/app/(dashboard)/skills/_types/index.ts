import type { Marketplace, SkillSummary } from '@orbit/shared/api';

/**
 * Search can preserve backend relevance; alphabetical sorting happens in the BFF.
 */
export type SkillSort = 'name-asc' | 'name-desc' | 'relevance';

/** Every Marketplace result page contains at most 24 Skills. */
export const PAGE_SIZE = 24;

/** Sort options accepted by the browse controls and BFF request parser. */
export const SORT_OPTIONS: readonly SkillSort[] = ['name-asc', 'name-desc', 'relevance'];

/** The default sort order: A to Z by Skill name. */
export const DEFAULT_SORT: SkillSort = 'name-asc';

/** Active searches keep the relevance order returned by skills-service. */
export const DEFAULT_SEARCH_SORT: SkillSort = 'relevance';

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
  /** Alphabetical order, or backend relevance during search. */
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
