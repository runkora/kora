import type { AgentTypeUsage } from '@orbit/shared/api';
import type { AgentsInsights, BudgetRow, Period, PeriodData, TenantBudget } from '../_lib/spend';
import { RUN_ROWS, USAGE } from './agents-mock-data';

// Mock data for the parts of the Agents page the platform does not serve yet:
// per-period usage, a model split, spend over time, and budgets. The figures are
// the 7-day mock usage scaled to each window.

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
      completed: Math.round(completed * factor),
      failed: Math.round(failed * factor),
      models: MODEL_SHARES.map((m) => ({ name: m.name, cost: m.share * total })),
      trend: { labels, values: weights.map((w) => w * total) },
      previousTotal: total / 1.18,
    };
  };

  return { '24h': build('24h'), '7d': build('7d'), '30d': build('30d') };
}

export const BUDGETS: BudgetRow[] = [
  { name: 'Code Reviewer', monthToDate: 88.4, limit: 150 },
  { name: 'Coder', monthToDate: 258.2, limit: 300 },
  { name: 'Security Reviewer', monthToDate: 49, limit: 100 },
  { name: 'Release Notes', monthToDate: 4.9 },
  { name: 'Triage Bot', monthToDate: 12, limit: 25 },
];

export const TENANT_BUDGET: TenantBudget = { limit: 1000, monthToDate: 412.5, daysLeft: 19 };

/** Every optional view switched on. */
export const INSIGHTS: AgentsInsights = {
  byPeriod: periodData(USAGE),
  budgets: BUDGETS,
  tenantBudget: TENANT_BUDGET,
};
