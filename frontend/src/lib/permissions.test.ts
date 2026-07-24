import { describe, it, expect } from 'vitest';
import { hasScope, hasAnyScope, hasAllScopes, scopeCovers, hasScopeCovering } from './permissions';

describe('hasScope', () => {
  it('returns true when the scope is present', () => {
    expect(hasScope(['platform:grants:read'], 'platform:grants:read')).toBe(true);
  });

  it('returns false when the scope is absent', () => {
    expect(hasScope(['platform:grants:read'], 'platform:grants:write')).toBe(false);
  });

  it('returns false when the scopes array is empty', () => {
    expect(hasScope([], 'platform:grants:read')).toBe(false);
  });

  it('requires an exact match — prefix does not satisfy a longer scope', () => {
    expect(hasScope(['platform:grants'], 'platform:grants:read')).toBe(false);
  });

  it('requires an exact match — longer scope does not satisfy a shorter required scope', () => {
    expect(hasScope(['platform:grants:read'], 'platform:grants')).toBe(false);
  });
});

describe('hasAnyScope', () => {
  it('returns true when at least one required scope matches', () => {
    expect(
      hasAnyScope(['platform:grants:read', 'platform:roles:read'], ['platform:grants:read']),
    ).toBe(true);
  });

  it('returns true when multiple required scopes match', () => {
    expect(
      hasAnyScope(
        ['platform:grants:read', 'platform:roles:read'],
        ['platform:grants:read', 'platform:roles:read'],
      ),
    ).toBe(true);
  });

  it('returns false when none of the required scopes match', () => {
    expect(
      hasAnyScope(['platform:grants:read'], ['platform:grants:write', 'platform:roles:write']),
    ).toBe(false);
  });

  it('returns false when the scopes array is empty', () => {
    expect(hasAnyScope([], ['platform:grants:read'])).toBe(false);
  });

  it('returns false when the required array is empty', () => {
    expect(hasAnyScope(['platform:grants:read'], [])).toBe(false);
  });
});

describe('hasAllScopes', () => {
  it('returns true when all required scopes are present', () => {
    expect(
      hasAllScopes(
        ['platform:grants:read', 'platform:roles:read'],
        ['platform:grants:read', 'platform:roles:read'],
      ),
    ).toBe(true);
  });

  it('returns false when one required scope is missing', () => {
    expect(
      hasAllScopes(['platform:grants:read'], ['platform:grants:read', 'platform:roles:read']),
    ).toBe(false);
  });

  it('returns false when the scopes array is empty and required is non-empty', () => {
    expect(hasAllScopes([], ['platform:grants:read'])).toBe(false);
  });

  it('returns true when the required array is empty', () => {
    expect(hasAllScopes([], [])).toBe(true);
  });

  it('returns true when the principal has more scopes than required', () => {
    expect(
      hasAllScopes(
        ['platform:grants:read', 'platform:grants:write', 'platform:roles:read'],
        ['platform:grants:read'],
      ),
    ).toBe(true);
  });
});

describe('scopeCovers', () => {
  it('covers an identical scope', () => {
    expect(scopeCovers('agents:types:write', 'agents:types:write')).toBe(true);
  });

  it('parent path prefix covers the mode-qualified child', () => {
    expect(scopeCovers('agents:types:write', 'agents:types:delegated:write')).toBe(true);
    expect(scopeCovers('agents:types:write', 'agents:types:autonomous:write')).toBe(true);
  });

  it('child does not cover the parent', () => {
    expect(scopeCovers('agents:types:delegated:write', 'agents:types:write')).toBe(false);
  });

  it('sibling modes do not cover each other', () => {
    expect(scopeCovers('agents:types:delegated:write', 'agents:types:autonomous:write')).toBe(
      false,
    );
  });

  it('different action does not cover', () => {
    expect(scopeCovers('agents:types:read', 'agents:types:delegated:write')).toBe(false);
  });

  it('different resource type does not cover', () => {
    expect(scopeCovers('platform:types:write', 'agents:types:delegated:write')).toBe(false);
  });

  it('single-segment scopes never cover anything but themselves', () => {
    expect(scopeCovers('agents', 'agents:types:write')).toBe(false);
  });
});

describe('hasScopeCovering', () => {
  it('finds a covering scope in the set', () => {
    expect(
      hasScopeCovering(['agents:types:read', 'agents:types:write'], 'agents:types:delegated:write'),
    ).toBe(true);
  });

  it('finds an exact scope in the set', () => {
    expect(hasScopeCovering(['agents:types:delegated:write'], 'agents:types:delegated:write')).toBe(
      true,
    );
  });

  it('returns false when nothing covers', () => {
    expect(
      hasScopeCovering(['agents:types:delegated:write'], 'agents:types:autonomous:write'),
    ).toBe(false);
  });

  it('returns false for an empty set', () => {
    expect(hasScopeCovering([], 'agents:types:delegated:write')).toBe(false);
  });
});
