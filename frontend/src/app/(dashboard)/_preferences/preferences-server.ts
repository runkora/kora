import 'server-only';
import { cache } from 'react';
import { config } from '@orbit/server/config';
import { mergeWithDefaults } from './preferences.schema';
import type { UserPreferences } from './preferences.schema';

/**
 * Fetch the user's stored preferences from the identity service.
 * Wrapped in React.cache() so multiple RSCs in the same request (layout + page)
 * share a single fetch rather than each issuing their own.
 */
export const getPreferences = cache(async (accessToken: string): Promise<UserPreferences> => {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 500);
    const res = await fetch(`${config.orbitApiUrl}/identity/preferences`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: controller.signal,
    });
    clearTimeout(timeout);
    if (!res.ok) return mergeWithDefaults({});
    const raw: unknown = await res.json();
    return mergeWithDefaults(
      typeof raw === 'object' && raw !== null ? (raw as Record<string, unknown>) : {},
    );
  } catch {
    return mergeWithDefaults({});
  }
});
