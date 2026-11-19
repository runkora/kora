/**
 * Labels for Grader results and the URL of the Runs tab filtered by grade. Every label names a
 * stored field in the Grader spec's vocabulary (docs/adr/2026-11-19-graders.md).
 */

import { APP_ROUTES } from '@runkora/lib/routing/routes';
import type {
  AnswerVerdict,
  GradeStatus,
  GraderQuestion,
  GraderQuestionType,
  GraderStatus,
  GradeVerdict,
} from '@runkora/shared/api';
import type { GradeFilter } from '../_services/agents-api';

/** A Grader's standing is read over its last `WINDOW` graded runs, kora-agents' `PassRateWindow`. */
export const WINDOW = 30;

export const VERDICT_LABEL: Record<AnswerVerdict, string> = {
  pass: 'Pass',
  fail: 'Fail',
  uncertain: 'Uncertain',
  skipped: 'Skipped',
};

/** Badge variants already used for run status, reused for verdicts. */
export const VERDICT_BADGE: Record<GradeVerdict, 'healthy' | 'error' | 'degraded'> = {
  pass: 'healthy',
  fail: 'error',
  uncertain: 'degraded',
};

export const VERDICT_ICON = {
  pass: { name: 'circle-check', color: 'status-healthy' },
  fail: { name: 'circle-x', color: 'status-error' },
  uncertain: { name: 'circle-alert', color: 'status-degraded' },
  skipped: { name: 'minus', color: 'subtle' },
} as const satisfies Record<AnswerVerdict, { name: string; color: string }>;

/** The label of a grade that has no verdict. */
export const GRADE_STATUS_LABEL: Record<Exclude<GradeStatus, 'graded'>, string> = {
  not_applicable: 'Not applicable',
  applies_uncertain: 'Applies: uncertain',
  too_large: 'Too large to grade',
  error: 'Grading failed',
};

/** Shown for a run that has not ended, from the run state. */
export const NOT_GRADED_YET = 'Not graded yet';

export const GRADER_STATUS_LABEL: Record<GraderStatus, string> = {
  failing: 'Failing',
  passing: 'Passing',
  no_minimum: 'No minimum',
  not_enough_runs: 'Not enough runs',
};

/** A Grader's visible name: its description, or its name without one. */
export const graderName = (g: { name: string; description: string }) => g.description || g.name;

export const QUESTION_TYPE_LABEL: Record<GraderQuestionType, string> = {
  boolean: 'Yes/No',
  choice: 'Choice',
  scale: 'Scale',
};

/** An answer as the pass condition names it: Yes or No for a boolean, else its description or name. */
export const answerLabel = (q: GraderQuestion, a: { name: string; description: string }) =>
  q.type === 'boolean' ? (a.name === 'yes' ? 'Yes' : 'No') : a.description || a.name;

/** A pass rate in whole percent; null with no graded runs. */
export const passRate = (passed: number, graded: number) =>
  graded ? Math.round((passed / graded) * 100) : null;

/** "Passes on Yes", "Passes on No", or "Passes on" and the accepted answers' descriptions. */
export function passesOn(q: GraderQuestion): string {
  if (q.type === 'boolean') return `Passes on ${q.expect === false ? 'No' : 'Yes'}`;
  const answers = q.options ?? q.levels ?? [];
  const names = (q.accept ?? []).map((n) => answers.find((o) => o.name === n)?.description ?? n);
  return `Passes on ${names.join(', ')}`;
}

/** The agent's Runs tab filtered by grade. */
export function gradeRunsHref(slug: string, filter: GradeFilter): string {
  const query = new URLSearchParams({ tab: 'runs', grader: filter.grader });
  if (filter.question) query.set('question', filter.question);
  if (filter.verdict) query.set('verdict', filter.verdict);
  return `${APP_ROUTES.agents.detail(slug)}?${query}`;
}

const isVerdict = (v: string | null): v is AnswerVerdict => v !== null && Object.hasOwn(VERDICT_LABEL, v);

/** The Runs tab's grade filter from the page URL; null without a Grader. */
export function gradeFilterOf(params: URLSearchParams): GradeFilter | null {
  const grader = params.get('grader');
  if (!grader) return null;
  const question = params.get('question') || undefined;
  const verdict = params.get('verdict');
  // kora-agents reads `skipped` only for a question.
  const known = isVerdict(verdict) && (verdict !== 'skipped' || !!question);
  return { grader, question, verdict: known ? verdict : undefined };
}
