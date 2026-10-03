// ── Agents insights API — client-side BFF call ───────────────────────────────
//
// Same no-throw contract as agents-api.ts: callers read the `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

/** Usage, model split, spend over time, budgets and run outcomes for one window. */
export async function fetchAgentInsights(
  period: Period,
): Promise<{ data: AgentInsightsResponse | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentInsights.window(period), { cache: 'no-store' });
  } catch {
    return { data: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  try {
    return { data: (await res.json()) as AgentInsightsResponse, error: null };
  } catch {
    return { data: null, error: 'Invalid response from the server.' };
  }
}
