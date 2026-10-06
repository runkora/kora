import 'server-only';

import { agentsClient } from '@orbit/server/agents-client';
import type { AgentOverviewResponse, AgentRunStats } from '@orbit/shared/api';
import { fetchGatewayInsights } from '../../_services/agents-insights-server';
import type { Period } from '../../_lib/spend';

/**
 * One agent's gateway insights and run stats, each read under the caller's token.
 * A part that fails or is not permitted is left out.
 */
export async function loadAgentOverview(
  accessToken: string,
  slug: string,
  period: Period,
): Promise<AgentOverviewResponse> {
  const [insights, runStats] = await Promise.all([
    fetchGatewayInsights(accessToken, period, slug),
    agentsClient(accessToken)
      .get<AgentRunStats>(`/api/v1/run-stats/${encodeURIComponent(slug)}?period=${period}`)
      .catch((err: unknown) => {
        console.error('[loadAgentOverview] run stats request failed', err);
        return undefined;
      }),
  ]);
  return {
    period,
    ...(insights.ok ? { insights: insights.data } : {}),
    ...(runStats ? { runStats } : {}),
  };
}
