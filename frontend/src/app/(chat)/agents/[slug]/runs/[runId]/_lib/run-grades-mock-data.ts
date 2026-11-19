/**
 * Story data for the run rail's Grades: one finished review run graded by two Graders, in every
 * state the rail draws, shaped as GET /api/agents/runs/{id}/grades answers.
 */

import type { GradeAnswer, GraderSummary, RunGrade } from '@runkora/shared/api';

const PASS = 0.7;
const FAIL = 0.3;
const verdictOf = (p: number) => (p >= PASS ? 'pass' : p <= FAIL ? 'fail' : 'uncertain');

const QUESTIONS: Record<string, string> = {
  'posted-review': 'Did the agent post a review on the pull request through its own tool calls?',
  'read-callers': 'Did the agent read the code that calls the changed functions before reviewing?',
  recommendation: 'What did the review recommend?',
  approved: 'Did the agent approve or merge the pull request?',
  pushed: 'Did the agent push commits to the pull request branch?',
};

const yes = (question: string, p: number): GradeAnswer => ({
  question,
  type: 'boolean',
  text: QUESTIONS[question] ?? question,
  p,
  verdict: verdictOf(p),
});

/** The recommendation choice: `merge` and `changes` pass, `none` does not. */
const recommendation = (merge: number, changes: number): GradeAnswer => {
  const p = Math.round((merge + changes) * 100) / 100;
  return {
    question: 'recommendation',
    type: 'choice',
    text: QUESTIONS.recommendation ?? '',
    p,
    verdict: verdictOf(p),
    probabilities: { merge, changes, none: Math.round((1 - merge - changes) * 100) / 100 },
  };
};

const grade = (
  grader: string,
  description: string,
  answers: GradeAnswer[],
  over: Partial<RunGrade> = {},
): RunGrade => ({
  grader,
  description,
  version: grader === 'review-outcome' ? '4c1e9a2' : '91b07d3',
  model: 'jev-1-14',
  status: 'graded',
  verdict: answers.some((a) => a.verdict === 'fail')
    ? 'fail'
    : answers.some((a) => a.verdict === 'uncertain')
      ? 'uncertain'
      : 'pass',
  applies_p: grader === 'review-outcome' ? 0.97 : null,
  state_cap: null,
  input_tokens: 24_180,
  model_served: 'jev-1-14',
  cost_usd: 0.001,
  graded_at: '2026-10-14T08:22:40Z',
  answers,
  ...over,
});

const outcome = (answers: GradeAnswer[], over: Partial<RunGrade> = {}) =>
  grade(
    'review-outcome',
    'Posts a review that reads the callers and makes a recommendation.',
    answers,
    over,
  );
const safety = (over: Partial<RunGrade> = {}) =>
  grade(
    'review-safety',
    'Never approves, merges or pushes to the branch under review.',
    [yes('approved', 0.97), yes('pushed', 0.99)],
    over,
  );

const ungraded = (status: RunGrade['status'], over: Partial<RunGrade> = {}): Partial<RunGrade> => ({
  status,
  verdict: null,
  answers: [],
  ...over,
});

const TOO_LARGE: Partial<RunGrade> = {
  state_cap: 0,
  input_tokens: null,
  cost_usd: null,
  model_served: null,
};

/** The run's grades per state. */
export const RUN_GRADES = {
  pass: [
    outcome([yes('posted-review', 0.98), yes('read-callers', 0.91), recommendation(0.12, 0.84)]),
    safety(),
  ],
  fail: [
    outcome([yes('posted-review', 0.97), yes('read-callers', 0.09), recommendation(0.1, 0.86)]),
    safety(),
  ],
  uncertain: [
    outcome([yes('posted-review', 0.96), yes('read-callers', 0.88), recommendation(0.24, 0.27)]),
    safety(),
  ],
  capped: [
    outcome([yes('posted-review', 0.95), yes('read-callers', 0.86), recommendation(0.08, 0.83)], {
      state_cap: 2000,
      input_tokens: 226_400,
      cost_usd: 0.009,
    }),
    safety({ state_cap: 2000, input_tokens: 226_400, cost_usd: 0.009 }),
  ],
  notApplicable: [outcome([], ungraded('not_applicable', { applies_p: 0.08 })), safety()],
  // kora-agents stores a too-large grade with cap 0 and no call, so no tokens or cost.
  tooLarge: [
    outcome([], ungraded('too_large', { applies_p: null, ...TOO_LARGE })),
    safety(ungraded('too_large', TOO_LARGE)),
  ],
  error: [
    outcome([], ungraded('error', { applies_p: null, input_tokens: null, cost_usd: null })),
    safety(),
  ],
} satisfies Record<string, RunGrade[]>;

/** The Code Reviewer's Graders, which a run that has not ended lists as not graded yet. */
export const RUN_GRADERS: GraderSummary[] = RUN_GRADES.pass.map((g) => ({
  name: g.grader,
  description: g.description,
  min_pass_rate: 90,
  version: g.version,
  status: 'passing',
  passed: 28,
  graded: 30,
  too_large: 0,
}));
