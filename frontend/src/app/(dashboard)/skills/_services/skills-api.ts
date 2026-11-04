// Client-side fetch helpers for the /api/skills BFF routes. Never calls skills-service directly.

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { SkillFile, SkillsPage } from '@runkora/shared/api';
import type { SkillsBrowse, SkillsLoadError } from '../_types';

export type FetchResult<T> = { data: T; error: null } | { data: null; error: SkillsLoadError };

/** Never throws; an aborted request returns an error the caller ignores. */
async function getData<T>(url: string, signal?: AbortSignal): Promise<FetchResult<T>> {
  try {
    const res = await fetch(url, { cache: 'no-store', signal });
    const body = (await res.json().catch(() => ({}))) as { data?: T; message?: string };
    if (res.ok && body.data) return { data: body.data, error: null };
    return {
      data: null,
      error: { status: res.status, message: body.message ?? `Request failed (${res.status})` },
    };
  } catch {
    return { data: null, error: { status: 0, message: 'Could not reach the server. Try again.' } };
  }
}

/** Fetches one page of the overview. */
export function fetchSkills(
  browse: SkillsBrowse,
  signal: AbortSignal,
): Promise<FetchResult<SkillsPage>> {
  return getData(ApiRoute.skills.GET(browse), signal);
}

/** Fetches one supporting file of a skill. */
export function fetchSkillFile(file: {
  marketplace: string;
  path: string;
  file: string;
}): Promise<FetchResult<SkillFile>> {
  return getData(ApiRoute.skillFile.GET(file));
}
