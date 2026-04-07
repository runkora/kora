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

// ── Context ───────────────────────────────────────────────────────────────────

const ModelsContext = createContext<ModelsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface ModelsProviderProps {
  initialData: ModelRow[];
  initialError: string | null;
  children: React.ReactNode;
}

export function ModelsProvider({ initialData, initialError, children }: ModelsProviderProps) {
  const state = useModels(initialData, initialError);
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
