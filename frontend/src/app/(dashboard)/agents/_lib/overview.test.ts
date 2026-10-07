import { describe, expect, it } from 'vitest';
import type { AgentOverviewResponse, AgentRunStats } from '@orbit/shared/api';
import { toAgentOverview } from './overview';

const RUN_STATS: AgentRunStats = {
  total: 10,
  triggered: 4,
  completed: 6,
  failed: 2,
  durations: {
    median_ms: 90_000,
    p95_ms: 200_000,
    p99_ms: 210_000,
    longest_ms: 220_000,
    capped: false,
    buckets: [1, 0, 0, 2, 0, 0, 3, 1, 0, 1],
    overflow: 0,
  },
  current: {
    total: 12,
    by_state: { completed: 8, failed: 2, active: 2 },
    live: 2,
    live_by_agent_type: { 'at-1': 2 },
    longest_live_ms: 60_000,
  },
};

const RESPONSE: AgentOverviewResponse = {
  period: '7d',
  runStats: RUN_STATS,
  insights: {
    period: '7d',
    usage: [
      {
        agent_slug: 'coder',
        prompt_tokens: 100,
        completion_tokens: 50,
        cache_read_tokens: 200,
        cache_creation_tokens: 100,
        total_tokens: 450,
        estimated_cost_usd: 6,
      },
    ],
    total_cost_usd: 6,
    previous_total_usd: 4,
    models: [{ model_id: 'claude-sonnet-4-5', estimated_cost_usd: 6 }],
    trend: [{ start: '2026-10-01T00:00:00Z', cost_usd: 6 }],
    budgets: [
      {
        agent_slug: 'coder',
        per_run_limit_usd: 2,
        windows: [
          { cadence: 'monthly', limit_usd: 100, spend_usd: 40, resets_at: '2026-11-01T00:00:00Z' },
        ],
      },
    ],
  },
};

describe('toAgentOverview', () => {
  it('derives the figures that are not on the wire', () => {
    const { usage, runs, budget } = toAgentOverview(RESPONSE);
    expect(usage).toMatchObject({ cachedShare: 75, spend: 6, change: 50 });
    expect(usage?.tokens).toEqual({ total: 450, cached: 300, input: 100, output: 50 });
    expect(runs).toMatchObject({ manual: 6, finished: 8, successRate: 75 });
    expect(budget?.windows[0]).toMatchObject({ limit: 100, spent: 40 });
    expect(budget?.perRunLimit).toBe(2);
  });

  it('reads an agent with no usage as zero, with no change and no cached share', () => {
    const { usage } = toAgentOverview({
      ...RESPONSE,
      insights: { ...RESPONSE.insights!, usage: [], previous_total_usd: 0, models: [] },
    });
    expect(usage).toMatchObject({ spend: 0, change: null, cachedShare: null });
  });

  it('has no success rate before any run has finished, and no budget without a row', () => {
    const { runs, budget } = toAgentOverview({
      period: '7d',
      runStats: { ...RUN_STATS, completed: 0, failed: 0 },
    });
    expect(runs?.successRate).toBeNull();
    expect(budget).toBeNull();
  });

  it('leaves out the part the response does not carry and carries the parts that errored', () => {
    expect(toAgentOverview({ period: '7d', runStats: RUN_STATS }).usage).toBeUndefined();
    const overview = toAgentOverview({
      period: '7d',
      insights: RESPONSE.insights!,
      failed: ['runStats'],
    });
    expect(overview.runs).toBeUndefined();
    expect(overview.failed).toEqual({ usage: false, runs: true });
  });
});
