import type { AgentInsightsPeriod, AgentTypeUsage } from '@orbit/shared/api';

/** The windows the Agents page can report spend and run outcomes over. */
export type Period = AgentInsightsPeriod;

/** Phrases for footers and descriptions, so every card names its window the same way. */
export const PERIOD_TEXT: Record<Period, { text: string; previous: string }> = {
  '24h': { text: 'last 24 hours', previous: 'previous 24 hours' },
  '7d': { text: 'last 7 days', previous: 'previous 7 days' },
  '30d': { text: 'last 30 days', previous: 'previous 30 days' },
  '90d': { text: 'last 90 days', previous: 'previous 90 days' },
};

/** The window the page opens on. The server render fetches it, so first paint has data. */
export const DEFAULT_PERIOD: Period = '7d';

const PERIODS = Object.keys(PERIOD_TEXT) as Period[];

export const PERIOD_OPTIONS = PERIODS.map((value) => ({ value, label: value }));

export function isPeriod(value: string): value is Period {
  return (PERIODS as string[]).includes(value);
}

export interface SpendRow {
  name: string;
  cost: number;
}

/** One budget window of an agent: its limit, the spend inside it now, and when it resets. */
export interface BudgetWindowRow {
  cadence: 'weekly' | 'monthly';
  limit: number;
  spent: number;
  /** RFC 3339 time the window resets. */
  resetsAt: string;
}

/**
 * An agent's budget: a row per agent that has a window limit, with a window for
 * each cadence it is limited on. Spend is by agent, so it can differ slightly
 * from the amount the limit check counts.
 */
export interface BudgetRow {
  name: string;
  windows: BudgetWindowRow[];
  /** The per-run limit, which has no window. */
  perRunLimit?: number;
}

/** Everything the page reports for one window. */
export interface PeriodData {
  /** Usage per agent slug. It includes agents that have since been deleted. */
  usage: Record<string, AgentTypeUsage>;
  /** Total spend of the window, which is the sum of `usage` and so includes deleted agents. */
  total: number;
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
 * What the page has loaded. A part that could not be loaded or is not permitted
 * is absent, so no card ever reports a number the platform has not produced.
 */
export interface AgentsInsights {
  /** A period that is not in the map has not been loaded. */
  byPeriod: Partial<Record<Period, PeriodData>>;
  budgets?: BudgetRow[];
}

/** Healthy under 80% of a limit, degraded from 80%, error at or over it. */
export function budgetTone(used: number): 'healthy' | 'degraded' | 'error' {
  return used >= 100 ? 'error' : used >= 80 ? 'degraded' : 'healthy';
}

/** The most legend rows a breakdown shows before the rest fold into "Other". */
export const LEGEND_ROWS = 6;

/** The most budget rows the card shows. */
export const BUDGET_ROWS = 8;

/** The most live runs the Running now table lists. Its header counts them all. */
export const RUNNING_ROWS = 10;

/**
 * Keeps the largest rows and folds the rest into one "Other" row. Rows are
 * expected largest first. Nothing is folded unless more than LEGEND_ROWS rows
 * exist, so the total is unchanged.
 */
export function groupTail(rows: SpendRow[]): SpendRow[] {
  if (rows.length <= LEGEND_ROWS) return rows;
  const head = rows.slice(0, LEGEND_ROWS - 1);
  const tail = rows.slice(LEGEND_ROWS - 1);
  return [
    ...head,
    { name: `Other (${tail.length})`, cost: tail.reduce((n, row) => n + row.cost, 0) },
  ];
}

/** The share of a limit that is spent. A zero limit is fully spent, however little is spent against it. */
export function usedShare(spent: number, limit: number): number {
  return limit > 0 ? spent / limit : Infinity;
}

/** The share of its tightest window an agent has spent. */
function rowUsedShare(row: BudgetRow): number {
  return Math.max(...row.windows.map((w) => usedShare(w.spent, w.limit)), 0);
}

/** Budget rows ordered by how close each is to a limit, the closest first. */
export function closestToLimit(rows: BudgetRow[]): BudgetRow[] {
  return [...rows].sort(
    (a, b) => rowUsedShare(b) - rowUsedShare(a) || a.name.localeCompare(b.name),
  );
}
