// Browser browse requests go through /api/marketplace/skills. Skill details
// load in the [marketplaceSlug]/[skillName] Server Component through marketplace-server instead.

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
