/**
 * Integration tests for the pricing display in the Favourites card grid.
 * Mirrors the Effective Price cell contract on the Models table: a
 * non_token_only model gets an indicator, never a blank/zero price.
 */
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { ModelCardGrid } from './model-card-grid';
import type { ModelRow, ModelObjectPricing } from '../_types';

function row(id: string, pricing?: ModelObjectPricing): ModelRow {
  return {
    id,
    name: id,
    owned_by: 'bedrock',
    family: 'anthropic',
    aliases: [],
    input_modalities: ['text'],
    output_modalities: ['text'],
    dialects: ['openai'],
    capabilities: [],
    status: 'available',
    pricing,
    favourite: true,
  };
}

describe('ModelCardGrid pricing', () => {
  it('renders the per-1M price for a token-priced model', () => {
    renderWithProviders(
      <ModelCardGrid
        models={[
          row('token-priced', { effective_input_cost_per_1m: 3, effective_output_cost_per_1m: 15 }),
        ]}
      />,
    );
    expect(screen.getByText('$3.0000/1M')).toBeInTheDocument();
  });

  // Before the fix this rendered "—/1M" for a model with real, non-zero cost —
  // indistinguishable from "no pricing data at all".
  it('does not render a blank price for a non-token-only model', () => {
    renderWithProviders(
      <ModelCardGrid
        models={[row('non-token', { non_token_only: true, image_input_cost: 0.0001 })]}
      />,
    );
    expect(screen.queryByText((_, el) => el?.textContent?.includes('/1M') ?? false)).toBeNull();
  });
});
