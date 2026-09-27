import { renderHook, act } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { useAgents } from './use-agents';
import type { AgentTypeRow } from '../_types';
import type { RunStats } from '@orbit/shared/api';

/**
 * Unit tests for useAgents — powers the /agents list page (GET
 * /api/agents/types + GET /api/agents/runs/stats via refresh()).
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

describe('useAgents — initial state', () => {
  it('starts in success status when initial agent types are provided', () => {
    const { result } = renderHook(() => useAgents([agentTypeRow()], runStats(), null));

    expect(result.current.status).toBe('success');
    expect(result.current.agentTypes).toHaveLength(1);
    expect(result.current.error).toBeNull();
  });

  it('starts idle when there are no initial agent types and no error', () => {
    const { result } = renderHook(() => useAgents([], runStats({ total: 0 }), null));

    expect(result.current.status).toBe('idle');
  });

  it('starts in error status when an initial error is provided', () => {
    const { result } = renderHook(() => useAgents([], runStats({ total: 0 }), 'Session expired'));

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Session expired');
  });
});

describe('useAgents — refresh', () => {
  it('replaces agent types and run stats on a successful refresh', async () => {
    const initialStats = runStats({ total: 10 });
    const refreshedAgent = agentTypeRow({ id: 'agent-2', name: 'Triage Bot' });
    const refreshedStats = runStats({ total: 42 });
    server.use(
      http.get('/api/agents/types', () =>
        HttpResponse.json({
          data: [
            {
              id: refreshedAgent.id,
              name: refreshedAgent.name,
              slug: refreshedAgent.slug,
              description: refreshedAgent.description,
              framework: 'claude-code',
              identity_mode: 'autonomous',
              state: 'active',
              tool_scopes: [],
              grant_scopes: [],
              max_concurrent_runs_per_pod: 1,
              system_prompt: '',
              created_at: '2026-01-01T00:00:00Z',
              updated_at: '2026-01-01T00:00:00Z',
            },
          ],
        }),
      ),
      http.get('/api/agents/runs/stats', () => HttpResponse.json(refreshedStats)),
    );

    const { result } = renderHook(() => useAgents([agentTypeRow()], initialStats, null));

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.status).toBe('success');
    expect(result.current.agentTypes).toHaveLength(1);
    expect(result.current.agentTypes[0]?.id).toBe('agent-2');
    expect(result.current.runStats).toEqual(refreshedStats);
  });

  it('sets error status and drops agent types when the agent types fetch fails', async () => {
    server.use(
      http.get('/api/agents/types', () =>
        HttpResponse.json({ message: 'Unable to load agents' }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() => useAgents([agentTypeRow()], runStats(), null));

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Unable to load agents');
  });

  it('preserves the previous run stats when the stats refresh fails', async () => {
    const initialStats = runStats({ total: 10, active: 1 });
    server.use(
      http.get('/api/agents/types', () => HttpResponse.json({ data: [] })),
      http.get('/api/agents/runs/stats', () =>
        HttpResponse.json({ message: 'Stats unavailable' }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() => useAgents([agentTypeRow()], initialStats, null));

    await act(async () => {
      await result.current.refresh();
    });

    // The agent types list still updates even though stats failed — only the
    // stats fetch has independent failure handling.
    expect(result.current.status).toBe('success');
    expect(result.current.error).toBeNull();
    expect(result.current.runStats).toEqual(initialStats);
  });
});
