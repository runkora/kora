import { MarketplaceOverviewPage } from '../_components/marketplace-overview-page';

interface MarketplacePageProps {
  params: Promise<{ marketplaceSlug: string }>;
}

export default async function MarketplacePage({ params }: MarketplacePageProps) {
  const { marketplaceSlug } = await params;
  return <MarketplaceOverviewPage marketplaceSlug={marketplaceSlug} />;
}
