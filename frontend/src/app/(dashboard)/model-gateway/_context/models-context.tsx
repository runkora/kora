'use client';

// ── ModelsContext ─────────────────────────────────────────────────────────────
//
// Provides the ModelsStateShape to any component inside the models page tree
// without prop-drilling. The RSC page passes `initialData` to `ModelsProvider`
// which bootstraps `useModels` synchronously — no useEffect hydration flash.

import { createContext, useContext } from 'react';
import { useModels } from '../_hooks/use-models';
import type { ModelsStateShape } from '../_hooks/use-models';
import type { ModelRow } from '../_types';
import { usePreferences } from '../../_preferences/preferences-context';

// ── Context ───────────────────────────────────────────────────────────────────

const ModelsContext = createContext<ModelsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface ModelsProviderProps {
  initialData: ModelRow[];
  initialError: string | null;
  children: React.ReactNode;
}

export function ModelsProvider({ initialData, initialError, children }: ModelsProviderProps) {
  const { preferences, updatePreferences } = usePreferences();

  function handleFavouritesChange(next: Set<string>) {
    updatePreferences((prev) => ({
      ...prev,
      modelGateway: { favouriteModels: [...next] },
    }));
  }

  // Pass preference-seeded favourites as the initial value. useModels only
  // consumes this in useState() initialization, so re-renders of ModelsProvider
  // won't overwrite the user's in-session toggles.
  const state = useModels(
    initialData,
    initialError,
    new Set(preferences.modelGateway.favouriteModels),
    handleFavouritesChange,
  );
  return <ModelsContext.Provider value={state}>{children}</ModelsContext.Provider>;
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

/**
 * useModelsContext
 *
 * Must be called inside a `<ModelsProvider>` subtree. Throws a clear error
 * if called outside to prevent silent null-access bugs.
 */
export function useModelsContext(): ModelsStateShape {
  const ctx = useContext(ModelsContext);
  if (!ctx) {
    throw new Error('useModelsContext must be used inside <ModelsProvider>');
  }
  return ctx;
}
