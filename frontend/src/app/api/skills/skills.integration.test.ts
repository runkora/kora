/** Integration tests for the /api/skills BFF route over a stubbed skills-service client. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@orbit/server/api-error';

const mocks = vi.hoisted(() => ({ fetchSkillsPage: vi.fn() }));
vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn().mockResolvedValue({ accessToken: 'test-token' }),
  isSessionValid: vi.fn().mockReturnValue(true),
}));
vi.mock('@runkora/server/config', () => ({ config: { koraApiUrl: 'http://test-identity' } }));
vi.mock('@runkora/server/skills-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkill: vi.fn(),
  fetchSkillsPage: mocks.fetchSkillsPage,
}));
import { GET } from './route';

const call = (query: string) => GET(new NextRequest(`http://localhost/api/skills?${query}`));

beforeEach(() => {
  mocks.fetchSkillsPage.mockReset();
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

describe('GET /api/skills', () => {
  it('forwards one page request, dropping a short search and an invalid page', async () => {
    const page = { skills: [], total: 0, offset: 0, limit: 10 };
    mocks.fetchSkillsPage.mockResolvedValue(page);

    const res = await call('marketplace=team-skills&search=pd&page=x&pageSize=10');

    expect(await res.json()).toEqual({ data: page });
    expect(mocks.fetchSkillsPage).toHaveBeenCalledExactlyOnceWith('test-token', 'team-skills', {
      search: '',
      offset: 0,
      limit: 10,
    });
  });

  it('passes a refusal through and answers any upstream failure with 502', async () => {
    mocks.fetchSkillsPage.mockRejectedValueOnce(new ApiError(403, 'forbidden', 'raw'));
    expect((await call('marketplace=team-skills')).status).toBe(403);

    mocks.fetchSkillsPage.mockRejectedValueOnce(new ApiError(503, 'UNKNOWN', 'raw'));
    expect((await call('marketplace=team-skills')).status).toBe(502);
  });
});
