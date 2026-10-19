import { describe, it, expect, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { RoutesTab } from './routes-tab';
import type { ProviderModel, RouteTarget, VirtualModel } from '../_types/catalog';

function route(overrides: Partial<RouteTarget> = {}): RouteTarget {
  return {
    id: 'r1',
    provider: 'compatible:openrouter',
    modelId: 'openai/gpt-oss-20b',
    priority: 1,
    weight: 0,
    inputCostPer1M: 0,
    outputCostPer1M: 0,
    enabled: true,
    ...overrides,
  };
}

function virtualModel(routes: RouteTarget[]): VirtualModel {
  return { routes, strategy: 'priority' } as unknown as VirtualModel;
}

const discovered: ProviderModel[] = [
  {
    provider: 'compatible:openrouter',
    modelId: 'openai/gpt-oss-20b',
    displayName: 'openai/gpt-oss-20b',
    family: 'openai',
    capabilities: ['chat'],
    aliases: [],
    inputModalities: ['text'],
    outputModalities: ['text'],
    dialects: ['openai'],
    enabled: true,
  },
];

describe('RoutesTab', () => {
  it('offers only the registered providers', () => {
    renderWithProviders(
      <RoutesTab
        model={virtualModel([route()])}
        onChange={vi.fn()}
        providerModels={discovered}
        providers={['bedrock:us', 'compatible:openrouter']}
      />,
    );
    const options = within(screen.getAllByRole('combobox')[0])
      .getAllByRole('option')
      .map((o) => o.getAttribute('value'));
    expect(options).toEqual(['bedrock:us', 'compatible:openrouter']);
  });

  it('keeps an unregistered provider selectable on an existing route', () => {
    renderWithProviders(
      <RoutesTab
        model={virtualModel([route({ provider: 'compatible:gone', modelId: 'm' })])}
        onChange={vi.fn()}
        providerModels={[]}
        providers={['compatible:openrouter']}
      />,
    );
    const options = within(screen.getAllByRole('combobox')[0])
      .getAllByRole('option')
      .map((o) => o.getAttribute('value'));
    expect(options).toEqual(['compatible:gone', 'compatible:openrouter']);
  });

  it('adds a route on the first registered provider', async () => {
    const onChange = vi.fn();
    renderWithProviders(
      <RoutesTab
        model={virtualModel([])}
        onChange={onChange}
        providerModels={discovered}
        providers={['compatible:openrouter']}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: /Add Route/ }));
    expect(onChange.mock.calls[0][0].routes[0].provider).toBe('compatible:openrouter');
  });

  it('shows an empty state and disables adding a route with no providers', () => {
    renderWithProviders(
      <RoutesTab model={virtualModel([])} onChange={vi.fn()} providerModels={[]} providers={[]} />,
    );
    expect(screen.getByText(/No providers are registered/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Add Route/ })).toBeDisabled();
  });
});
