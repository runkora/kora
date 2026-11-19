import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';

const nav = vi.hoisted(() => ({ search: '', push: vi.fn(), replace: vi.fn() }));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: nav.push, replace: nav.replace, refresh: vi.fn() }),
  usePathname: () => '/agents/code-reviewer',
  useSearchParams: () => new URLSearchParams(nav.search),
}));

import { AgentDetailShell } from './agent-detail-shell';
import { CODE_REVIEWER } from '../../_components/agents-mock-data';
import { rowFor, runRowsFor } from './agent-detail-page-mock-data';
import { mockOverview } from './agent-overview-mock-data';
import { graderFleet } from './agent-grades-mock-data';

const agent = rowFor(CODE_REVIEWER);
const fleet = graderFleet();

/** Serves the agent's Graders and runs, recording each runs query. */
function serve() {
  const runQueries: URLSearchParams[] = [];
  server.use(
    http.get(`/api/agents/types/${agent.id}/graders`, () =>
      HttpResponse.json({ graders: fleet.list }),
    ),
    http.get(`/api/agents/types/${agent.id}/graders/:name`, ({ params }) =>
      HttpResponse.json(fleet.detail[String(params.name)]),
    ),
    http.get(`/api/agents/types/${agent.id}/runs`, ({ request }) => {
      runQueries.push(new URL(request.url).searchParams);
      return HttpResponse.json({ data: [], next_cursor: null });
    }),
    http.get('/api/agents/types/overview/:slug', ({ params }) =>
      HttpResponse.json(mockOverview(String(params.slug), '7d')),
    ),
  );
  return runQueries;
}

const page = () => (
  <AgentDetailShell
    initialAgent={agent}
    initialRuns={runRowsFor(CODE_REVIEWER)}
    initialNextCursor={null}
    initialError={null}
    initialStatus="success"
    initialOverview={mockOverview(CODE_REVIEWER.slug, '7d')}
  />
);

describe('AgentDetailPage URL', () => {
  it('opens the Grader the URL names, then follows a link to its failed runs', async () => {
    const runQueries = serve();
    nav.search = 'tab=grades&grader=summary-accurate';
    const { rerender } = renderWithProviders(page());
    expect(
      await screen.findByRole('heading', { name: fleet.detail['summary-accurate']!.description }),
    ).toBeInTheDocument();
    expect(runQueries).toHaveLength(0);

    nav.search = 'tab=runs&grader=summary-accurate&question=summary-correct&verdict=fail';
    rerender(page());
    await waitFor(() => expect(runQueries).toHaveLength(1));
    expect(runQueries[0]!.get('grader')).toBe('summary-accurate');
    expect(runQueries[0]!.get('question')).toBe('summary-correct');
    expect(runQueries[0]!.get('verdict')).toBe('fail');
    expect(
      await screen.findByText('Grader summary-accurate · Question summary-correct · Fail'),
    ).toBeInTheDocument();

    // The same URL again does not read the runs again.
    rerender(page());
    expect(runQueries).toHaveLength(1);
  });

  it('pushes a history entry to open a Grader and replaces it to switch tabs', async () => {
    serve();
    nav.search = 'tab=grades';
    renderWithProviders(page());
    await userEvent.click(await screen.findByText('ci-read'));
    expect(nav.push).toHaveBeenCalledWith('/agents/code-reviewer?tab=grades&grader=ci-read', {
      scroll: false,
    });
    await userEvent.click(screen.getByRole('tab', { name: /Runs/ }));
    expect(nav.replace).toHaveBeenCalledWith('/agents/code-reviewer?tab=runs', { scroll: false });
  });
});
