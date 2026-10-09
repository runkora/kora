import { getSession, isSessionValid } from '@orbit/server/session';
import { listMarketplaces } from '@orbit/server/marketplace-client';
import type { Marketplace } from '@orbit/lib/marketplace';
import { MarketplaceShell } from './_components/marketplace-shell';

export default async function MarketplacePage() {
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
