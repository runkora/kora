import { renderHook, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { useAgentDetail } from './use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { RunStats } from '@orbit/shared/api';

/**
 * Unit tests for useAgentDetail — powers the /agents/[slug] detail page's
 * runs tab (GET /api/agents/types/{id}/runs + GET /api/agents/runs/stats via
 * refresh()).
 */

function agentTypeRow(overrides: Partial<AgentTypeRow> = {}): AgentTypeRow {
  return {
    id: 'agent-1',
    name: 'Release Notes Bot',
    slug: 'release-notes-bot',
    description: 'Drafts release notes from merged PRs.',
    framework: 'Claude Code',
    frameworkKey: 'claude-code',
    identityMode: 'autonomous',
    state: 'active',
    toolScopes: [],
    grantScopes: [],
    connectivity: { mode: 'none' },
    connectors: [],
    mcpTools: [],
    maxConcurrentRunsPerPod: 1,
    systemPrompt: 'You draft release notes.',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    modelId: 'anthropic/claude-sonnet-4-6',
    budget: null,
    environmentId: '',
    ...overrides,
  };
}

function runStats(overrides: Partial<RunStats> = {}): RunStats {
  return {
    total: 10,
    active: 1,
    completed: 8,
    failed: 1,
    avg_completed_duration_ms: 12_345,
    ...overrides,
  };
}

function runRow(overrides: Partial<RunRow> = {}): RunRow {
  return {
    id: 'run-1',
    agentTypeId: 'agent-1',
    agentTypeName: 'Release Notes Bot',
    agentTypeSlug: 'release-notes-bot',
    state: 'completed',
    createdBy: 'user-1',
    createdByName: 'Ada Lovelace',
    startedByTrigger: null,
    delegated: false,
    startedAt: '2026-01-01T00:00:00Z',
    completedAt: '2026-01-01T00:05:00Z',
    createdAt: '2026-01-01T00:00:00Z',
    durationMs: 300_000,
    tokensUsed: 1000,
    toolCalls: 3,
    summary: 'Drafted release notes.',
    ...overrides,
  };
}

describe('useAgentDetail — refresh', () => {
  it('replaces runs and run stats on a successful refresh', async () => {
    const agent = agentTypeRow();
    const refreshedStats = runStats({ total: 99 });
    server.use(
      http.get('/api/agents/types/agent-1/runs', () =>
        HttpResponse.json({ data: [], has_more: false, next_cursor: null }),
      ),
      http.get('/api/agents/runs/stats', () => HttpResponse.json(refreshedStats)),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, [runRow()], false, null, runStats(), null, 'success'),
    );

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.status).toBe('success');
    expect(result.current.runs).toHaveLength(0);
    expect(result.current.runStats).toEqual(refreshedStats);
  });

  it('sets error status and keeps stale state when the runs fetch fails', async () => {
    const agent = agentTypeRow();
    const initialRuns = [runRow()];
    const initialStats = runStats();
    server.use(
      http.get('/api/agents/types/agent-1/runs', () =>
        HttpResponse.json({ message: 'Runs unavailable' }, { status: 500 }),
      ),
      http.get('/api/agents/runs/stats', () => HttpResponse.json(runStats({ total: 999 }))),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, initialRuns, false, null, initialStats, null, 'success'),
    );

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Runs unavailable');
    expect(result.current.runs).toEqual(initialRuns);
    expect(result.current.runStats).toEqual(initialStats);
  });

  it('preserves the previous run stats when the stats fetch fails but runs succeed', async () => {
    const agent = agentTypeRow();
    const initialStats = runStats({ total: 10, active: 1 });
    const refreshedRuns = [runRow({ id: 'run-2' })];
    server.use(
      http.get('/api/agents/types/agent-1/runs', () =>
        HttpResponse.json({
          data: [
            {
              id: 'run-2',
              agent_type_id: 'agent-1',
              state: 'completed',
              created_by: 'user-1',
              created_at: '2026-01-01T00:00:00Z',
              started_at: '2026-01-01T00:00:00Z',
              completed_at: '2026-01-01T00:05:00Z',
            },
          ],
          has_more: false,
          next_cursor: null,
        }),
      ),
      http.get('/api/agents/runs/stats', () =>
        HttpResponse.json({ message: 'Stats unavailable' }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, [runRow()], false, null, initialStats, null, 'success'),
    );

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.status).toBe('success');
    expect(result.current.error).toBeNull();
    expect(result.current.runs.map((r) => r.id)).toEqual(refreshedRuns.map((r) => r.id));
    expect(result.current.runStats).toEqual(initialStats);
  });
});

describe('useAgentDetail — loadMoreRuns', () => {
  it('appends the next page and advances the cursor on success', async () => {
    const agent = agentTypeRow();
    server.use(
      http.get('/api/agents/types/agent-1/runs', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        expect(cursor).toBe('cursor-1');
        return HttpResponse.json({
          data: [
            {
              id: 'run-2',
              agent_type_id: 'agent-1',
              state: 'completed',
              created_by: 'user-1',
              created_at: '2026-01-01T00:00:00Z',
            },
          ],
          has_more: false,
          next_cursor: null,
        });
      }),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, [runRow()], true, 'cursor-1', runStats(), null, 'success'),
    );

    await act(async () => {
      await result.current.loadMoreRuns();
    });

    expect(result.current.runs.map((r) => r.id)).toEqual(['run-1', 'run-2']);
    expect(result.current.hasMoreRuns).toBe(false);
    expect(result.current.loadMoreRunsError).toBeNull();
  });

  it('surfaces an error and leaves hasMoreRuns/runs untouched so a retry re-requests the same page', async () => {
    const agent = agentTypeRow();
    const initialRuns = [runRow()];
    server.use(
      http.get('/api/agents/types/agent-1/runs', () =>
        HttpResponse.json({ message: 'Runs unavailable' }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, initialRuns, true, 'cursor-1', runStats(), null, 'success'),
    );

    await act(async () => {
      await result.current.loadMoreRuns();
    });

    expect(result.current.loadMoreRunsError).toBe('Runs unavailable');
    expect(result.current.runs).toEqual(initialRuns);
    expect(result.current.hasMoreRuns).toBe(true);
    expect(result.current.loadingMoreRuns).toBe(false);
  });

  it('clears the previous loadMoreRuns error once a retry succeeds', async () => {
    const agent = agentTypeRow();
    let attempt = 0;
    server.use(
      http.get('/api/agents/types/agent-1/runs', () => {
        attempt += 1;
        if (attempt === 1) {
          return HttpResponse.json({ message: 'Runs unavailable' }, { status: 500 });
        }
        return HttpResponse.json({
          data: [
            {
              id: 'run-2',
              agent_type_id: 'agent-1',
              state: 'completed',
              created_by: 'user-1',
              created_at: '2026-01-01T00:00:00Z',
            },
          ],
          has_more: false,
          next_cursor: null,
        });
      }),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, [runRow()], true, 'cursor-1', runStats(), null, 'success'),
    );

    await act(async () => {
      await result.current.loadMoreRuns();
    });
    expect(result.current.loadMoreRunsError).toBe('Runs unavailable');

    await act(async () => {
      await result.current.loadMoreRuns();
    });
    expect(result.current.loadMoreRunsError).toBeNull();
    expect(result.current.runs.map((r) => r.id)).toEqual(['run-1', 'run-2']);
  });
});

describe('useAgentDetail — loadMoreRuns guard', () => {
  it('fetches the next page once when load more is triggered twice before it resolves', async () => {
    const agent = agentTypeRow();
    let requests = 0;
    server.use(
      http.get('/api/agents/types/agent-1/runs', () => {
        requests += 1;
        return HttpResponse.json({ data: [], has_more: false, next_cursor: null });
      }),
    );

    const { result } = renderHook(() =>
      useAgentDetail(agent, [runRow()], true, 'cursor-1', runStats(), null, 'success'),
    );

    await act(async () => {
      await Promise.all([result.current.loadMoreRuns(), result.current.loadMoreRuns()]);
    });

    expect(requests).toBe(1);
  });
});

describe('useAgentDetail — deleteRun', () => {
  it('decrements the total and the deleted run’s status bucket', async () => {
    const agent = agentTypeRow();
    server.use(
      http.delete('/api/agents/runs/run-1', () => new HttpResponse(null, { status: 204 })),
    );

    const { result } = renderHook(() =>
      useAgentDetail(
        agent,
        [runRow({ id: 'run-1', state: 'failed' })],
        false,
        null,
        runStats({ total: 10, completed: 8, failed: 1, active: 1 }),
        null,
        'success',
      ),
    );

    await act(async () => {
      await result.current.deleteRun('run-1');
    });

    expect(result.current.runs).toEqual([]);
    expect(result.current.runStats).toMatchObject({ total: 9, completed: 8, failed: 0, active: 1 });
  });

  it('leaves the status buckets alone when the deleted run was never loaded', async () => {
    const agent = agentTypeRow();
    server.use(
      http.delete('/api/agents/runs/run-unloaded', () => new HttpResponse(null, { status: 204 })),
    );

    const { result } = renderHook(() =>
      useAgentDetail(
        agent,
        [runRow()],
        false,
        null,
        runStats({ total: 10, completed: 8, failed: 1, active: 1 }),
        null,
        'success',
      ),
    );

    await act(async () => {
      await result.current.deleteRun('run-unloaded');
    });

    expect(result.current.runStats).toMatchObject({ total: 9, completed: 8, failed: 1, active: 1 });
  });
});
