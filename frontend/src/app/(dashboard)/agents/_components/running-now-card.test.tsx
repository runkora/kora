import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { RUNNING_ROWS } from '../_lib/spend';
import { RunningNowCard, isLiveRun } from './running-now-card';
import { RUN_ROWS } from './agents-mock-data';

describe('RunningNowCard', () => {
  const template = RUN_ROWS.find(isLiveRun);
  const runs = (n: number) =>
    Array.from({ length: n }, (_, i) => ({ ...template!, id: `run-${i}` }));

  it('lists the first runs and counts the rest while the header counts all of them', () => {
    const live = runs(RUNNING_ROWS + 2);
    renderWithProviders(<RunningNowCard runs={live} />);

    expect(screen.getByText(`${live.length} runs in progress`)).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'Open' })).toHaveLength(RUNNING_ROWS);
    expect(screen.getByText('+2 more runs not shown')).toBeInTheDocument();
  });

  it('shows no count line when every run fits', () => {
    renderWithProviders(<RunningNowCard runs={runs(RUNNING_ROWS)} />);
    expect(screen.queryByText(/not shown/)).not.toBeInTheDocument();
  });
});
