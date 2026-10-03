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
import { MOCK_BUDGETS_RESPONSE, mockInsightsResponse } from './agents-insights-mock-data';
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
    http.get('/api/agents/insights/budgets', () => HttpResponse.json(MOCK_BUDGETS_RESPONSE)),
  );
}

function renderPage() {
  return renderWithProviders(
    <AgentsShell initialAgentTypes={AGENT_ROWS} initialRuns={RUN_ROWS} initialError={null} />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('Agents page insights', () => {
  it('loads the 7-day window after first render, then each period once when picked', async () => {
    serve();
    renderPage();

    // The page is up before any insights arrive.
    expect(screen.getByRole('heading', { name: 'Agents' })).toBeInTheDocument();
    const control = await screen.findByRole('button', { name: '30d' });
    expect(requested).toEqual(['7d']);

    await userEvent.click(control);
    await waitFor(() => expect(requested).toEqual(['7d', '30d']));
    await userEvent.click(screen.getByRole('button', { name: '7d' }));
    await userEvent.click(screen.getByRole('button', { name: '30d' }));
    await userEvent.click(screen.getByRole('button', { name: '90d' }));
    await waitFor(() => expect(requested).toEqual(['7d', '30d', '90d']));
  });

  it('says spend is unavailable when the window cannot be loaded, and offers no period control', async () => {
    serve({ failPeriod: '7d' });
    renderPage();

    expect(await screen.findByText('Spend is unavailable right now.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '30d' })).not.toBeInTheDocument();
  });
});
