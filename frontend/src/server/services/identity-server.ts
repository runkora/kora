import 'server-only';

import { ApiError, orbitClient } from '@orbit/server/client';
import type { ListPATsResponse, PAT } from '@orbit/shared/api';

// ── Result types ──────────────────────────────────────────────────────────────

export type ListPATsResult =
  | { ok: true; tokens: PAT[] }
  | { ok: false; status: number; message: string };

// ── Helpers ───────────────────────────────────────────────────────────────────

function fromApiError(err: unknown): { ok: false; status: number; message: string } {
  if (err instanceof ApiError) {
    return { ok: false, status: err.status, message: err.message };
  }
  return {
    ok: false,
    status: 502,
    message: 'Unexpected error communicating with identity service',
  };
}

// ── Service functions ─────────────────────────────────────────────────────────

export async function listPATsFromIdentity(accessToken: string): Promise<ListPATsResult> {
  try {
    const data = await orbitClient.get<ListPATsResponse>('/identity/tokens', {
      Authorization: `Bearer ${accessToken}`,
    });
    return { ok: true, tokens: data?.tokens ?? [] };
  } catch (err) {
    return fromApiError(err);
  }
}

// ── Principal display names ───────────────────────────────────────────────────

interface IdentityUserEntry {
  id: string;
  email: string;
  display_name: string;
}

interface IdentityUsersResponse {
  users: IdentityUserEntry[];
}

/**
 * Resolves principal ids to human-readable names via the identity service.
 * Ids that are not tenant users (service principals, webhook sources) simply
 * get no entry — callers fall back to the raw principal. Best-effort: any
 * upstream failure returns an empty map rather than failing the caller.
 */
// Upstream /identity/users caps a single lookup at this many ids, so a page
// with more distinct starters is split into batches and merged.
const USER_LOOKUP_BATCH = 100;

export async function resolveUserDisplayNames(
  accessToken: string,
  principalIds: string[],
): Promise<Map<string, string>> {
  const ids = [...new Set(principalIds.filter(Boolean))];
  const names = new Map<string, string>();
  if (ids.length === 0) return names;
  try {
    for (let i = 0; i < ids.length; i += USER_LOOKUP_BATCH) {
      const batch = ids.slice(i, i + USER_LOOKUP_BATCH);
      const data = await orbitClient.get<IdentityUsersResponse>(
        `/identity/users?ids=${encodeURIComponent(batch.join(','))}&limit=${USER_LOOKUP_BATCH}`,
        { Authorization: `Bearer ${accessToken}` },
      );
      for (const u of data?.users ?? []) {
        names.set(u.id, u.display_name || u.email);
      }
    }
    return names;
  } catch {
    return names;
  }
}
