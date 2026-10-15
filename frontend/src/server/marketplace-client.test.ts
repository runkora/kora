/**
 * Unit tests for the Marketplace client's requests to skills-service.
 *
 * These check the request shape (URL, headers, query encoding) and the error
 * mapping, the same way preferences-server.test.ts checks a real client.
 * Sort, page, and search-match behavior belong to the skills-service itself
 * and to marketplace-server.ts, not to this client.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@orbit/server/config', () => ({
  config: { skillsServiceUrl: 'http://skills-service.test' },
}));

import { ApiError } from './api-error';
import { fetchMarketplaces, fetchSkillDetail, fetchSkills } from './marketplace-client';

function stubFetch(respond: (url: string, init: RequestInit) => Promise<Response>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init: RequestInit) => respond(url, init)),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('fetchMarketplaces', () => {
  it('requests GET /marketplaces with a bearer token', async () => {
    let seenUrl = '';
    let seenAuth: string | null = null;
    stubFetch(async (url, init) => {
      seenUrl = url;
      seenAuth = (init.headers as Record<string, string>).Authorization;
      return Response.json({ marketplaces: [] });
    });

    const result = await fetchMarketplaces('token-a');

    expect(seenUrl).toBe('http://skills-service.test/marketplaces');
    expect(seenAuth).toBe('Bearer token-a');
    expect(result).toEqual({ marketplaces: [] });
  });
});

describe('fetchSkills', () => {
  it('omits ?search= for an empty query', async () => {
    let seenUrl = '';
    stubFetch(async (url) => {
      seenUrl = url;
      return Response.json({ skills: [] });
    });

    await fetchSkills('token', 'acme__skills', '');

    expect(seenUrl).toBe('http://skills-service.test/marketplaces/acme__skills/skills');
  });

  it('encodes the search query', async () => {
    let seenUrl = '';
    stubFetch(async (url) => {
      seenUrl = url;
      return Response.json({ skills: [] });
    });

    await fetchSkills('token', 'acme__skills', 'react native');

    expect(seenUrl).toBe(
      'http://skills-service.test/marketplaces/acme__skills/skills?search=react+native',
    );
  });

  it('throws an ApiError for an unknown Marketplace', async () => {
    stubFetch(async () =>
      Response.json(
        { code: 'resource_not_found', message: 'marketplace not found' },
        { status: 404 },
      ),
    );

    await expect(fetchSkills('token', 'nobody__nothing', '')).rejects.toMatchObject({
      status: 404,
      code: 'resource_not_found',
    });
    await expect(fetchSkills('token', 'nobody__nothing', '')).rejects.toBeInstanceOf(ApiError);
  });
});

describe('fetchSkillDetail', () => {
  it('returns the Skill on a successful response', async () => {
    const skill = {
      name: 'React Expert',
      description: 'd',
      skillSlug: 'acme__skills__react-expert',
      skill: { path: 'SKILL.md', name: 'SKILL.md', content: '# React Expert' },
      documents: [],
    };
    stubFetch(async () => Response.json(skill));

    const result = await fetchSkillDetail('token', 'acme__skills', 'acme__skills__react-expert');

    expect(result).toEqual(skill);
  });

  it('returns null for a missing Skill instead of throwing', async () => {
    stubFetch(async () =>
      Response.json({ code: 'resource_not_found', message: 'skill not found' }, { status: 404 }),
    );

    const result = await fetchSkillDetail('token', 'acme__skills', 'acme__skills__missing');

    expect(result).toBeNull();
  });

  it('still throws for a non-404 failure', async () => {
    stubFetch(async () => new Response('unavailable', { status: 503 }));

    await expect(
      fetchSkillDetail('token', 'acme__skills', 'acme__skills__x'),
    ).rejects.toMatchObject({ status: 503 });
  });
});
