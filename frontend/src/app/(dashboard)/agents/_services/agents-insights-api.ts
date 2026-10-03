// ── Agents insights API — client-side BFF call ───────────────────────────────

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

/** The rollups for one window, or null when they cannot be had. Never throws. */
export async function fetchAgentInsights(period: Period): Promise<AgentInsightsResponse | null> {
  try {
    const res = await fetch(ApiRoute.agentInsights.window(period), { cache: 'no-store' });
    return res.ok ? ((await res.json()) as AgentInsightsResponse) : null;
  } catch {
    return null;
  }
}
