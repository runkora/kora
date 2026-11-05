/** Unit tests for the requests the BFF sends to skills-service. */
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@runkora/server/config', () => ({
  config: { skillsServiceUrl: 'http://skills-service.test/v1' },
}));

import { ApiError } from './api-error';
import { fetchSkill, fetchSkillFile, fetchSkills } from './skills-client';

function stubFetch(response: Response) {
  const fetchMock = vi.fn<(url: string, init: RequestInit) => Promise<Response>>(() =>
    Promise.resolve(response),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => vi.unstubAllGlobals());

describe('skills-client', () => {
  it('asks for one page with its Marketplace, one keys parameter per key, search and the bearer token', async () => {
    const fetchMock = stubFetch(Response.json({ skills: [], total: 0 }));

    await fetchSkills('token-a', {
      marketplace: 'team-skills',
      keys: ['team-skills:skills/pdf', 'team-skills:skills/a,b'],
      search: 'pdf',
      offset: 10,
      limit: 10,
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(
      'http://skills-service.test/v1/marketplaces/skills?offset=10&limit=10&marketplace=team-skills&keys=team-skills%3Askills%2Fpdf&keys=team-skills%3Askills%2Fa%2Cb&search=pdf',
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

  it('asks for one supporting file by its path in the query', async () => {
    const fetchMock = stubFetch(Response.json({}));

    await fetchSkillFile('token-a', 'team-skills', 'skills/pdf', 'reference/a b.md');

    expect(fetchMock.mock.calls[0][0]).toBe(
      'http://skills-service.test/v1/marketplaces/team-skills/skills/skills/pdf?file=reference%2Fa+b.md',
    );
  });

  it.each([
    '../SKILL.md',
    'reference/./forms.md',
    'reference//forms.md',
    '/forms.md',
    'forms.md/',
    '',
  ])('reads file %j as not found without a request', async (file) => {
    const fetchMock = stubFetch(Response.json({}));

    await expect(
      fetchSkillFile('token-a', 'team-skills', 'skills/pdf', file),
    ).rejects.toMatchObject({
      status: 404,
    });
    expect(fetchMock).not.toHaveBeenCalled();
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
