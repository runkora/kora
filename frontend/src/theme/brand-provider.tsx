'use client';

/**
 * The effective brand for client components: the kora default overlaid by the
 * deployment's theme. The root layout provides it from `getBrand()`; a tree
 * rendered without the provider, such as a test or a story, sees the default.
 * See the white-labelling theme ADR, decision 5.
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
