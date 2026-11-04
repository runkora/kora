// Client-side fetch helper for the /api/skills BFF route. Never calls skills-service directly.

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { MarketplaceSkillsPage } from '@runkora/shared/api';
import type { SkillsBrowse, SkillsLoadError } from '../_types';

export type FetchSkillsPageResult =
  { data: MarketplaceSkillsPage; error: null } | { data: null; error: SkillsLoadError };

/** Fetches one page of skills. Never throws; an aborted request returns an error the caller ignores. */
export async function fetchSkillsPage(
  browse: SkillsBrowse,
  pageSize: number,
  signal: AbortSignal,
): Promise<FetchSkillsPageResult> {
  try {
    const res = await fetch(ApiRoute.skills.GET({ ...browse, pageSize }), {
      cache: 'no-store',
      signal,
    });
    const body = (await res.json().catch(() => ({}))) as {
      data?: MarketplaceSkillsPage;
      message?: string;
    };
    if (res.ok && body.data) return { data: body.data, error: null };
    return {
      data: null,
      error: { status: res.status, message: body.message ?? `Request failed (${res.status})` },
    };
  } catch {
    return { data: null, error: { status: 0, message: 'Could not reach the server. Try again.' } };
  }
}
