/**
 * Integration tests for the Models tab: the Effective Price column and the
 * capability filter.
 *
 * EffectivePriceCell is private, so the tests drive it through the exported
 * ModelsTab and assert on what the table renders.
 */
import { describe, it, expect } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { ModelsTab } from './models-tab';
import { ModelsProvider } from '../_context/models-context';
import type { ModelRow, ModelObjectPricing } from '../_types';

function row(
  id: string,
  pricing?: ModelObjectPricing,
  extra: Partial<Pick<ModelRow, 'capabilities' | 'status'>> = {},
): ModelRow {
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
    ...extra,
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

describe('EffectivePriceCell tooltip', () => {
  // TooltipTrigger asChild clones its child with pointer/focus handlers. Icon
  // does not forward unknown props to its underlying span, so those handlers
  // never reach the DOM and the tooltip cannot open.
  it('opens the tooltip on hover for a non-token-only model', async () => {
    const user = userEvent.setup();
    renderTab([row('non-token', { non_token_only: true, image_input_cost: 0.0001 })]);

    await user.hover(screen.getByRole('button', { name: /non-token pricing/i }));
    expect(
      (await screen.findAllByText('Non-token pricing — see model details')).length,
    ).toBeGreaterThan(0);
  });
});

describe('Capability filter', () => {
  const rows = [
    row('chat-up', undefined, { capabilities: ['chat'] }),
    row('chat-down', undefined, { capabilities: ['chat'], status: 'unavailable' }),
    row('embed-up', undefined, { capabilities: ['embedding'] }),
    row('multi-up', undefined, { capabilities: ['chat', 'embedding'] }),
    row('image-up', undefined, { capabilities: ['image'] }),
  ];

  function visibleIds(): string[] {
    return screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => rows.find((m) => r.textContent?.includes(m.id))?.id ?? '')
      .sort();
  }

  // Header filter buttons are all named "Filter"; Availability precedes Capabilities.
  async function openCapabilityFilter(user: ReturnType<typeof userEvent.setup>) {
    const buttons = screen.getAllByRole('button', { name: 'Filter' });
    await user.click(buttons[buttons.length - 1]);
    return within(screen.getByRole('group', { name: 'Filter options' }));
  }

  it('offers only the capabilities present in the loaded models', async () => {
    const user = userEvent.setup();
    renderTab(rows);
    const options = await openCapabilityFilter(user);
    expect(options.getAllByRole('button').map((b) => b.textContent)).toEqual([
      'chat',
      'embedding',
      'image',
    ]);
  });

  it('narrows the rows to the selected capability', async () => {
    const user = userEvent.setup();
    renderTab(rows);
    const options = await openCapabilityFilter(user);
    await user.click(options.getByRole('button', { name: 'embedding' }));
    expect(visibleIds()).toEqual(['embed-up', 'multi-up']);
  });

  it('keeps models matching any of several selected capabilities', async () => {
    const user = userEvent.setup();
    renderTab(rows);
    const options = await openCapabilityFilter(user);
    await user.click(options.getByRole('button', { name: 'embedding' }));
    await user.click(options.getByRole('button', { name: 'image' }));
    expect(visibleIds()).toEqual(['embed-up', 'image-up', 'multi-up']);
  });

  it('restores every row when the filter is cleared', async () => {
    const user = userEvent.setup();
    renderTab(rows);
    const options = await openCapabilityFilter(user);
    await user.click(options.getByRole('button', { name: 'image' }));
    expect(visibleIds()).toEqual(['image-up']);
    await user.click(screen.getByRole('button', { name: /Clear filter/ }));
    expect(visibleIds()).toEqual(['chat-down', 'chat-up', 'embed-up', 'image-up', 'multi-up']);
  });

  it('combines with the availability quick filter', async () => {
    const user = userEvent.setup();
    renderTab(rows);
    const options = await openCapabilityFilter(user);
    await user.click(options.getByRole('button', { name: 'chat' }));
    expect(visibleIds()).toEqual(['chat-down', 'chat-up', 'multi-up']);

    await user.keyboard('{Escape}');
    await user.click(screen.getByRole('button', { name: /^Unavailable/ }));
    expect(visibleIds()).toEqual(['chat-down']);
  });
});
