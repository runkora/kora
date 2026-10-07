import { beforeEach, describe, expect, it, vi } from 'vitest';

const client = vi.hoisted(() => ({
  listEnvironments: vi.fn(),
  listSandboxImages: vi.fn(),
  getEnvironmentSizes: vi.fn(),
}));

vi.mock('@orbit/server/environments-client', () => client);
vi.mock('../agents/_services/agents-server', () => ({
  fetchAgentsFromBFF: vi.fn().mockResolvedValue({ ok: false }),
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
  });
});
