/** Unit tests for the requests the BFF sends to skills-service. */
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@orbit/server/config', () => ({
  config: { skillsServiceUrl: 'http://skills-service.test/v1' },
}));

import { ApiError } from './api-error';
import { fetchSkill, fetchSkillsPage } from './skills-client';

function stubFetch(response: Response) {
  const fetchMock = vi.fn((_url: string, _init: RequestInit) => Promise.resolve(response));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => vi.unstubAllGlobals());

describe('skills-client', () => {
  it('asks for one page with offset, limit, search and the bearer token', async () => {
    const fetchMock = stubFetch(Response.json({ skills: [], total: 0 }));

    await fetchSkillsPage('token-a', 'team skills', { search: 'pdf', offset: 25, limit: 25 });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(
      'http://skills-service.test/v1/marketplaces/team%20skills?offset=25&limit=25&search=pdf',
    );
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer token-a');
  });

  it('encodes each segment of a skill path and keeps its slashes', async () => {
    const fetchMock = stubFetch(Response.json({}));

    await fetchSkill('token-a', 'team-skills', 'skills/my skill');

    expect(fetchMock.mock.calls[0][0]).toBe(
      'http://skills-service.test/v1/marketplaces/team-skills/skills/skills/my%20skill',
    );
  });

  it.each([
    ['team-skills', '../../marketplaces'],
    ['team-skills', 'skills/./pdf'],
    ['team-skills', 'skills//pdf'],
    ['team-skills', '/skills/pdf'],
    ['team-skills', 'skills/pdf/'],
    ['..', 'skills/pdf'],
  ])('reads slug %s and path %s as not found without a request', async (slug, path) => {
    const fetchMock = stubFetch(Response.json({}));

    await expect(fetchSkill('token-a', slug, path)).rejects.toMatchObject({ status: 404 });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('throws an ApiError with the upstream status, code and message', async () => {
    stubFetch(
      Response.json({ code: 'marketplace_unavailable', message: 'Unavailable.' }, { status: 502 }),
    );

    await expect(fetchSkill('token-a', 'team-skills', 'skills/pdf')).rejects.toMatchObject(
      new ApiError(502, 'marketplace_unavailable', 'Unavailable.'),
    );
  });
});
