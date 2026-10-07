/**
 * Unit tests for the `marketplace.lastSelected` preference key.
 *
 * The Marketplace page saves the Marketplace that the user selected last, and
 * opens it again when the URL names no Marketplace.
 */
import { describe, expect, it } from 'vitest';
import { PREFERENCE_DEFAULTS, mergeWithDefaults, serializeSparse } from './preferences.schema';

describe('marketplace.lastSelected', () => {
  it('reads a stored Marketplace name', () => {
    const prefs = mergeWithDefaults({ marketplace: { lastSelected: 'ClearRoute Core' } });
    expect(prefs.marketplace.lastSelected).toBe('ClearRoute Core');
  });

  it('uses the default when the stored value is missing or not a string', () => {
    expect(mergeWithDefaults({}).marketplace.lastSelected).toBe('');
    expect(mergeWithDefaults({ marketplace: null }).marketplace.lastSelected).toBe('');
    expect(mergeWithDefaults({ marketplace: { lastSelected: 42 } }).marketplace.lastSelected).toBe(
      '',
    );
  });

  it('sends the name only when it differs from the default', () => {
    expect(serializeSparse(PREFERENCE_DEFAULTS)).toEqual({});
    expect(
      serializeSparse({ ...PREFERENCE_DEFAULTS, marketplace: { lastSelected: 'ClearRoute Core' } }),
    ).toEqual({ marketplace: { lastSelected: 'ClearRoute Core' } });
  });
});
