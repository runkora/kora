import { describe, expect, it, vi } from 'vitest';

vi.mock('server-only', () => ({}));
vi.mock('@runkora/server/agents-client', () => ({
  agentsClient: () => ({ get: vi.fn().mockResolvedValue({ agent_types: [] }) }),
  fetchTenantRunStats: vi.fn().mockResolvedValue(null),
}));
vi.mock('@runkora/server/services/runs-server', () => ({
  listRunsWithNames: vi.fn().mockResolvedValue({
    data: [
      {
        id: 'run-1',
        tenant_id: 't-1',
        agent_type_id: 'agent-1',
        state: 'active',
        created_by: 'CgNkZXYSBWxvY2Fs',
        created_by_name: 'Dev',
        started_at: '2026-11-11T00:00:00Z',
        created_at: '2026-11-11T00:00:00Z',
        updated_at: '2026-11-11T00:00:00Z',
      },
    ],
    next_cursor: null,
  }),
}));

import { fetchAgentsFromBFF } from './agents-server';

describe('fetchAgentsFromBFF', () => {
  it('names the person who started each run', async () => {
    const result = await fetchAgentsFromBFF('token', 't-1', new URLSearchParams());
    expect(result.runs.ok && result.runs.data[0].createdByName).toBe('Dev');
  });
});
