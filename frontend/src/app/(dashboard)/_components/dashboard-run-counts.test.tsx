/** The dashboard's Active Runs card reads the server's run counts, not the recent runs it lists. */
import { describe, it, expect, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/',
  useSearchParams: () => new URLSearchParams(),
}));

import { DashboardPage } from './dashboard-page';
import { RUN_ROWS } from '../agents/_components/agents-mock-data';

const props = {
  agentTypes: [],
  connections: [],
  environments: [],
  analytics: null,
  tenantCap: null,
};

function activeRunsCard() {
  return screen.getByText('Active Runs').closest('[data-slot="stat-card"]') as HTMLElement;
}

describe('Dashboard run counts', () => {
  it('shows the live and total counts from the server', () => {
    renderWithProviders(
      <DashboardPage
        {...props}
        recentRuns={RUN_ROWS.slice(0, 2)}
        runCounts={{
          total: 1234,
          by_state: {},
          live: 17,
          live_by_agent_type: {},
          longest_live_ms: 0,
        }}
      />,
    );
    const card = within(activeRunsCard());
    expect(card.getByText('17')).toBeInTheDocument();
    expect(card.getByText('1234 total')).toBeInTheDocument();
  });

  it('says the counts are unavailable when they could not be read', () => {
    renderWithProviders(<DashboardPage {...props} recentRuns={RUN_ROWS} runCounts={null} />);
    const card = within(activeRunsCard());
    expect(card.getByText('–')).toBeInTheDocument();
    expect(card.getByText('unavailable')).toBeInTheDocument();
  });
});
