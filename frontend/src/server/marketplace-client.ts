import 'server-only';

import type { GetMarketplaces, ListSkillsResponse, Skill } from '@orbit/shared/api';
import { config } from '@orbit/server/config';
import { ApiError } from './api-error';

// Marketplace client. Calls skills-service (backend/internal/skills) over
// SKILLS_SERVICE_URL. Each function matches one skills-service endpoint; see
// docs/adr/2026-08-28-marketplace-adr-backend.md for the route contract.
//
// The skills-service returns cursor batches in slug order. This client requests
// only the first batch; it does not follow nextCursor. The server loader does the
// sort/page/total work (see marketplace-server.ts) over that one batch. A
// Marketplace with more than SKILL_LIST_CAP Skills is not fully shown yet.

const UPSTREAM_TIMEOUT_MS = 10_000;

/** The most Skills that the skills-service returns for one list request. */
export const SKILL_LIST_CAP = 200;

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

/**
 * GET /marketplaces/{marketplaceSlug}/skills?search=
 *
 * A 404 from the skills-service (unknown Marketplace) surfaces as an
 * ApiError, same as any other failed response.
 */
export async function fetchSkills(
  accessToken: string,
  marketplaceSlug: string,
  search: string,
  signal?: AbortSignal,
): Promise<ListSkillsResponse> {
  const query = new URLSearchParams();
  if (search) {
    query.set('search', search);
  }
  const qs = query.toString();
  return request<ListSkillsResponse>(
    `/marketplaces/${marketplaceSlug}/skills${qs ? `?${qs}` : ''}`,
    accessToken,
    signal,
  );
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
