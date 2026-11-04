import 'server-only';

import type { Marketplace, MarketplaceSkillsPage, Skill, SkillFile } from '@runkora/shared/api';
import { ApiError } from '@runkora/server/api-error';
import { loadFailure, type Loaded } from '@runkora/server/load-failure';
import {
  fetchMarketplaces,
  fetchSkill,
  fetchSkillFile,
  fetchSkillsPage,
} from '@runkora/server/skills-client';
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

/** Copy for an upstream failure that carries no reason of its own. */
const UPSTREAM_FAILURE = 'This Marketplace could not be loaded. Try again later.';

/**
 * A 502 means the Marketplace is unavailable, with skills-service's reason for the user. A listed
 * Marketplace's 404 is a missing repository or ref, so the skills page reads it the same way.
 */
function failure(caller: string, err: unknown, subject: string, listed = false): Loaded<never> {
  console.error(`${caller} failed:`, err);
  const unavailable =
    err instanceof ApiError &&
    (err.code === 'marketplace_unavailable' || (listed && err.status === 404));
  if (unavailable) return { ok: false, status: 502, message: err.message || UPSTREAM_FAILURE };
  const failed = loadFailure(err, subject);
  return !failed.ok && failed.status >= 500
    ? { ok: false, status: 500, message: UPSTREAM_FAILURE }
    : failed;
}
