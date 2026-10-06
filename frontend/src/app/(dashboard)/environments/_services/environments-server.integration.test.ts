/**
 * Integration tests for the environments page loader. MSW stands in for
 * orbit-agents and the connections gateway.
 */
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { orbitAgentsUrl: 'http://test-agents' },
}));

vi.mock('@orbit/server/connections-client', () => ({
  listConnections: vi.fn().mockResolvedValue([]),
}));

import { fetchEnvironments } from './environments-server';

const SIZES = {
  default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
};

function agents(sizes: () => Response) {
  server.use(
    http.get('http://test-agents/api/v1/environments', () =>
      HttpResponse.json({ environments: [] }),
    ),
    http.get('http://test-agents/api/v1/sandbox-images', () =>
      HttpResponse.json({ sandbox_images: [] }),
    ),
    http.get('http://test-agents/api/v1/environment-sizes', sizes),
  );
}

describe('fetchEnvironments', () => {
  it('loads the size limits with the page', async () => {
    agents(() => HttpResponse.json(SIZES));
    const result = await fetchEnvironments('token');
    expect(result).toMatchObject({ ok: true, sizes: SIZES });
  });

  it('still loads the page when the size limits fail', async () => {
    agents(() => HttpResponse.json({ message: 'forbidden' }, { status: 403 }));
    const result = await fetchEnvironments('token');
    expect(result).toMatchObject({ ok: true, sizes: null });
  });
});
