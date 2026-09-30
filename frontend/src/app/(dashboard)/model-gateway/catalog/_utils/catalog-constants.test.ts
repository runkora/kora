import { describe, it, expect } from 'vitest';
import { providerBadgeVariant, providerLabel } from './catalog-constants';

describe('providerLabel', () => {
  it.each([
    ['bedrock', 'AWS Bedrock'],
    ['bedrock:aus', 'AWS Bedrock (aus)'],
    ['openai:openrouter', 'OpenAI-compatible (openrouter)'],
    ['anthropic:claude', 'Anthropic (claude)'],
    ['vertexai:gcp-prod', 'Vertex AI (gcp-prod)'],
    ['mystery', 'mystery'],
  ])('%s is labelled %s', (provider, label) => {
    expect(providerLabel(provider)).toBe(label);
  });
});

describe('providerBadgeVariant', () => {
  it('shares the type style across named providers', () => {
    expect(providerBadgeVariant('bedrock:aus')).toBe('provider-bedrock');
    expect(providerBadgeVariant('azure:eastus')).toBe('provider-azure');
    expect(providerBadgeVariant('openai:openrouter')).toBe('outline');
  });
});
