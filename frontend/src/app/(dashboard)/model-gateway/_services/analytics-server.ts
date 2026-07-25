import 'server-only';

import { config } from '@orbit/server/config';
import type { AnalyticsResponse, PrincipalUsageSummary } from '../_types';

// ── Shared params ─────────────────────────────────────────────────────────────

export interface AnalyticsParams {
  days?: number;
  granularity?: string;
  /** Personal path only — PAT UUID to filter by a specific API key. */
  tokenId?: string;
  /** Tenant-wide path only — virtual model ID to filter by model. */
  modelId?: string;
}

// ── Result types ──────────────────────────────────────────────────────────────

export type AnalyticsResult =
  | { ok: true; data: AnalyticsResponse }
  | { ok: false; status: number; message: string };

// Raw (pre-enrichment) response from the inference gateway principals endpoint.
export interface RawPrincipalsResponse {
  data: PrincipalUsageSummary[];
  total: number;
  page: number;
  limit: number;
}

export type AnalyticsPrincipalsRawResult =
  | { ok: true; data: RawPrincipalsResponse }
  | { ok: false; status: number; message: string };

// ── Internal helper ───────────────────────────────────────────────────────────

async function fetchAnalytics(
  accessToken: string,
  path: string,
  params: AnalyticsParams,
): Promise<AnalyticsResult> {
  const qs = new URLSearchParams();
  if (params.days !== undefined) qs.set('days', String(params.days));
  if (params.granularity) qs.set('granularity', params.granularity);
  if (params.tokenId) qs.set('token_id', params.tokenId);
  if (params.modelId) qs.set('model_id', params.modelId);

  const url = `${config.inferenceGatewayUrl}${path}${qs.size > 0 ? `?${qs.toString()}` : ''}`;

  let res: Response;
  try {
    res = await fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(15_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchAnalytics] ${path} ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchAnalytics] ${path} responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as AnalyticsResponse;
    return { ok: true, data };
  } catch {
    console.error(`[fetchAnalytics] ${path} failed to parse response as JSON`);
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}

// ── Public API ────────────────────────────────────────────────────────────────

export async function fetchAnalyticsMeFromGateway(
  accessToken: string,
  params: AnalyticsParams = {},
): Promise<AnalyticsResult> {
  return fetchAnalytics(accessToken, '/analytics/me', params);
}

export async function fetchAnalyticsFromGateway(
  accessToken: string,
  params: AnalyticsParams = {},
): Promise<AnalyticsResult> {
  return fetchAnalytics(accessToken, '/analytics', params);
}

// Admin drill-down: same response shape as /analytics/me, for a caller-chosen
// principal rather than the caller's own identity. Requires
// platform:analytics:read — enforced by the inference gateway, not here.
export async function fetchAnalyticsPrincipalByIdFromGateway(
  accessToken: string,
  principalId: string,
  params: AnalyticsParams = {},
): Promise<AnalyticsResult> {
  return fetchAnalytics(
    accessToken,
    `/analytics/principals/${encodeURIComponent(principalId)}`,
    params,
  );
}

// ── Principals ────────────────────────────────────────────────────────────────

export async function fetchAnalyticsPrincipalsFromGateway(
  accessToken: string,
  params: { days?: number; page?: number; limit?: number; sort_by?: string; order?: string } = {},
): Promise<AnalyticsPrincipalsRawResult> {
  const qs = new URLSearchParams();
  if (params.days !== undefined) qs.set('days', String(params.days));
  if (params.page !== undefined) qs.set('page', String(params.page));
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.sort_by !== undefined) qs.set('sort_by', params.sort_by);
  if (params.order !== undefined) qs.set('order', params.order);

  const path = '/analytics/principals';
  const url = `${config.inferenceGatewayUrl}${path}${qs.size > 0 ? `?${qs.toString()}` : ''}`;

  let res: Response;
  try {
    res = await fetch(url, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(15_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchAnalyticsPrincipals] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Inference gateway timed out' : 'Failed to reach inference gateway',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchAnalyticsPrincipals] responded ${res.status} ${res.statusText}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  try {
    const data = (await res.json()) as RawPrincipalsResponse;
    return { ok: true, data };
  } catch {
    return { ok: false, status: 502, message: 'Invalid response from inference gateway' };
  }
}
