import 'server-only';

import { config } from '@orbit/server/config';
import type { InferenceUsageRecord, ListUsageResponse } from '../_types';

// ── Usage stats ───────────────────────────────────────────────────────────────

export interface TokenUsageStat {
  token_id: string;
  request_count: number;
  last_used: string; // RFC3339
}

export type UsageStatsResult =
  | { ok: true; stats: TokenUsageStat[] }
  | { ok: false; status: number; message: string };

export async function fetchUsageStats(accessToken: string): Promise<UsageStatsResult> {
  const url = `${config.inferenceGatewayUrl}/usage/stats`;
  let res: Response;
  try {
    res = await fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const body = (await res.json()) as { data: TokenUsageStat[] };
    return { ok: true, stats: body.data ?? [] };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

// ── ListUsageParams ───────────────────────────────────────────────────────────

export interface ListUsageParams {
  tokenId?: string;
  provider?: string;
  apiDialect?: string;
  status?: string;
  /** Filters by the raw provider-side model_id column value. */
  modelId?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
  /**
   * Admin-only narrowing filters, honoured only by `fetchAllUsageFromGateway`
   * (GET /v1/usage/all). The self-scoped GET /v1/usage ignores them — its
   * principal is always the caller. They can only narrow *within* the tenant;
   * the tenant itself is always forced from the verified JWT server-side.
   */
  principalId?: string;
  principalType?: string;
}

// ── Query builder ──────────────────────────────────────────────────────────────

// Builds the shared usage query string. principal_id / principal_type are only
// meaningful on the /usage/all path; the self-scoped /usage endpoint ignores them.
function buildUsageQuery(params: ListUsageParams): string {
  const qs = new URLSearchParams();
  if (params.tokenId) qs.set('token_id', params.tokenId);
  if (params.provider) qs.set('provider', params.provider);
  if (params.apiDialect) qs.set('api_dialect', params.apiDialect);
  if (params.status) qs.set('status', params.status);
  if (params.modelId) qs.set('model_id', params.modelId);
  if (params.from) qs.set('from', params.from);
  if (params.to) qs.set('to', params.to);
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.cursor) qs.set('cursor', params.cursor);
  if (params.principalId) qs.set('principal_id', params.principalId);
  if (params.principalType) qs.set('principal_type', params.principalType);
  return qs.toString();
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
  const qs = buildUsageQuery(params);
  const url = `${config.inferenceGatewayUrl}/usage${qs ? `?${qs}` : ''}`;

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

// ── fetchAllUsageFromGateway ───────────────────────────────────────────────────
//
// Server-only utility. Calls GET /v1/usage/all on the inference gateway — the
// tenant-wide, cross-principal inference log backing the admin "All logs" view.
// The gateway requires platform:analytics:read (enforced at the edge) and always
// scopes rows to the caller's tenant from the verified JWT. `principalId` /
// `principalType` are optional narrowing filters; they can only narrow *within*
// the tenant, never widen or cross it.
//
// Shares ListUsageResult with fetchUsageFromGateway so callers pattern-match on
// the same shape. Never call this from client-side code.

export async function fetchAllUsageFromGateway(
  accessToken: string,
  params: ListUsageParams = {},
): Promise<ListUsageResult> {
  const qs = buildUsageQuery(params);
  const url = `${config.inferenceGatewayUrl}/usage/all${qs ? `?${qs}` : ''}`;

  let res: Response;
  try {
    res = await fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchAllUsageFromGateway] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchAllUsageFromGateway] gateway responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  let body: ListUsageResponse;
  try {
    body = (await res.json()) as ListUsageResponse;
  } catch {
    console.error('[fetchAllUsageFromGateway] failed to parse gateway response as JSON');
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
