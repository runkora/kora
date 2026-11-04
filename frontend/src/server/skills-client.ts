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

/** GET /marketplaces */
export function fetchMarketplaces(accessToken: string): Promise<MarketplaceList> {
  return request<MarketplaceList>('/marketplaces', accessToken);
}

/** GET /marketplaces/{slug}: one page of skills, A to Z. */
export function fetchSkillsPage(
  accessToken: string,
  slug: string,
  page: { search: string; offset: number; limit: number },
): Promise<MarketplaceSkillsPage> {
  const query = new URLSearchParams({ offset: String(page.offset), limit: String(page.limit) });
  if (page.search) query.set('search', page.search);
  return request<MarketplaceSkillsPage>(
    `/marketplaces/${encodeURIComponent(slug)}?${query.toString()}`,
    accessToken,
  );
}

/** GET /marketplaces/{slug}/skills/{path} */
export function fetchSkill(accessToken: string, slug: string, path: string): Promise<Skill> {
  const segments = path.split('/').map(encodeURIComponent).join('/');
  return request<Skill>(
    `/marketplaces/${encodeURIComponent(slug)}/skills/${segments}`,
    accessToken,
  );
}
