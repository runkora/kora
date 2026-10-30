'use client';

/** @tier 3 — marketplace-scoped. Client boundary: mounts MarketplaceProvider and renders the overview. */

import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceOverviewData } from '../_types';
import { MarketplacePage } from './marketplace-page';

interface MarketplaceShellProps {
  initialOverview: MarketplaceOverviewData;
  initialError: string | null;
}

/**
 * Client boundary for the Marketplace overview.
 *
 * Receives the first result page from the RSC page as props, mounts
 * MarketplaceProvider, and renders the overview. The shell has no state.
 * It only connects the RSC page to the provider.
 */
export function MarketplaceShell({ initialOverview, initialError }: MarketplaceShellProps) {
  return (
    <MarketplaceProvider initialData={initialOverview} initialError={initialError}>
      <MarketplacePage />
    </MarketplaceProvider>
  );
}
