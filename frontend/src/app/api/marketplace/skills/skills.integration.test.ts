import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@orbit/server/api-error';

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
  fetchMarketplaces: vi.fn(),
  fetchSkills: vi.fn(),
}));
vi.mock('@orbit/server/session', () => ({
  getSession: mocks.getSession,
  isSessionValid: mocks.isSessionValid,
}));
vi.mock('@orbit/server/config', () => ({ config: { orbitApiUrl: 'http://test-identity' } }));
// The production client is fixture-backed. Stub its boundary, keeping the
// actual auth wrapper, parameter validation, service, and response mapping.
vi.mock('@orbit/server/marketplace-client', () => ({
  fetchMarketplaces: mocks.fetchMarketplaces,
  fetchSkills: mocks.fetchSkills,
  fetchSkillDetail: vi.fn(),
}));
import { GET } from './route';

beforeEach(() => {
  vi.resetAllMocks();
  mocks.getSession.mockResolvedValue({ accessToken: 'test-token' });
  mocks.isSessionValid.mockReturnValue(true);
  mocks.fetchMarketplaces.mockResolvedValue({
    marketplaces: [{ name: 'Core' }, { name: 'Other' }],
  });
  mocks.fetchSkills.mockResolvedValue({ packages: [], total: 0 });
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => vi.restoreAllMocks());

function request(search = '?marketplace=Core', signal?: AbortSignal) {
  return new NextRequest(`http://localhost/api/marketplace/skills${search}`, { signal });
}

describe('GET /api/marketplace/skills', () => {
  it('rejects an invalid session before loading any Marketplace data', async () => {
    mocks.isSessionValid.mockReturnValue(false);
    const response = await GET(request());
    expect(response.status).toBe(401);
    expect(await response.json()).toMatchObject({ code: 'unauthorized' });
    expect(mocks.fetchMarketplaces).not.toHaveBeenCalled();
  });

  it('validates browse parameters and returns the authorized result page', async () => {
    const response = await GET(
      request('?marketplace=Core&page=-2&pageSize=999&sort=invalid&query=ab'),
    );
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({
      data: { browse: { marketplace: 'Core', page: 1, pageSize: 24, sort: 'name-asc', query: '' } },
    });
    expect(mocks.fetchSkills).toHaveBeenCalledWith(
      'test-token',
      'Core',
      { offset: 0, limit: 24, search: '', sort: 'name-asc' },
      expect.any(AbortSignal),
    );
  });

  it.each([403, 404, 429, 500, 502, 504])(
    'maps upstream status %i and retains the loaded Marketplace list',
    async (status) => {
      mocks.fetchSkills.mockRejectedValue(new ApiError(status, 'upstream_error', 'Unavailable'));
      const response = await GET(request());
      expect(response.status).toBe(status >= 500 ? 502 : status);
      expect(await response.json()).toMatchObject({
        code: 'upstream_error',
        data: { marketplaces: [{ name: 'Core' }, { name: 'Other' }], skills: [] },
      });
    },
  );

  it('maps an unexpected Marketplace-list failure to a gateway error', async () => {
    mocks.fetchMarketplaces.mockRejectedValue(new Error('Unavailable'));
    const response = await GET(request());
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: 'upstream_error',
      message: 'Failed to load Skills',
    });
  });

  it('maps cancellation to 499 without requesting Skills', async () => {
    const controller = new AbortController();
    controller.abort();
    mocks.fetchMarketplaces.mockRejectedValue(controller.signal.reason);
    const response = await GET(request('?marketplace=Core', controller.signal));
    expect(response.status).toBe(499);
    expect(await response.json()).toEqual({ code: 'cancelled', message: 'Request cancelled' });
    expect(mocks.fetchSkills).not.toHaveBeenCalled();
  });
});
