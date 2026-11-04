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
    return failure('[loadSkillsPage]', err, 'the skills', true);
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

/**
 * A 502 means the Marketplace is unavailable, with skills-service's reason for the user. A listed
 * Marketplace's 404 is a missing repository or ref, so the skills page reads it the same way.
 */
function failure(caller: string, err: unknown, subject: string, listed = false): Loaded<never> {
  console.error(`${caller} failed:`, err);
  const unavailable =
    err instanceof ApiError &&
    (err.code === 'marketplace_unavailable' || (listed && err.status === 404));
  if (unavailable) return { ok: false, status: 502, message: err.message };
  const failed = loadFailure(err, subject);
  return !failed.ok && failed.status >= 500 ? { ...failed, status: 500 } : failed;
}
