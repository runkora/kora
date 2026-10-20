import { beforeEach, describe, expect, it, vi } from 'vitest';

const client = vi.hoisted(() => ({
  listEnvironments: vi.fn(),
  listSandboxImages: vi.fn(),
  getEnvironmentSizes: vi.fn(),
}));

vi.mock('@runkora/server/environments-client', () => client);
vi.mock('../agents/_services/agents-server', () => ({
  fetchAgentsFromBFF: vi.fn().mockResolvedValue({
    agentTypes: { ok: false, status: 403, message: 'You do not have access to agents.' },
    runs: { ok: false, status: 403, message: 'You do not have access to runs.' },
    runStats: null,
  }),
}));
vi.mock('../connections/_services/connections-server', () => ({
  fetchConnections: vi.fn().mockResolvedValue({ ok: false }),
}));
vi.mock('../model-gateway/_services/analytics-server', () => ({
  fetchAnalyticsFromGateway: vi.fn().mockResolvedValue({ ok: false }),
}));
vi.mock('../model-gateway/_services/budgets-server', () => ({
  fetchBudgetPrincipalsFromGateway: vi.fn().mockResolvedValue({ ok: false }),
}));

import { ApiError } from '@runkora/server/api-error';
import { fetchDashboardData } from './dashboard-server';

const ENV = {
  id: 'env-1',
  tenant_id: 't-1',
  name: 'go',
  description: '',
  sandbox_image: 'kora-base',
  size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  egress_hosts: [],
  connectors: [],
  created_at: '',
  updated_at: '',
};

describe('fetchDashboardData', () => {
  beforeEach(() => vi.clearAllMocks());

  it('loads only the environment list', async () => {
    client.listEnvironments.mockResolvedValue([ENV]);
    const data = await fetchDashboardData('token', 't-1');
    expect(data.environments.map((e) => e.name)).toEqual(['go']);
    expect(client.listSandboxImages).not.toHaveBeenCalled();
    expect(client.getEnvironmentSizes).not.toHaveBeenCalled();
  });

  it('shows no environments when the list fails', async () => {
    client.listEnvironments.mockRejectedValue(new Error('down'));
    const data = await fetchDashboardData('token', 't-1');
    expect(data.environments).toEqual([]);
    expect(data.environmentsError).toBe('Could not load environments. Try again.');
  });

  it('reports a refused section as no access, not as empty', async () => {
    client.listEnvironments.mockRejectedValue(new ApiError(403, 'forbidden', 'forbidden'));
    const data = await fetchDashboardData('token', 't-1');
    expect(data.environmentsError).toBe('You do not have access to environments.');
    expect(data.agentsError).toBe('You do not have access to agents.');
    expect(data.runsError).toBe('You do not have access to runs.');
  });
});
