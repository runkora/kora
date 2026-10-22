/**
 * Unit tests for the Marketplace client's requests to skills-service.
 *
 * These check the request shape (URL, headers, query encoding) and the error
 * mapping, the same way preferences-server.test.ts checks a real client.
 * Sort, page, and search-match behavior belong to the skills-service itself
 * and to marketplace-server.ts, not to this client.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ListSkillsResponse, SkillSummary } from '@orbit/shared/api';

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
  vi.restoreAllMocks();
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

  it('loads every cursor batch with the same source, search, and bearer token', async () => {
    const skills: SkillSummary[] = Array.from({ length: 405 }, (_, index) => ({
      name: `Skill ${index}`,
      description: '',
      skillSlug: `acme__skills__${index}`,
    }));
    const firstCursor = 'next+ /?&=';
    const secondCursor = 'last/+=&?#';
    const batches = new Map<string, ListSkillsResponse>([
      ['', { skills: skills.slice(0, 200), nextCursor: firstCursor }],
      [firstCursor, { skills: skills.slice(200, 400), nextCursor: secondCursor }],
      [secondCursor, { skills: skills.slice(400) }],
    ]);
    const requests: {
      path: string;
      search: string | null;
      cursor: string | null;
      authorization: string | null;
    }[] = [];
    stubFetch(async (url, init) => {
      const parsed = new URL(url);
      const cursor = parsed.searchParams.get('cursor');
      requests.push({
        path: parsed.pathname,
        search: parsed.searchParams.get('search'),
        cursor,
        authorization: new Headers(init.headers).get('Authorization'),
      });
      const batch = batches.get(cursor ?? '');
      if (!batch) throw new Error('Unexpected cursor');
      return Response.json(batch);
    });

    const result = await fetchSkills('session-token', 'acme__skills', 'react native');

    expect(result.skills).toHaveLength(405);
    expect(result.skills).toEqual(skills);
    expect(requests).toEqual(
      [null, firstCursor, secondCursor].map((cursor) => ({
        path: '/marketplaces/acme__skills/skills',
        search: 'react native',
        cursor,
        authorization: 'Bearer session-token',
      })),
    );
  });

  it('rejects a failed later batch instead of returning a partial list', async () => {
    let calls = 0;
    stubFetch(async () => {
      calls += 1;
      return calls === 1
        ? Response.json({
            skills: [{ name: 'First', description: '', skillSlug: 'first' }],
            nextCursor: 'next',
          })
        : Response.json({ code: 'unavailable', message: 'Unavailable' }, { status: 503 });
    });

    await expect(fetchSkills('token', 'acme__skills', '')).rejects.toMatchObject({ status: 503 });
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('stops before requesting another batch when the caller cancels', async () => {
    const controller = new AbortController();
    stubFetch(async () => {
      controller.abort();
      return Response.json({ skills: [], nextCursor: 'next' });
    });

    await expect(fetchSkills('token', 'acme__skills', '', controller.signal)).rejects.toMatchObject(
      { name: 'AbortError' },
    );
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('keeps the initial deadline active during later batches', async () => {
    const deadlines: AbortController[] = [];
    vi.spyOn(AbortSignal, 'timeout').mockImplementation(() => {
      const controller = new AbortController();
      deadlines.push(controller);
      return controller.signal;
    });
    let calls = 0;
    stubFetch(async (_url, init) => {
      calls += 1;
      if (calls === 1) return Response.json({ skills: [], nextCursor: 'next' });
      const deadline = deadlines[0];
      if (!deadline || !init.signal) throw new Error('Missing request deadline');
      deadline.abort(new DOMException('Request timed out', 'TimeoutError'));
      init.signal.throwIfAborted();
      return Response.json({ skills: [] });
    });

    await expect(fetchSkills('token', 'acme__skills', '')).rejects.toMatchObject({
      name: 'TimeoutError',
    });
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('rejects a repeated cursor instead of requesting the same batch indefinitely', async () => {
    stubFetch(async () => Response.json({ skills: [], nextCursor: 'same-cursor' }));

    await expect(fetchSkills('token', 'acme__skills', '')).rejects.toMatchObject({
      status: 502,
      code: 'invalid_response',
    });
    expect(fetch).toHaveBeenCalledTimes(2);
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
  it('encodes both identifiers as opaque upstream path segments', async () => {
    let seenUrl = '';
    stubFetch(async (url) => {
      seenUrl = url;
      return Response.json({});
    });

    await fetchSkillDetail('token', 'team/repo ?#', 'skill/50% ?#');

    expect(seenUrl).toBe(
      'http://skills-service.test/marketplaces/team%2Frepo%20%3F%23/skills/skill%2F50%25%20%3F%23',
    );
  });

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
