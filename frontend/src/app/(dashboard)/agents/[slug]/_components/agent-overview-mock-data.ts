import type {
  AgentBudgetStatus,
  AgentInsightsResponse,
  AgentOverviewResponse,
  AgentRunStats,
  AgentTypeUsage,
} from '@orbit/shared/api';
import { SHAPES } from '../../_components/agents-insights-mock-data';
import { DAY, USAGE, ahead } from '../../_components/agents-mock-data';
import type { Period } from '../../_lib/spend';

// Mock BFF overview for the agent detail stories: the 7-day mock usage scaled to each window.

/** A weekly and a monthly window plus a per-run limit. */
const BUDGET: AgentBudgetStatus = {
  agent_slug: 'code-reviewer',
  per_run_limit_usd: 4,
  windows: [
    { cadence: 'weekly', limit_usd: 40, spend_usd: 31.2, resets_at: ahead(3 * DAY) },
    { cadence: 'monthly', limit_usd: 150, spend_usd: 96.4, resets_at: ahead(20 * DAY) },
  ],
};

type Durations = AgentRunStats['durations'];

const NO_DURATIONS: Durations = {
  median_ms: 0,
  p95_ms: 0,
  p99_ms: 0,
  longest_ms: 0,
  capped: false,
  buckets: Array<number>(10).fill(0),
  overflow: 0,
};

interface Profile {
  /** Share of spend per model. */
  models: [string, number][];
  /** Runs in 7 days. */
  runs: { completed: number; failed: number; triggered: number };
  durations: Durations;
  /** How much the window before was worth, relative to this one. */
  previous: number;
}

const PROFILES: Record<string, Profile> = {
  'code-reviewer': {
    models: [
      ['claude-sonnet-4-5', 0.62],
      ['claude-opus-4-1', 0.27],
      ['claude-haiku-4-5', 0.11],
    ],
    runs: { completed: 41, failed: 5, triggered: 38 },
    durations: {
      median_ms: 232_000,
      p95_ms: 662_000,
      p99_ms: 900_000,
      longest_ms: 1_300_000,
      capped: true,
      buckets: [4, 12, 11, 7, 4, 3, 2, 1, 0, 1],
      overflow: 1,
    },
    previous: 0.84,
  },
  'release-notes': {
    models: [['claude-haiku-4-5', 1]],
    runs: { completed: 7, failed: 0, triggered: 7 },
    durations: {
      median_ms: 66_000,
      p95_ms: 130_000,
      p99_ms: 130_000,
      longest_ms: 130_000,
      capped: false,
      buckets: [0, 2, 2, 1, 1, 0, 0, 0, 0, 1],
      overflow: 0,
    },
    previous: 1.6,
  },
  'single-run': {
    models: [['claude-haiku-4-5', 1]],
    runs: { completed: 1, failed: 0, triggered: 0 },
    durations: {
      median_ms: 200_000,
      p95_ms: 200_000,
      p99_ms: 200_000,
      longest_ms: 200_000,
      capped: false,
      buckets: [0, 0, 0, 0, 0, 0, 0, 0, 0, 1],
      overflow: 0,
    },
    previous: 0,
  },
};

export interface OverviewOptions {
  /** Adds one runaway run of this length to the 90 day window, as the overflow bar. */
  outlierMs?: number;
  /** Leaves the agent's budget row out of the insights. */
  noBudget?: boolean;
}

/** What the BFF returns for one agent and period. */
export function mockOverview(
  slug: string,
  period: Period,
  { outlierMs, noBudget }: OverviewOptions = {},
): AgentOverviewResponse {
  const { factor, bucketHours, buckets } = SHAPES[period];
  const base = USAGE.find((u) => u.agent_slug === slug);
  const profile = PROFILES[slug];
  const scale = (n: number) => (n <= 1 ? n : Math.round(n * factor));
  const usage: AgentTypeUsage = {
    agent_slug: slug,
    prompt_tokens: scale(base?.prompt_tokens ?? 0),
    completion_tokens: scale(base?.completion_tokens ?? 0),
    cache_read_tokens: scale(base?.cache_read_tokens ?? 0),
    cache_creation_tokens: scale(base?.cache_creation_tokens ?? 0),
    total_tokens: scale(base?.total_tokens ?? 0),
    estimated_cost_usd: (base?.estimated_cost_usd ?? 0) * factor,
  };
  const total = usage.estimated_cost_usd;
  // A gentle wave per agent, normalized so the buckets add up to the window's spend.
  const phase = slug.length % 5;
  const wave = Array.from({ length: buckets }, (_, i) => 1 + 0.45 * Math.sin(i * 0.9 + phase));
  const waveSum = wave.reduce((n, w) => n + w, 0);
  const start = Date.now() - buckets * bucketHours * 3_600_000;
  const runs = profile?.runs ?? { completed: 0, failed: 0, triggered: 0 };
  const completed = scale(runs.completed);
  const failed = scale(runs.failed);
  const triggered = scale(runs.triggered);
  const durations = profile?.durations ?? NO_DURATIONS;
  const outlier = outlierMs && period === '90d';

  const insights: AgentInsightsResponse = {
    period,
    usage: base ? [usage] : [],
    total_cost_usd: total,
    previous_total_usd: total * (profile?.previous ?? 0),
    models: (profile?.models ?? []).map(([model_id, share]) => ({
      model_id,
      estimated_cost_usd: share * total,
    })),
    trend: wave.map((w, i) => ({
      start: new Date(start + i * bucketHours * 3_600_000).toISOString(),
      cost_usd: (w / waveSum) * total,
    })),
    ...(slug === BUDGET.agent_slug && !noBudget ? { budgets: [BUDGET] } : {}),
  };
  return {
    period,
    insights,
    runStats: {
      total: Math.max(completed + failed, triggered),
      triggered,
      completed,
      failed,
      durations: outlier
        ? { ...durations, capped: true, longest_ms: outlierMs, overflow: durations.overflow + 1 }
        : durations,
      current: {
        total: Math.max(completed + failed, triggered) + 2,
        by_state: { completed, failed, active: 1, idle: 1 },
        live: 1,
        live_by_agent_type: {},
        longest_live_ms: 4 * 60_000,
      },
    },
  };
}

/** The same overview without the part a caller without agents:usage:read cannot read. */
export function mockRunStatsOnly(slug: string, period: Period): AgentOverviewResponse {
  const { runStats } = mockOverview(slug, period);
  return { period, ...(runStats ? { runStats } : {}) };
}

/** Long enough to be clamped: headings, nested and numbered lists, a code fence and a quote. */
export const LONG_INSTRUCTIONS = [
  '# Code reviewer',
  '',
  'You review pull requests for the platform team. Read the diff and the surrounding code before commenting.',
  '',
  '## Scope',
  '',
  '- Review only the files in the diff.',
  '  - Follow imports one level to understand a change.',
  '- Do not edit code or push commits.',
  '',
  '## Method',
  '',
  '1. Read the pull request description and linked issue.',
  '2. Read the diff file by file, then read the tests.',
  '3. Leave inline comments only where you can name a concrete problem and a fix.',
  '',
  '## Commenting',
  '',
  'Write **one** summary comment: what changed, what worries you, what is fine.',
  '',
  '```go',
  'if err != nil {',
  '\treturn fmt.Errorf("load agent %s: %w", slug, err)',
  '}',
  '```',
  '',
  '> Never approve a change that touches migrations or auth without a human reviewer.',
  '',
  '## Output',
  '',
  '- One summary comment, then a final verdict line.',
].join('\n');
