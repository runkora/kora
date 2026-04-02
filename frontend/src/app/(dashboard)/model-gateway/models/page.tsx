'use client';

import { useState } from 'react';
import { Heading, Separator, Text } from '@orbit/terra';
import { ModelCardGrid } from './_components/model-card-grid';
import { ModelsTable } from './_components/models-table';

// Seed with a couple of favourites so the section isn't empty on first load
const DEFAULT_FAVOURITES = new Set(['claude-sonnet-4-5', 'gpt-4o']);

// Curated top picks with "why" labels
const TOP_PICKS = new Set(['claude-opus-4-5', 'gpt-4o-mini', 'gemini-2-5-flash']);
const TOP_PICK_TAGS: Record<string, string> = {
  'claude-opus-4-5': 'Most Capable',
  'gpt-4o-mini': 'Best Value',
  'gemini-2-5-flash': 'Fastest',
};

export default function ModelsPage() {
  const [favourites, setFavourites] = useState<Set<string>>(DEFAULT_FAVOURITES);

  function toggleFavourite(id: string) {
    setFavourites((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }

  return (
    <div className="px-6 py-8 max-w-screen-xl mx-auto">
      <div className="flex flex-col gap-10">
        {/* ── Page header ── */}
        <div>
          <Heading variant="title">Models</Heading>
          <div className="mt-1">
            <Text size="supporting" color="muted">
              Browse available AI models, compare pricing, and manage your favourites.
            </Text>
          </div>
        </div>

        {/* ── Section 1: Favourites ── */}
        <ModelCardGrid models={favourites} onRemove={toggleFavourite} />

        <Separator />

        {/* ── Section 2: Top picks — read-only with "why" tags ── */}
        <ModelCardGrid
          models={TOP_PICKS}
          tags={TOP_PICK_TAGS}
          title="Top picks"
          description="Curated recommendations from the Orbit team"
        />

        <Separator />

        {/* ── Section 3: Full model list ── */}
        <div className="flex flex-col gap-4">
          <div>
            <Heading variant="section">All models</Heading>
            <div className="mt-0.5">
              <Text size="supporting" color="muted">
                Click any row to view full details. Use the column toggle to customise your view.
              </Text>
            </div>
          </div>
          <ModelsTable favourites={favourites} onFavouriteToggle={toggleFavourite} />
        </div>
      </div>
    </div>
  );
}
