import { fmtDateAxis } from '@orbit/utils';
import type { AgentBudgetStatus, AgentInsightsResponse } from '@orbit/shared/api';
import { closestToLimit } from './spend';
import type { BudgetRow, Period, PeriodData } from './spend';

/** The axis label for a bucket start: local clock time for 24h, weekday for 7d, date beyond. */
export function trendLabel(period: Period, startIso: string): string {
  const start = new Date(startIso);
  if (period === '24h') {
    return start.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  if (period === '7d') return start.toLocaleDateString('en-US', { weekday: 'short' });
  return fmtDateAxis(startIso.slice(0, 10));
}

export function toPeriodData(period: Period, resp: AgentInsightsResponse): PeriodData {
  return {
    usage: Object.fromEntries(resp.usage.map((u) => [u.agent_slug, u])),
    total: resp.total_cost_usd,
    ...(resp.outcomes ? { outcomes: resp.outcomes } : {}),
    models: resp.models.map((m) => ({ name: m.model_id, cost: m.estimated_cost_usd })),
    trend: {
      labels: resp.trend.map((p) => trendLabel(period, p.start)),
      values: resp.trend.map((p) => p.cost_usd),
    },
    previousTotal: resp.previous_total_usd,
  };
}

/** One row per agent that has a window limit, the closest to a limit first. */
export function toBudgetRows(
  budgets: AgentBudgetStatus[],
  agents: { slug: string; name: string }[],
): BudgetRow[] {
  const names = new Map(agents.map((a) => [a.slug, a.name]));
  return closestToLimit(
    budgets.map((b): BudgetRow => ({
      name: names.get(b.agent_slug) ?? b.agent_slug,
      windows: b.windows.map((w) => ({
        cadence: w.cadence,
        limit: w.limit_usd,
        spent: w.spend_usd,
        resetsAt: w.resets_at,
      })),
      ...(b.per_run_limit_usd ? { perRunLimit: b.per_run_limit_usd } : {}),
    })),
  );
}
