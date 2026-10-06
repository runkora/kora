import 'server-only';

import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { AgentOverviewResponse, AgentRunStats } from '@orbit/shared/api';
import { fetchGatewayInsights } from '../../_services/agents-insights-server';
import type { Period } from '../../_lib/spend';

type RunStatsResult = { stats: AgentRunStats } | { refused: true } | { failed: true };

async function fetchRunStats(
  accessToken: string,
  slug: string,
  period: Period,
): Promise<RunStatsResult> {
  try {
    const stats = await agentsClient(accessToken).get<AgentRunStats>(
      `/api/v1/run-stats/${encodeURIComponent(slug)}?period=${period}`,
    );
    return stats ? { stats } : { failed: true };
  } catch (err) {
    // A caller without `agents:runs:{slug}:read` gets a 404, so neither status is a failure.
    if (err instanceof ApiError && (err.status === 403 || err.status === 404)) {
      return { refused: true };
    }
    console.error('[loadAgentOverview] run stats request failed', err);
    return { failed: true };
  }
}

/**
 * One agent's gateway insights and run stats, each read under the caller's token.
 * A refused part (403 or 404) is left out; a part that errors is left out and named in `failed`.
 */
export async function loadAgentOverview(
  accessToken: string,
  slug: string,
  period: Period,
): Promise<AgentOverviewResponse> {
  const [insights, runStats] = await Promise.all([
    fetchGatewayInsights(accessToken, period, slug),
    fetchRunStats(accessToken, slug, period),
  ]);
  const failed: NonNullable<AgentOverviewResponse['failed']> = [];
  if (!insights.ok && insights.status !== 403) failed.push('insights');
  if ('failed' in runStats) failed.push('runStats');
  return {
    period,
    ...(insights.ok ? { insights: insights.data } : {}),
    ...('stats' in runStats ? { runStats: runStats.stats } : {}),
    ...(failed.length > 0 ? { failed } : {}),
  };
}
