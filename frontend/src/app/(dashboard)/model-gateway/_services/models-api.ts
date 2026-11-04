// ── Models API service ────────────────────────────────────────────────────────
//
// Client-side fetch helper. Calls the BFF route handler at /api/models — never
// the Go backend directly (which is server-only via koraClient).

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { ModelRow } from '../_types';

export interface FetchModelsResult {
  models: ModelRow[];
  error: string | null;
}

/**
 * Fetch all models from the BFF proxy route.
 *
 * Returns a normalised result object — never throws — so callers can
 * pattern-match on `error` without a try/catch at every call site.
 */
export async function fetchModels(): Promise<FetchModelsResult> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.models.GET(), { cache: 'no-store' });
  } catch {
    return { models: [], error: 'Network error: could not reach the server.' };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // ignore parse errors; keep the status-code message
    }
    return { models: [], error: message };
  }

  const data = (await res.json()) as { data: ModelRow[] };
  return {
    models: data.data ?? [],
    error: null,
  };
}
