import type {
  AgentBudgetStatus,
  AgentInsightsResponse,
  AgentOverviewResponse,
  AgentRunStats,
  AgentTypeUsage,
} from '@orbit/shared/api';
import { SHAPES } from '../../_components/agents-insights-mock-data';
import { USAGE, ahead } from '../../_components/agents-mock-data';
import type { Period } from '../../_lib/spend';

// Mock BFF overview for the agent detail stories: the 7-day mock usage scaled to each window.

const DAY = 24 * 60 * 60 * 1000;

/** A weekly and a monthly window plus a per-run limit, so the Budget view shows all three. */
const BUDGET: AgentBudgetStatus = {
  agent_slug: 'code-reviewer',
  per_run_limit_usd: 4,
  windows: [
    { cadence: 'weekly', limit_usd: 40, spend_usd: 31.2, resets_at: ahead(3 * DAY) },
    { cadence: 'monthly', limit_usd: 150, spend_usd: 96.4, resets_at: ahead(20 * DAY) },
  ],
};

interface Profile {
  /** Share of spend per model. */
  models: [string, number][];
  /** Runs in 7 days. */
  runs: { completed: number; failed: number; triggered: number; manual: number };
  /** Run duration in 7 days; the other periods vary around it. */
  p50Ms: number;
  p95Ms: number;
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
    runs: { completed: 41, failed: 5, triggered: 38, manual: 8 },
    p50Ms: 232_000,
    p95Ms: 662_000,
    previous: 0.84,
  },
  coder: {
    models: [
      ['claude-opus-4-1', 0.58],
      ['claude-sonnet-4-5', 0.42],
    ],
    runs: { completed: 64, failed: 11, triggered: 12, manual: 63 },
    p50Ms: 1_050_000,
    p95Ms: 3_400_000,
    previous: 1.31,
  },
  'security-reviewer': {
    models: [
      ['claude-sonnet-4-5', 0.7],
      ['gpt-5', 0.3],
    ],
    runs: { completed: 18, failed: 1, triggered: 17, manual: 2 },
    p50Ms: 480_000,
    p95Ms: 1_500_000,
    previous: 0.97,
  },
  'release-notes': {
    models: [['claude-haiku-4-5', 1]],
    runs: { completed: 7, failed: 0, triggered: 7, manual: 0 },
    p50Ms: 66_000,
    p95Ms: 130_000,
    previous: 1.6,
  },
  'single-run': {
    models: [['claude-haiku-4-5', 1]],
    runs: { completed: 1, failed: 0, triggered: 0, manual: 1 },
    p50Ms: 200_000,
    p95Ms: 200_000,
    previous: 0,
  },
  'triage-bot': {
    models: [
      ['claude-haiku-4-5', 0.8],
      ['claude-sonnet-4-5', 0.2],
    ],
    runs: { completed: 133, failed: 9, triggered: 140, manual: 2 },
    p50Ms: 31_000,
    p95Ms: 96_000,
    previous: 0.72,
  },
};

/** Durations move a little between windows. */
const DURATION_DRIFT: Record<Period, number> = { '24h': 0.9, '7d': 1, '30d': 1.06, '90d': 0.97 };

/** Standard normal CDF (Abramowitz and Stegun 7.1.26). */
function normalCdf(z: number): number {
  const t = 1 / (1 + (0.3275911 * Math.abs(z)) / Math.SQRT2);
  const poly =
    t *
    (0.254829592 + t * (-0.284496736 + t * (1.421413741 + t * (-1.453152027 + t * 1.061405429))));
  const erf = 1 - poly * Math.exp((-z * z) / 2);
  return 0.5 * (1 + (z >= 0 ? erf : -erf));
}

/** The z whose CDF is q, by bisection; the mock does not need more precision. */
function normalQuantile(q: number): number {
  let lo = -6;
  let hi = 6;
  for (let i = 0; i < 40; i++) {
    const mid = (lo + hi) / 2;
    if (normalCdf(mid) < q) lo = mid;
    else hi = mid;
  }
  return (lo + hi) / 2;
}

/** n run durations at evenly spaced quantiles of a log-normal through the median and p95: right-skewed, most runs short, a thin tail. */
function mockRunDurations(n: number, p50: number, p95: number): number[] {
  const sigma = Math.log(Math.max(p95, p50 * 1.01) / p50) / 1.645;
  return Array.from({ length: n }, (_, i) =>
    Math.round(p50 * Math.exp(sigma * normalQuantile((i + 0.5) / n))),
  );
}

/** The backend's rule: from 20 finished runs the histogram stops at p99, below that it spans the longest run. */
const MIN_RUNS_FOR_TAIL = 20;

/** Everything the run stats carry about durations, from the durations themselves. */
function summarizeDurations(durations: number[]): AgentRunStats['durations'] {
  const sorted = [...durations].sort((a, b) => a - b);
  const n = sorted.length;
  const buckets = Array<number>(10).fill(0);
  if (n === 0) {
    return {
      finished: 0,
      median_ms: 0,
      average_ms: 0,
      p95_ms: 0,
      p99_ms: 0,
      longest_ms: 0,
      capped: false,
      range_ms: 0,
      buckets,
      overflow: 0,
    };
  }
  const at = (q: number) => sorted[Math.min(n - 1, Math.ceil(q * n) - 1)] ?? 0;
  const longest = sorted[n - 1] ?? 0;
  const p99 = at(0.99);
  const capped = n >= MIN_RUNS_FOR_TAIL;
  const range = capped ? p99 : longest;
  let overflow = 0;
  for (const d of sorted) {
    if (d > range) overflow++;
    else buckets[Math.min(9, Math.floor((d / Math.max(range, 1)) * 10))]++;
  }
  const mid = n % 2 === 1 ? at(0.5) : ((sorted[n / 2 - 1] ?? 0) + (sorted[n / 2] ?? 0)) / 2;
  return {
    finished: n,
    median_ms: Math.round(mid),
    average_ms: Math.round(sorted.reduce((sum, d) => sum + d, 0) / n),
    p95_ms: at(0.95),
    p99_ms: p99,
    longest_ms: longest,
    capped,
    range_ms: range,
    buckets,
    overflow,
  };
}

const EMPTY: Profile['runs'] & Pick<Profile, 'p50Ms' | 'p95Ms'> = {
  completed: 0,
  failed: 0,
  triggered: 0,
  manual: 0,
  p50Ms: 0,
  p95Ms: 0,
};

export interface OverviewOptions {
  /** Adds one runaway run of this length to the 90 day window. */
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
  // A gentle wave per agent, normalised so the buckets add up to the window's spend.
  const phase = slug.length % 5;
  const wave = Array.from({ length: buckets }, (_, i) => 1 + 0.45 * Math.sin(i * 0.9 + phase));
  const waveSum = wave.reduce((n, w) => n + w, 0);
  const start = Date.now() - buckets * bucketHours * 3_600_000;
  const runs = profile?.runs ?? EMPTY;
  const p50Ms = Math.round((profile?.p50Ms ?? 0) * DURATION_DRIFT[period]);
  const p95Ms = Math.round((profile?.p95Ms ?? 0) * DURATION_DRIFT[period]);
  const completed = scale(runs.completed);
  const failed = scale(runs.failed);
  const triggered = scale(runs.triggered);
  const manual = Math.max(0, completed + failed - triggered);

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
      period,
      total: triggered + manual,
      triggered,
      manual,
      completed,
      failed,
      durations: summarizeDurations([
        ...mockRunDurations(completed + failed, p50Ms, p95Ms),
        ...(outlierMs && period === '90d' ? [outlierMs] : []),
      ]),
    },
  };
}

/** The same overview without the part a caller without agents:usage:read cannot read. */
export function mockRunStatsOnly(slug: string, period: Period): AgentOverviewResponse {
  const { runStats } = mockOverview(slug, period);
  return { period, ...(runStats ? { runStats } : {}) };
}

/** About 54 lines of markdown: headings, nested and numbered lists, bold, inline code, a code fence, a quote and two variables. */
export const LONG_INSTRUCTIONS = [
  '# Code reviewer',
  '',
  'You review pull requests for the platform team. Read the diff and the surrounding code before commenting. Greet the author as {{author}} in the first comment.',
  '',
  '## Scope',
  '',
  '- Review only the files in the diff.',
  '  - Follow imports one level to understand a change.',
  '  - Skip generated files and lockfiles.',
  '- Do not edit code or push commits.',
  '- Ignore style points that the formatter already enforces.',
  '',
  '## Method',
  '',
  '1. Read the pull request description and linked issue.',
  '2. Read the diff file by file, then read the tests.',
  '3. Run through the checklist below before writing anything.',
  '4. Leave inline comments only where you can name a concrete problem and a fix.',
  '',
  '### Checklist',
  '',
  '- Does the change touch migrations or auth? If so, a human must review it.',
  '- Are errors wrapped and logged with context?',
  '- Is there a test for the new branch?',
  '',
  '## Commenting',
  '',
  'Write **one** summary comment: what changed, what worries you, what is fine. Keep inline comments to a single `suggestion` each.',
  '',
  '```go',
  'if err != nil {',
  '\treturn fmt.Errorf("load agent %s: %w", slug, err)',
  '}',
  '```',
  '',
  '> Never approve a change that touches migrations or auth without a human reviewer.',
  '',
  '## Tone',
  '',
  '- Be direct and specific.',
  '- Name the file and line.',
  '- Praise good changes in one short sentence.',
  '',
  '## Escalation',
  '',
  '1. Security concerns go to {{security_channel}} with a link to the line.',
  '2. Flaky tests are reported once per pull request, not per run.',
  '3. If you are unsure, say so and ask the author.',
  '',
  '## Output',
  '',
  '- One summary comment.',
  '- At most ten inline comments, most important first.',
  '- A final verdict line: `approve`, `request changes` or `needs a human`.',
].join('\n');
