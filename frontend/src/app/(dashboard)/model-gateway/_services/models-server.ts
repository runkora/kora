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

type GatewayFetchError = { ok: false; status: number; message: string };

export type GatewayModelsResult = { ok: true; models: ModelRow[] } | GatewayFetchError;

export type GatewayRawModelsResult =
  { ok: true; models: ModelsListResponse['data'] } | GatewayFetchError;

// `capability` is forwarded to the gateway's `?capability=` filter (any-of
// semantics). Pass it as repeated values or one comma-separated string.
async function fetchFromGateway(
  accessToken: string,
  capability: string[] = [],
): Promise<{ ok: true; body: ModelsListResponse } | GatewayFetchError> {
  const query = new URLSearchParams();
  for (const c of capability) query.append('capability', c);
  const qs = query.size > 0 ? `?${query.toString()}` : '';

  let res: Response;
  try {
    res = await fetch(`${config.inferenceGatewayUrl}/models${qs}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchModelsFromGateway] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchModelsFromGateway] gateway responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const body = (await res.json()) as ModelsListResponse;
    return { ok: true, body };
  } catch {
    console.error('[fetchModelsFromGateway] failed to parse gateway response as JSON');
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

// Returns models transformed into ModelRow display objects (for model-gateway UI).
export async function fetchModelsFromGateway(
  accessToken: string,
  capability: string[] = [],
): Promise<GatewayModelsResult> {
  const result = await fetchFromGateway(accessToken, capability);
  if (!result.ok) return result;
  return { ok: true, models: (result.body.data ?? []).map(toModelRow) };
}

// Returns raw ModelObject[] as returned by the inference gateway (for BFF routes).
export async function fetchRawModelsFromGateway(
  accessToken: string,
  capability: string[] = [],
): Promise<GatewayRawModelsResult> {
  const result = await fetchFromGateway(accessToken, capability);
  if (!result.ok) return result;
  return { ok: true, models: result.body.data ?? [] };
}
