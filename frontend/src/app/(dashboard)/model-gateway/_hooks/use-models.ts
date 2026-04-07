'use client';

// ── useModels hook ────────────────────────────────────────────────────────────
//
// Owns all state for the model-gateway Models page. The shape is expressed as
// an explicit interface (`ModelsStateShape`) that doubles as the upgrade
// contract: when this feature graduates to Zustand, the store's state + actions
// must satisfy this same interface so callers need zero changes.
//
// FeatureStateShape upgrade contract
// ───────────────────────────────────
// export interface FeatureStateShape {
//   // --- data ---
//   items: unknown[];
//   status: 'idle' | 'loading' | 'success' | 'error';
//   error: string | null;
//   // --- actions ---
//   refresh: () => Promise<void>;
// }

import { useState, useCallback } from 'react';
import { fetchModels } from '../_services/models-api';
import type { ModelRow } from '../_types';

// ── State shape ───────────────────────────────────────────────────────────────

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface ModelsStateShape {
  // ── data ────────────────────────────────────────────────────────────────────
  models: ModelRow[];
  status: LoadStatus;
  error: string | null;
  favourites: Set<string>;

  // ── actions ─────────────────────────────────────────────────────────────────
  /** Re-fetch models from the BFF. Safe to call at any time. */
  refresh: () => Promise<void>;
  /** Toggle a model's favourite status. */
  toggleFavourite: (id: string) => void;
}

// ── Default favourites ────────────────────────────────────────────────────────
//
// Empty by default — real model IDs come from the API and cannot be predicted
// at build time. Favourites are toggled by the user at runtime.

const DEFAULT_FAVOURITES = new Set<string>();

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * useModels
 *
 * Manages the full state for the Models page. Accepts `initialData` from the
 * RSC parent — the RSC fetches on the server and passes the result down so
 * the client renders immediately without a loading flash.
 *
 * Calling `refresh()` triggers a fresh client-side fetch (e.g. after a manual
 * reload or when the user returns to the tab).
 */
export function useModels(
  initialData: ModelRow[],
  initialError: string | null = null,
): ModelsStateShape {
  const [models, setModels] = useState<ModelRow[]>(initialData);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialData.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);
  const [favourites, setFavourites] = useState<Set<string>>(DEFAULT_FAVOURITES);

  const refresh = useCallback(async () => {
    setStatus('loading');
    setError(null);

    const result = await fetchModels();

    if (result.error) {
      setError(result.error);
      setStatus('error');
    } else {
      setModels(result.models);
      setStatus('success');
    }
  }, []);

  const toggleFavourite = useCallback((id: string) => {
    setFavourites((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  return { models, status, error, favourites, refresh, toggleFavourite };
}
