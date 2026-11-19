import { describe, expect, it } from 'vitest';
import type { GraderQuestion } from '@runkora/shared/api';
import { gradeFilterOf, gradeRunsHref, passRate, passesOn } from './grades';

const question = (over: Partial<GraderQuestion>): GraderQuestion => ({
  name: 'q',
  type: 'boolean',
  question: 'Q?',
  answers: [],
  failed: 0,
  uncertain: 0,
  runs: [],
  ...over,
});

describe('passesOn', () => {
  it('names the expected boolean answer', () => {
    expect(passesOn(question({ expect: true }))).toBe('Passes on Yes');
    expect(passesOn(question({ expect: false }))).toBe('Passes on No');
  });

  it("names a choice's accepted options by description", () => {
    const q = question({
      type: 'choice',
      options: [
        { name: 'merge', description: 'Safe to merge' },
        { name: 'none', description: 'None of the above' },
      ],
      accept: ['merge'],
    });
    expect(passesOn(q)).toBe('Passes on Safe to merge');
  });

  it("names a scale's accepted levels by description", () => {
    const q = question({
      type: 'scale',
      levels: [
        { name: 'no-pr', description: 'No pull request was opened' },
        { name: 'one', description: 'One round' },
      ],
      accept: ['one'],
    });
    expect(passesOn(q)).toBe('Passes on One round');
  });
});

describe('passRate', () => {
  it('rounds to a whole percent and has none without graded runs', () => {
    expect(passRate(2, 3)).toBe(67);
    expect(passRate(0, 0)).toBeNull();
  });
});

describe('grade filter URL', () => {
  it('round-trips through the Runs tab link', () => {
    const href = gradeRunsHref('code-reviewer', {
      grader: 'review-outcome',
      question: 'read-callers',
      verdict: 'fail',
    });
    expect(href).toBe(
      '/agents/code-reviewer?tab=runs&grader=review-outcome&question=read-callers&verdict=fail',
    );
    expect(gradeFilterOf(new URL(href, 'http://x').searchParams)).toEqual({
      grader: 'review-outcome',
      question: 'read-callers',
      verdict: 'fail',
    });
  });

  it('drops skipped without a question, which kora-agents rejects', () => {
    expect(gradeFilterOf(new URLSearchParams('grader=g&verdict=skipped'))?.verdict).toBeUndefined();
    expect(gradeFilterOf(new URLSearchParams('grader=g&question=q&verdict=skipped'))?.verdict).toBe(
      'skipped',
    );
  });

  it('has no filter without a Grader and drops an unknown verdict', () => {
    expect(gradeFilterOf(new URLSearchParams('verdict=fail'))).toBeNull();
    expect(gradeFilterOf(new URLSearchParams('grader=g&verdict=bad'))).toEqual({
      grader: 'g',
      question: undefined,
      verdict: undefined,
    });
  });
});
