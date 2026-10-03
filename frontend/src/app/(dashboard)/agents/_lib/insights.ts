import type { AgentBudgetStatus, AgentInsightsResponse } from '@orbit/shared/api';
import { closestToLimit } from './spend';
import type { BudgetRow, Period, PeriodData, TenantBudget } from './spend';

// Turns the BFF response into the shapes the page draws.

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * The axis label for a chart point that starts at `startIso`. Buckets count from
 * the window start, so they begin at the time of day the request was made, and
 * all labels read in local time: a clock time for 24h, a weekday for 7d, a date
 * for the longer windows.
 */
export function trendLabel(period: Period, startIso: string): string {
  const start = new Date(startIso);
  if (period === '24h') {
    return start.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  if (period === '7d') return start.toLocaleDateString('en-US', { weekday: 'short' });
  return start.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function toPeriodData(period: Period, resp: AgentInsightsResponse): PeriodData {
  return {
    usage: Object.fromEntries(resp.usage.map((u) => [u.agent_slug, u])),
    total: resp.total_cost_usd,
    delegatedOwnOnly: !resp.all_delegated_usage,
    ...(resp.outcomes ? { outcomes: resp.outcomes } : {}),
    models: resp.models.map((m) => ({ name: m.model_id, cost: m.cost_usd })),
    trend: {
      labels: resp.trend.map((p) => trendLabel(period, p.start)),
      values: resp.trend.map((p) => p.cost_usd),
    },
    previousTotal: resp.previous_total_usd,
  };
}

/**
 * One row per agent that has a window limit, the closest to a limit first. A
 * delegated agent gets no row: its limit is shared by everyone who runs it, so
 * a bar of one person's spend against it would mislead.
 */
export function toBudgetRows(
  budgets: AgentBudgetStatus[],
  agents: { slug: string; name: string; identityMode?: string }[],
): BudgetRow[] {
  const bySlug = new Map(agents.map((a) => [a.slug, a]));
  return closestToLimit(
    budgets
      .filter((b) => bySlug.get(b.agent_slug)?.identityMode !== 'delegated')
      .map((b): BudgetRow => ({
        name: bySlug.get(b.agent_slug)?.name ?? b.agent_slug,
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

export function toTenantBudget(
  cap: AgentInsightsResponse['tenant_cap'],
  now: Date,
): TenantBudget | undefined {
  if (!cap || cap.limit_usd <= 0) return undefined;
  const cadence = cap.reset_cadence === 'weekly' ? ('weekly' as const) : undefined;
  const daysLeft = Math.max(
    Math.ceil((new Date(cap.resets_at).getTime() - now.getTime()) / DAY_MS),
    0,
  );
  return {
    limit: cap.limit_usd,
    spent: cap.spend_usd,
    daysLeft,
    ...(cadence ? { cadence } : {}),
  };
}
