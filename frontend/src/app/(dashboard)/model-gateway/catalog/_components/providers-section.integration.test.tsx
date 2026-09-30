import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { ProvidersSection } from './providers-section';
import type { ProviderStatusWire } from '../../_services/catalog-server';

function provider(overrides: Partial<ProviderStatusWire> = {}): ProviderStatusWire {
  return {
    name: 'openai:openrouter',
    type: 'openai',
    source: 'resource',
    resource: 'openrouter',
    location: 'https://openrouter.ai/api/v1',
    credential_mode: 'secret',
    status: 'ready',
    model_count: 2,
    ...overrides,
  };
}

describe('ProvidersSection', () => {
  it('shows the resource to apply when no provider is registered', () => {
    renderWithProviders(<ProvidersSection providers={[]} />);
    expect(screen.getByText(/No providers are registered/)).toBeInTheDocument();
    expect(screen.getByText(/kind: InferenceProvider/)).toBeInTheDocument();
  });

  it('lists each provider with its source, credentials and status', () => {
    renderWithProviders(
      <ProvidersSection
        providers={[
          provider(),
          provider({
            name: 'bedrock:us',
            type: 'bedrock',
            source: 'environment',
            resource: undefined,
            location: 'us-west-2',
            credential_mode: 'workload identity',
            status: 'error',
            error: 'list models: AccessDenied',
            model_count: 0,
          }),
        ]}
      />,
    );
    expect(screen.getByText('OpenAI-compatible (openrouter)')).toBeInTheDocument();
    expect(screen.getByText('InferenceProvider/openrouter')).toBeInTheDocument();
    expect(screen.getByText('AWS Bedrock (us)')).toBeInTheDocument();
    expect(screen.getByText('Env vars (deprecated)')).toBeInTheDocument();
    expect(screen.getByText('workload identity')).toBeInTheDocument();
    expect(screen.getByText('list models: AccessDenied')).toBeInTheDocument();
    expect(screen.getByText('Ready')).toBeInTheDocument();
    expect(screen.getByText('Error')).toBeInTheDocument();
  });
});
