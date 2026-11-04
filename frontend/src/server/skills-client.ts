import 'server-only';

import type { MarketplaceList, MarketplaceSkillsPage, Skill } from '@runkora/shared/api';
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
 * Encodes one URL path segment. encodeURIComponent leaves '.' and '..' as they are, and fetch
 * would resolve them out of the intended route with the user's token, so they read as not found.
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

/** GET /marketplaces/{slug}: one page of skills, A to Z. */
export async function fetchSkillsPage(
  accessToken: string,
  slug: string,
  page: { search: string; offset: number; limit: number },
): Promise<MarketplaceSkillsPage> {
  const query = new URLSearchParams({ offset: String(page.offset), limit: String(page.limit) });
  if (page.search) query.set('search', page.search);
  return request<MarketplaceSkillsPage>(
    `/marketplaces/${segment(slug)}?${query.toString()}`,
    accessToken,
  );
}

/** GET /marketplaces/{slug}/skills/{path} */
export async function fetchSkill(accessToken: string, slug: string, path: string): Promise<Skill> {
  const segments = path.split('/').map(segment).join('/');
  return request<Skill>(`/marketplaces/${segment(slug)}/skills/${segments}`, accessToken);
}
