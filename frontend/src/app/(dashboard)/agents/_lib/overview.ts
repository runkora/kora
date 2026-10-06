import type { AgentOverviewResponse, AgentRunStats } from '@orbit/shared/api';
import { toBudgetRows, toPeriodData } from './insights';
import { spendChange } from './spend';
import type { BudgetRow, Period, SpendRow } from './spend';

/** What the Overview shows for one window. A part that could not be read is absent. */
export interface AgentOverview {
  /** The window these figures cover. */
  period: Period;
  /** Tokens and spend. Absent without agents:usage:read. */
  usage?: {
    tokens: { total: number; cached: number; input: number; output: number };
    /** Cached share of the prompt side, as a percentage; null with no prompt tokens. */
    cachedShare: number | null;
    spend: number;
    previousSpend: number;
    /** Change on the previous window as a percentage; null when that window had no spend. */
    change: number | null;
    /** Spend by model, largest first. */
    models: SpendRow[];
    trend: { labels: string[]; values: number[] };
  };
  /** The run stats as received, with the figures that are not on the wire. */
  runs?: AgentRunStats & {
    manual: number;
    finished: number;
    /** Completed share of the finished runs, as a percentage; null with none. */
    successRate: number | null;
  };
  /** The agent's own budget; null when it has none. */
  budget: BudgetRow | null;
  /** Parts that errored, as opposed to refused. */
  failed: { usage: boolean; runs: boolean };
}

const share = (part: number, whole: number) =>
  whole > 0 ? Math.round((part / whole) * 100) : null;

export function toAgentOverview(resp: AgentOverviewResponse): AgentOverview {
  const { insights, runStats } = resp;
  const data = insights && toPeriodData(resp.period, insights);
  const u = insights?.usage[0];
  const cached = (u?.cache_read_tokens ?? 0) + (u?.cache_creation_tokens ?? 0);
  const input = u?.prompt_tokens ?? 0;
  const finished = runStats ? runStats.completed + runStats.failed : 0;
  return {
    period: resp.period,
    ...(data
      ? {
          usage: {
            tokens: {
              total: u?.total_tokens ?? 0,
              cached,
              input,
              output: u?.completion_tokens ?? 0,
            },
            cachedShare: share(cached, cached + input),
            spend: u?.estimated_cost_usd ?? 0,
            previousSpend: data.previousTotal ?? 0,
            change: spendChange(u?.estimated_cost_usd ?? 0, data.previousTotal),
            models: data.models ?? [],
            trend: data.trend ?? { labels: [], values: [] },
          },
        }
      : {}),
    ...(runStats
      ? {
          runs: {
            ...runStats,
            manual: runStats.total - runStats.triggered,
            finished,
            successRate: share(runStats.completed, finished),
          },
        }
      : {}),
    budget: toBudgetRows(insights?.budgets ?? [], [])[0] ?? null,
    failed: {
      usage: resp.failed?.includes('insights') ?? false,
      runs: resp.failed?.includes('runStats') ?? false,
    },
  };
}
