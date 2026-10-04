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
import { AGENT_ROWS, RUN_ROWS } from './agents-mock-data';
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
        initialRuns={RUN_ROWS}
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
