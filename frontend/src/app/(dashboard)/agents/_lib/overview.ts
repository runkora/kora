import type { AgentOverviewResponse } from '@orbit/shared/api';
import { trendLabel, toBudgetRows } from './insights';
import type { BudgetRow, Period, SpendRow } from './spend';

/** Durations of the runs that finished in the window, in milliseconds. */
export interface RunDurations {
  finished: number;
  medianMs: number;
  p95Ms: number;
  p99Ms: number;
  longestMs: number;
  /** True when `buckets` stop at p99 and `overflow` counts the slower runs. */
  capped: boolean;
  buckets: number[];
  overflow: number;
}

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
    /** Change on the previous window, as a percentage; null when that window had no spend. */
    change: number | null;
    /** Spend by model, largest first. */
    models: SpendRow[];
    trend: { labels: string[]; values: number[] };
  };
  runs?: {
    total: number;
    triggered: number;
    manual: number;
    completed: number;
    failed: number;
    finished: number;
    /** Completed share of the finished runs, as a percentage; null with none. */
    successRate: number | null;
    durations: RunDurations;
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
  const u = insights?.usage[0];
  const spend = u?.estimated_cost_usd ?? 0;
  const cached = (u?.cache_read_tokens ?? 0) + (u?.cache_creation_tokens ?? 0);
  const input = u?.prompt_tokens ?? 0;
  const previousSpend = insights?.previous_total_usd ?? 0;
  const finished = runStats ? runStats.completed + runStats.failed : 0;
  return {
    period: resp.period,
    ...(insights
      ? {
          usage: {
            tokens: {
              total: u?.total_tokens ?? 0,
              cached,
              input,
              output: u?.completion_tokens ?? 0,
            },
            cachedShare: share(cached, cached + input),
            spend,
            previousSpend,
            change:
              previousSpend > 0
                ? Math.round(((spend - previousSpend) / previousSpend) * 100)
                : null,
            models: insights.models.map((m) => ({ name: m.model_id, cost: m.estimated_cost_usd })),
            trend: {
              labels: insights.trend.map((p) => trendLabel(resp.period, p.start)),
              values: insights.trend.map((p) => p.cost_usd),
            },
          },
        }
      : {}),
    ...(runStats
      ? {
          runs: {
            total: runStats.total,
            triggered: runStats.triggered,
            manual: runStats.manual,
            completed: runStats.completed,
            failed: runStats.failed,
            finished,
            successRate: share(runStats.completed, finished),
            durations: {
              finished: runStats.durations.finished,
              medianMs: runStats.durations.median_ms,
              p95Ms: runStats.durations.p95_ms,
              p99Ms: runStats.durations.p99_ms,
              longestMs: runStats.durations.longest_ms,
              capped: runStats.durations.capped,
              buckets: runStats.durations.buckets,
              overflow: runStats.durations.overflow,
            },
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
