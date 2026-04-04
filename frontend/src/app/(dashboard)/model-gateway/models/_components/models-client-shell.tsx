'use client';

// ── ModelsClientShell ─────────────────────────────────────────────────────────
//
// The client boundary for the Models page. The RSC page.tsx fetches initial
// model data on the server and passes it here synchronously — no loading flash.
//
// All interactive state (favourites, refresh, etc.) lives in ModelsProvider via
// the useModels hook. Children access it via useModelsContext().

import { Heading, Separator, Text } from '@orbit/terra';
import { ModelsProvider, useModelsContext } from '../../_context/models-context';
import type { ModelRow } from '../../_types';
import { ModelCardGrid } from './model-card-grid';
import { ModelsTable } from './models-table';

// ── Shell ─────────────────────────────────────────────────────────────────────

interface ModelsClientShellProps {
  initialData: ModelRow[];
}

/**
 * ModelsClientShell
 *
 * Wraps the full Models page UI in <ModelsProvider>, bootstrapping it with
 * server-fetched initial data so the client renders without a loading state.
 */
export function ModelsClientShell({ initialData }: ModelsClientShellProps) {
  return (
    <ModelsProvider initialData={initialData}>
      <ModelsPageContent />
    </ModelsProvider>
  );
}

// ── Favourites section ────────────────────────────────────────────────────────
//
// Separate component so it can call useModelsContext() inside the provider tree.

function FavouritesSection() {
  const { favourites, toggleFavourite } = useModelsContext();
  return <ModelCardGrid models={favourites} onRemove={toggleFavourite} />;
}

// ── Page content ──────────────────────────────────────────────────────────────

function ModelsPageContent() {
  return (
    <div className="px-6 py-8 max-w-screen-xl mx-auto">
      <div className="flex flex-col gap-10">
        {/* ── Page header ── */}
        <div>
          <Heading variant="title">Models</Heading>
          <div className="mt-1">
            <Text size="supporting" color="muted">
              Browse available AI models and manage your favourites.
            </Text>
          </div>
        </div>

        {/* ── Section 1: Favourites ── */}
        <FavouritesSection />

        <Separator />

        {/* ── Section 2: Full model list ── */}
        <div className="flex flex-col gap-4">
          <div>
            <Heading variant="section">All models</Heading>
            <div className="mt-0.5">
              <Text size="supporting" color="muted">
                Click any row to view full details. Use the column toggle to customise your view.
              </Text>
            </div>
          </div>
          <ModelsTable />
        </div>
      </div>
    </div>
  );
}
