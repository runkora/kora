import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import type { RunStats } from '@orbit/shared/api';

import type { AgentTypeRow, RunRow } from '../../_types';
import type { AgentDetailStateShape } from '../_hooks/use-agent-detail';

/**
 * The runs tab renders the paginated runs from the agent detail context: the
 * load-more control and its failure state, status counts taken from the
 * server-computed stats rather than the loaded page, and row navigation.
 */

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push, replace: vi.fn(), refresh: vi.fn(), prefetch: vi.fn() }),
  usePathname: () => '/agents/coder',
  useSearchParams: () => new URLSearchParams(),
}));

let state: AgentDetailStateShape;
vi.mock('../_context/agent-detail-context', () => ({
  useAgentDetailContext: () => state,
}));

import { AgentRunsTab } from './agent-runs-tab';

function run(id: string, overrides: Partial<RunRow> = {}): RunRow {
  return {
    id,
    agentTypeId: 'at-1',
    agentTypeName: 'Coder',
    agentTypeSlug: 'coder',
    state: 'completed',
    createdBy: 'user-1',
    createdByName: 'Ada Lovelace',
    startedByTrigger: null,
    delegated: false,
    startedAt: '2026-09-01T00:00:00Z',
    completedAt: '2026-09-01T00:01:00Z',
    createdAt: '2026-09-01T00:00:00Z',
    durationMs: 60_000,
    tokensUsed: 100,
    toolCalls: 1,
    summary: `Task for ${id}`,
    ...overrides,
  };
}

function stats(overrides: Partial<RunStats> = {}): RunStats {
  return {
    total: 44,
    active: 0,
    completed: 31,
    failed: 13,
    avg_completed_duration_ms: 1000,
    ...overrides,
  };
}

function setState(overrides: Partial<AgentDetailStateShape> = {}) {
  state = {
    agent: { id: 'at-1', name: 'Coder', slug: 'coder' } as AgentTypeRow,
    runs: [run('aaaaaaaa-1'), run('bbbbbbbb-2', { state: 'failed' })],
    hasMoreRuns: false,
    loadingMoreRuns: false,
    loadMoreRunsError: null,
    loadMoreRuns: vi.fn().mockResolvedValue(undefined),
    runStats: stats(),
    status: 'success',
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
    deleteRun: vi.fn().mockResolvedValue({ error: null }),
    ...overrides,
  } as AgentDetailStateShape;
}

describe('AgentRunsTab', () => {
  beforeEach(() => {
    push.mockClear();
    setState();
  });

  it('hides the load-more control when there are no more runs', () => {
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    expect(screen.queryByRole('button', { name: /load more runs|retry/i })).not.toBeInTheDocument();
  });

  it('shows "Load more runs" and loads the next page on click', () => {
    setState({ hasMoreRuns: true });
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    fireEvent.click(screen.getByRole('button', { name: 'Load more runs' }));

    expect(state.loadMoreRuns).toHaveBeenCalledTimes(1);
  });

  it('disables the control while a page is loading', () => {
    setState({ hasMoreRuns: true, loadingMoreRuns: true });
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    expect(screen.getByRole('button', { name: 'Loading…' })).toBeDisabled();
  });

  it('shows the failure beneath a Retry button that requests the same page again', () => {
    setState({ hasMoreRuns: true, loadMoreRunsError: 'Service Unavailable' });
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    expect(screen.getByText('Service Unavailable')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Load more runs' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(state.loadMoreRuns).toHaveBeenCalledTimes(1);
  });

  it('counts runs by status from the server stats, not the loaded page', () => {
    setState({ hasMoreRuns: true });
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    const filter = screen.getByRole('group', { name: 'Filter runs by status' });
    expect(filter).toHaveTextContent('All44');
    expect(filter).toHaveTextContent('Completed31');
    expect(filter).toHaveTextContent('Failed13');
  });

  it('filters the loaded runs by status group', () => {
    setState({
      runs: [
        run('aaaaaaaa-1', { state: 'completed' }),
        run('bbbbbbbb-2', { state: 'failed' }),
        run('cccccccc-3', { state: 'budget_exhausted' }),
        run('dddddddd-4', { state: 'idle' }),
      ],
    });
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    fireEvent.click(screen.getByRole('button', { name: /^Failed/ }));
    expect(screen.getByText('Task for bbbbbbbb-2')).toBeInTheDocument();
    expect(screen.getByText('Task for cccccccc-3')).toBeInTheDocument();
    expect(screen.queryByText('Task for aaaaaaaa-1')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /^Active/ }));
    expect(screen.getByText('Task for dddddddd-4')).toBeInTheDocument();
    expect(screen.queryByText('Task for bbbbbbbb-2')).not.toBeInTheDocument();
  });

  it('opens a run when its row is clicked', () => {
    renderWithProviders(<AgentRunsTab onStartRun={vi.fn()} />);

    fireEvent.click(screen.getByText('Task for aaaaaaaa-1'));

    expect(push).toHaveBeenCalledWith(expect.stringContaining('/agents/coder/runs/aaaaaaaa-1'));
  });
});
