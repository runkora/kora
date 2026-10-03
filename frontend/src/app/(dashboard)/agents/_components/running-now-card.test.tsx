import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { RunningNowCard, isLiveRun } from './running-now-card';
import { SCALE_RUN_ROWS } from './agents-mock-data';

describe('RunningNowCard', () => {
  const live = SCALE_RUN_ROWS.filter(isLiveRun);

  it('lists ten runs and counts the rest while the header counts all of them', () => {
    expect(live.length).toBeGreaterThan(10);
    renderWithProviders(<RunningNowCard runs={live} />);

    expect(screen.getByText(`${live.length} runs in progress`)).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'Open' })).toHaveLength(10);
    expect(screen.getByText(`+${live.length - 10} more runs not shown`)).toBeInTheDocument();
  });

  it('shows no count line when every run fits', () => {
    renderWithProviders(<RunningNowCard runs={live.slice(0, 10)} />);
    expect(screen.queryByText(/not shown/)).not.toBeInTheDocument();
  });
});
