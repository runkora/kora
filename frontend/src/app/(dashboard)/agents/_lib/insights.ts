import type { AgentBudgetsResponse, AgentInsightsResponse } from '@orbit/shared/api';
import type { BudgetRow, Period, PeriodData, TenantBudget } from './spend';

// Turns the BFF responses into the shapes the page draws.

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * The axis label for a chart point that starts at `startIso`. Buckets begin on
 * UTC boundaries (every three hours for 24h, midnight otherwise), so dated
 * labels read in UTC. Hour labels read in local time, which is what a person
 * looking at a day of activity expects.
 */
export function trendLabel(period: Period, startIso: string): string {
  const start = new Date(startIso);
  if (period === '24h') {
    return start.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  if (period === '7d') {
    return start.toLocaleDateString('en-US', { weekday: 'short', timeZone: 'UTC' });
  }
  return start.toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
}

export function toPeriodData(period: Period, resp: AgentInsightsResponse): PeriodData {
  return {
    usage: Object.fromEntries(resp.usage.map((u) => [u.agent_slug, u])),
    ...(resp.outcomes ? { outcomes: resp.outcomes } : {}),
    models: resp.models.map((m) => ({ name: m.model_id, cost: m.cost_usd })),
    trend: {
      labels: resp.trend.map((p) => trendLabel(period, p.start)),
      values: resp.trend.map((p) => p.cost_usd),
    },
    previousTotal: resp.previous_total_usd,
  };
}

/** One row per visible agent that has spend or a limit, largest spend first. */
export function toBudgetRows(
  resp: AgentBudgetsResponse,
  agents: { slug: string; name: string }[],
): BudgetRow[] {
  const names = new Map(agents.map((a) => [a.slug, a.name]));
  return resp.agents
    .map((a): BudgetRow => {
      const weekly = a.reset_cadence === 'weekly';
      return {
        name: names.get(a.agent_slug) ?? a.agent_slug,
        spent: weekly ? a.week_to_date_usd : a.month_to_date_usd,
        ...(a.limit_usd ? { limit: a.limit_usd } : {}),
        ...(weekly ? { cadence: 'weekly' as const } : {}),
      };
    })
    .sort((x, y) => y.spent - x.spent);
}

/** The days left in the cap's window: to the next Monday for a weekly cap, to the 1st otherwise. */
function daysUntilReset(cadence: 'weekly' | undefined, now: Date): number {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  if (cadence === 'weekly') {
    const sinceMonday = (now.getUTCDay() + 6) % 7;
    return 7 - sinceMonday;
  }
  const nextMonth = Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1);
  return Math.round((nextMonth - today) / DAY_MS);
}

export function toTenantBudget(
  cap: AgentBudgetsResponse['tenant_cap'],
  now: Date,
): TenantBudget | undefined {
  if (!cap || cap.limit_usd <= 0) return undefined;
  const cadence = cap.reset_cadence === 'weekly' ? ('weekly' as const) : undefined;
  return {
    limit: cap.limit_usd,
    spent: cap.spend_usd,
    daysLeft: daysUntilReset(cadence, now),
    ...(cadence ? { cadence } : {}),
  };
}
