// RSC — loads the default result page with the saved Marketplace, so the page
// renders with real data. Later browse changes go through the BFF route
// (see _hooks/use-marketplace.ts). The co-located loading.tsx shows a skeleton
// while this load completes. Browser query parameters do not own browse state.

import { getSession, isSessionValid } from '@orbit/server/session';
import { listMarketplaces } from '@orbit/server/marketplace-client';
import type { Marketplace } from '@orbit/lib/marketplace';
import { MarketplaceShell } from './_components/marketplace-shell';
import { parseBrowseParams } from './_utils/browse-params';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from './_types';

export default async function MarketplaceOverviewPage() {
  const browse = parseBrowseParams(new URLSearchParams());
  const session = await getSession();

  let marketplaces: Marketplace[] = [];
  let error: string | null = null;

  if (isSessionValid(session)) {
    try {
      marketplaces = await listMarketplaces(session.accessToken);
    } catch {
      error = 'Failed to load marketplaces';
    }
  }

  return <MarketplaceShell marketplaces={marketplaces} error={error} />;
}
