// RSC — reads the browse values (marketplace, query, page, pageSize, sort) from the
// URL, validates them, and loads that result page server-side, so the page
// renders with real data. Later browse changes go through the BFF route
// (see _hooks/use-marketplace.ts). The co-located loading.tsx shows a
// skeleton while this load completes.

import { getSession, isSessionValid } from '@orbit/server/session';
import { loadMarketplaceOverview } from './_services/marketplace-server';
import { MarketplaceShell } from './_components/marketplace-shell';
import { parseBrowseParams, recordToSearchParams } from './_utils/browse-params';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from './_types';

interface MarketplaceOverviewPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function MarketplaceOverviewPage({
  searchParams,
}: MarketplaceOverviewPageProps) {
  const browse = parseBrowseParams(recordToSearchParams(await searchParams));
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <MarketplaceShell
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
        initialOverview={result.overview ?? emptyOverview(browse)}
        initialError={result.message}
      />
    );
  }

  return <MarketplaceShell initialOverview={result.overview} initialError={null} />;
}

/** An overview without data, for a failed first load. */
function emptyOverview(browse: MarketplaceBrowseParams): MarketplaceOverviewData {
  return { marketplaces: [], requested: browse, browse, skills: [], total: 0 };
}
