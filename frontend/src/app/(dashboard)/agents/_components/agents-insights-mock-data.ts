import type { AgentBudgetStatus, AgentInsightsResponse, AgentTypeUsage } from '@orbit/shared/api';
import type { RunRow } from '../_types';
import type { AgentsInsights, BudgetRow, Period, PeriodData, TenantBudget } from '../_lib/spend';
import { RUN_ROWS, SCALE_AGENT_ROWS, SCALE_RUN_ROWS, SCALE_USAGE, USAGE } from './agents-mock-data';

// Mock data for the parts of the Agents page the platform does not serve yet:
// per-period usage, a model split, spend over time, and budgets in a story. The
// figures are the 7-day mock usage scaled to each window, and each window's
// points match the buckets the gateway cuts: 3-hourly, daily, 3-daily, 6-daily.

interface PeriodShape {
  /** Multiplier applied to the 7-day mock figures. */
  factor: number;
  labels: string[];
  /** Share of the window's spend in each point, summing to 1. */
  weights: number[];
}

const SHAPES: Record<Period, PeriodShape> = {
  '24h': {
    factor: 0.16,
    labels: ['00:00', '03:00', '06:00', '09:00', '12:00', '15:00', '18:00', '21:00'],
    weights: [0.04, 0.03, 0.05, 0.14, 0.22, 0.2, 0.2, 0.12],
  },
  '7d': {
    factor: 1,
    labels: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'],
    weights: [0.09, 0.12, 0.15, 0.18, 0.14, 0.17, 0.15],
  },
  '30d': {
    factor: 4.1,
    labels: [
      'Sep 3',
      'Sep 6',
      'Sep 9',
      'Sep 12',
      'Sep 15',
      'Sep 18',
      'Sep 21',
      'Sep 24',
      'Sep 27',
      'Sep 30',
    ],
    weights: [0.07, 0.09, 0.1, 0.12, 0.09, 0.11, 0.1, 0.11, 0.1, 0.11],
  },
  '90d': {
    factor: 12.4,
    labels: [
      'Jul 6',
      'Jul 12',
      'Jul 18',
      'Jul 24',
      'Jul 30',
      'Aug 5',
      'Aug 11',
      'Aug 17',
      'Aug 23',
      'Aug 29',
      'Sep 4',
      'Sep 10',
      'Sep 16',
      'Sep 22',
      'Sep 28',
    ],
    weights: [
      0.04, 0.05, 0.05, 0.06, 0.06, 0.07, 0.07, 0.07, 0.08, 0.08, 0.08, 0.08, 0.07, 0.07, 0.07,
    ],
  },
};

const MODEL_SHARES = [
  { name: 'claude-sonnet-4-5', share: 0.555 },
  { name: 'claude-opus-4-1', share: 0.298 },
  { name: 'claude-haiku-4-5', share: 0.103 },
  { name: 'gpt-5', share: 0.044 },
];

function scaleUsage(usage: AgentTypeUsage[], factor: number): Record<string, AgentTypeUsage> {
  return Object.fromEntries(
    usage.map((u) => [
      u.agent_slug,
      {
        ...u,
        prompt_tokens: Math.round(u.prompt_tokens * factor),
        completion_tokens: Math.round(u.completion_tokens * factor),
        cache_read_tokens: Math.round(u.cache_read_tokens * factor),
        cache_creation_tokens: Math.round(u.cache_creation_tokens * factor),
        total_tokens: Math.round(u.total_tokens * factor),
        estimated_cost_usd: u.estimated_cost_usd * factor,
      },
    ]),
  );
}

/** Spend of an agent that was deleted since, which is in every total and listed as "Deleted agents". */
const RETIRED_USAGE: AgentTypeUsage = {
  agent_slug: 'retired-agent',
  prompt_tokens: 120_000,
  completion_tokens: 30_000,
  cache_read_tokens: 200_000,
  cache_creation_tokens: 20_000,
  total_tokens: 370_000,
  estimated_cost_usd: 6.5,
};

/** Build the per-period figures for a set of usage rows. */
export function periodData(
  usage: AgentTypeUsage[],
  runs: RunRow[] = RUN_ROWS,
): Record<Period, PeriodData> {
  const completed = runs.filter((r) => r.state === 'completed').length;
  const failed = runs.filter((r) => r.state === 'failed').length;

  const build = (period: Period): PeriodData => {
    const { factor, labels, weights } = SHAPES[period];
    // The window also holds spend of an agent that has since been deleted.
    const scaled = scaleUsage([...usage, RETIRED_USAGE], factor);
    const total = Object.values(scaled).reduce((n, u) => n + u.estimated_cost_usd, 0);
    return {
      usage: scaled,
      total,
      delegatedOwnOnly: false,
      outcomes: { completed: Math.round(completed * factor), failed: Math.round(failed * factor) },
      models: MODEL_SHARES.map((m) => ({ name: m.name, cost: m.share * total })),
      trend: { labels, values: weights.map((w) => w * total) },
      previousTotal: total / 1.18,
    };
  };

  return { '24h': build('24h'), '7d': build('7d'), '30d': build('30d'), '90d': build('90d') };
}

const monthly = (limit: number, spent: number) => ({
  cadence: 'monthly' as const,
  limit,
  spent,
  resetsAt: '2026-11-01T00:00:00Z',
});
const weekly = (limit: number, spent: number) => ({
  cadence: 'weekly' as const,
  limit,
  spent,
  resetsAt: '2026-10-05T00:00:00Z',
});

export const BUDGETS: BudgetRow[] = [
  { name: 'Code Reviewer', windows: [monthly(150, 88.4)] },
  { name: 'Coder', windows: [monthly(300, 258.2)], perRunLimit: 12 },
  { name: 'Security Reviewer', windows: [monthly(100, 49)] },
  { name: 'Triage Bot', windows: [weekly(25, 12), monthly(80, 30)] },
  // A limit and no spend: the bar is empty, not missing.
  { name: 'Incident Scribe', windows: [monthly(40, 0)] },
];

export const TENANT_BUDGET: TenantBudget = { limit: 1000, spent: 412.5, daysLeft: 19 };

/** Every optional view switched on. */
export const INSIGHTS: AgentsInsights = {
  byPeriod: periodData(USAGE),
  budgets: BUDGETS,
  tenantBudget: TENANT_BUDGET,
};

const BUCKET_SECONDS: Record<Period, number> = {
  '24h': 3 * 3600,
  '7d': 86400,
  '30d': 3 * 86400,
  '90d': 6 * 86400,
};

const slugOf = (name: string): string => name.toLowerCase().replace(/\s+/g, '-');

const toStatus = (b: BudgetRow): AgentBudgetStatus => ({
  agent_slug: slugOf(b.name),
  ...(b.perRunLimit ? { per_run_limit_usd: b.perRunLimit } : {}),
  windows: b.windows.map((w) => ({
    cadence: w.cadence,
    limit_usd: w.limit,
    spend_usd: w.spent,
    resets_at: w.resetsAt,
  })),
});

/** The BFF response behind `INSIGHTS`, for stories that load the page from mocked routes. */
export function mockInsightsResponse(
  period: Period,
  source: AgentsInsights = INSIGHTS,
): AgentInsightsResponse {
  const data = source.byPeriod?.[period];
  if (!data?.trend) throw new Error(`no mock insights for ${period}`);
  const bucket = BUCKET_SECONDS[period] * 1000;
  const start = Date.UTC(2026, 9, 4) - bucket * data.trend.values.length;
  return {
    period,
    start: new Date(start).toISOString(),
    end: new Date(start + bucket * data.trend.values.length).toISOString(),
    bucket_seconds: BUCKET_SECONDS[period],
    usage: Object.values(data.usage),
    total_cost_usd: data.total,
    previous_total_usd: data.previousTotal ?? 0,
    models: (data.models ?? []).map((m) => ({ model_id: m.name, cost_usd: m.cost })),
    trend: data.trend.values.map((cost_usd, i) => ({
      start: new Date(start + i * bucket).toISOString(),
      cost_usd,
    })),
    all_delegated_usage: !data.delegatedOwnOnly,
    budgets: (source.budgets ?? []).map(toStatus),
    ...(data.outcomes ? { outcomes: data.outcomes } : {}),
    ...(source.tenantBudget
      ? {
          tenant_cap: {
            limit_usd: source.tenantBudget.limit,
            reset_cadence: 'monthly',
            spend_usd: source.tenantBudget.spent,
          },
        }
      : {}),
  };
}

/** The insights of the 150-agent workspace: spend across every agent and a budget on most. */
export const SCALE_INSIGHTS: AgentsInsights = {
  byPeriod: periodData(SCALE_USAGE, SCALE_RUN_ROWS),
  budgets: SCALE_AGENT_ROWS.flatMap((a, i): BudgetRow[] => {
    // Every third agent has no limit and so no row.
    if (i % 3 === 0) return [];
    const spent = SCALE_USAGE[i]?.estimated_cost_usd ?? 0;
    return [
      { name: a.name, windows: [monthly(Math.max(spent * (1 + ((i * 37) % 100) / 60), 1), spent)] },
    ];
  }),
  tenantBudget: TENANT_BUDGET,
};
