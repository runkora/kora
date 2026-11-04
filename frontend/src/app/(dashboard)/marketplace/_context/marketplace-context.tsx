'use client';

// ── MarketplaceContext ────────────────────────────────────────────────────────
//
// Gives the browse state (useMarketplace) to the overview tree without prop
// drilling. The RSC page passes the first result page to
// `MarketplaceProvider`, which starts `useMarketplace` from it synchronously,
// so there is no loading flash after hydration.
//

import { createContext, useContext } from 'react';
import { useMarketplace } from '../_hooks/use-marketplace';
import type { MarketplaceStateShape } from '../_hooks/use-marketplace';
import type { MarketplaceOverviewData } from '../_types';

// ── Context ───────────────────────────────────────────────────────────────────

/** The browse state of the Marketplace overview. Skill details have their own route. */
export type MarketplaceContextValue = MarketplaceStateShape;

const MarketplaceContext = createContext<MarketplaceContextValue | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface MarketplaceProviderProps {
  initialData: MarketplaceOverviewData;
  initialError: string | null;
  children: React.ReactNode;
}

export function MarketplaceProvider({
  initialData,
  initialError,
  children,
}: MarketplaceProviderProps) {
  const state = useMarketplace(initialData, initialError);

  return <MarketplaceContext.Provider value={state}>{children}</MarketplaceContext.Provider>;
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

/**
 * Returns the current MarketplaceContextValue.
 *
 * Throws if called outside a MarketplaceProvider. This is intentional.
 */
export function useMarketplaceContext(): MarketplaceContextValue {
  const ctx = useContext(MarketplaceContext);
  if (!ctx) {
    throw new Error('useMarketplaceContext must be used inside <MarketplaceProvider>');
  }
  return ctx;
}
