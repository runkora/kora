// RSC — reads marketplace/query/page/pageSize from the URL and fetches just
// that page from the mock endpoint (offset/limit/search), matching the real
// per-marketplace paginated contract in `GetPackageOverviewParams`. Sends no
// Skill request until a Marketplace is selected.

import type { PackageOverview } from '@orbit/shared/api';
import { fetchMarketplaces, fetchSkills } from './_services/marketplace-server';
import { MarketplaceShell } from './_components/marketplace-shell';

const DEFAULT_PAGE_SIZE = 24;

interface MarketplaceOverviewPageProps {
  searchParams: Promise<{
    marketplace?: string;
    query?: string;
    page?: string;
    pageSize?: string;
  }>;
}

export default async function MarketplaceOverviewPage({
  searchParams,
}: MarketplaceOverviewPageProps) {
  const { marketplaces: fetchedMarketplaces } = await fetchMarketplaces();
  const marketplaces = [...fetchedMarketplaces].sort((a, b) => a.name.localeCompare(b.name));
  const params = await searchParams;
  const selectedMarketplace = params.marketplace ?? '';
  const query = params.query ?? '';
  const currentPage = Number(params.page) || 1;
  const pageSize = Number(params.pageSize) || DEFAULT_PAGE_SIZE;

  let packages: PackageOverview[] = [];
  let total = 0;

  if (selectedMarketplace) {
    const result = await fetchSkills(selectedMarketplace, {
      offset: (currentPage - 1) * pageSize,
      limit: pageSize,
      search: query,
    });
    packages = result.packages;
    total = result.total;
  }

  return (
    <MarketplaceShell
      marketplaces={marketplaces}
      selectedMarketplace={selectedMarketplace}
      query={query}
      currentPage={currentPage}
      pageSize={pageSize}
      skills={packages}
      total={total}
    />
  );
}
