import type { Marketplace, SkillSummary } from '@runkora/shared/api';
import type { UserPreferences } from '../../_preferences/preferences.schema';
import { APP_ROUTES } from '@runkora/lib/routing';

/** The shortest search skills-service applies; the page uses the same rule. */
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

/** The preferences with one skill added to or removed from the favourites. */
export function toggleFavouriteKey(prev: UserPreferences, key: string): UserPreferences {
  const keys = prev.skills.favouriteSkills;
  const next = keys.includes(key) ? keys.filter((k) => k !== key) : [...keys, key];
  return { ...prev, skills: { favouriteSkills: next } };
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

/** The length of a search in code points, as skills-service counts runes. */
export function searchLength(text: string): number {
  return [...text.trim()].length;
}

/** Search text as the API applies it: trimmed, and empty below MIN_SEARCH_LENGTH. */
export function toSearch(text: unknown): string {
  const trimmed = typeof text === 'string' ? text.trim() : '';
  return searchLength(trimmed) < MIN_SEARCH_LENGTH ? '' : trimmed;
}

/** Reads browse values from a URL or request query. A missing or invalid page is page 1. */
export function toBrowse(query: {
  marketplace?: unknown;
  favourites?: unknown;
  search?: unknown;
  page?: unknown;
}): SkillsBrowse {
  const page = Number(query.page);
  // skills-service rejects an offset past its integer range; no tenant has this many pages.
  const lastPage = 100_000;
  const favourites = query.favourites === '1';
  return {
    marketplace: !favourites && typeof query.marketplace === 'string' ? query.marketplace : '',
    favourites,
    search: toSearch(query.search),
    page: Number.isInteger(page) && page > 1 ? Math.min(page, lastPage) : 1,
  };
}
