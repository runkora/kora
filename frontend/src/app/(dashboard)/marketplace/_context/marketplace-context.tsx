'use client';

// ── MarketplaceContext ────────────────────────────────────────────────────────
//
// Gives the browse state (useMarketplace) to the overview tree without prop
// drilling. The RSC page passes the first result page to
// `MarketplaceProvider`, which starts `useMarketplace` from it synchronously,
// so there is no loading flash after hydration.
//
// The provider also saves the Marketplace that the user picks in the picker in
// the user preferences (`marketplace.lastSelected`). The overview's initial
// server load uses it when the overview path does not name a Marketplace.
//
// A preferences PUT replaces the whole document. So the provider saves only
// after an explicit pick that loads successfully, and never after a failed
// preferences read, when the client holds only the defaults.

import { createContext, useCallback, useContext } from 'react';
import { usePreferences } from '../../_preferences/preferences-context';
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
  const { updatePreferences, loaded } = usePreferences();

  // Called once after the load of a picked Marketplace succeeds. The server
  // has checked the slug against the Marketplace list.
  const handleMarketplacePicked = useCallback(
    (marketplaceSlug: string) => {
      if (!loaded) {
        // The preferences read failed, so the client holds only the defaults.
        // A PUT now would replace the user's stored preferences with them.
        return;
      }
      updatePreferences((prev) => {
        if (prev.marketplace.lastSelected === marketplaceSlug) {
          // No change. Keep the same object, so that no PUT is sent.
          return prev;
        }
        return { ...prev, marketplace: { lastSelected: marketplaceSlug } };
      });
    },
    [loaded, updatePreferences],
  );

  const state = useMarketplace(initialData, initialError, handleMarketplacePicked);

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
