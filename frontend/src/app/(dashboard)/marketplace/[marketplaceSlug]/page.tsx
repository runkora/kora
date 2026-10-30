import { getSession, isSessionValid } from '@orbit/server/session';
import { loadMarketplacePage } from '../_services/marketplace-server';
import { MarketplaceShell } from '../_components/marketplace-shell';

interface MarketplacePageProps {
  params: Promise<{ marketplaceSlug: string }>;
}

export default async function MarketplacePage({ params }: MarketplacePageProps) {
  const { marketplaceSlug } = await params;
  const session = await getSession();
  const result = await loadMarketplacePage(
    isSessionValid(session) ? session.accessToken : null,
    marketplaceSlug,
  );

  return (
    <MarketplaceShell
      key={marketplaceSlug}
      initialOverview={result.overview}
      initialError={result.ok ? null : result.message}
    />
  );
}
