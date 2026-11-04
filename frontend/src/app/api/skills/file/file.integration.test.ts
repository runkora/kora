/** Integration tests for the /api/skills/file BFF route over a stubbed skills-service client. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { ApiError } from '@runkora/server/api-error';

const mocks = vi.hoisted(() => ({ fetchSkillFile: vi.fn() }));
vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn().mockResolvedValue({ accessToken: 'test-token' }),
  isSessionValid: vi.fn().mockReturnValue(true),
}));
vi.mock('@runkora/server/config', () => ({ config: { koraApiUrl: 'http://test-identity' } }));
vi.mock('@runkora/server/skills-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkill: vi.fn(),
  fetchSkills: vi.fn(),
  fetchSkillFile: mocks.fetchSkillFile,
}));
import { GET } from './route';

const call = () =>
  GET(
    new NextRequest(
      'http://localhost/api/skills/file?marketplace=team-skills&path=skills%2Fpdf&file=reference%2Fforms.md',
    ),
  );

beforeEach(() => {
  mocks.fetchSkillFile.mockReset();
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

describe('GET /api/skills/file', () => {
  it('forwards the Marketplace, skill path and file', async () => {
    const file = { path: 'reference/forms.md', content: 'Fill each field.' };
    mocks.fetchSkillFile.mockResolvedValue(file);

    expect(await (await call()).json()).toEqual({ data: file });
    expect(mocks.fetchSkillFile).toHaveBeenCalledExactlyOnceWith(
      'test-token',
      'team-skills',
      'skills/pdf',
      'reference/forms.md',
    );
  });

  it('answers an unavailable Marketplace with 502 and its message', async () => {
    mocks.fetchSkillFile.mockRejectedValue(
      new ApiError(502, 'marketplace_unavailable', 'This Marketplace could not be loaded.'),
    );

    const res = await call();

    expect(res.status).toBe(502);
    expect((await res.json()).message).toBe('This Marketplace could not be loaded.');
  });
});
