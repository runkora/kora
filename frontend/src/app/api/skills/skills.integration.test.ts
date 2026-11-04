/** Integration tests for the /api/skills BFF route over a stubbed skills-service client. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@orbit/server/api-error';

const mocks = vi.hoisted(() => ({ fetchSkills: vi.fn() }));
vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn().mockResolvedValue({ accessToken: 'test-token' }),
  isSessionValid: vi.fn().mockReturnValue(true),
}));
vi.mock('@runkora/server/config', () => ({ config: { koraApiUrl: 'http://test-identity' } }));
vi.mock('@runkora/server/skills-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkill: vi.fn(),
  fetchSkills: mocks.fetchSkills,
}));
import { GET } from './route';

const call = (query: string) => GET(new NextRequest(`http://localhost/api/skills?${query}`));

beforeEach(() => {
  mocks.fetchSkills.mockReset();
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

describe('GET /api/skills', () => {
  it('forwards one page request, dropping a short search and an invalid page', async () => {
    const page = { skills: [], total: 0, offset: 0, limit: 10 };
    mocks.fetchSkills.mockResolvedValue(page);

    const res = await call('marketplace=team-skills&search=pd&page=x');

    expect(await res.json()).toEqual({ data: page });
    expect(mocks.fetchSkills).toHaveBeenCalledExactlyOnceWith('test-token', {
      marketplace: 'team-skills',
      keys: [],
      search: '',
      offset: 0,
      limit: 10,
    });
  });

  it('forwards the favourite keys for the Favourites view', async () => {
    mocks.fetchSkills.mockResolvedValue({ skills: [], total: 0, offset: 0, limit: 10 });

    await call('favourites=1&keys=a%3Askills%2Fpdf%2Cb%3Atools%2Flint&page=2');

    expect(mocks.fetchSkills).toHaveBeenCalledWith('test-token', {
      marketplace: '',
      keys: ['a:skills/pdf', 'b:tools/lint'],
      search: '',
      offset: 10,
      limit: 10,
    });
  });

  it('passes a refusal through and answers an unavailable Marketplace with 502', async () => {
    mocks.fetchSkills.mockRejectedValueOnce(new ApiError(403, 'forbidden', 'raw'));
    expect((await call('marketplace=team-skills')).status).toBe(403);

    mocks.fetchSkills.mockRejectedValueOnce(
      new ApiError(502, 'marketplace_unavailable', 'No credential.'),
    );
    expect((await call('marketplace=team-skills')).status).toBe(502);
  });
});
