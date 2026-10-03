import 'server-only';

import { config } from '@orbit/server/config';
import { agentsClient } from '@orbit/server/agents-client';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

// The Agents page rollups. The inference gateway counts usage and budgets, and
// orbit-agents counts runs, each under the caller's own token. The BFF only
// composes what each returns.

export type InsightsResult =
  { ok: true; data: AgentInsightsResponse } | { ok: false; status: number };

async function fetchGatewayInsights(accessToken: string, period: Period): Promise<InsightsResult> {
  try {
    const res = await fetch(`${config.inferenceGatewayUrl}/usage/agent-insights?period=${period}`, {
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

/**
 * Usage, model split, spend over time and budgets from the gateway, and run
 * outcomes from orbit-agents, all at once. Outcomes are optional: when they
 * cannot be counted the response has none and the page leaves them out.
 */
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
