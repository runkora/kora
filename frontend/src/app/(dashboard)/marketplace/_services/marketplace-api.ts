// Client-side fetch helpers. Call the BFF routes at /api/marketplace/skills
// and /api/marketplace/skill. Never call the Marketplace Service directly —
// that is server-only.

import type { Skill } from '@orbit/shared/api';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

export interface FetchMarketplaceOverviewResult {
  /** The loaded result page. Null when the request failed or was cancelled. */
  overview: MarketplaceOverviewData | null;
  error: string | null;
  /** True when a newer request cancelled this one. The caller ignores the result. */
  cancelled: boolean;
  /** A loaded Marketplace list, if only the Skills request failed. */
  partialOverview?: MarketplaceOverviewData;
}

/**
 * Fetches one result page of the Marketplace overview from the BFF.
 *
 * Returns a normalised result object — never throws — so that the hook can
 * examine `cancelled` and `error` without a try/catch.
 */
export async function fetchMarketplaceOverview(
  browse: MarketplaceBrowseParams,
  signal: AbortSignal,
): Promise<FetchMarketplaceOverviewResult> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.marketplaceSkills.GET(browse), { cache: 'no-store', signal });
  } catch (err) {
    if (isAbortError(err)) {
      return { overview: null, error: null, cancelled: true };
    }
    return {
      overview: null,
      error: 'Network error: could not reach the server.',
      cancelled: false,
    };
  }

  if (!res.ok) {
    const failure = await readError(res);
    return {
      overview: null,
      error: failure.message,
      cancelled: false,
      partialOverview: failure.overview,
    };
  }

  try {
    const body = (await res.json()) as { data: MarketplaceOverviewData };
    return { overview: body.data, error: null, cancelled: false };
  } catch (err) {
    if (isAbortError(err)) {
      // The request was cancelled while the body was read.
      return { overview: null, error: null, cancelled: true };
    }
    return { overview: null, error: 'Invalid response from the server.', cancelled: false };
  }
}

export interface FetchSkillDetailResult {
  /** The loaded Skill. Null when the request failed, found nothing, or was cancelled. */
  skill: Skill | null;
  /** True when the Marketplace has no Skill with this skillSlug (404). */
  notFound: boolean;
  error: string | null;
  /** True when a newer request cancelled this one. The caller ignores the result. */
  cancelled: boolean;
}

/**
 * Fetches the details of one Skill from the BFF.
 *
 * Returns a normalised result object — never throws — like
 * fetchMarketplaceOverview.
 */
export async function fetchSkillDetail(
  marketplaceSlug: string,
  skillSlug: string,
  signal: AbortSignal,
): Promise<FetchSkillDetailResult> {
  const failed = { skill: null, notFound: false, cancelled: false };
  let res: Response;
  try {
    res = await fetch(ApiRoute.marketplaceSkill.GET(marketplaceSlug, skillSlug), {
      cache: 'no-store',
      signal,
    });
  } catch (err) {
    if (isAbortError(err)) {
      return { ...failed, error: null, cancelled: true };
    }
    return { ...failed, error: 'Network error: could not reach the server.' };
  }

  if (res.status === 404) {
    // No Skill has this slug. This is not a failure to retry.
    return { ...failed, notFound: true, error: null };
  }

  if (!res.ok) {
    const failure = await readError(res);
    return { ...failed, error: failure.message };
  }

  try {
    const body = (await res.json()) as { data: Skill };
    return { skill: body.data, notFound: false, error: null, cancelled: false };
  } catch (err) {
    if (isAbortError(err)) {
      // The request was cancelled while the body was read.
      return { ...failed, error: null, cancelled: true };
    }
    return { ...failed, error: 'Invalid response from the server.' };
  }
}

async function readError(
  res: Response,
): Promise<{ message: string; overview?: MarketplaceOverviewData }> {
  let body: { message?: string; data?: MarketplaceOverviewData };
  try {
    body = (await res.json()) as { message?: string; data?: MarketplaceOverviewData };
  } catch {
    // The body is not JSON. Show the status code.
    return { message: `Request failed (${res.status})` };
  }

  if (body.message) {
    // The BFF explained the failure.
    return { message: body.message, overview: body.data };
  }
  // No explanation. Show the status code.
  return { message: `Request failed (${res.status})`, overview: body.data };
}

function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError';
}
