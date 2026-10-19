import { describe, it, expect } from 'vitest';
import { scopeCovers, hasScopeCovering } from './permissions';

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

  it('a 2-part scope covers every 3-part scope of the same type and action', () => {
    expect(hasScopeCovering(['agents:read'], 'agents:types:read')).toBe(true);
  });

  it('a per-slug scope does not cover its parent', () => {
    expect(hasScopeCovering(['agents:runs:coder:write'], 'agents:runs:write')).toBe(false);
  });
});
