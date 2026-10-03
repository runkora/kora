// ── Agents insights API — client-side BFF calls ──────────────────────────────
//
// Same no-throw contract as agents-api.ts: callers read the `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentBudgetsResponse, AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

async function getJson<T>(url: string): Promise<{ data: T | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(url, { cache: 'no-store' });
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
    return { data: (await res.json()) as T, error: null };
  } catch {
    return { data: null, error: 'Invalid response from the server.' };
  }
}

/** Usage, model split, spend over time and run outcomes for one window. */
export function fetchAgentInsights(
  period: Period,
): Promise<{ data: AgentInsightsResponse | null; error: string | null }> {
  return getJson<AgentInsightsResponse>(ApiRoute.agentInsights.window(period));
}

/** Per-agent budget status and, for callers who may read it, the tenant cap. */
export function fetchAgentBudgets(): Promise<{
  data: AgentBudgetsResponse | null;
  error: string | null;
}> {
  return getJson<AgentBudgetsResponse>(ApiRoute.agentInsights.budgets());
}
