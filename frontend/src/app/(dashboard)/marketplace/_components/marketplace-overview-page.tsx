/** @tier 3 — marketplace-scoped. Shared server load for the default entry and selected Marketplace overview. */

import 'server-only';

import { getSession, isSessionValid } from '@orbit/server/session';
import { loadMarketplaceOverview } from '../_services/marketplace-server';
import { MarketplaceShell } from './marketplace-shell';
import { parseBrowseParams } from '../_utils/browse-params';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

export async function MarketplaceOverviewPage({
  marketplaceSlug = '',
}: {
  marketplaceSlug?: string;
}) {
  const browse = parseBrowseParams(new URLSearchParams({ marketplace: marketplaceSlug }));
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <MarketplaceShell
        key={marketplaceSlug}
        initialOverview={emptyOverview(browse)}
        initialError="Session expired. Please sign in again."
      />
    );
  }

  const result = await loadMarketplaceOverview(session.accessToken, browse);

  if (!result.ok) {
    // The load failed. The overview shows the error and a retry button.
    return (
      <MarketplaceShell
        key={marketplaceSlug}
        initialOverview={result.overview ?? emptyOverview(browse)}
        initialError={result.message}
      />
    );
  }

  return (
    <MarketplaceShell key={marketplaceSlug} initialOverview={result.overview} initialError={null} />
  );
}

/** An overview without data, for a failed first load. */
function emptyOverview(browse: MarketplaceBrowseParams): MarketplaceOverviewData {
  return { marketplaces: [], requested: browse, browse, skills: [], total: 0 };
}
