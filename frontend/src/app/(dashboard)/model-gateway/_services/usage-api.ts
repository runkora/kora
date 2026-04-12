// ── Usage API service ─────────────────────────────────────────────────────────
//
// Client-side fetch helper. Calls the BFF route handler at /api/usage — never
// the Go backend directly (which is server-only via orbitClient).

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

export interface FetchUsageResult {
  records: InferenceUsageRecord[];
  total: number;
  pats: PAT[];
  error: string | null;
}

/**
 * Fetch usage records from the BFF proxy route.
 *
 * Pass an optional `key` (PAT UUID) to filter by a specific API key.
 *
 * Returns a normalised result object — never throws — so callers can
 * pattern-match on `error` without a try/catch at every call site.
 */
export async function fetchUsage(key?: string): Promise<FetchUsageResult> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.usage.GET(key), { cache: 'no-store' });
  } catch {
    return {
      records: [],
      total: 0,
      pats: [],
      error: 'Network error — could not reach the server.',
    };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // ignore parse errors; keep the status-code message
    }
    return { records: [], total: 0, pats: [], error: message };
  }

  const data = (await res.json()) as {
    records: InferenceUsageRecord[];
    total: number;
    pats: PAT[];
  };
  return {
    records: data.records ?? [],
    total: data.total ?? 0,
    pats: data.pats ?? [],
    error: null,
  };
}
