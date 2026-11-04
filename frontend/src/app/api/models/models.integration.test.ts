/** Integration tests for GET /api/models: capability forwarding and gateway error mapping. */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextRequest } from 'next/server';

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

vi.mock('../../(dashboard)/model-gateway/_services/models-server', () => ({
  fetchRawModelsFromGateway: vi.fn(),
}));

vi.mock('../../(dashboard)/policy/_services/policy-server', () => ({
  fetchEffectiveScopes: vi.fn(),
}));

import { GET } from './route';
import { getSession } from '@runkora/server/session';
import { fetchRawModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';
import { fetchEffectiveScopes } from '../../(dashboard)/policy/_services/policy-server';

const get = (query = '') => GET(new NextRequest(`http://localhost/api/models${query}`));

describe('GET /api/models', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSession).mockResolvedValue({ accessToken: 'test-access-token' } as never);
    vi.mocked(fetchEffectiveScopes).mockResolvedValue({ ok: true, data: { scopes: [] } } as never);
    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({ ok: true, models: [] });
  });

  describe('capability forwarding', () => {
    it.each([
      ['repeated params', '?capability=chat&capability=embedding', ['chat', 'embedding']],
      ['a comma value, as given', '?capability=chat,embedding', ['chat,embedding']],
      ['no param', '', []],
    ])('forwards %s to the gateway', async (_name, query, expected) => {
      const res = await get(query);

      expect(res.status).toBe(200);
      expect(fetchRawModelsFromGateway).toHaveBeenCalledWith('test-access-token', expected);
    });
  });

  describe('gateway errors', () => {
    it('returns 400 invalid_request with the gateway message for a JSON 400', async () => {
      vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
        ok: false,
        status: 400,
        message: JSON.stringify({
          code: 'invalid_request',
          message: 'unknown model capability "bogus": valid values are chat',
        }),
      });

      const res = await get('?capability=bogus');

      expect(res.status).toBe(400);
      expect(await res.json()).toEqual({
        code: 'invalid_request',
        message: 'unknown model capability "bogus": valid values are chat',
      });
    });

    it('returns 400 invalid_request with a generic message for a plain-text 400', async () => {
      vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
        ok: false,
        status: 400,
        message: 'bad request',
      });

      const res = await get('?capability=bogus');

      expect(res.status).toBe(400);
      expect(await res.json()).toEqual({ code: 'invalid_request', message: 'Invalid request' });
    });

    it('still maps a gateway 5xx to 502 upstream_error', async () => {
      vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
        ok: false,
        status: 503,
        message: 'unavailable',
      });

      const res = await get();

      expect(res.status).toBe(502);
      expect(await res.json()).toEqual({
        code: 'upstream_error',
        message: 'Failed to load models from inference gateway',
      });
    });
  });
});
