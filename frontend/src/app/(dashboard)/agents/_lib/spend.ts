import type { AgentTypeUsage } from '@orbit/shared/api';

/** The windows the Agents page can report spend and run outcomes over. */
export type Period = '24h' | '7d' | '30d' | '90d';

/** Phrases for footers and descriptions, so every card names its window the same way. */
export const PERIOD_TEXT: Record<Period, { text: string; previous: string }> = {
  '24h': { text: 'last 24 hours', previous: 'previous 24 hours' },
  '7d': { text: 'last 7 days', previous: 'previous 7 days' },
  '30d': { text: 'last 30 days', previous: 'previous 30 days' },
  '90d': { text: 'last 90 days', previous: 'previous 90 days' },
};

export const PERIOD_OPTIONS = (Object.keys(PERIOD_TEXT) as Period[]).map((value) => ({
  value,
  label: value,
}));

export interface SpendRow {
  name: string;
  cost: number;
}

/**
 * An agent's spend in its budget window against its own limit, when it has one.
 * The window is the calendar month unless `cadence` says the budget resets weekly.
 */
export interface BudgetRow {
  name: string;
  spent: number;
  limit?: number;
  cadence?: 'weekly';
}

/** The tenant-wide cap. Its window is the calendar month unless `cadence` says weekly. */
export interface TenantBudget {
  limit: number;
  spent: number;
  daysLeft: number;
  cadence?: 'weekly';
}

/** Everything the page reports for one window. */
export interface PeriodData {
  usage: Record<string, AgentTypeUsage>;
  /** Runs that finished in the window. Absent when the run counts are not available. */
  outcomes?: { completed: number; failed: number };
  /** Spend split by model, when the source provides it. */
  models?: SpendRow[];
  /** Spend over time, oldest first, with one axis label per point. */
  trend?: { labels: string[]; values: number[] };
  /** Total spend for the window before this one. */
  previousTotal?: number;
}

/**
 * The optional parts of the page. Each part switches a view on, and a part that
 * could not be loaded or is not permitted is simply absent, so no card ever
 * reports a number the platform has not produced.
 */
export interface AgentsInsights {
  /**
   * Presence adds the period control and scopes the period figures to it. A
   * period that is not in the map has not been loaded.
   */
  byPeriod?: Partial<Record<Period, PeriodData>>;
  budgets?: BudgetRow[];
  tenantBudget?: TenantBudget;
}

/** Healthy under 80% of a limit, degraded from 80%, error at or over it. */
export function budgetTone(used: number): 'healthy' | 'degraded' | 'error' {
  return used >= 100 ? 'error' : used >= 80 ? 'degraded' : 'healthy';
}
