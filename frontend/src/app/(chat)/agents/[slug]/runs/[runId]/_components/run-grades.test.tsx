import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import type { RunGrade } from '@runkora/shared/api';
import { RUN_GRADES } from '../_lib/run-grades-mock-data';
import { RailGrades } from './run-grades';

const ended = (grades: RunGrade[]) =>
  renderWithProviders(<RailGrades grades={{ ended: true, grades }} />);

describe('RailGrades', () => {
  it("shows each Grader's verdict and each question's verdict and p", () => {
    ended(RUN_GRADES.fail);
    expect(screen.getByText('review-outcome')).toBeInTheDocument();
    expect(screen.getByText('Fail')).toBeInTheDocument();
    expect(screen.getByText('Pass')).toBeInTheDocument();
    expect(screen.getByText('read-callers')).toBeInTheDocument();
    expect(screen.getByText('0.09')).toBeInTheDocument();
    expect(screen.getByLabelText('Fail')).toBeInTheDocument();
  });

  it('shows the distribution of an uncertain choice answer', () => {
    ended(RUN_GRADES.uncertain);
    expect(screen.getByText('Uncertain')).toBeInTheDocument();
    expect(screen.getByText('none 0.49 · changes 0.27 · merge 0.24')).toBeInTheDocument();
  });

  it('sums tokens and cost over the Graders that called the model and names it from the API', () => {
    ended(RUN_GRADES.pass);
    expect(screen.getByText('jev-1-14 · 48k input tokens · $0.002')).toBeInTheDocument();
  });

  it('names the state cap, and a cap of 0 as no bodies', () => {
    ended(RUN_GRADES.capped);
    expect(screen.getByText('Tool results capped at 2,000 characters')).toBeInTheDocument();
    const [first, second] = RUN_GRADES.pass;
    ended([{ ...first!, state_cap: 0 }, second!]);
    expect(screen.getByText('Tool result bodies left out')).toBeInTheDocument();
  });

  it('leaves the cost out when a Grader model has no price', () => {
    const [first, second] = RUN_GRADES.pass;
    ended([{ ...first!, cost_usd: null }, second!]);
    expect(screen.getByText('jev-1-14 · 48k input tokens')).toBeInTheDocument();
  });

  it('labels the grades that have no verdict', () => {
    ended(RUN_GRADES.notApplicable);
    expect(screen.getByText('Not applicable')).toBeInTheDocument();
    expect(screen.getByText('Applies p 0.08')).toBeInTheDocument();
  });

  it('counts a not applicable grade in the footnote and names no cap for a too large grade', () => {
    const [outcome] = RUN_GRADES.notApplicable;
    const [, tooLarge] = RUN_GRADES.tooLarge;
    ended([outcome!, tooLarge!]);
    expect(screen.getByText('jev-1-14 · 24k input tokens · $0.001')).toBeInTheDocument();
    expect(screen.queryByText(/Tool result/)).toBeNull();
  });

  it('labels too large and error grades', () => {
    ended(RUN_GRADES.tooLarge);
    expect(screen.getAllByText('Too large to grade')).toHaveLength(2);
    // A too-large state was not graded, so the footnote names no model call and no cap.
    expect(screen.queryByText(/input tokens|Tool result/)).toBeNull();
    ended(RUN_GRADES.error);
    expect(screen.getByText('Grading failed')).toBeInTheDocument();
  });

  it('shows a skipped question without a p', () => {
    const [first] = RUN_GRADES.pass;
    ended([
      {
        ...first!,
        answers: [
          {
            question: 'rounds',
            type: 'scale',
            text: 'How many rounds?',
            p: null,
            verdict: 'skipped',
          },
        ],
      },
    ]);
    expect(screen.getByText('Skipped')).toBeInTheDocument();
  });

  it("names a live run's Graders as not graded yet", () => {
    renderWithProviders(<RailGrades grades={{ ended: false, graders: ['review-outcome'] }} />);
    expect(screen.getByText('review-outcome')).toBeInTheDocument();
    expect(screen.getByText('Not graded yet')).toBeInTheDocument();
  });

  it('draws nothing without Graders', () => {
    const { container } = renderWithProviders(<RailGrades grades={{ ended: true, grades: [] }} />);
    expect(container.textContent).toBe('');
  });
});
