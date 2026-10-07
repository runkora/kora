import { describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import type { RunStateCounts } from '@orbit/shared/api';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/agents/code-reviewer',
  useSearchParams: () => new URLSearchParams(),
}));

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentRunsTab } from './agent-runs-tab';
import { CODE_REVIEWER } from '../../_components/agents-mock-data';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../../_types';

const agent = toAgentTypeRow(CODE_REVIEWER);

/** Raw runs `run-<from>` to `run-<to - 1>`, newest first. */
const rawRuns = (from: number, to: number, state = 'completed') =>
  Array.from({ length: to - from }, (_, i) => ({
    id: `run-${from + i}-${state}`,
    tenant_id: 't1',
    agent_type_id: agent.id,
    state,
    pod_ordinal: 0,
    created_by: 'user-1',
    started_at: null,
    completed_at: null,
    created_at: new Date(Date.UTC(2026, 9, 1, 0, 60 - from - i)).toISOString(),
    updated_at: new Date(Date.UTC(2026, 9, 1)).toISOString(),
    summary: `Task ${from + i}`,
  }));

const COUNTS: RunStateCounts = {
  total: 12,
  by_state: { completed: 9, failed: 2, budget_exhausted: 1 },
  live: 0,
  live_by_agent_type: {},
  longest_live_ms: 0,
};

function renderTab(firstPage: ReturnType<typeof rawRuns>, nextCursor: string | null) {
  const rows = firstPage.map((r) => toRunRow(r as never, indexAgentTypes([agent])));
  renderWithProviders(
    <AgentDetailProvider
      initialAgent={agent}
      initialRuns={rows}
      initialNextCursor={nextCursor}
      initialStateCounts={COUNTS}
      initialError={null}
      initialStatus="success"
    >
      <AgentRunsTab onStartRun={vi.fn()} />
    </AgentDetailProvider>,
  );
}

/** Serves the agent's runs route, recording each query, from `pages` keyed by cursor ('' first). */
function serveRuns(pages: Record<string, { data: unknown[]; next_cursor: string | null }>) {
  const queries: URLSearchParams[] = [];
  server.use(
    http.get(`/api/agents/types/${agent.id}/runs`, ({ request }) => {
      const q = new URL(request.url).searchParams;
      queries.push(q);
      return HttpResponse.json(pages[`${q.get('state') ?? ''}:${q.get('cursor') ?? ''}`]);
    }),
  );
  return queries;
}

describe('Agent runs tab', () => {
  it('appends the next page on load more and hides the button on the last page', async () => {
    const queries = serveRuns({ ':page-2': { data: rawRuns(3, 5), next_cursor: null } });
    renderTab(rawRuns(0, 3), 'page-2');

    expect(screen.getByText('Task 0')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Load more runs' }));

    await waitFor(() => expect(screen.getByText('Task 4')).toBeInTheDocument());
    expect(screen.getByText('Task 0')).toBeInTheDocument();
    expect(queries[0].get('cursor')).toBe('page-2');
    expect(queries[0].get('limit')).toBe('50');
    expect(screen.queryByRole('button', { name: 'Load more runs' })).not.toBeInTheDocument();
  });

  it('keeps the cursor after a failed load so a retry asks for the same page', async () => {
    let calls = 0;
    server.use(
      http.get(`/api/agents/types/${agent.id}/runs`, ({ request }) => {
        calls++;
        expect(new URL(request.url).searchParams.get('cursor')).toBe('page-2');
        return calls === 1
          ? HttpResponse.json({ message: 'upstream down' }, { status: 502 })
          : HttpResponse.json({ data: rawRuns(3, 4), next_cursor: null });
      }),
    );
    renderTab(rawRuns(0, 3), 'page-2');

    await userEvent.click(screen.getByRole('button', { name: 'Load more runs' }));
    expect(await screen.findByText('upstream down')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await waitFor(() => expect(screen.getByText('Task 3')).toBeInTheDocument());
    expect(calls).toBe(2);
  });

  it('refreshes the counts after a run is deleted', async () => {
    server.use(
      http.delete('/api/agents/runs/:id', () => new HttpResponse(null, { status: 204 })),
      http.get(`/api/agents/types/overview/${agent.slug}`, () =>
        HttpResponse.json({
          period: '7d',
          runStats: {
            total: 0,
            triggered: 0,
            completed: 0,
            failed: 0,
            durations: {},
            current: { ...COUNTS, total: 11, by_state: { ...COUNTS.by_state, failed: 1 } },
          },
        }),
      ),
    );
    renderTab(rawRuns(0, 2, 'failed'), null);
    expect(screen.getByRole('button', { name: /Failed/ })).toHaveTextContent('3');

    await userEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(screen.queryByText('Task 0')).not.toBeInTheDocument());
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Failed/ })).toHaveTextContent('2'),
    );
    expect(screen.getByRole('button', { name: /All/ })).toHaveTextContent('11');
  });

  it('filters on the server and counts from the server totals', async () => {
    const queries = serveRuns({
      'failed,budget_exhausted:': { data: rawRuns(10, 12, 'failed'), next_cursor: null },
    });
    renderTab(rawRuns(0, 3), 'page-2');

    const failed = screen.getByRole('button', { name: /Failed/ });
    expect(failed).toHaveTextContent('3');
    expect(screen.getByRole('button', { name: /All/ })).toHaveTextContent('12');
    await userEvent.click(failed);

    await waitFor(() => expect(screen.getByText('Task 11')).toBeInTheDocument());
    expect(screen.queryByText('Task 0')).not.toBeInTheDocument();
    expect(queries[0].get('state')).toBe('failed,budget_exhausted');
    expect(queries[0].has('cursor')).toBe(false);
    expect(screen.queryByRole('button', { name: 'Load more runs' })).not.toBeInTheDocument();
  });
});
