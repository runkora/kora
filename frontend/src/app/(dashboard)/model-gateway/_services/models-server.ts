import 'server-only';

import { config } from '@orbit/server/config';
import type { ModelsListResponse } from '@orbit/shared/api';
import { toModelRow } from '../_types';
import type { ModelRow } from '../_types';

// ── fetchModelsFromGateway ────────────────────────────────────────────────────
//
// Server-only utility. Calls the inference gateway directly with a session
// access token. Imported by both:
//   - The RSC page ([tab]/page.tsx) for server-side rendering
//   - The BFF route (/api/models/route.ts) for client-initiated fetches
//
// Returns a discriminated union so each caller can handle errors in the way
// that makes sense for its context (RSC: fall back to []; BFF: return 5xx).
//
// Never call this from client-side code — use the BFF route instead.

export type GatewayModelsResult =
  | { ok: true; models: ModelRow[] }
  | { ok: false; status: number; message: string };

export async function fetchModelsFromGateway(accessToken: string): Promise<GatewayModelsResult> {
  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/models`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
    });
  } catch (err) {
    console.error('[fetchModelsFromGateway] network error:', err);
    return { ok: false, status: 502, message: 'Failed to reach inference gateway' };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchModelsFromGateway] gateway responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  let body: ModelsListResponse;
  try {
    body = (await res.json()) as ModelsListResponse;
  } catch {
    console.error('[fetchModelsFromGateway] failed to parse gateway response as JSON');
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
  return { ok: true, models: (body.data ?? []).map(toModelRow) };
}
