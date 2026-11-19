/**
 * Story data for the Grades tab: a fleet of Graders for one agent with generated grades, read into
 * the API's shapes the way kora-agents does (the last 30 graded runs, the 15-run minimum, failing
 * first). Deterministic, so every render shows the same runs.
 */

import type {
  GraderDetail,
  GraderFlaggedRun,
  GraderQuestion,
  GraderStatus,
  GraderSummary,
  GradeVerdict,
} from '@runkora/shared/api';
import { WINDOW } from '../../_lib/grades';

/** Below this many graded runs a Grader with a minimum reads "Not enough runs". */
const MIN_RUNS = 15;

interface FleetEntry {
  name: string;
  description: string;
  appliesWhen?: string;
  minPassRate?: number;
  questions: [name: string, question: string, expect?: boolean][];
  /** `worse` and `better` move one question's fail rate on the newest runs; `few` are mostly not applicable. */
  trend: 'stable' | 'worse' | 'better' | 'few';
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

/** Extra Graders for paging: name, description and one question. */
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
const MORE_ENTRIES: FleetEntry[] = MORE.map(([name, description, question], i) => {
  const q = name.replace(/^checks?-|^no-/, '');
  return {
    name,
    description,
    questions: [[q, question, !name.startsWith('no-')]],
    trend: i === 3 || i === 17 ? 'few' : i % 10 === 1 ? 'worse' : 'stable',
    moves: q,
    minPassRate: i % 7 === 2 ? undefined : [75, 80, 85, 80][i % 4],
  };
});

function rng(seed: number) {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function hash(s: string) {
  let h = 0;
  for (const c of s) h = (Math.imul(h, 31) + c.charCodeAt(0)) | 0;
  return Math.abs(h);
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

/** A run's title; unique for the first 90 runs. */
const titleFor = (i: number) =>
  `Review ${REPOS[i % REPOS.length]}/${1400 + i}: ${ACTIONS[i % ACTIONS.length]} ${
    AREAS[Math.floor(i / ACTIONS.length) % AREAS.length]
  }`;

/** One generated graded run: its verdict and each question's answer verdict. */
interface MockRun {
  runId: string;
  title: string;
  startedAt: string;
  verdict: GradeVerdict;
  answers: Record<string, GradeVerdict>;
}

/** The chance a question fails and the chance it is uncertain, on old runs and on the newest 28. */
function odds(e: FleetEntry, q: string, recent: boolean): [number, number] {
  if (q === e.moves && e.trend === 'worse') return recent ? [0.4, 0.04] : [0.04, 0.03];
  if (q === e.moves && e.trend === 'better') return recent ? [0.02, 0.02] : [0.35, 0.04];
  return [0.03, q === 'summary-correct' ? 0.3 : 0.03];
}

const RUNS = 82;
const END = Date.parse('2026-11-18T06:00:00Z');

/** A Grader's graded runs, newest first, and how many runs were too large to grade. */
function gradesOf(e: FleetEntry): { runs: MockRun[]; tooLarge: number } {
  const r = rng(hash(e.name));
  const step = (30 * 86_400_000) / RUNS;
  const runs: MockRun[] = [];
  let tooLarge = 0;
  for (let i = 0; i < RUNS; i++) {
    const roll = r();
    if (e.trend === 'few' && roll < 0.92) continue;
    if (roll > 0.985) {
      tooLarge += 1;
      continue;
    }
    const answers: Record<string, GradeVerdict> = {};
    for (const [q] of e.questions) {
      const [f, u] = odds(e, q, i >= RUNS - 28);
      const x = r();
      answers[q] = x < f ? 'fail' : x < f + u ? 'uncertain' : 'pass';
    }
    const verdicts = Object.values(answers);
    runs.push({
      runId: `run_${String(i + 1).padStart(4, '0')}`,
      title: titleFor(i + 1),
      startedAt: new Date(END - (RUNS - i) * step).toISOString(),
      verdict: verdicts.includes('fail')
        ? 'fail'
        : verdicts.includes('uncertain')
          ? 'uncertain'
          : 'pass',
      answers,
    });
  }
  return { runs: runs.reverse(), tooLarge };
}

function statusOf(min: number | undefined, passed: number, graded: number): GraderStatus {
  if (min === undefined) return 'no_minimum';
  if (graded < MIN_RUNS) return 'not_enough_runs';
  return passed * 100 >= min * graded ? 'passing' : 'failing';
}

function questionOf(
  [name, question, expect = true]: FleetEntry['questions'][number],
  window: MockRun[],
): GraderQuestion {
  const runs: GraderFlaggedRun[] = window.flatMap((w) => {
    const v = w.answers[name];
    return v === 'fail' || v === 'uncertain'
      ? [{ run_id: w.runId, title: w.title, started_at: w.startedAt, verdict: v }]
      : [];
  });
  return {
    name,
    type: 'boolean',
    question,
    expect,
    failed: runs.filter((x) => x.verdict === 'fail').length,
    uncertain: runs.filter((x) => x.verdict === 'uncertain').length,
    runs,
  };
}

function detailOf(e: FleetEntry): GraderDetail {
  const { runs, tooLarge } = gradesOf(e);
  const window = runs.slice(0, WINDOW);
  const passed = window.filter((w) => w.verdict === 'pass').length;
  return {
    name: e.name,
    description: e.description,
    min_pass_rate: e.minPassRate ?? null,
    version: hash(e.name).toString(16).slice(0, 7),
    status: statusOf(e.minPassRate, passed, window.length),
    passed,
    graded: window.length,
    too_large: tooLarge,
    model: 'jev-1-14',
    threshold: { pass: 0.7, fail: 0.3 },
    applies_when: e.appliesWhen,
    questions: e.questions
      .map((q) => questionOf(q, window))
      .sort(
        (a, b) => b.failed + b.uncertain - (a.failed + a.uncertain) || a.name.localeCompare(b.name),
      ),
  };
}

const summaryOf = ({
  name,
  description,
  min_pass_rate,
  version,
  status,
  passed,
  graded,
  too_large,
}: GraderDetail): GraderSummary => ({
  name,
  description,
  min_pass_rate,
  version,
  status,
  passed,
  graded,
  too_large,
});

/** Failing first, then by pass rate, lowest first; no graded runs last. */
function compare(a: GraderSummary, b: GraderSummary) {
  const failing = Number(b.status === 'failing') - Number(a.status === 'failing');
  if (failing) return failing;
  const none = Number(a.graded === 0) - Number(b.graded === 0);
  if (none) return none;
  return a.passed * b.graded - b.passed * a.graded || a.name.localeCompare(b.name);
}

/** A fleet's Graders as the list and detail endpoints answer them. */
export interface GraderFleet {
  list: GraderSummary[];
  detail: Record<string, GraderDetail>;
}

/** Twelve Graders; `many` adds 28 more for paging; `none` has no Graders. */
export function graderFleet(mode: 'twelve' | 'many' | 'none' = 'twelve'): GraderFleet {
  const entries = mode === 'none' ? [] : mode === 'many' ? [...FLEET, ...MORE_ENTRIES] : FLEET;
  const details = entries.map(detailOf);
  return {
    list: details.map(summaryOf).sort(compare),
    detail: Object.fromEntries(details.map((d) => [d.name, d])),
  };
}
