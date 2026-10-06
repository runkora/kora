import 'server-only';

import { config } from '@orbit/server/config';
import { agentsClient } from '@orbit/server/agents-client';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

// The BFF composes gateway usage and budgets with orbit-agents run outcomes, each under the caller's token.

export type InsightsResult =
  { ok: true; data: AgentInsightsResponse } | { ok: false; status: number };

/** The tenant's agent insights, or the insights of one agent when `slug` is set. */
export async function fetchGatewayInsights(
  accessToken: string,
  period: Period,
  slug?: string,
): Promise<InsightsResult> {
  const path = slug ? `agent-type/${encodeURIComponent(slug)}` : 'agent-insights';
  try {
    const res = await fetch(`${config.inferenceGatewayUrl}/usage/${path}?period=${period}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
    if (!res.ok) return { ok: false, status: res.status };
    return { ok: true, data: (await res.json()) as AgentInsightsResponse };
  } catch (err) {
    console.error('[loadInsights] inference gateway request failed', err);
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    return { ok: false, status: isTimeout ? 504 : 502 };
  }
}

/** Gateway rollups plus run outcomes, fetched together. Outcomes are optional. */
export async function loadInsights(accessToken: string, period: Period): Promise<InsightsResult> {
  const [usage, outcomes] = await Promise.all([
    fetchGatewayInsights(accessToken, period),
    agentsClient(accessToken)
      .get<{ completed: number; failed: number }>(`/api/v1/run-outcomes?period=${period}`)
      .catch((err: unknown) => {
        console.error('[loadInsights] run outcomes request failed', err);
        return undefined;
      }),
  ]);
  if (!usage.ok) return usage;
  return { ok: true, data: { ...usage.data, ...(outcomes ? { outcomes } : {}) } };
}
