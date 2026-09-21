/**
 * Integration tests for the Effective Price column in the Models tab.
 *
 * EffectivePriceCell is private, so the tests drive it through the exported
 * ModelsTab and assert on what the table renders.
 */
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { ModelsTab } from './models-tab';
import { ModelsProvider } from '../_context/models-context';
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
    favourite: false,
  };
}

function renderTab(rows: ModelRow[]) {
  return renderWithProviders(
    <ModelsProvider initialData={rows} initialError={null}>
      <ModelsTab />
    </ModelsProvider>,
  );
}

describe('EffectivePriceCell', () => {
  it('renders Unknown when the model has no pricing at all', () => {
    renderTab([row('no-pricing')]);
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });

  it('renders the per-1M price for a token-priced model', () => {
    renderTab([
      row('token-priced', {
        effective_input_cost_per_1m: 3,
        effective_output_cost_per_1m: 15,
      }),
    ]);
    const priced = screen.getAllByText((_, el) => el?.textContent?.includes('/1M') ?? false);
    expect(priced.length).toBeGreaterThan(0);
    expect(screen.queryByText('$0.00/1M')).not.toBeInTheDocument();
  });

  // The whole point of non_token_only: these models have real costs, just not
  // in a token unit, and rendering them as $0.00 reads as "free".
  it('renders an indicator rather than a zero price for a non-token-only model', () => {
    renderTab([row('non-token', { non_token_only: true, image_input_cost: 0.0001 })]);
    expect(screen.queryByText((_, el) => el?.textContent?.includes('/1M') ?? false)).toBeNull();
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument();
  });

  it('flags a token-priced model that also carries non-token dimensions', () => {
    renderTab([
      row('mixed', {
        effective_input_cost_per_1m: 3,
        effective_output_cost_per_1m: 15,
        audio_input_cost_per_1m: 100,
      }),
    ]);
    expect(screen.getByText('+ other dims')).toBeInTheDocument();
  });
});

describe('Effective Price sorting', () => {
  // Non-token-only models have no comparable price. They must land together
  // rather than scattered through the token-priced models.
  it('groups the non-token-only models when sorting by price', async () => {
    const user = userEvent.setup();
    renderTab([
      row('cheap', { effective_input_cost_per_1m: 1, effective_output_cost_per_1m: 1 }),
      row('nontoken-a', { non_token_only: true, image_input_cost: 0.0001 }),
      row('dear', { effective_input_cost_per_1m: 50, effective_output_cost_per_1m: 100 }),
      row('nontoken-b', { non_token_only: true, second_input_cost: 0.002 }),
      row('unknown', undefined),
    ]);

    await user.click(screen.getByRole('button', { name: 'Sort by Effective Price' }));

    const order = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '');

    const a = order.findIndex((t) => t.includes('nontoken-a'));
    const b = order.findIndex((t) => t.includes('nontoken-b'));
    expect(a).toBeGreaterThanOrEqual(0);
    expect(b).toBeGreaterThanOrEqual(0);
    expect(Math.abs(a - b)).toBe(1);
  });
});
