import type { AgentTypeUsage } from '@orbit/shared/api';

/** The windows the Agents page can report spend and run outcomes over. */
export type Period = '24h' | '7d' | '30d';

/** Phrases for footers and descriptions, so every card names its window the same way. */
export const PERIOD_TEXT: Record<Period, { text: string; previous: string }> = {
  '24h': { text: 'last 24 hours', previous: 'previous 24 hours' },
  '7d': { text: 'last 7 days', previous: 'previous 7 days' },
  '30d': { text: 'last 30 days', previous: 'previous 30 days' },
};

export const PERIOD_OPTIONS = (Object.keys(PERIOD_TEXT) as Period[]).map((value) => ({
  value,
  label: value,
}));

export interface SpendRow {
  name: string;
  cost: number;
}

/** An agent's month-to-date spend against its own monthly limit, when it has one. */
export interface BudgetRow {
  name: string;
  monthToDate: number;
  limit?: number;
}

export interface TenantBudget {
  limit: number;
  monthToDate: number;
  daysLeft: number;
}

/** Everything the page reports for one window. */
export interface PeriodData {
  usage: Record<string, AgentTypeUsage>;
  completed: number;
  failed: number;
  /** Spend split by model, when the source provides it. */
  models?: SpendRow[];
  /** Spend over time, oldest first, with one axis label per point. */
  trend?: { labels: string[]; values: number[] };
  /** Total spend for the window before this one. */
  previousTotal?: number;
}

/**
 * Optional data the page cannot fetch yet. Each part switches a view on: with
 * none of it the page shows the 7-day agent split and nothing else, so no card
 * ever reports a number the platform has not produced.
 */
export interface AgentsInsights {
  /** Presence adds the period control and scopes the period figures to it. */
  byPeriod?: Record<Period, PeriodData>;
  budgets?: BudgetRow[];
  tenantBudget?: TenantBudget;
}

/** Healthy under 80% of a limit, degraded from 80%, error at or over it. */
export function budgetTone(used: number): 'healthy' | 'degraded' | 'error' {
  return used >= 100 ? 'error' : used >= 80 ? 'degraded' : 'healthy';
}
