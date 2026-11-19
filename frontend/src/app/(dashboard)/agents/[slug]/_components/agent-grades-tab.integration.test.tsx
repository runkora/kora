import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { delay, http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/agents/code-reviewer',
  useSearchParams: () => new URLSearchParams(),
}));

import { useState } from 'react';
import { AgentGradesTab } from './agent-grades';
import { graderFleet } from './agent-grades-mock-data';

const fleet = graderFleet();
const titleOf = (name: string) => fleet.detail[name].description || name;

/** Serves the fleet. `slow` names a Grader whose detail answers after 300ms. */
function serveFleet(slow?: string) {
  server.use(
    http.get('/api/agents/types/at-1/graders', () => HttpResponse.json({ graders: fleet.list })),
    http.get('/api/agents/types/at-1/graders/:name', async ({ params }) => {
      const name = String(params.name);
      if (name === slow) await delay(300);
      const detail = fleet.detail[name];
      return detail
        ? HttpResponse.json(detail)
        : HttpResponse.json({ code: 'not_found', message: 'Grader not found' }, { status: 404 });
    }),
  );
}

/** The tab with its open Grader held in state, as the agent page holds it in the URL. */
function Tab() {
  const [grader, setGrader] = useState<string | undefined>();
  return (
    <AgentGradesTab agentTypeId="at-1" slug="code-reviewer" grader={grader} onGrader={setGrader} />
  );
}

describe('AgentGradesTab', () => {
  it('loads the list, opens a Grader and goes back', async () => {
    serveFleet();
    renderWithProviders(<Tab />);
    expect(screen.getByText('Loading Graders…')).toBeInTheDocument();
    await userEvent.click(await screen.findByText('summary-accurate'));
    expect(
      await screen.findByRole('heading', { name: titleOf('summary-accurate') }),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /All Graders/ }));
    expect(await screen.findByText('summary-accurate')).toBeInTheDocument();
  });

  it('shows the API error when the list fails', async () => {
    server.use(
      http.get('/api/agents/types/at-1/graders', () =>
        HttpResponse.json({ code: 'internal', message: 'Agents are unavailable' }, { status: 500 }),
      ),
    );
    renderWithProviders(<Tab />);
    expect(await screen.findByText('Failed to load Graders')).toBeInTheDocument();
    expect(screen.getByText('Agents are unavailable')).toBeInTheDocument();
  });

  it('says when an opened Grader is gone', async () => {
    serveFleet();
    renderWithProviders(
      <AgentGradesTab
        agentTypeId="at-1"
        slug="code-reviewer"
        grader="deleted"
        onGrader={vi.fn()}
      />,
    );
    expect(screen.getByText('Loading Grader…')).toBeInTheDocument();
    expect(await screen.findByText('Grader not found')).toBeInTheDocument();
  });

  it('shows the Grader opened last when an earlier read answers late', async () => {
    serveFleet('summary-accurate');
    renderWithProviders(<Tab />);
    await userEvent.click(await screen.findByText('summary-accurate'));
    await userEvent.click(screen.getByRole('button', { name: /All Graders/ }));
    await userEvent.click(await screen.findByText('ci-read'));
    expect(await screen.findByRole('heading', { name: titleOf('ci-read') })).toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 400));
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: titleOf('summary-accurate') })).toBeNull(),
    );
    expect(screen.getByRole('heading', { name: titleOf('ci-read') })).toBeInTheDocument();
  });
});
