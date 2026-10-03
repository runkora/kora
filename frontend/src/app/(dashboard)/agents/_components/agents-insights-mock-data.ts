import type {
  AgentBudgetsResponse,
  AgentInsightsResponse,
  AgentTypeUsage,
} from '@orbit/shared/api';
import type { AgentsInsights, BudgetRow, Period, PeriodData, TenantBudget } from '../_lib/spend';
import { RUN_ROWS, USAGE } from './agents-mock-data';

// Mock data for the parts of the Agents page the platform does not serve yet:
// per-period usage, a model split, spend over time, and budgets in a story. The
// figures are the 7-day mock usage scaled to each window, and each window's
// points match the buckets the gateway cuts: 3-hourly, daily, 3-daily, weekly.

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
      'Jul 13',
      'Jul 20',
      'Jul 27',
      'Aug 3',
      'Aug 10',
      'Aug 17',
      'Aug 24',
      'Aug 31',
      'Sep 7',
      'Sep 14',
      'Sep 21',
      'Sep 28',
    ],
    weights: [0.05, 0.06, 0.07, 0.07, 0.08, 0.08, 0.08, 0.09, 0.09, 0.09, 0.09, 0.08, 0.07],
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

/** Build the per-period figures for a set of usage rows. */
export function periodData(usage: AgentTypeUsage[]): Record<Period, PeriodData> {
  const completed = RUN_ROWS.filter((r) => r.state === 'completed').length;
  const failed = RUN_ROWS.filter((r) => r.state === 'failed').length;

  const build = (period: Period): PeriodData => {
    const { factor, labels, weights } = SHAPES[period];
    const scaled = scaleUsage(usage, factor);
    const total = Object.values(scaled).reduce((n, u) => n + u.estimated_cost_usd, 0);
    return {
      usage: scaled,
      outcomes: { completed: Math.round(completed * factor), failed: Math.round(failed * factor) },
      models: MODEL_SHARES.map((m) => ({ name: m.name, cost: m.share * total })),
      trend: { labels, values: weights.map((w) => w * total) },
      previousTotal: total / 1.18,
    };
  };

  return { '24h': build('24h'), '7d': build('7d'), '30d': build('30d'), '90d': build('90d') };
}

export const BUDGETS: BudgetRow[] = [
  { name: 'Code Reviewer', spent: 88.4, limit: 150 },
  { name: 'Coder', spent: 258.2, limit: 300 },
  { name: 'Security Reviewer', spent: 49, limit: 100 },
  { name: 'Release Notes', spent: 4.9 },
  { name: 'Triage Bot', spent: 12, limit: 25, cadence: 'weekly' },
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
  '90d': 7 * 86400,
};

/** The BFF response behind `INSIGHTS`, for stories that load the page from mocked routes. */
export function mockInsightsResponse(period: Period): AgentInsightsResponse {
  const data = INSIGHTS.byPeriod?.[period];
  if (!data?.trend) throw new Error(`no mock insights for ${period}`);
  const bucket = BUCKET_SECONDS[period] * 1000;
  const start = Date.UTC(2026, 9, 4) - bucket * data.trend.values.length;
  const usage = Object.values(data.usage);
  return {
    period,
    start: new Date(start).toISOString(),
    end: new Date(start + bucket * data.trend.values.length).toISOString(),
    bucket_seconds: BUCKET_SECONDS[period],
    usage,
    total_cost_usd: usage.reduce((n, u) => n + u.estimated_cost_usd, 0),
    previous_total_usd: data.previousTotal ?? 0,
    models: (data.models ?? []).map((m) => ({ model_id: m.name, cost_usd: m.cost })),
    trend: data.trend.values.map((cost_usd, i) => ({
      start: new Date(start + i * bucket).toISOString(),
      cost_usd,
    })),
    ...(data.outcomes ? { outcomes: data.outcomes } : {}),
  };
}

/** The BFF budgets response behind `BUDGETS` and `TENANT_BUDGET`. */
export const MOCK_BUDGETS_RESPONSE: AgentBudgetsResponse = {
  agents: BUDGETS.map((b) => ({
    agent_slug: b.name.toLowerCase().replace(/\s+/g, '-'),
    week_to_date_usd: b.cadence === 'weekly' ? b.spent : b.spent / 4,
    month_to_date_usd: b.spent,
    ...(b.limit ? { limit_usd: b.limit, reset_cadence: b.cadence ?? 'monthly' } : {}),
  })),
  tenant_cap: {
    limit_usd: TENANT_BUDGET.limit,
    reset_cadence: 'monthly',
    spend_usd: TENANT_BUDGET.spent,
  },
};
