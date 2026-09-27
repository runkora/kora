import 'server-only';

import type { Marketplace, PackageDetail, PackageOverview } from '@orbit/shared/api';
import { ApiError } from '@orbit/server/api-error';
import { fetchMarketplaces, fetchSkillDetail, fetchSkills } from '@orbit/server/marketplace-client';
import { getPreferences } from '../../_preferences/preferences-server';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

// The only module that calls the Marketplace client. The RSC pages and the
// BFF route share it. Each function returns a discriminated union and never
// throws, so that each caller can handle a failure in its own way:
//   - RSC page: pass initialError to the shell
//   - BFF route: return an HTTP status to the browser

export type MarketplaceOverviewResult =
  { ok: true; overview: MarketplaceOverviewData } | { ok: false; status: number; message: string };

export type SkillDetailResult =
  { ok: true; skill: PackageDetail } | { ok: false; status: number; message: string };

/** The status of a request that its caller cancelled. Nobody reads the response. */
const CANCELLED_STATUS = 499;

type SkillsPageResult =
  | { ok: true; skills: PackageOverview[]; total: number }
  | { ok: false; status: number; message: string };

/**
 * Loads one result page of the overview. The browse values must come from
 * parseBrowseParams. This function also corrects the two values that need the
 * data: the Marketplace (see selectMarketplace), and a page after the last
 * page, which becomes the last page.
 */
export async function loadMarketplaceOverview(
  accessToken: string,
  browse: MarketplaceBrowseParams,
  signal?: AbortSignal,
): Promise<MarketplaceOverviewResult> {
  let marketplaces: Marketplace[];
  try {
    const response = await fetchMarketplaces(accessToken, signal);
    // Sort a copy. The client can return an array that other callers share.
    marketplaces = [...response.marketplaces].sort((a, b) => a.name.localeCompare(b.name));
  } catch (err) {
    if (signal && signal.aborted) {
      // The caller cancelled the request. This is not a failure.
      return { ok: false, status: CANCELLED_STATUS, message: 'Request cancelled' };
    }
    console.error('[loadMarketplaceOverview] failed to load Marketplaces:', err);
    return fromApiError(err, 'Failed to load Marketplaces');
  }

  const saved = await readSavedMarketplace(accessToken, browse);
  const marketplace = selectMarketplace(marketplaces, browse.marketplace, saved);
  if (!marketplace) {
    // No Marketplaces exist, so there are no Skills to load.
    return {
      ok: true,
      overview: {
        marketplaces,
        requested: browse,
        browse: { ...browse, marketplace: '', page: 1 },
        skills: [],
        total: 0,
      },
    };
  }

  let page = browse.page;
  let result = await loadSkillsPage(accessToken, marketplace, browse, signal);
  if (!result.ok) return result;

  const lastPage = Math.max(1, Math.ceil(result.total / browse.pageSize));
  if (page > lastPage) {
    // The URL asks for a page after the last page. Load the last page.
    page = lastPage;
    result = await loadSkillsPage(accessToken, marketplace, { ...browse, page }, signal);
    if (!result.ok) return result;
  }

  return {
    ok: true,
    overview: {
      marketplaces,
      requested: browse,
      browse: { ...browse, marketplace, page },
      skills: result.skills,
      total: result.total,
    },
  };
}

/** Loads one Skill for the detail route. A missing Skill is a 404. */
export async function loadSkillDetail(
  accessToken: string,
  slug: string,
): Promise<SkillDetailResult> {
  let skill: PackageDetail | null;
  try {
    skill = await fetchSkillDetail(accessToken, slug);
  } catch (err) {
    console.error('[loadSkillDetail] failed to load the Skill:', err);
    return fromApiError(err, 'Failed to load the Skill');
  }

  if (skill === null) {
    return { ok: false, status: 404, message: 'Skill not found' };
  }
  return { ok: true, skill };
}

/**
 * Returns the Marketplace that the user selected last, from the user
 * preferences. A Marketplace in the URL always wins, so this function reads
 * the preferences only when the URL names no Marketplace. Returns '' when
 * nothing is saved.
 */
async function readSavedMarketplace(
  accessToken: string,
  browse: MarketplaceBrowseParams,
): Promise<string> {
  if (browse.marketplace) {
    // The URL names a Marketplace. The saved selection does not apply.
    return '';
  }
  // getPreferences never throws. On a failure it returns the defaults, which
  // have no saved Marketplace. In an RSC render, the dashboard layout has
  // already read the preferences, and React cache() reuses that result.
  const preferences = await getPreferences(accessToken);
  return preferences.marketplace.lastSelected;
}

/**
 * Returns the name of the Marketplace to show, in this order: the requested
 * Marketplace, the saved Marketplace, the first Marketplace. A name counts
 * only when that Marketplace exists. Returns '' when no Marketplaces exist.
 */
function selectMarketplace(marketplaces: Marketplace[], requested: string, saved: string): string {
  const requestedMarketplace = marketplaces.find((marketplace) => marketplace.name === requested);
  const savedMarketplace = marketplaces.find((marketplace) => marketplace.name === saved);
  const firstMarketplace = marketplaces[0];
  if (requestedMarketplace) {
    // The URL names a Marketplace that exists.
    return requestedMarketplace.name;
  } else if (savedMarketplace) {
    // No Marketplace in the URL. Use the Marketplace that the user selected last.
    return savedMarketplace.name;
  } else if (firstMarketplace) {
    // A first visit, an unknown name in the URL, or a saved Marketplace that
    // no longer exists. Use the first Marketplace.
    return firstMarketplace.name;
  } else {
    // No Marketplaces exist.
    return '';
  }
}

async function loadSkillsPage(
  accessToken: string,
  marketplace: string,
  browse: MarketplaceBrowseParams,
  signal?: AbortSignal,
): Promise<SkillsPageResult> {
  try {
    const page = await fetchSkills(
      accessToken,
      marketplace,
      {
        offset: (browse.page - 1) * browse.pageSize,
        limit: browse.pageSize,
        search: browse.query,
        sort: browse.sort,
      },
      signal,
    );
    return { ok: true, skills: page.packages, total: page.total };
  } catch (err) {
    if (signal && signal.aborted) {
      // The caller cancelled the request. This is not a failure.
      return { ok: false, status: CANCELLED_STATUS, message: 'Request cancelled' };
    }
    console.error('[loadSkillsPage] failed to load Skills:', err);
    return fromApiError(err, 'Failed to load Skills');
  }
}

/**
 * Converts an error from the Marketplace client to a failed result. An
 * ApiError keeps its upstream status and message, as in the other server
 * services, so that a refusal (403) or a missing Skill (404) reaches the
 * caller. Any other error becomes a 502.
 */
function fromApiError(
  err: unknown,
  fallbackMessage: string,
): { ok: false; status: number; message: string } {
  if (err instanceof ApiError) {
    // The Marketplace Service answered with an error status.
    return { ok: false, status: err.status, message: err.message };
  }
  // No answer from the Marketplace Service, or an unexpected error.
  return { ok: false, status: 502, message: fallbackMessage };
}
