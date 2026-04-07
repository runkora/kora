'use client';

// ── ModelsTabShell ────────────────────────────────────────────────────────────
//
// Client boundary for the models tab. Owns the ModelsProvider so model state
// (loaded rows, favourites) is scoped to this tab only — other tabs never pay
// the cost of the models fetch.
//
// `initialModels` is server-fetched by the RSC page and passed down here,
// so the tab renders immediately without a client-side loading flash.

import { ModelsProvider } from '../_context/models-context';
import { ModelsTab } from './models-tab';
import type { ModelRow } from '../_types';

interface ModelsTabShellProps {
  initialModels: ModelRow[];
  initialError: string | null;
}

export function ModelsTabShell({ initialModels, initialError }: ModelsTabShellProps) {
  return (
    <ModelsProvider initialData={initialModels} initialError={initialError}>
      <ModelsTab />
    </ModelsProvider>
  );
}
