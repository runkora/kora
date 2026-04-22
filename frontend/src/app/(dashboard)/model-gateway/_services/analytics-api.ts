// ── Analytics API service ─────────────────────────────────────────────────────
//
// Client-side fetch helpers. Call the BFF route handlers at /api/analytics/me
// and /api/analytics — never the Go backend directly.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AnalyticsResponse } from '../_types';

export interface FetchAnalyticsResult {
  data: AnalyticsResponse | null;
  error: string | null;
}

const emptyResult: FetchAnalyticsResult = { data: null, error: null };

async function fetchFromBFF(url: string): Promise<FetchAnalyticsResult> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
  } catch {
    return { ...emptyResult, error: 'Network error — could not reach the server.' };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // ignore parse errors; keep the status-code message
    }
    return { ...emptyResult, error: message };
  }

  try {
    const data = (await res.json()) as AnalyticsResponse;
    return { data, error: null };
  } catch {
    return { ...emptyResult, error: 'Invalid response from server.' };
  }
}

export async function fetchAnalyticsMe(params?: {
  days?: number;
  granularity?: string;
  tokenId?: string;
}): Promise<FetchAnalyticsResult> {
  return fetchFromBFF(ApiRoute.analyticsMe.GET(params));
}

export async function fetchAnalytics(params?: {
  days?: number;
  granularity?: string;
  modelId?: string;
}): Promise<FetchAnalyticsResult> {
  return fetchFromBFF(ApiRoute.analytics.GET(params));
}
