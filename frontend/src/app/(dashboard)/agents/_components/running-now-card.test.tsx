import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { RUNNING_ROWS } from '../_lib/spend';
import { LIVE_RUN_STATES } from '../_types';
import { RunningNowCard } from './running-now-card';
import { RUN_ROWS } from './agents-mock-data';

describe('RunningNowCard', () => {
  const template = RUN_ROWS.find((r) => LIVE_RUN_STATES.includes(r.state));
  const runs = (n: number) =>
    Array.from({ length: n }, (_, i) => ({ ...template!, id: `run-${i}` }));
  const counts = (total: number) => ({
    running: total - 3,
    starting: 2,
    queued: 1,
    total,
    longestMs: 90_000,
  });

  it('lists the runs it was given and takes the totals from the server counts', () => {
    renderWithProviders(
      <RunningNowCard runs={runs(RUNNING_ROWS)} counts={counts(RUNNING_ROWS + 2)} />,
    );

    expect(screen.getByText(`${RUNNING_ROWS + 2} runs in progress`)).toBeInTheDocument();
    expect(
      screen.getByText(`${RUNNING_ROWS - 1} running · 2 starting · 1 queued`),
    ).toBeInTheDocument();
    expect(screen.getByText('Longest 1m 30s')).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'Open' })).toHaveLength(RUNNING_ROWS);
    expect(screen.getByText('+2 more runs not shown')).toBeInTheDocument();
  });

  it('shows no count line when every run fits', () => {
    renderWithProviders(<RunningNowCard runs={runs(4)} counts={counts(4)} />);
    expect(screen.queryByText(/not shown/)).not.toBeInTheDocument();
  });

  it('shows the empty line when nothing is live', () => {
    renderWithProviders(
      <RunningNowCard runs={[]} counts={{ ...counts(0), running: 0, starting: 0, queued: 0 }} />,
    );
    expect(screen.getByText(/Nothing is running/)).toBeInTheDocument();
  });
});

describe('RunningNowCard without server counts', () => {
  it('counts the listed runs and leaves out the breakdown', () => {
    const template = RUN_ROWS.find((r) => LIVE_RUN_STATES.includes(r.state))!;
    renderWithProviders(
      <RunningNowCard
        runs={[
          { ...template, id: 'a' },
          { ...template, id: 'b' },
        ]}
        counts={null}
      />,
    );
    expect(screen.getByText('2 runs in progress')).toBeInTheDocument();
    expect(screen.queryByText(/running ·/)).not.toBeInTheDocument();
  });
});
