'use client';

/** @tier 3 — marketplace-scoped. Remembers browse controls inside the shared dashboard. */

import { createContext, useCallback, useState, type ReactNode } from 'react';
import { usePathname } from 'next/navigation';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { MarketplaceBrowseParams, SkillSort } from '../_types';

type BrowseByMarketplace = Partial<
  Record<
    string,
    {
      browse: MarketplaceBrowseParams;
      browseSort: Exclude<SkillSort, 'relevance'>;
    }
  >
>;

interface MarketplaceBrowseState {
  browseByMarketplace: BrowseByMarketplace;
  rememberBrowse(
    browse: MarketplaceBrowseParams,
    browseSort: Exclude<SkillSort, 'relevance'>,
  ): void;
}

export const MarketplaceBrowseStateContext = createContext<MarketplaceBrowseState | null>(null);

export function MarketplaceBrowseStateProvider({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const inMarketplace =
    pathname === APP_ROUTES.marketplace.overview ||
    pathname?.startsWith(`${APP_ROUTES.marketplace.overview}/`) === true;
  const [state, setState] = useState<{
    inMarketplace: boolean;
    browseByMarketplace: BrowseByMarketplace;
  }>({
    inMarketplace,
    browseByMarketplace: {},
  });

  // The dashboard stays mounted when its page changes. Clear the controls on exit.
  if (state.inMarketplace !== inMarketplace) {
    setState({
      inMarketplace,
      browseByMarketplace: inMarketplace ? state.browseByMarketplace : {},
    });
  }

  const rememberBrowse = useCallback(
    (browse: MarketplaceBrowseParams, browseSort: Exclude<SkillSort, 'relevance'>) => {
      setState((previous) => ({
        ...previous,
        browseByMarketplace: {
          ...previous.browseByMarketplace,
          [browse.marketplace]: { browse, browseSort },
        },
      }));
    },
    [],
  );

  return (
    <MarketplaceBrowseStateContext.Provider
      value={{ browseByMarketplace: state.browseByMarketplace, rememberBrowse }}
    >
      {children}
    </MarketplaceBrowseStateContext.Provider>
  );
}
