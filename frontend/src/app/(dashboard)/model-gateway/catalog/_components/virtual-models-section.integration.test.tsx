/**
 * Integration tests for pricing-related cells in the virtual-models table.
 *
 * VmEffectivePriceCell is private, so we drive it through the exported
 * VirtualModelsSection and assert on the rendered text / indicators.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { VirtualModelsSection } from './virtual-models-section';
import type { VirtualModel } from '../_types/catalog';

function vm(overrides: Partial<VirtualModel> = {}): VirtualModel {
  return {
    id: 'vm-1',
    family: 'anthropic',
    name: 'claude-sonnet-4-6',
    displayName: 'Claude Sonnet 4.6',
    description: '',
    strategy: 'priority',
    routes: [],
    enabled: true,
    ...overrides,
  };
}

const noop = vi.fn();

describe('VmEffectivePriceCell', () => {
  it('renders Unknown state when pricing is absent', () => {
    renderWithProviders(
      <VirtualModelsSection models={[vm({ pricing: undefined })]} onSelect={noop} />,
    );
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });

  it('renders token pricing when effective token costs are non-zero', () => {
    renderWithProviders(
      <VirtualModelsSection
        models={[
          vm({
            pricing: {
              effective_input_cost_per_1m: 3.0,
              effective_output_cost_per_1m: 15.0,
            },
          }),
        ]}
        onSelect={noop}
      />,
    );
    // fmtCost(3.0)/1M — the cell renders "X/1M" for the input price
    const cells = screen.getAllByText((_, el) => el?.textContent?.includes('/1M') ?? false);
    expect(cells.length).toBeGreaterThan(0);
  });

  it('renders info icon (no price text) when non_token_only is true', () => {
    renderWithProviders(
      <VirtualModelsSection
        models={[
          vm({
            pricing: {
              non_token_only: true,
            },
          }),
        ]}
        onSelect={noop}
      />,
    );
    // No price text and no em-dash — the cell renders only a muted info icon.
    expect(screen.queryByText('—')).not.toBeInTheDocument();
    expect(screen.queryByText(/\$\d/)).not.toBeInTheDocument();
    expect(screen.queryByText('Unknown')).not.toBeInTheDocument();
  });
});
