/** Integration tests for the /api/skills BFF route over a stubbed skills-service client. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@orbit/server/api-error';

const mocks = vi.hoisted(() => ({ fetchSkills: vi.fn(), getPreferences: vi.fn() }));
vi.mock('../../(dashboard)/_preferences/preferences-server', () => ({
  getPreferences: mocks.getPreferences,
}));
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

  it('reads the favourite keys from the stored preferences for the Favourites view', async () => {
    mocks.fetchSkills.mockResolvedValue({ skills: [], total: 0, offset: 0, limit: 10 });
    mocks.getPreferences.mockResolvedValue({
      skills: { favouriteSkills: ['a:skills/pdf', 'b:tools/lint'] },
    });

    await call('favourites=1&keys=ignored%3Askills%2Fx&page=2');

    expect(mocks.fetchSkills).toHaveBeenCalledWith('test-token', {
      marketplace: '',
      keys: ['a:skills/pdf', 'b:tools/lint'],
      search: '',
      offset: 10,
      limit: 10,
    });
  });

  it('passes an upstream refusal through', async () => {
    mocks.fetchSkills.mockRejectedValueOnce(new ApiError(403, 'forbidden', 'raw'));
    expect((await call('marketplace=team-skills')).status).toBe(403);
  });
});
