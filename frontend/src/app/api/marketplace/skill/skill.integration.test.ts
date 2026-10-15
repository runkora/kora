import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@orbit/server/api-error';
import type { Skill } from '@orbit/shared/api';

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
  fetchSkillDetail: vi.fn(),
}));
vi.mock('@orbit/server/session', () => ({
  getSession: mocks.getSession,
  isSessionValid: mocks.isSessionValid,
}));
vi.mock('@orbit/server/config', () => ({ config: { orbitApiUrl: 'http://test-identity' } }));
// The production client is fixture-backed. Stub its boundary, keeping the
// actual auth wrapper, service, and response mapping.
vi.mock('@orbit/server/marketplace-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkills: vi.fn(),
  fetchSkillDetail: mocks.fetchSkillDetail,
}));
import { GET } from './route';

const MARKETPLACE = 'clear-route__core?#%';
const SKILL_SLUG = 'clear-route__core__reviewer&x=1';

const SKILL: Skill = {
  name: 'Reviewer',
  description: 'Reviews code.',
  skillSlug: SKILL_SLUG,
  skill: { path: 'skills/reviewer/SKILL.md', name: 'SKILL.md', content: '# Reviewer' },
  documents: [],
};

beforeEach(() => {
  vi.resetAllMocks();
  mocks.getSession.mockResolvedValue({ accessToken: 'test-token' });
  mocks.isSessionValid.mockReturnValue(true);
  mocks.fetchSkillDetail.mockResolvedValue(SKILL);
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => vi.restoreAllMocks());

function request(
  search = `?marketplace=${encodeURIComponent(MARKETPLACE)}&skill=${encodeURIComponent(SKILL_SLUG)}`,
) {
  return new NextRequest(`http://localhost/api/marketplace/skill${search}`);
}

describe('GET /api/marketplace/skill', () => {
  it('rejects an invalid session before loading the Skill', async () => {
    mocks.isSessionValid.mockReturnValue(false);
    const response = await GET(request());
    expect(response.status).toBe(401);
    expect(await response.json()).toMatchObject({ code: 'unauthorized' });
    expect(mocks.fetchSkillDetail).not.toHaveBeenCalled();
  });

  it.each([
    ['no slugs', ''],
    ['no Skill slug', `?marketplace=${encodeURIComponent(MARKETPLACE)}`],
    ['no Marketplace slug', `?skill=${encodeURIComponent(SKILL_SLUG)}`],
  ])('rejects a request with %s', async (_, search) => {
    const response = await GET(request(search));
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: 'invalid_request' });
    expect(mocks.fetchSkillDetail).not.toHaveBeenCalled();
  });

  it('returns the Skill for the decoded slugs and the session token', async () => {
    const response = await GET(request());
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ data: SKILL });
    expect(mocks.fetchSkillDetail).toHaveBeenCalledWith('test-token', MARKETPLACE, SKILL_SLUG);
  });

  it('maps a missing Skill to 404', async () => {
    mocks.fetchSkillDetail.mockResolvedValue(null);
    const response = await GET(request());
    expect(response.status).toBe(404);
    expect(await response.json()).toMatchObject({ code: 'not_found' });
  });

  it.each([403, 404, 429, 500, 502, 504])('maps upstream status %i', async (status) => {
    mocks.fetchSkillDetail.mockRejectedValue(new ApiError(status, 'upstream_error', 'Unavailable'));
    const response = await GET(request());
    expect(response.status).toBe(status >= 500 ? 502 : status);
  });

  it('maps an unexpected failure to a gateway error', async () => {
    mocks.fetchSkillDetail.mockRejectedValue(new Error('boom'));
    const response = await GET(request());
    expect(response.status).toBe(502);
    expect(await response.json()).toMatchObject({ code: 'upstream_error' });
  });
});
