import 'server-only';
import { cache } from 'react';
import { config } from '@orbit/server/config';
import { mergeWithDefaults } from './preferences.schema';
import type { UserPreferences } from './preferences.schema';

/**
 * The result of a preferences read. `loaded` is false when the read failed or
 * timed out. Then `preferences` holds only the defaults, and a write would
 * replace the stored document with them.
 */
export interface PreferencesReadResult {
  loaded: boolean;
  preferences: UserPreferences;
}

/**
 * Fetch the user's stored preferences from the identity service, and tell
 * whether the read succeeded. Wrapped in React.cache() so multiple RSCs in the
 * same request (layout + page) share a single fetch rather than each issuing
 * their own.
 */
export const readPreferences = cache(
  async (accessToken: string): Promise<PreferencesReadResult> => {
    try {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 500);
      const res = await fetch(`${config.orbitApiUrl}/identity/preferences`, {
        headers: { Authorization: `Bearer ${accessToken}` },
        cache: 'no-store',
        signal: controller.signal,
      });
      clearTimeout(timeout);
      if (!res.ok) return { loaded: false, preferences: mergeWithDefaults({}) };
      const raw: unknown = await res.json();
      if (typeof raw === 'object' && raw !== null) {
        return { loaded: true, preferences: mergeWithDefaults(raw as Record<string, unknown>) };
      }
      return { loaded: false, preferences: mergeWithDefaults({}) };
    } catch {
      return { loaded: false, preferences: mergeWithDefaults({}) };
    }
  },
);

/**
 * Fetch the user's stored preferences from the identity service. On a failed
 * read, returns the defaults. Shares the cached read with readPreferences().
 */
export const getPreferences = cache(async (accessToken: string): Promise<UserPreferences> => {
  const result = await readPreferences(accessToken);
  return result.preferences;
});
