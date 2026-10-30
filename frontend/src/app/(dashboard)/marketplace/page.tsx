import { getSession, isSessionValid } from '@orbit/server/session';
import { loadMarketplacePage } from './_services/marketplace-server';
import { MarketplaceShell } from './_components/marketplace-shell';

export default async function MarketplaceEntryPage() {
  const session = await getSession();
  const result = await loadMarketplacePage(isSessionValid(session) ? session.accessToken : null);

  return (
    <MarketplaceShell
      key=""
      initialOverview={result.overview}
      initialError={result.ok ? null : result.message}
    />
  );
}
