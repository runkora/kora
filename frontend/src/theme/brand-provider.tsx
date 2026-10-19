'use client';

/**
 * The effective brand for client components: the kora default overlaid by the theme.
 * Without a provider, as in a test or a story, it is the default.
 */
import { createContext, useContext, type ReactNode } from 'react';
import { defaultTheme, type IdentityTheme } from './default';

const BrandContext = createContext<IdentityTheme>(defaultTheme);

export interface BrandProviderProps {
  brand: IdentityTheme;
  children: ReactNode;
}

export function BrandProvider({ brand, children }: BrandProviderProps) {
  return <BrandContext.Provider value={brand}>{children}</BrandContext.Provider>;
}

/** The effective brand: product name, logo and favicon sources. */
export function useBrand(): IdentityTheme {
  return useContext(BrandContext);
}
