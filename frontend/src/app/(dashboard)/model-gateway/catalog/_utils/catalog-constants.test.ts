import { describe, it, expect } from 'vitest';
import { providerBadgeVariant, providerLabel, providerTitle } from './catalog-constants';

describe('providerLabel', () => {
  it.each([
    ['bedrock:aus', 'aus'],
    ['compatible:openrouter', 'openrouter'],
    ['vertexai:gcp-prod', 'gcp-prod'],
    ['bedrock', 'AWS Bedrock'],
    ['mystery', 'mystery'],
  ])('%s shows %s', (provider, label) => {
    expect(providerLabel(provider)).toBe(label);
  });
});

describe('providerTitle', () => {
  it.each([
    ['bedrock:aus', 'AWS Bedrock: aus'],
    ['compatible:openrouter', 'Compatible endpoint: openrouter'],
    ['azure', 'Azure OpenAI'],
    ['mystery:x', 'mystery:x'],
  ])('%s is titled %s', (provider, title) => {
    expect(providerTitle(provider)).toBe(title);
  });
});

describe('providerBadgeVariant', () => {
  it('shares the type style across named providers', () => {
    expect(providerBadgeVariant('bedrock:aus')).toBe('provider-bedrock');
    expect(providerBadgeVariant('azure:eastus')).toBe('provider-azure');
    expect(providerBadgeVariant('compatible:openrouter')).toBe('outline');
  });
});
