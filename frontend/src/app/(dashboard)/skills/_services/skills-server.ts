import 'server-only';

import type { Marketplace, Skill, SkillFile, SkillsPage } from '@runkora/shared/api';
import { ApiError } from '@runkora/server/api-error';
import { loadFailure, type Loaded } from '@runkora/server/load-failure';
import {
  fetchMarketplaces,
  fetchSkill,
  fetchSkillFile,
  fetchSkills,
} from '@runkora/server/skills-client';
import { PAGE_SIZE, type SkillsBrowse } from '../_types';

export async function loadMarketplaces(accessToken: string): Promise<Loaded<Marketplace[]>> {
  try {
    return { ok: true, data: (await fetchMarketplaces(accessToken)).marketplaces };
  } catch (err) {
    return failure('[loadMarketplaces]', err, 'the Marketplaces');
  }
}

/** skills-service takes at most 200 keys. */
const MAX_KEYS = 200;

const EMPTY_PAGE: SkillsPage = { skills: [], total: 0, offset: 0, limit: PAGE_SIZE };

/**
 * One page of the overview. The Favourites view asks for the user's keys; with none it is empty,
 * since no keys would otherwise mean every skill.
 */
export async function loadSkills(
  accessToken: string,
  browse: SkillsBrowse,
  favourites: string[],
): Promise<Loaded<SkillsPage>> {
  const keys = favourites.filter(Boolean).slice(0, MAX_KEYS);
  if (browse.favourites && keys.length === 0) return { ok: true, data: EMPTY_PAGE };
  try {
    const data = await fetchSkills(accessToken, {
      marketplace: browse.marketplace,
      keys: browse.favourites ? keys : [],
      search: browse.search,
      offset: (browse.page - 1) * PAGE_SIZE,
      limit: PAGE_SIZE,
    });
    return { ok: true, data };
  } catch (err) {
    return failure('[loadSkills]', err, 'the skills', !!browse.marketplace);
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

export async function loadSkillFile(
  accessToken: string,
  marketplace: string,
  path: string,
  file: string,
): Promise<Loaded<SkillFile>> {
  try {
    return { ok: true, data: await fetchSkillFile(accessToken, marketplace, path, file) };
  } catch (err) {
    return failure('[loadSkillFile]', err, 'this file');
  }
}

/** Copy for an unavailable Marketplace whose 502 carries no reason of its own. */
const UPSTREAM_FAILURE = 'This Marketplace could not be loaded. Try again later.';

/**
 * A 502 means the Marketplace is unavailable, with skills-service's reason for the user. On the
 * skills page a listed Marketplace's 404 means a missing repository or ref and reads the same.
 */
function failure(caller: string, err: unknown, subject: string, listed = false): Loaded<never> {
  console.error(`${caller} failed:`, err);
  const unavailable =
    err instanceof ApiError &&
    (err.code === 'marketplace_unavailable' || (listed && err.status === 404));
  if (unavailable) return { ok: false, status: 502, message: err.message || UPSTREAM_FAILURE };
  const failed = loadFailure(err, subject);
  return !failed.ok && failed.status >= 500 ? { ...failed, status: 500 } : failed;
}
