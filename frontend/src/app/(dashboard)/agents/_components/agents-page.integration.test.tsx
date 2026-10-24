import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';

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

describe('Agents page permissions', () => {
  const VIEWER_SCOPES = ['agents:types:read', 'agents:environments:read', 'triggers:read'];

  function renderAgents(
    scopes: string[],
    agentTypes = AGENT_ROWS,
    runsError: string | null = null,
    apiAuthoring = true,
  ) {
    server.use(http.get('/api/agents/types/drafts', () => HttpResponse.json({ data: [] })));
    return renderWithProviders(
      <AgentsShell
        initialAgentTypes={agentTypes}
        {...(runsError ? { initialLiveRuns: [], initialRunStats: null } : mockRunArgs(RUN_ROWS))}
        initialError={null}
        initialRunsError={runsError}
        initialInsights={mockInsightsResponse('7d')}
      />,
      { scopes, apiAuthoring },
    );
  }

  it('shows a viewer the agents with create and run controls disabled and explained', () => {
    renderAgents(
      VIEWER_SCOPES,
      AGENT_ROWS.map((a) => ({ ...a, canRun: false, canEdit: false })),
    );
    expect(screen.getAllByText(AGENT_ROWS[0].name).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: /new agent/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /new run/i })).toBeDisabled();
    for (const run of screen.getAllByRole('button', { name: /^run$/i })) {
      expect(run).toBeDisabled();
    }
    expect(
      screen.getByLabelText("You don't have permission to create agents."),
    ).toBeInTheDocument();
    expect(
      screen.getAllByLabelText("You don't have permission to start runs of this agent.").length,
    ).toBe(AGENT_ROWS.length);
  });

  it('enables Run only on the agents the caller may run', () => {
    renderAgents(
      VIEWER_SCOPES,
      AGENT_ROWS.map((a, i) => ({ ...a, canRun: i === 0 })),
    );
    const enabled = screen
      .getAllByRole('button', { name: /^run$/i })
      .filter((b) => !(b as HTMLButtonElement).disabled);
    expect(enabled).toHaveLength(AGENT_ROWS[0].state === 'active' ? 1 : 0);
    expect(screen.getByRole('button', { name: /new run/i })).toBeEnabled();
  });

  it('enables New Agent for a delegated-only creator', () => {
    renderAgents([...VIEWER_SCOPES, 'agents:types:delegated:write']);
    expect(screen.getByRole('button', { name: /new agent/i })).toBeEnabled();
  });

  it('hides create in a locked tenant and keeps runs', () => {
    renderAgents(
      [...VIEWER_SCOPES, 'agents:types:delegated:write'],
      AGENT_ROWS.map((a) => ({ ...a, canRun: true })),
      null,
      false,
    );
    expect(screen.queryByRole('button', { name: /new agent/i })).not.toBeInTheDocument();
    expect(screen.getByText(/Agents in this tenant are managed in git/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new run/i })).toBeEnabled();
  });

  it('marks a declared agent as managed in git', () => {
    renderAgents(
      VIEWER_SCOPES,
      AGENT_ROWS.map((a, i) => ({ ...a, declared: i === 0 })),
    );
    expect(screen.getAllByText('Managed in git')).toHaveLength(1);
  });

  it('keeps the agents on screen when the runs are refused', () => {
    renderAgents(VIEWER_SCOPES, AGENT_ROWS, 'You do not have access to runs.');
    expect(screen.getAllByText(AGENT_ROWS[0].name).length).toBeGreaterThan(0);
    expect(screen.getByText('You do not have access to runs.')).toBeInTheDocument();
  });
});
