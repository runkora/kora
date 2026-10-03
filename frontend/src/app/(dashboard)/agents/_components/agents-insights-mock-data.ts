import type { AgentBudgetStatus, AgentInsightsResponse, AgentTypeUsage } from '@orbit/shared/api';
import type { RunRow } from '../_types';
import { toBudgetRows, toPeriodData, toTenantBudget } from '../_lib/insights';
import type { AgentsInsights, Period } from '../_lib/spend';
import {
  AGENT_ROWS,
  RUN_ROWS,
  SCALE_AGENT_ROWS,
  SCALE_RUN_ROWS,
  SCALE_USAGE,
  USAGE,
} from './agents-mock-data';

// Mock insights for the Agents page stories. The figures are the 7-day mock usage
// scaled to each window, shaped as the BFF response and read through the page's
// own mapping, so a story exercises the same code the live page does.

/** Scale applied to the 7-day figures, and the bucket width and count each window charts. */
const SHAPES: Record<Period, { factor: number; bucketHours: number; buckets: number }> = {
  '24h': { factor: 0.16, bucketHours: 3, buckets: 8 },
  '7d': { factor: 1, bucketHours: 24, buckets: 7 },
  '30d': { factor: 4.1, bucketHours: 72, buckets: 10 },
  '90d': { factor: 12.4, bucketHours: 144, buckets: 15 },
};

const MODEL_SHARES: [string, number][] = [
  ['claude-sonnet-4-5', 0.555],
  ['claude-opus-4-1', 0.298],
  ['claude-haiku-4-5', 0.103],
  ['gpt-5', 0.044],
];

/** Spend of an agent that was deleted since: it is in every total and listed as "Deleted agents". */
const RETIRED_USAGE: AgentTypeUsage = {
  agent_slug: 'retired-agent',
  prompt_tokens: 120_000,
  completion_tokens: 30_000,
  cache_read_tokens: 200_000,
  cache_creation_tokens: 20_000,
  total_tokens: 370_000,
  estimated_cost_usd: 6.5,
};

const window = (cadence: 'weekly' | 'monthly', limit_usd: number, spend_usd: number) => ({
  cadence,
  limit_usd,
  spend_usd,
  resets_at: cadence === 'weekly' ? '2026-10-05T00:00:00Z' : '2026-11-01T00:00:00Z',
});

export const BUDGETS: AgentBudgetStatus[] = [
  { agent_slug: 'code-reviewer', windows: [window('monthly', 150, 88.4)] },
  { agent_slug: 'coder', per_run_limit_usd: 12, windows: [window('monthly', 300, 258.2)] },
  { agent_slug: 'security-reviewer', windows: [window('monthly', 100, 49)] },
  { agent_slug: 'triage-bot', windows: [window('weekly', 25, 12), window('monthly', 80, 30)] },
  // A limit and no spend: the bar is empty, not missing.
  { agent_slug: 'incident-scribe', windows: [window('monthly', 40, 0)] },
];

const TENANT_CAP = {
  limit_usd: 1000,
  reset_cadence: 'monthly',
  spend_usd: 412.5,
  resets_at: '2026-11-01T00:00:00Z',
};

/** The BFF response for one window of a workspace's usage. */
export function mockInsightsResponse(
  period: Period,
  usage: AgentTypeUsage[] = USAGE,
  runs: RunRow[] = RUN_ROWS,
  budgets: AgentBudgetStatus[] = BUDGETS,
): AgentInsightsResponse {
  const { factor, bucketHours, buckets } = SHAPES[period];
  const scaled = [...usage, RETIRED_USAGE].map((u) => ({
    ...u,
    prompt_tokens: Math.round(u.prompt_tokens * factor),
    completion_tokens: Math.round(u.completion_tokens * factor),
    cache_read_tokens: Math.round(u.cache_read_tokens * factor),
    cache_creation_tokens: Math.round(u.cache_creation_tokens * factor),
    total_tokens: Math.round(u.total_tokens * factor),
    estimated_cost_usd: u.estimated_cost_usd * factor,
  }));
  const total = scaled.reduce((n, u) => n + u.estimated_cost_usd, 0);
  // A gentle wave, normalised so the points add up to the window's spend.
  const wave = Array.from({ length: buckets }, (_, i) => 1 + 0.4 * Math.sin(i * 0.9));
  const waveSum = wave.reduce((n, w) => n + w, 0);
  const start = Date.UTC(2026, 9, 4) - buckets * bucketHours * 3_600_000;
  const done = (state: string) => runs.filter((r) => r.state === state).length;
  return {
    period,
    usage: scaled,
    total_cost_usd: total,
    previous_total_usd: total / 1.18,
    models: MODEL_SHARES.map(([model_id, share]) => ({ model_id, cost_usd: share * total })),
    trend: wave.map((w, i) => ({
      start: new Date(start + i * bucketHours * 3_600_000).toISOString(),
      cost_usd: (w / waveSum) * total,
    })),
    all_delegated_usage: true,
    budgets,
    outcomes: {
      completed: Math.round(done('completed') * factor),
      failed: Math.round(done('failed') * factor),
    },
    tenant_cap: TENANT_CAP,
  };
}

const PERIODS = Object.keys(SHAPES) as Period[];

/** The page's optional parts, built from the responses of every window. */
function insightsOf(
  agents: typeof AGENT_ROWS,
  respond: (period: Period) => AgentInsightsResponse,
): AgentsInsights {
  const first = respond('7d');
  return {
    byPeriod: Object.fromEntries(PERIODS.map((p) => [p, toPeriodData(p, respond(p))])),
    budgets: toBudgetRows(first.budgets ?? [], agents),
    tenantBudget: toTenantBudget(first.tenant_cap, new Date('2026-10-03T14:00:00Z')),
  };
}

/** Every optional view switched on. */
export const INSIGHTS = insightsOf(AGENT_ROWS, (p) => mockInsightsResponse(p));

/** The 150-agent workspace: spend across every agent and a budget on two in three. */
export const SCALE_INSIGHTS = insightsOf(SCALE_AGENT_ROWS, (p) =>
  mockInsightsResponse(
    p,
    SCALE_USAGE,
    SCALE_RUN_ROWS,
    SCALE_AGENT_ROWS.flatMap((a, i): AgentBudgetStatus[] => {
      const spend = SCALE_USAGE[i]?.estimated_cost_usd ?? 0;
      return i % 3 === 0
        ? []
        : [
            {
              agent_slug: a.slug,
              windows: [window('monthly', Math.max(spend * (1 + ((i * 37) % 100) / 60), 1), spend)],
            },
          ];
    }),
  ),
);
