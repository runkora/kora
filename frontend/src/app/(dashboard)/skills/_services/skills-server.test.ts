/** Unit tests for the paging and failure mapping between the pages and skills-service. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@runkora/server/api-error';

const { fetchSkills } = vi.hoisted(() => ({ fetchSkills: vi.fn() }));
vi.mock('@runkora/server/skills-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkill: vi.fn(),
  fetchSkillFile: vi.fn(),
  fetchSkills,
}));

import { loadSkills } from './skills-server';

const browse = { marketplace: 'team-skills', favourites: false, search: 'pdf', page: 3 };

beforeEach(() => {
  fetchSkills.mockReset();
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

describe('loadSkills', () => {
  it('asks for ten skills a page from the page number', async () => {
    fetchSkills.mockResolvedValue({ skills: [], total: 0 });

    await loadSkills('token', browse, ['team-skills:skills/pdf']);

    expect(fetchSkills).toHaveBeenCalledWith('token', {
      marketplace: 'team-skills',
      keys: [],
      search: 'pdf',
      offset: 20,
      limit: 10,
    });
  });

  it('sends the favourites as keys, and with none answers an empty page without a request', async () => {
    fetchSkills.mockResolvedValue({ skills: [], total: 0 });
    const favourites = { marketplace: '', favourites: true, search: '', page: 1 };

    await loadSkills('token', favourites, ['a:skills/pdf']);
    expect(fetchSkills).toHaveBeenCalledWith(
      'token',
      expect.objectContaining({ keys: ['a:skills/pdf'] }),
    );

    fetchSkills.mockClear();
    expect(await loadSkills('token', favourites, [])).toMatchObject({
      ok: true,
      data: { total: 0 },
    });
    expect(fetchSkills).not.toHaveBeenCalled();
  });

  it.each([
    [
      new ApiError(
        502,
        'marketplace_unavailable',
        'This Marketplace could not be loaded. Try again later.',
      ),
      { status: 502, message: 'This Marketplace could not be loaded. Try again later.' },
    ],
    [
      new ApiError(403, 'forbidden', 'raw'),
      { status: 403, message: 'You do not have access to the skills.' },
    ],
    [
      new ApiError(502, 'marketplace_unavailable', ''),
      { status: 502, message: 'This Marketplace could not be loaded. Try again later.' },
    ],
    [
      new ApiError(404, 'not_found', 'Repository not found.'),
      { status: 502, message: 'Repository not found.' },
    ],
    [
      new TypeError('fetch failed'),
      { status: 500, message: 'Could not load the skills. Try again.' },
    ],
    [
      new ApiError(502, 'UNKNOWN', 'Bad Gateway'),
      { status: 500, message: 'Could not load the skills. Try again.' },
    ],
  ])('maps %s to a page message', async (err, expected) => {
    fetchSkills.mockRejectedValue(err);

    expect(await loadSkills('token', browse, [])).toEqual({ ok: false, ...expected });
  });
});
