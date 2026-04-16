import 'server-only';

import { config } from '@orbit/server/config';
import type { InferenceUsageRecord, ListUsageResponse } from '../_types';

// ── ListUsageParams ───────────────────────────────────────────────────────────

export interface ListUsageParams {
  tokenId?: string;
  provider?: string;
  apiDialect?: string;
  status?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
}

// ── Result types ──────────────────────────────────────────────────────────────

export type ListUsageResult =
  | {
      ok: true;
      records: InferenceUsageRecord[];
      hasNextPage: boolean;
      nextCursor: string | null;
      limit: number;
    }
  | { ok: false; status: number; message: string };

// ── fetchUsageFromGateway ─────────────────────────────────────────────────────
//
// Server-only utility. Calls GET /v1/usage on the inference gateway with the
// authenticated user's access token. TenantID and PrincipalID are always
// enforced server-side from the JWT — they cannot be overridden here.
//
// Never call this from client-side code.

export async function fetchUsageFromGateway(
  accessToken: string,
  params: ListUsageParams = {},
): Promise<ListUsageResult> {
  const qs = new URLSearchParams();
  if (params.tokenId) qs.set('token_id', params.tokenId);
  if (params.provider) qs.set('provider', params.provider);
  if (params.apiDialect) qs.set('api_dialect', params.apiDialect);
  if (params.status) qs.set('status', params.status);
  if (params.from) qs.set('from', params.from);
  if (params.to) qs.set('to', params.to);
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.cursor) qs.set('cursor', params.cursor);

  const url = `${config.inferenceGatewayUrl}/usage${qs.size > 0 ? `?${qs.toString()}` : ''}`;

  let res: Response;
  try {
    res = await fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchUsageFromGateway] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchUsageFromGateway] gateway responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  let body: ListUsageResponse;
  try {
    body = (await res.json()) as ListUsageResponse;
  } catch {
    console.error('[fetchUsageFromGateway] failed to parse gateway response as JSON');
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }

  return {
    ok: true,
    records: body.data ?? [],
    hasNextPage: body.has_next_page,
    nextCursor: body.next_cursor,
    limit: body.limit,
  };
}
