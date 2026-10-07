import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/agents',
  useSearchParams: () => new URLSearchParams(),
}));

import { AgentsShell } from './agents-shell';
import { AGENT_ROWS, RUN_ROWS, mockRunArgs, mockRunStats } from './agents-mock-data';
import { mockInsightsResponse } from './agents-insights-mock-data';
import type { Period } from '../_lib/spend';

describe('Agents page insights', () => {
  it('renders the window it was given without fetching it, then fetches another period when selected', async () => {
    const requested: string[] = [];
    server.use(
      http.get('/api/agents/types/drafts', () => HttpResponse.json({ data: [] })),
      http.get('/api/agents/insights', ({ request }) => {
        const period = new URL(request.url).searchParams.get('period') ?? '';
        requested.push(period);
        return HttpResponse.json(mockInsightsResponse(period as Period));
      }),
    );
    renderWithProviders(
      <AgentsShell
        initialAgentTypes={AGENT_ROWS}
        {...mockRunArgs(RUN_ROWS)}
        initialError={null}
        initialInsights={mockInsightsResponse('7d')}
      />,
    );

    // First paint has the figures: the control and the spend are there at once.
    expect(screen.getByText('Last 7 days')).toBeInTheDocument();
    expect(requested).toEqual([]);

    await userEvent.click(screen.getByRole('button', { name: '30d' }));
    await waitFor(() => expect(screen.getByText('Last 30 days')).toBeInTheDocument());
    expect(requested).toEqual(['30d']);
  });
});

describe('Agents page run figures', () => {
  it('takes live counts from the server stats rather than the runs it lists', async () => {
    server.use(http.get('/api/agents/types/drafts', () => HttpResponse.json({ data: [] })));
    const stats = mockRunStats([]);
    const agent = AGENT_ROWS[0];
    stats.current = {
      total: 900,
      by_state: { active: 40, completing: 2, starting: 3, pending: 5, completed: 850 },
      live: 50,
      live_by_agent_type: { [agent.id]: 50 },
      longest_live_ms: 3_600_000,
    };
    const live = RUN_ROWS.filter((r) =>
      ['active', 'starting', 'pending', 'completing'].includes(r.state),
    );
    renderWithProviders(
      <AgentsShell
        initialAgentTypes={AGENT_ROWS}
        initialLiveRuns={live}
        initialRunStats={stats}
        initialError={null}
        initialInsights={mockInsightsResponse('7d')}
      />,
    );

    expect(screen.getByText('50 runs in progress')).toBeInTheDocument();
    expect(screen.getByText('42 running · 3 starting · 5 queued')).toBeInTheDocument();
    expect(screen.getByText('50 running now')).toBeInTheDocument();
    expect(screen.getByText(`+${50 - live.length} more runs not shown`)).toBeInTheDocument();
  });
});

describe('Agents page without run stats', () => {
  it('shows the run counts as unavailable rather than zero', () => {
    server.use(http.get('/api/agents/types/drafts', () => HttpResponse.json({ data: [] })));
    renderWithProviders(
      <AgentsShell
        initialAgentTypes={AGENT_ROWS}
        initialLiveRuns={[]}
        initialRunStats={null}
        initialError={null}
        initialInsights={mockInsightsResponse('7d')}
      />,
    );
    expect(screen.getByText('Unavailable')).toBeInTheDocument();
    expect(screen.getAllByText('Run count unavailable')).toHaveLength(AGENT_ROWS.length);
    expect(screen.queryByText('Nothing running')).not.toBeInTheDocument();
  });
});
