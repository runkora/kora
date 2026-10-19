/**
 * Integration tests for pricing cells in the provider-catalog table.
 *
 * EffectivePriceCell and NonTokenPricingRows are private, so we drive them
 * through the exported ProviderCatalogSection with controlled props.
 */
import { describe, it, expect } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { ProviderCatalogSection } from './provider-catalog-section';
import type { ProviderModel } from '../_types/catalog';
import type { PricingEntry } from '../_types/catalog';

function pricing(overrides: Partial<PricingEntry> = {}): PricingEntry {
  return {
    provider: 'bedrock',
    model_id: 'test-model',
    synced_input_cost_per_1m: 0,
    synced_output_cost_per_1m: 0,
    synced_cache_read_cost_per_1m: 0,
    synced_cache_write_cost_per_1m: 0,
    override_input_cost_per_1m: null,
    override_output_cost_per_1m: null,
    override_cache_read_cost_per_1m: null,
    override_cache_write_cost_per_1m: null,
    effective_input_cost_per_1m: 0,
    effective_output_cost_per_1m: 0,
    has_override: false,
    last_synced_at: '2026-07-01T00:00:00Z',
    ...overrides,
  };
}

function model(overrides: Partial<ProviderModel> = {}): ProviderModel {
  return {
    provider: 'bedrock',
    modelId: 'test-model',
    displayName: 'Test Model',
    family: 'anthropic',
    capabilities: ['chat'],
    aliases: [],
    inputModalities: ['text'],
    outputModalities: ['text'],
    dialects: ['openai'],
    enabled: true,
    ...overrides,
  };
}

describe('EffectivePriceCell (provider catalog)', () => {
  it('renders Unknown when model has no pricing', () => {
    renderWithProviders(<ProviderCatalogSection models={[model({ pricing: undefined })]} />);
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });

  it('renders token pricing when effective costs are non-zero', () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              effective_input_cost_per_1m: 3.0,
              effective_output_cost_per_1m: 15.0,
            }),
          }),
        ]}
      />,
    );
    // fmtCost(3.0)/1M — assert the /1M suffix appears as part of a token-priced cell
    const cells = screen.getAllByText((_, el) => el?.textContent?.includes('/1M') ?? false);
    expect(cells.length).toBeGreaterThan(0);
  });

  it('renders info icon (no price text) when token costs are zero (non-token-only model)', () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              effective_input_cost_per_1m: 0,
              effective_output_cost_per_1m: 0,
              synced_image_input_cost_per_image: 0.001,
            }),
          }),
        ]}
      />,
    );
    // No em-dash and no dollar price — just a muted info icon.
    expect(screen.queryByText('—')).not.toBeInTheDocument();
    expect(screen.queryByText(/\$\d/)).not.toBeInTheDocument();
  });
});

describe('NonTokenPricingRows', () => {
  it('renders image input row when synced_image_input_cost_per_image is set', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              synced_image_input_cost_per_image: 0.001,
            }),
          }),
        ]}
      />,
    );
    // Open the detail overlay by clicking the row.
    fireEvent.click(screen.getByText('test-model'));
    // The overlay renders NonTokenPricingRows — assert the label is present.
    expect(await screen.findByText('Image input /image')).toBeInTheDocument();
  });

  it('renders audio pricing rows when audio costs are set', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              synced_audio_input_cost_per_1m: 1.5,
              synced_audio_output_cost_per_1m: 3.0,
            }),
          }),
        ]}
      />,
    );
    // Open the detail overlay by clicking the row.
    fireEvent.click(screen.getByText('test-model'));
    expect(await screen.findByText('Audio input /1M')).toBeInTheDocument();
    expect(await screen.findByText('Audio output /1M')).toBeInTheDocument();
  });
});

describe('EffectivePriceCell tooltip (provider catalog)', () => {
  it('opens the tooltip on hover for a non-token-only model', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[model({ pricing: pricing({ synced_image_input_cost_per_image: 0.001 }) })]}
      />,
    );
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.setup().hover(screen.getByRole('button', { name: /non-token pricing/i }));
    expect(
      (await screen.findAllByText('Non-token pricing — see model details')).length,
    ).toBeGreaterThan(0);
  });
});

describe('Detail overlay token rows', () => {
  // A non-token-only model has no legitimate token price. Showing "$0.00" rows
  // next to the real image/audio rows reads as free, which it is not.
  it('omits the synced token rows for a non-token-only model', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[model({ pricing: pricing({ synced_image_input_cost_per_image: 0.001 }) })]}
      />,
    );
    fireEvent.click(screen.getByText('test-model'));
    expect(await screen.findByText('Image input /image')).toBeInTheDocument();
    expect(screen.queryByText('Synced input /1M')).not.toBeInTheDocument();
    expect(screen.queryByText('Synced output /1M')).not.toBeInTheDocument();
  });

  it('keeps the synced token rows for a token-priced model', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              synced_input_cost_per_1m: 3,
              synced_output_cost_per_1m: 15,
              effective_input_cost_per_1m: 3,
              effective_output_cost_per_1m: 15,
            }),
          }),
        ]}
      />,
    );
    fireEvent.click(screen.getByText('test-model'));
    expect(await screen.findByText('Synced input /1M')).toBeInTheDocument();
    expect(await screen.findByText('Synced output /1M')).toBeInTheDocument();
  });

  // Image costs are a dollar-per-image figure with no other cue to that unit;
  // unlike the audio rows there is no "/1M" in the label.
  it('labels image dimensions with the per-image unit', async () => {
    renderWithProviders(
      <ProviderCatalogSection
        models={[
          model({
            pricing: pricing({
              synced_image_input_cost_per_image: 0.001,
              synced_image_output_cost_per_image: 0.002,
            }),
          }),
        ]}
      />,
    );
    fireEvent.click(screen.getByText('test-model'));
    expect(await screen.findByText('Image input /image')).toBeInTheDocument();
    expect(await screen.findByText('Image output /image')).toBeInTheDocument();
  });
});
