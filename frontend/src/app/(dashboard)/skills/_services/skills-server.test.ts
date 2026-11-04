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
      new ApiError(502, 'marketplace_unavailable', 'The GitHub App credential is unavailable.'),
      { status: 502, message: 'The GitHub App credential is unavailable.' },
    ],
    [
      new ApiError(403, 'forbidden', 'raw'),
      { status: 403, message: 'You do not have access to the skills.' },
    ],
    [
      new ApiError(404, 'not_found', 'raw'),
      { status: 404, message: 'Could not load the skills. Try again.' },
    ],
    [
      new TypeError('fetch failed'),
      { status: 500, message: 'Could not load the skills. Try again.' },
    ],
  ])('maps %s to a page message', async (err, expected) => {
    fetchSkillsPage.mockRejectedValue(err);

    expect(await loadSkillsPage('token', browse, 25)).toEqual({ ok: false, ...expected });
  });
});
