/**
 * Unit tests for the preferences read.
 *
 * readPreferences() tells whether the read succeeded (`loaded`). After a failed
 * read, the values are only the defaults, so a write would replace the stored
 * document with them. getPreferences() keeps its old behavior: it returns the
 * defaults on a failed read.
 *
 * Each test uses its own access token, because React cache() keys on the
 * arguments.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PREFERENCE_DEFAULTS } from './preferences.schema';

vi.mock('@orbit/server/config', () => ({ config: { orbitApiUrl: 'http://identity.test' } }));

import { getPreferences, readPreferences } from './preferences-server';

function stubFetch(respond: () => Promise<Response>) {
  vi.stubGlobal('fetch', vi.fn(respond));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('readPreferences', () => {
  it('reports a loaded read with the stored document', async () => {
    stubFetch(async () => Response.json({ analytics: { defaultPeriod: '30d' } }));

    const result = await readPreferences('token-stored');

    expect(result.loaded).toBe(true);
    expect(result.preferences.analytics.defaultPeriod).toBe('30d');
  });

  it('reports a loaded read when the user has no stored preferences', async () => {
    stubFetch(async () => Response.json({}));

    const result = await readPreferences('token-empty');

    expect(result).toEqual({ loaded: true, preferences: PREFERENCE_DEFAULTS });
  });

  it('reports a failed read for an error status', async () => {
    stubFetch(async () => new Response('unavailable', { status: 503 }));

    const result = await readPreferences('token-503');

    expect(result).toEqual({ loaded: false, preferences: PREFERENCE_DEFAULTS });
  });

  it('reports a failed read when the request fails', async () => {
    stubFetch(async () => {
      throw new DOMException('The operation was aborted.', 'AbortError');
    });

    const result = await readPreferences('token-timeout');

    expect(result).toEqual({ loaded: false, preferences: PREFERENCE_DEFAULTS });
  });
});

describe('getPreferences', () => {
  it('returns the stored document after a successful read', async () => {
    stubFetch(async () => Response.json({ myUsage: { defaultPeriod: '90d' } }));

    const preferences = await getPreferences('token-get-stored');

    expect(preferences.myUsage.defaultPeriod).toBe('90d');
  });

  it('returns the defaults after a failed read, as before', async () => {
    stubFetch(async () => new Response('unavailable', { status: 503 }));

    const preferences = await getPreferences('token-get-503');

    expect(preferences).toEqual(PREFERENCE_DEFAULTS);
  });
});
