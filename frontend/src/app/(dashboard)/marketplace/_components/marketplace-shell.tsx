'use client';

/** @tier 3 — marketplace-scoped. Client boundary for the marketplace list page. */

import type { Marketplace } from '@orbit/lib/marketplace';
import { MarketplacePage } from './marketplace-page';

interface MarketplaceShellProps {
  marketplaces: Marketplace[];
  error: string | null;
}

export function MarketplaceShell({ marketplaces, error }: MarketplaceShellProps) {
  return <MarketplacePage marketplaces={marketplaces} error={error} />;
}
