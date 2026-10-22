import { beforeEach, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import type { ListSkillsResponse, SkillSummary } from '@orbit/shared/api';
import type { SkillSort } from '../_types';

vi.mock('@orbit/server/config', () => ({
  config: { skillsServiceUrl: 'http://skills-service.test/v1' },
}));
vi.mock('../../_preferences/preferences-server', () => ({ getPreferences: vi.fn() }));

import { loadMarketplaceOverview } from './marketplace-server';

const MARKETPLACE = {
  name: 'Team Skills',
  marketplaceSlug: 'acme__skills',
  link: 'https://github.com/acme/skills',
};
const SKILLS: SkillSummary[] = Array.from({ length: 405 }, (_, index) => ({
  name: `Skill ${String(404 - index).padStart(3, '0')}`,
  description: '',
  skillSlug: `acme__skills__${String(index).padStart(3, '0')}`,
}));
const BATCHES = new Map<string, ListSkillsResponse>([
  ['', { skills: SKILLS.slice(0, 200), nextCursor: 'second-batch' }],
  ['second-batch', { skills: SKILLS.slice(200, 400), nextCursor: 'third-batch' }],
  ['third-batch', { skills: SKILLS.slice(400) }],
]);
let requestedCursors: (string | null)[];

beforeEach(() => {
  requestedCursors = [];
  server.use(
    http.get('http://skills-service.test/v1/marketplaces', () =>
      HttpResponse.json({ marketplaces: [MARKETPLACE] }),
    ),
    http.get('http://skills-service.test/v1/marketplaces/acme__skills/skills', ({ request }) => {
      const cursor = new URL(request.url).searchParams.get('cursor');
      requestedCursors.push(cursor);
      const batch = BATCHES.get(cursor ?? '');
      return batch
        ? HttpResponse.json(batch)
        : HttpResponse.json({ message: 'Invalid cursor' }, { status: 400 });
    }),
  );
});

const SCENARIOS: { sort: SkillSort; page: number; first: string; last: string; count: number }[] = [
  { sort: 'name-asc', page: 17, first: 'Skill 384', last: 'Skill 404', count: 21 },
  { sort: 'name-desc', page: 1, first: 'Skill 404', last: 'Skill 381', count: 24 },
  { sort: 'name-asc', page: 100, first: 'Skill 384', last: 'Skill 404', count: 21 },
];

describe('Marketplace cursor pagination', () => {
  it.each(SCENARIOS)('sorts and counts every batch for $sort page $page', async (scenario) => {
    const result = await loadMarketplaceOverview('test-token', {
      marketplace: MARKETPLACE.marketplaceSlug,
      query: 'Skill',
      sort: scenario.sort,
      page: scenario.page,
      pageSize: 24,
    });

    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error(result.message);
    expect(result.overview.total).toBe(405);
    expect(result.overview.skills).toHaveLength(scenario.count);
    expect(result.overview.skills[0].name).toBe(scenario.first);
    expect(result.overview.skills.at(-1)?.name).toBe(scenario.last);
    expect(result.overview.browse.page).toBe(scenario.sort === 'name-desc' ? 1 : 17);
    expect(requestedCursors).toEqual([null, 'second-batch', 'third-batch']);
  });
});
