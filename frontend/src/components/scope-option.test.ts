import { describe, it, expect } from 'vitest';
import { scopeOptionsFor, toScopeOption } from './scope-option';

describe('toScopeOption', () => {
  it('maps vocabulary fields and drops empty ones', () => {
    expect(
      toScopeOption({
        scope: 'agents:runs:read',
        resource_type: 'agents',
        path: ['runs'],
        description: '',
      }),
    ).toEqual({
      scope: 'agents:runs:read',
      resourceType: 'agents',
      path: ['runs'],
      description: undefined,
    });
  });
});

describe('scopeOptionsFor', () => {
  it('keeps the allowed order and falls back to bare strings for unknown scopes', () => {
    const out = scopeOptionsFor(
      ['b:x', 'a:y'],
      [{ scope: 'a:y', resource_type: 'a', path: [], description: 'Why' }],
    );
    expect(out.map((o) => o.scope)).toEqual(['b:x', 'a:y']);
    expect(out[0]).toEqual({ scope: 'b:x' });
    expect(out[1].description).toBe('Why');
  });
});

describe('edge cases', () => {
  it('maps a null path to undefined', () => {
    expect(toScopeOption({ scope: 'a:b', path: null }).path).toBeUndefined();
  });

  it('keeps duplicates in the allowed list as given', () => {
    expect(scopeOptionsFor(['a:b', 'a:b'], []).map((o) => o.scope)).toEqual(['a:b', 'a:b']);
  });
});
