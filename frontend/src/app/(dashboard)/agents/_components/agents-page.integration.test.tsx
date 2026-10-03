import { beforeEach, describe, expect, it, vi } from 'vitest';
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
import { AGENT_ROWS, RUN_ROWS } from './agents-mock-data';
import { mockInsightsResponse } from './agents-insights-mock-data';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { Period } from '../_lib/spend';

const requested: string[] = [];

function serve(opts: { failPeriod?: string } = {}) {
  requested.length = 0;
  server.use(
    http.get('/api/agents/types/drafts', () => HttpResponse.json({ data: [] })),
    http.get('/api/agents/insights', ({ request }) => {
      const period = new URL(request.url).searchParams.get('period') ?? '7d';
      requested.push(period);
      if (period === opts.failPeriod) {
        return HttpResponse.json({ code: 'insights_unavailable' }, { status: 503 });
      }
      return HttpResponse.json(mockInsightsResponse(period as Period));
    }),
  );
}

function renderPage(initialInsights: AgentInsightsResponse | null = null) {
  return renderWithProviders(
    <AgentsShell
      initialAgentTypes={AGENT_ROWS}
      initialRuns={RUN_ROWS}
      initialError={null}
      initialInsights={initialInsights}
    />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('Agents page insights', () => {
  it('renders the 7-day window it was given without fetching it, then fetches each other period once', async () => {
    serve();
    renderPage(mockInsightsResponse('7d'));

    // First paint has the figures: the control and the spend are there at once.
    const control = screen.getByRole('button', { name: '30d' });
    expect(screen.getByText('Last 7 days')).toBeInTheDocument();
    expect(requested).toEqual([]);

    await userEvent.click(control);
    await waitFor(() => expect(requested).toEqual(['30d']));
    await userEvent.click(screen.getByRole('button', { name: '7d' }));
    await userEvent.click(screen.getByRole('button', { name: '30d' }));
    await userEvent.click(screen.getByRole('button', { name: '90d' }));
    await waitFor(() => expect(requested).toEqual(['30d', '90d']));
  });

  it('fetches the 7-day window itself when the server render had none', async () => {
    serve();
    renderPage(null);
    await screen.findByRole('button', { name: '30d' });
    expect(requested).toEqual(['7d']);
  });

  it('says spend is unavailable when the window cannot be loaded, and keeps the period control', async () => {
    serve({ failPeriod: '7d' });
    renderPage(null);

    expect(await screen.findByText('Spend is unavailable right now.')).toBeInTheDocument();
    // The control stays so another period can be tried.
    expect(screen.getByRole('button', { name: '30d' })).toBeInTheDocument();
  });
});
