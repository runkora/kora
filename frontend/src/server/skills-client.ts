import 'server-only';

import type { MarketplaceList, Skill, SkillFile, SkillsPage } from '@runkora/shared/api';
import { config } from '@runkora/server/config';
import { ApiError } from './api-error';

const UPSTREAM_TIMEOUT_MS = 10_000;

/** skills-service takes the tenant from the forwarded session token. */
async function request<T>(path: string, accessToken: string): Promise<T> {
  const response = await fetch(`${config.skillsServiceUrl}${path}`, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}` },
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  });

  if (!response.ok) {
    const err = (await response.json().catch(() => ({}))) as { code?: string; message?: string };
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }

  return (await response.json()) as T;
}

/**
 * Encodes one URL path segment. encodeURIComponent leaves '.' and '..' as they are and fetch would
 * resolve them out of the intended route with the user's token; they read as not found instead.
 */
function segment(value: string): string {
  if (value === '' || value === '.' || value === '..') {
    throw new ApiError(404, 'not_found', 'Not found.');
  }
  return encodeURIComponent(value);
}

/** GET /marketplaces */
export function fetchMarketplaces(accessToken: string): Promise<MarketplaceList> {
  return request<MarketplaceList>('/marketplaces', accessToken);
}

/** What one page of GET /marketplaces/skills asks for. No marketplace and no keys means every Marketplace. */
export interface SkillsQuery {
  marketplace: string;
  keys: string[];
  search: string;
  offset: number;
  limit: number;
}

/** GET /marketplaces/skills: one page of skills in title order. */
export async function fetchSkills(accessToken: string, page: SkillsQuery): Promise<SkillsPage> {
  const query = new URLSearchParams({ offset: String(page.offset), limit: String(page.limit) });
  if (page.marketplace) query.set('marketplace', page.marketplace);
  if (page.keys.length > 0) query.set('keys', page.keys.join(','));
  if (page.search) query.set('search', page.search);
  return request<SkillsPage>(`/marketplaces/skills?${query.toString()}`, accessToken);
}

function skillPath(slug: string, path: string): string {
  return `/marketplaces/${segment(slug)}/skills/${path.split('/').map(segment).join('/')}`;
}

/** GET /marketplaces/{slug}/skills/{path}: SKILL.md and the list of other files. */
export async function fetchSkill(accessToken: string, slug: string, path: string): Promise<Skill> {
  return request<Skill>(skillPath(slug, path), accessToken);
}

/** GET /marketplaces/{slug}/skills/{path}?file=: one supporting file, held to the same segment rules. */
export async function fetchSkillFile(
  accessToken: string,
  slug: string,
  path: string,
  file: string,
): Promise<SkillFile> {
  file.split('/').forEach(segment);
  const query = new URLSearchParams({ file });
  return request<SkillFile>(`${skillPath(slug, path)}?${query.toString()}`, accessToken);
}
