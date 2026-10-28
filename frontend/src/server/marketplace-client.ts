import 'server-only';

import type { GetMarketplaces, ListSkillsResponse, Skill, SkillSummary } from '@orbit/shared/api';
import { config } from '@orbit/server/config';
import { ApiError } from './api-error';

const UPSTREAM_TIMEOUT_MS = 10_000;

/** Combines the caller's own cancellation, when given, with a fixed timeout. */
function requestSignal(signal?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(UPSTREAM_TIMEOUT_MS);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function request<T>(path: string, accessToken: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`${config.skillsServiceUrl}${path}`, {
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${accessToken}`,
    },
    cache: 'no-store',
    signal: requestSignal(signal),
  });

  if (!response.ok) {
    const errJson = await response.json().catch(() => ({}));
    const err = errJson as { code?: string; message?: string };
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }

  return (await response.json()) as T;
}

/** GET /marketplaces */
export async function fetchMarketplaces(
  accessToken: string,
  signal?: AbortSignal,
): Promise<GetMarketplaces> {
  return request<GetMarketplaces>('/marketplaces', accessToken, signal);
}

/** Loads every matching Skill summary across cursor batches. */
export async function fetchSkills(
  accessToken: string,
  marketplaceSlug: string,
  search: string,
  signal?: AbortSignal,
): Promise<{ skills: SkillSummary[] }> {
  const batchSignal = requestSignal(signal);
  const query = new URLSearchParams();
  if (search) {
    query.set('search', search);
  }
  const skills: SkillSummary[] = [];
  const seenCursors = new Set<string>();
  let nextCursor: string | undefined;

  do {
    batchSignal.throwIfAborted();
    if (nextCursor) query.set('cursor', nextCursor);
    const qs = query.toString();
    const batch = await request<ListSkillsResponse>(
      `/marketplaces/${marketplaceSlug}/skills${qs ? `?${qs}` : ''}`,
      accessToken,
      batchSignal,
    );
    skills.push(...batch.skills);
    nextCursor = batch.nextCursor;
    if (nextCursor) {
      if (seenCursors.has(nextCursor)) {
        throw new ApiError(502, 'invalid_response', 'Failed to load Skills');
      }
      seenCursors.add(nextCursor);
    }
  } while (nextCursor);

  return { skills };
}

/**
 * GET /marketplaces/{marketplaceSlug}/skills/{skillSlug}
 *
 * Returns null for a missing Skill (404), so a caller can show a not-found
 * state without needing to inspect the error.
 */
export async function fetchSkillDetail(
  accessToken: string,
  marketplaceSlug: string,
  skillSlug: string,
): Promise<Skill | null> {
  try {
    return await request<Skill>(
      `/marketplaces/${encodeURIComponent(marketplaceSlug)}/skills/${encodeURIComponent(skillSlug)}`,
      accessToken,
    );
  } catch (err) {
    if (err instanceof ApiError && err.isNotFound) {
      return null;
    }
    throw err;
  }
}
