import { describe, it, expect } from 'vitest';
import { filterModelsByScopes, modelUseScope } from './model-scope-filter';

const models = [
  { id: 'anthropic/claude-sonnet-4-6' },
  { id: 'openai/gpt-5-4' },
  { id: 'openai/gpt-oss-120b' },
];

const ids = (ms: { id: string }[]) => ms.map((m) => m.id);
const filtered = (scopes: string[]) => ids(filterModelsByScopes(models, scopes));

describe('modelUseScope', () => {
  it('derives the per-model use scope', () => {
    expect(modelUseScope('anthropic/claude-sonnet-4-6')).toBe(
      'inference:anthropic:claude-sonnet-4-6:use',
    );
  });

  it('returns null for non-standard ids', () => {
    expect(modelUseScope('weird')).toBeNull();
    expect(modelUseScope('a/b/c')).toBeNull();
    expect(modelUseScope('anthropic/')).toBeNull();
  });
});

describe('filterModelsByScopes', () => {
  it('passes all models when the caller holds no inference use-scopes', () => {
    expect(filtered(['inference:invoke', 'inference:list'])).toEqual(ids(models));
  });

  it('passes all models on an empty scope set', () => {
    expect(filtered([])).toEqual(ids(models));
  });

  it('broad inference:use covers every model', () => {
    expect(filtered(['inference:use'])).toEqual(ids(models));
  });

  it('a family scope covers only that family', () => {
    expect(filtered(['inference:anthropic:use'])).toEqual(['anthropic/claude-sonnet-4-6']);
    expect(filtered(['inference:openai:use'])).toEqual(['openai/gpt-5-4', 'openai/gpt-oss-120b']);
  });

  it('a family scope mixed with a broad scope still resolves via covering', () => {
    // Before the fix these both ended in :use but neither EQUALLED a model
    // scope, so every model was filtered out and the picker showed 0.
    expect(filtered(['inference:use', 'inference:anthropic:use'])).toEqual(ids(models));
  });

  it('an exact model scope covers only that model', () => {
    expect(filtered(['inference:openai:gpt-5-4:use'])).toEqual(['openai/gpt-5-4']);
  });

  it('does not pass models the caller cannot cover', () => {
    expect(filtered(['inference:anthropic:use'])).not.toContain('openai/gpt-5-4');
  });
});
