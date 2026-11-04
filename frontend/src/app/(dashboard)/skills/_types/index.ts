import type { Marketplace, SkillSummary } from '@orbit/shared/api';

/** skills-service treats a shorter search as no search, so the page does too. */
export const MIN_SEARCH_LENGTH = 3;

/** The browse state the overview URL holds: the path names the Marketplace. */
export interface SkillsBrowse {
  marketplace: string;
  search: string;
  page: number;
}

/** What the overview shows: the Marketplaces for the picker and one page of skills. */
export interface SkillsOverview {
  marketplaces: Marketplace[];
  browse: SkillsBrowse;
  skills: SkillSummary[];
  total: number;
}

/** A failed load. A 502 means the Marketplace is unavailable, and the message says why. */
export interface SkillsLoadError {
  status: number;
  message: string;
}

/** Search text as the API applies it: trimmed, and empty below MIN_SEARCH_LENGTH. */
export function toSearch(text: unknown): string {
  const trimmed = typeof text === 'string' ? text.trim() : '';
  return trimmed.length < MIN_SEARCH_LENGTH ? '' : trimmed;
}

/** Reads browse values from a URL or request query. A missing or invalid page is page 1. */
export function toBrowse(marketplace: string, search: unknown, page: unknown): SkillsBrowse {
  const pageNumber = Number(page);
  return {
    marketplace,
    search: toSearch(search),
    page: Number.isInteger(pageNumber) && pageNumber > 1 ? pageNumber : 1,
  };
}
