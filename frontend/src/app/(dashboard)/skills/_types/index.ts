import type { Marketplace, SkillSummary } from '@runkora/shared/api';
import { APP_ROUTES } from '@runkora/lib/routing';

/** skills-service treats a shorter search as no search, so the page does too. */
export const MIN_SEARCH_LENGTH = 3;

/** Both views show ten skills a page, an even number that keeps the two-column grid full. */
export const PAGE_SIZE = 10;

/**
 * The browse state the overview URL holds. `marketplace` is a slug, or empty for every
 * Marketplace; `favourites` shows the user's favourites instead.
 */
export interface SkillsBrowse {
  marketplace: string;
  favourites: boolean;
  search: string;
  page: number;
}

/** What the overview shows: the Marketplaces for the chips and one page of skills. */
export interface SkillsOverview {
  marketplaces: Marketplace[];
  browse: SkillsBrowse;
  skills: SkillSummary[];
  total: number;
  /** Marketplaces an all-Marketplaces page left out because they could not be read. */
  failures: { marketplace: string; message: string }[];
}

/** A failed load. A 502 means the Marketplace is unavailable, and the message says why. */
export interface SkillsLoadError {
  status: number;
  message: string;
}

/** The `marketplace:path` key a favourite is stored under. */
export function skillKey(skill: { marketplace: string; path: string }): string {
  return `${skill.marketplace}:${skill.path}`;
}

/** The browse values as a query string, for the overview URL and a skill's way back to it. */
export function browseQuery(browse: SkillsBrowse): string {
  const query = new URLSearchParams();
  if (browse.favourites) query.set('favourites', '1');
  else if (browse.marketplace) query.set('marketplace', browse.marketplace);
  if (browse.search) query.set('search', browse.search);
  if (browse.page > 1) query.set('page', String(browse.page));
  const qs = query.toString();
  return qs ? `?${qs}` : '';
}

/** A skill's page, carrying the browse values so its Skills link returns to the same results. */
export function skillHref(skill: { marketplace: string; path: string }, browse: SkillsBrowse) {
  return `${APP_ROUTES.skills.skill(skill.marketplace, skill.path)}${browseQuery(browse)}`;
}

/** Next hands dynamic params over still percent-encoded. A malformed escape reads as not found. */
export function decodeParam(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return '';
  }
}

/** Search text as the API applies it: trimmed, and empty below MIN_SEARCH_LENGTH. */
export function toSearch(text: unknown): string {
  const trimmed = typeof text === 'string' ? text.trim() : '';
  return trimmed.length < MIN_SEARCH_LENGTH ? '' : trimmed;
}

/** Reads browse values from a URL or request query. A missing or invalid page is page 1. */
export function toBrowse(query: {
  marketplace?: unknown;
  favourites?: unknown;
  search?: unknown;
  page?: unknown;
}): SkillsBrowse {
  const page = Number(query.page);
  const favourites = query.favourites === '1';
  return {
    marketplace: !favourites && typeof query.marketplace === 'string' ? query.marketplace : '',
    favourites,
    search: toSearch(query.search),
    page: Number.isInteger(page) && page > 1 ? page : 1,
  };
}
