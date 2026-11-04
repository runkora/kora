import { getSession, isSessionValid } from '@runkora/server/session';
import { loadMarketplacePage } from '../_services/skills-server';
import { MarketplaceShell } from '../_components/skills-shell';

interface MarketplacePageProps {
  params: Promise<{ marketplace: string }>;
}

export default async function MarketplacePage({ params }: MarketplacePageProps) {
  const { marketplace: marketplaceSlug } = await params;
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
