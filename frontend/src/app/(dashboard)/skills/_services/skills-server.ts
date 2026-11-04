import 'server-only';

import type { Marketplace, MarketplaceSkillsPage, Skill } from '@runkora/shared/api';
import { ApiError } from '@runkora/server/api-error';
import { loadFailure, type Loaded } from '@runkora/server/load-failure';
import { fetchMarketplaces, fetchSkill, fetchSkillsPage } from '@runkora/server/skills-client';
import type { SkillsBrowse } from '../_types';

export async function loadMarketplaces(accessToken: string): Promise<Loaded<Marketplace[]>> {
  try {
    return { ok: true, data: (await fetchMarketplaces(accessToken)).marketplaces };
  } catch (err) {
    return failure('[loadMarketplaces]', err, 'the Marketplaces');
  }
}

export async function loadSkillsPage(
  accessToken: string,
  browse: SkillsBrowse,
  pageSize: number,
): Promise<Loaded<MarketplaceSkillsPage>> {
  try {
    const data = await fetchSkillsPage(accessToken, browse.marketplace, {
      search: browse.search,
      offset: (browse.page - 1) * pageSize,
      limit: pageSize,
    });
    return { ok: true, data };
  } catch (err) {
    return failure('[loadSkillsPage]', err, 'the skills');
  }
}

export async function loadSkill(
  accessToken: string,
  marketplace: string,
  path: string,
): Promise<Loaded<Skill>> {
  try {
    return { ok: true, data: await fetchSkill(accessToken, marketplace, path) };
  } catch (err) {
    return failure('[loadSkill]', err, 'this skill');
  }
}

/** skills-service words a 502 for the user, so its reason reaches the page as is. */
function failure(caller: string, err: unknown, subject: string): Loaded<never> {
  console.error(`${caller} failed:`, err);
  if (err instanceof ApiError && err.code === 'marketplace_unavailable') {
    return { ok: false, status: 502, message: err.message };
  }
  return loadFailure(err, subject);
}
