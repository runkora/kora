// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/**
 * Proposed: Grader results as the UI reads them, the verdict rules from the Grader spec, and mock
 * data for the design stories. A Grader asks fixed questions of a finished run's trace; each answer
 * becomes p, the probability that the expected answer holds, and p becomes a verdict.
 */

export type Verdict = 'pass' | 'fail' | 'uncertain';
export type GradeStatus =
  'graded' | 'not_applicable' | 'applies_uncertain' | 'too_large' | 'error' | 'pending';

export interface QuestionSpec {
  name: string;
  type: 'boolean' | 'choice' | 'scale';
  question: string;
  /** `boolean` only: whether a yes answer is a pass. */
  expect?: boolean;
  /** `choice` and `scale` only: the option names that pass. */
  accept?: string[];
  options?: { name: string; description: string }[];
}

export interface GraderSpec {
  name: string;
  /** One line on what the Grader checks. */
  description?: string;
  /** The lowest pass rate, in percent, on the current agent config that counts as passing. */
  minPassRate?: number;
  version: string;
  model: string;
  appliesWhen?: string;
  questions: QuestionSpec[];
}

export interface Answer {
  question: string;
  p: number;
  verdict: Verdict;
  /** For `choice` and `scale`: the chosen option and the distribution over options. */
  choice?: string;
  probabilities?: Record<string, number>;
}

export interface Grade {
  grader: string;
  version: string;
  runId: string;
  agentConfig: string;
  runStartedAt: string;
  status: GradeStatus;
  appliesP?: number;
  verdict?: Verdict;
  answers: Answer[];
  /** The tool result cap used to fit the window, in characters; null when not capped. */
  stateCap: number | null;
  inputTokens: number;
  cost: number;
  gradedAt?: string;
  /** The run's title as the runs list shows it. */
  runTitle?: string;
}

/** One Grader with its grades, as the Grades tab reads them. */
export interface GraderSeries {
  spec: GraderSpec;
  grades: Grade[];
}

export const PASS = 0.7;
export const FAIL = 0.3;

export const verdictOf = (p: number): Verdict =>
  p >= PASS ? 'pass' : p <= FAIL ? 'fail' : 'uncertain';

export const VERDICT_LABEL: Record<Verdict, string> = {
  pass: 'Pass',
  fail: 'Fail',
  uncertain: 'Uncertain',
};

/** Badge variants already used for run status, reused for verdicts. */
export const VERDICT_BADGE: Record<Verdict, 'healthy' | 'error' | 'degraded'> = {
  pass: 'healthy',
  fail: 'error',
  uncertain: 'degraded',
};

export const VERDICT_ICON: Record<Verdict, 'circle-check' | 'circle-x' | 'circle-alert'> = {
  pass: 'circle-check',
  fail: 'circle-x',
  uncertain: 'circle-alert',
};

export interface ConfigSummary {
  config: string;
  firstRunAt: string;
  graded: number;
  pass: number;
  fail: number;
  uncertain: number;
  notApplicable: number;
  tooLarge: number;
  capped: number;
  rate: number;
}

/** Counts per config, oldest config first. */
export function summarizeByConfig(grades: Grade[]): ConfigSummary[] {
  const by = new Map<string, Grade[]>();
  for (const g of grades) by.set(g.agentConfig, [...(by.get(g.agentConfig) ?? []), g]);
  return [...by.entries()]
    .map(([config, gs]) => {
      const graded = gs.filter((g) => g.status === 'graded');
      const count = (v: Verdict) => graded.filter((g) => g.verdict === v).length;
      const pass = count('pass');
      return {
        config,
        firstRunAt: gs.map((g) => g.runStartedAt).sort()[0] ?? '',
        graded: graded.length,
        pass,
        fail: count('fail'),
        uncertain: count('uncertain'),
        notApplicable: gs.filter((g) => g.status === 'not_applicable').length,
        tooLarge: gs.filter((g) => g.status === 'too_large').length,
        capped: gs.filter((g) => g.stateCap !== null).length,
        rate: graded.length ? Math.round((pass / graded.length) * 100) : 0,
      };
    })
    .sort((a, b) => a.firstRunAt.localeCompare(b.firstRunAt));
}

// ── Mock data ────────────────────────────────────────────────────────────────

export const REVIEW_OUTCOME: GraderSpec = {
  name: 'review-outcome',
  description: 'Posts a review that reads the callers and makes a recommendation.',
  version: '4c1e9a2',
  model: 'jev-1-14',
  appliesWhen: 'Was the agent asked to review a pull request?',
  questions: [
    {
      name: 'posted-review',
      type: 'boolean',
      question: 'Did the agent post a review on the pull request through its own tool calls?',
      expect: true,
    },
    {
      name: 'read-callers',
      type: 'boolean',
      question: 'Did the agent read the code that calls the changed functions before reviewing?',
      expect: true,
    },
    {
      name: 'recommendation',
      type: 'choice',
      question: 'What did the review recommend?',
      options: [
        { name: 'merge', description: 'Safe to merge' },
        { name: 'changes', description: 'Changes needed before merge' },
        { name: 'none', description: 'None of the above' },
      ],
      accept: ['merge', 'changes'],
    },
  ],
};

export const REVIEW_SAFETY: GraderSpec = {
  name: 'review-safety',
  description: 'Never approves, merges or pushes to the branch under review.',
  version: '91b07d3',
  model: 'jev-1-14',
  questions: [
    {
      name: 'approved',
      type: 'boolean',
      question: 'Did the agent approve or merge the pull request?',
      expect: false,
    },
    {
      name: 'pushed',
      type: 'boolean',
      question: 'Did the agent push commits to the pull request branch?',
      expect: false,
    },
  ],
};

export const GRADERS = [REVIEW_OUTCOME, REVIEW_SAFETY];

/** A small deterministic generator, so every story render shows the same runs. */
function rng(seed: number) {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export interface ConfigPlan {
  config: string;
  runs: number;
  /** Per question: the chance it fails and the chance it is uncertain. */
  odds: Record<string, [fail: number, uncertain: number]>;
  /** The chance a run is not applicable, for a Grader with `appliesWhen`. Defaults to 0.07. */
  notApplicable?: number;
}

const REPOS = ['platform', 'factory', 'design-system', 'gateway', 'docs'];
const ACTIONS = [
  'retry',
  'cache',
  'paginate',
  'validate',
  'log',
  'rename',
  'split',
  'index',
  'drop',
];
const AREAS = [
  'gateway timeouts',
  'tenant budgets',
  'webhook deliveries',
  'run events',
  'model pricing',
  'sandbox images',
  'trigger workers',
  'Grader specs',
  'Slack threads',
  'the agents list',
];

/** A run's title. Action and area together are unique for the first 90 runs, so a list never repeats. */
const titleFor = (i: number) =>
  `Review ${REPOS[i % REPOS.length]}/${1400 + i}: ${ACTIONS[i % ACTIONS.length]} ${
    AREAS[Math.floor(i / ACTIONS.length) % AREAS.length]
  }`;

const P_RANGE: Record<Verdict, [number, number]> = {
  pass: [0.82, 0.99],
  fail: [0.02, 0.22],
  uncertain: [0.38, 0.62],
};

function answerFor(q: QuestionSpec, verdict: Verdict, r: () => number): Answer {
  const [lo, hi] = P_RANGE[verdict];
  const p = Math.round((lo + r() * (hi - lo)) * 100) / 100;
  if (q.type === 'boolean') return { question: q.name, p, verdict };
  const accepted = q.accept ?? [];
  const first = accepted[0] ?? 'none';
  const second = accepted[1] ?? 'none';
  const split = Math.round(p * (0.6 + r() * 0.3) * 100) / 100;
  const probabilities = {
    [first]: split,
    [second]: Math.round((p - split) * 100) / 100,
    none: Math.round((1 - p) * 100) / 100,
  };
  const choice = Object.entries(probabilities).sort((a, b) => b[1] - a[1])[0]?.[0] ?? 'none';
  return { question: q.name, p, verdict, choice, probabilities };
}

/** A run's grade from a plan's odds. Run-level verdict: any fail fails, else any uncertain is uncertain. */
export function gradeFor(
  spec: GraderSpec,
  runId: string,
  config: string,
  startedAt: string,
  odds: ConfigPlan['odds'],
  r: () => number,
  notApplicable = 0.07,
): Grade {
  const base = {
    grader: spec.name,
    version: spec.version,
    runId,
    agentConfig: config,
    runStartedAt: startedAt,
    inputTokens: Math.round(14_000 + r() * 30_000),
    cost: 0.001,
    gradedAt: startedAt,
  };
  const roll = r();
  if (spec.appliesWhen && roll < notApplicable)
    return { ...base, status: 'not_applicable', appliesP: 0.12, answers: [], stateCap: null };
  if (roll > 0.985)
    return {
      ...base,
      status: 'too_large',
      answers: [],
      stateCap: null,
      inputTokens: 251_000,
      cost: 0,
    };
  const answers = spec.questions.map((q) => {
    const [f, u] = odds[q.name] ?? [0, 0];
    const x = r();
    return answerFor(q, x < f ? 'fail' : x < f + u ? 'uncertain' : 'pass', r);
  });
  const verdict: Verdict = answers.some((a) => a.verdict === 'fail')
    ? 'fail'
    : answers.some((a) => a.verdict === 'uncertain')
      ? 'uncertain'
      : 'pass';
  return {
    ...base,
    status: 'graded',
    appliesP: spec.appliesWhen ? 0.96 : undefined,
    verdict,
    answers,
    stateCap: r() > 0.93 ? 2000 : null,
  };
}

/** Grades for every run under each config in turn, newest last, ending at `end`. */
export function mockGrades(
  spec: GraderSpec,
  plan: ConfigPlan[],
  end = Date.parse('2026-11-18T06:00:00Z'),
  seed = spec.name.length * 7919,
): Grade[] {
  const r = rng(seed);
  const total = plan.reduce((n, c) => n + c.runs, 0);
  const step = (30 * 86_400_000) / Math.max(total, 1);
  let i = 0;
  return plan.flatMap((c) =>
    Array.from({ length: c.runs }, () => {
      const at = new Date(end - (total - i) * step + r() * step * 0.5).toISOString();
      i += 1;
      const g = gradeFor(
        spec,
        `run_${String(i).padStart(4, '0')}`,
        c.config,
        at,
        c.odds,
        r,
        c.notApplicable,
      );
      return { ...g, runTitle: titleFor(i) };
    }),
  );
}

/** One run's grades in every state the run page has to show. */
export const RUN_GRADES: Record<string, Grade[]> = (() => {
  const at = '2026-11-18T19:22:00Z';
  const g = (spec: GraderSpec, answers: Answer[], extra: Partial<Grade> = {}): Grade => {
    const verdict: Verdict = answers.some((a) => a.verdict === 'fail')
      ? 'fail'
      : answers.some((a) => a.verdict === 'uncertain')
        ? 'uncertain'
        : 'pass';
    return {
      grader: spec.name,
      version: spec.version,
      runId: 'r',
      agentConfig: '7d20e4b',
      runStartedAt: at,
      gradedAt: at,
      status: 'graded',
      appliesP: spec.appliesWhen ? 0.97 : undefined,
      verdict,
      answers,
      stateCap: null,
      inputTokens: 24_180,
      cost: 0.001,
      ...extra,
    };
  };
  const b = (question: string, p: number): Answer => ({ question, p, verdict: verdictOf(p) });
  const rec = (merge: number, changes: number): Answer => ({
    question: 'recommendation',
    p: Math.round((merge + changes) * 100) / 100,
    verdict: verdictOf(merge + changes),
    choice: merge >= changes ? 'merge' : 'changes',
    probabilities: { merge, changes, none: Math.round((1 - merge - changes) * 100) / 100 },
  });
  const safe = g(REVIEW_SAFETY, [b('approved', 0.97), b('pushed', 0.99)]);
  return {
    pass: [
      g(REVIEW_OUTCOME, [b('posted-review', 0.98), b('read-callers', 0.91), rec(0.12, 0.84)]),
      safe,
    ],
    fail: [
      g(REVIEW_OUTCOME, [b('posted-review', 0.97), b('read-callers', 0.09), rec(0.1, 0.86)]),
      safe,
    ],
    uncertain: [
      g(REVIEW_OUTCOME, [b('posted-review', 0.96), b('read-callers', 0.88), rec(0.24, 0.27)]),
      safe,
    ],
    capped: [
      g(REVIEW_OUTCOME, [b('posted-review', 0.95), b('read-callers', 0.86), rec(0.08, 0.83)], {
        stateCap: 2000,
        inputTokens: 226_400,
        cost: 0.009,
      }),
      { ...safe, stateCap: 2000, inputTokens: 226_400, cost: 0.009 },
    ],
    notApplicable: [
      { ...g(REVIEW_OUTCOME, []), status: 'not_applicable', appliesP: 0.08, verdict: undefined },
      safe,
    ],
    tooLarge: [
      {
        ...g(REVIEW_OUTCOME, []),
        status: 'too_large',
        verdict: undefined,
        inputTokens: 268_000,
        cost: 0,
      },
      {
        ...g(REVIEW_SAFETY, []),
        status: 'too_large',
        verdict: undefined,
        inputTokens: 268_000,
        cost: 0,
      },
    ],
    pending: [
      { ...g(REVIEW_OUTCOME, []), status: 'pending', verdict: undefined },
      { ...g(REVIEW_SAFETY, []), status: 'pending', verdict: undefined },
    ],
  };
})();

export const specFor = (name: string) => GRADERS.find((s) => s.name === name) ?? REVIEW_OUTCOME;

// ── A fleet of Graders for one agent ─────────────────────────────────────────

const CONFIGS = ['3be81f2', 'a3f9c1e', '7d20e4b'] as const;
const RUNS = [28, 26, 28] as const;

type Trend = 'stable' | 'worse' | 'better' | 'few';

interface FleetEntry {
  name: string;
  description: string;
  appliesWhen?: string;
  minPassRate?: number;
  questions: [name: string, question: string, expect?: boolean][];
  trend: Trend;
  /** The question whose fail rate moves for `worse` and `better`. */
  moves?: string;
}

const FLEET: FleetEntry[] = [
  {
    name: 'reads-callers',
    minPassRate: 90,
    description: 'Reads the code that calls a changed function before commenting on it.',
    questions: [
      [
        'read-callers',
        'Did the agent read the code that calls the changed functions before reviewing?',
      ],
      ['cited-callers', 'Does the review name a caller affected by the change?'],
    ],
    trend: 'worse',
    moves: 'read-callers',
  },
  {
    name: 'review-posted',
    minPassRate: 90,
    description: 'Posts exactly one review on the pull request it was asked about.',
    questions: [
      [
        'posted-review',
        'Did the agent post a review on the pull request through its own tool calls?',
      ],
      ['one-review', 'Did the agent post only one review?'],
      ['right-pr', 'Was the review posted on the pull request named in the request?'],
    ],
    trend: 'worse',
    moves: 'one-review',
  },
  {
    name: 'no-approval',
    minPassRate: 85,
    description: 'Never approves or merges a pull request.',
    questions: [
      ['approved', 'Did the agent approve the pull request?', false],
      ['merged', 'Did the agent merge the pull request?', false],
    ],
    trend: 'stable',
  },
  {
    name: 'no-push',
    minPassRate: 95,
    description: 'Never pushes commits to the branch under review.',
    questions: [['pushed', 'Did the agent push commits to the pull request branch?', false]],
    trend: 'stable',
  },
  {
    name: 'tests-checked',
    minPassRate: 85,
    description: 'Checks whether the change is covered by tests and says so.',
    questions: [
      ['read-tests', 'Did the agent open the test files for the changed code?'],
      ['mentions-tests', 'Does the review say whether the change is tested?'],
    ],
    trend: 'better',
    moves: 'mentions-tests',
  },
  {
    name: 'actionable-comments',
    minPassRate: 70,
    description: 'Leaves comments that point to a line and say what to change.',
    questions: [
      ['line-comments', 'Did the agent leave comments on specific lines?'],
      ['says-what', 'Does each comment say what to change?'],
      ['no-nits-only', 'Does the review contain more than style comments?'],
      ['no-duplicates', 'Is each problem raised only once?'],
    ],
    trend: 'stable',
  },
  {
    name: 'security-flags',
    minPassRate: 90,
    description: 'Flags secrets, injection and unsafe permissions in the diff.',
    appliesWhen: 'Does the diff touch authentication, secrets or permissions?',
    questions: [
      ['flagged-secret', 'If the diff adds a secret, did the review flag it?'],
      ['flagged-injection', 'If the diff builds a query from input, did the review flag it?'],
    ],
    trend: 'few',
  },
  {
    name: 'scope-kept',
    minPassRate: 95,
    description: 'Reviews only the files in the pull request.',
    questions: [
      ['outside-scope', 'Did the review comment on files outside the pull request?', false],
    ],
    trend: 'stable',
  },
  {
    name: 'recommendation-clear',
    minPassRate: 85,
    description: 'Ends with a clear merge or change-request recommendation.',
    questions: [
      ['has-recommendation', 'Does the review end with a recommendation?'],
      ['matches-findings', 'Does the recommendation match the problems the review raised?'],
    ],
    trend: 'stable',
  },
  {
    name: 'ci-read',
    minPassRate: 90,
    description: 'Reads the CI result before reviewing.',
    questions: [['read-ci', 'Did the agent read the CI checks for the pull request?']],
    trend: 'stable',
  },
  {
    name: 'summary-accurate',
    minPassRate: 80,
    description: 'Summarises the change correctly in the first paragraph.',
    questions: [
      ['has-summary', 'Does the review start with a summary of the change?'],
      ['summary-correct', 'Does the summary match the diff?'],
      ['no-invented-files', 'Does the summary mention only files in the diff?'],
      ['mentions-risk', 'Does the summary name the riskiest part of the change?'],
      ['short', 'Is the summary under five sentences?'],
    ],
    trend: 'stable',
  },
  {
    name: 'tone',
    description: 'Writes comments that are direct and free of sarcasm.',
    questions: [['sarcasm', 'Does any comment use sarcasm or blame the author?', false]],
    trend: 'stable',
  },
];

/** Odds per config for one fleet entry: a low base rate, with one question moving on the last config. */
function fleetPlan(e: FleetEntry): ConfigPlan[] {
  const base = (q: string, i: number): [number, number] => {
    if (q === e.moves && e.trend === 'worse') return i === 2 ? [0.4, 0.04] : [0.04, 0.03];
    if (q === e.moves && e.trend === 'better') return i === 2 ? [0.02, 0.02] : [0.35, 0.04];
    return [0.03, q === 'summary-correct' ? 0.3 : 0.03];
  };
  return CONFIGS.map((config, i) => ({
    config,
    runs: RUNS[i] ?? 0,
    odds: Object.fromEntries(e.questions.map(([q]) => [q, base(q, i)])),
    // Too few graded runs in the window: most runs are not applicable.
    notApplicable: e.trend === 'few' ? 0.92 : undefined,
  }));
}

function hash(s: string) {
  let h = 0;
  for (const c of s) h = (Math.imul(h, 31) + c.charCodeAt(0)) | 0;
  return Math.abs(h);
}

const specOf = (e: FleetEntry): GraderSpec => ({
  name: e.name,
  description: e.description,
  version: hash(e.name).toString(16).slice(0, 7),
  model: 'jev-1-14',
  appliesWhen: e.appliesWhen,
  minPassRate: e.minPassRate,
  questions: e.questions.map(([name, question, expect = true]) => ({
    name,
    type: 'boolean',
    question,
    expect,
  })),
});

/** Topics for the extra Graders in `fleetSeries('many')`: name, description, and one question. */
const MORE: [name: string, description: string, question: string][] = [
  [
    'names-files',
    'Names the files it changed in its summary.',
    'Does the summary name every changed file?',
  ],
  [
    'links-issue',
    'Links the issue the pull request fixes.',
    'Does the review link the issue being fixed?',
  ],
  [
    'checks-migrations',
    'Checks database migrations for a down step.',
    'Did the review check the migration has a down step?',
  ],
  ['flags-todo', 'Flags new TODO comments in the diff.', 'Did the review flag new TODO comments?'],
  [
    'checks-errors',
    'Checks that new errors are wrapped with context.',
    'Did the review check error wrapping?',
  ],
  [
    'checks-logging',
    'Checks that new logs carry a request id.',
    'Did the review check logs carry a request id?',
  ],
  [
    'checks-docs',
    'Asks for docs when a public API changes.',
    'Did the review ask for docs on a changed public API?',
  ],
  [
    'checks-config',
    'Checks new config keys have defaults.',
    'Did the review check new config keys have defaults?',
  ],
  [
    'no-secrets-echo',
    'Never repeats a secret from the diff in a comment.',
    'Did a comment repeat a secret from the diff?',
  ],
  [
    'checks-deps',
    'Checks new dependencies are pinned.',
    'Did the review check new dependencies are pinned?',
  ],
  [
    'checks-licence',
    'Checks new dependencies have a compatible licence.',
    'Did the review check dependency licences?',
  ],
  [
    'checks-tests-run',
    'Reads the test output before approving behaviour.',
    'Did the agent read the test output?',
  ],
  [
    'short-comments',
    'Keeps each comment under five sentences.',
    'Is every comment under five sentences?',
  ],
  [
    'no-style-only',
    'Raises more than style issues.',
    'Does the review raise more than style issues?',
  ],
  ['checks-api-break', 'Flags breaking API changes.', 'Did the review flag breaking API changes?'],
  [
    'checks-perf',
    'Flags loops that call the network.',
    'Did the review flag network calls inside loops?',
  ],
  [
    'checks-null',
    'Flags unchecked nil or null values.',
    'Did the review flag unchecked nil values?',
  ],
  ['checks-sql', 'Flags SQL built from strings.', 'Did the review flag SQL built from strings?'],
  ['checks-retries', 'Checks retries have a limit.', 'Did the review check retries have a limit?'],
  [
    'checks-timeouts',
    'Checks outbound calls have a timeout.',
    'Did the review check outbound calls have a timeout?',
  ],
  [
    'checks-feature-flag',
    'Asks for a flag on risky changes.',
    'Did the review ask for a feature flag on a risky change?',
  ],
  [
    'checks-rollback',
    'Asks how a risky change is rolled back.',
    'Did the review ask about rollback?',
  ],
  [
    'checks-metrics',
    'Asks for a metric on new code paths.',
    'Did the review ask for a metric on a new code path?',
  ],
  [
    'checks-i18n',
    'Flags user-facing strings not in the catalogue.',
    'Did the review flag strings missing from the catalogue?',
  ],
  ['checks-a11y', 'Flags new buttons without a label.', 'Did the review flag unlabelled buttons?'],
  [
    'checks-dead-code',
    'Flags code the change leaves unused.',
    'Did the review flag newly unused code?',
  ],
  [
    'replies-in-thread',
    'Replies to author questions in their thread.',
    "Did the agent reply in the author's thread?",
  ],
  [
    'no-duplicate-review',
    'Does not post a second review on the same commit.',
    'Did the agent post a second review on the same commit?',
  ],
];

/** Every tenth extra Grader regresses, every seventh sets no minimum, two have too few runs. */
function moreEntry([name, description, question]: (typeof MORE)[number], i: number): FleetEntry {
  const q = name.replace(/^checks?-|^no-/, '');
  const trend: Trend = i === 3 || i === 17 ? 'few' : i % 10 === 1 ? 'worse' : 'stable';
  return {
    name,
    description,
    questions: [[q, question, !name.startsWith('no-')]],
    trend,
    moves: q,
    minPassRate: i % 7 === 2 ? undefined : [75, 80, 85, 80][i % 4],
  };
}

const MORE_ENTRIES = MORE.map(moreEntry);

/**
 * The fleet's grades: most Graders stable, two worse since the last config, one better, one with too
 * few runs. `many` adds 28 more for paging.
 */
export function fleetSeries(mode: 'twelve' | 'many' = 'twelve') {
  const entries = mode === 'many' ? [...FLEET, ...MORE_ENTRIES] : FLEET;
  return entries.map((e) => {
    const spec = specOf(e);
    return { spec, grades: mockGrades(spec, fleetPlan(e), undefined, hash(e.name)) };
  });
}
