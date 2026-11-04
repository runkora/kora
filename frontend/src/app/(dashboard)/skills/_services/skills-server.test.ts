/** Unit tests for the paging and failure mapping between the pages and skills-service. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@runkora/server/api-error';

const { fetchSkillsPage } = vi.hoisted(() => ({ fetchSkillsPage: vi.fn() }));
vi.mock('@runkora/server/skills-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkill: vi.fn(),
  fetchSkillsPage,
}));

import { loadSkillsPage } from './skills-server';

const browse = { marketplace: 'team-skills', search: 'pdf', page: 3 };

beforeEach(() => {
  fetchSkillsPage.mockReset();
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

describe('loadSkillsPage', () => {
  it('turns the page number into an offset of whole pages', async () => {
    fetchSkillsPage.mockResolvedValue({ skills: [], total: 0 });

    await loadSkillsPage('token', browse, 25);

    expect(fetchSkillsPage).toHaveBeenCalledWith('token', 'team-skills', {
      search: 'pdf',
      offset: 50,
      limit: 25,
    });
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
      new ApiError(404, 'not_found', 'Repository not found.'),
      { status: 502, message: 'Repository not found.' },
    ],
    [
      new TypeError('fetch failed'),
      { status: 500, message: 'This Marketplace could not be loaded. Try again later.' },
    ],
    [
      new ApiError(502, 'UNKNOWN', 'Bad Gateway'),
      { status: 500, message: 'This Marketplace could not be loaded. Try again later.' },
    ],
  ])('maps %s to a page message', async (err, expected) => {
    fetchSkillsPage.mockRejectedValue(err);

    expect(await loadSkillsPage('token', browse, 25)).toEqual({ ok: false, ...expected });
  });
});
