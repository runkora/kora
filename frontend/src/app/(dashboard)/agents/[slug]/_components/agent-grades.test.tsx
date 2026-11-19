import { describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/agents/code-reviewer',
  useSearchParams: () => new URLSearchParams(),
}));

import { GraderPage, GradersList } from './agent-grades';
import { graderFleet } from './agent-grades-mock-data';

const fleet = graderFleet();

describe('GradersList', () => {
  it('shows each status as the API reads it', () => {
    renderWithProviders(<GradersList graders={fleet.list} onOpen={vi.fn()} />);
    const failing = fleet.list.filter((g) => g.status === 'failing').length;
    expect(
      screen.getByText(`${failing} of ${fleet.list.length} Graders failing`),
    ).toBeInTheDocument();
    // security-flags has a minimum and under 15 graded runs.
    const few = screen.getByText('security-flags').closest('[role="row"]') as HTMLElement;
    expect(within(few).getByText('Not enough runs')).toBeInTheDocument();
    // tone has no minimum, so its rate has no "needs".
    const tone = screen.getByText('tone').closest('[role="row"]') as HTMLElement;
    expect(within(tone).getByText(/% passed$/)).toBeInTheDocument();
    expect(within(tone).queryByText(/needs/)).toBeNull();
  });

  it('adds the too large count to the graded runs', () => {
    const [first, ...rest] = fleet.list;
    renderWithProviders(
      <GradersList graders={[{ ...first!, too_large: 1 }, ...rest]} onOpen={vi.fn()} />,
    );
    const row = screen.getByText(first!.name).closest('[role="row"]') as HTMLElement;
    expect(within(row).getByText(`${first!.graded} · 1 too large`)).toBeInTheDocument();
  });

  it('filters to failing Graders', async () => {
    renderWithProviders(<GradersList graders={fleet.list} onOpen={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: /^Failing/ }));
    expect(screen.getByText('reads-callers')).toBeInTheDocument();
    expect(screen.queryByText('tone')).toBeNull();
  });

  it('opens a Grader by name', async () => {
    const onOpen = vi.fn();
    renderWithProviders(<GradersList graders={fleet.list} onOpen={onOpen} />);
    await userEvent.click(screen.getByText('no-push'));
    expect(onOpen).toHaveBeenCalledWith('no-push');
  });

  it('shows the empty state without Graders', () => {
    renderWithProviders(<GradersList graders={[]} onOpen={vi.fn()} />);
    expect(screen.getByText('No Graders')).toBeInTheDocument();
  });
});

describe('GraderPage', () => {
  it('lists questions with counts and links their failed runs to the Runs tab', () => {
    const grader = fleet.detail['reads-callers']!;
    renderWithProviders(<GraderPage grader={grader} slug="code-reviewer" onBack={vi.fn()} />);
    expect(
      screen.getByText(`${grader.passed} of the last ${grader.graded} runs passed`, {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(/needs 90%/)).toBeInTheDocument();
    const link = screen.getAllByRole('link', { name: /All failed runs for this question/ })[0];
    expect(link).toHaveAttribute(
      'href',
      `/agents/code-reviewer?tab=runs&grader=reads-callers&question=${grader.questions[0]!.name}&verdict=fail`,
    );
  });

  it('lists a single question with its runs after it', () => {
    const grader = fleet.detail['ci-read']!;
    renderWithProviders(<GraderPage grader={grader} slug="code-reviewer" onBack={vi.fn()} />);
    expect(screen.queryByText('Verdicts')).toBeNull();
    const only = grader.questions[0]!;
    if (only.runs.length)
      expect(
        screen.getByText(`Failed or uncertain runs (${only.runs.length})`),
      ).toBeInTheDocument();
    else
      expect(
        screen.getByText(`No failed or uncertain runs in the last ${grader.graded}`),
      ).toBeInTheDocument();
  });

  it('counts runs too large to grade and says when a question has no failed or uncertain runs', () => {
    const base = fleet.detail['reads-callers']!;
    const quiet = { ...base.questions[0]!, failed: 0, uncertain: 0, runs: [] };
    const grader = { ...base, too_large: 2, questions: [base.questions[1]!, quiet] };
    renderWithProviders(<GraderPage grader={grader} slug="code-reviewer" onBack={vi.fn()} />);
    expect(screen.getByText(/· 2 too large to grade/)).toBeInTheDocument();
    expect(screen.getByText('No failed or uncertain runs')).toBeInTheDocument();
    expect(screen.queryByText(/Uncertain on/)).toBeNull();
  });

  it('reads "not enough runs" under the minimum count', () => {
    renderWithProviders(
      <GraderPage grader={fleet.detail['security-flags']!} slug="code-reviewer" onBack={vi.fn()} />,
    );
    expect(screen.getByText('Not enough runs')).toBeInTheDocument();
    expect(screen.getByText(/graded runs · not enough runs/)).toBeInTheDocument();
  });

  it('shows five runs and then all of them', async () => {
    const base = fleet.detail['reads-callers']!;
    const runs = Array.from({ length: 7 }, (_, i) => ({
      run_id: `run-${i}`,
      title: `Run ${i}`,
      started_at: '2026-11-18T00:00:00Z',
      verdict: 'fail' as const,
    }));
    const grader = {
      ...base,
      questions: [{ ...base.questions[0]!, failed: 7, uncertain: 0, runs }],
    };
    renderWithProviders(<GraderPage grader={grader} slug="code-reviewer" onBack={vi.fn()} />);
    expect(screen.queryByText('Run 5')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Show all 7' }));
    expect(screen.getByText('Run 6')).toBeInTheDocument();
  });
});
