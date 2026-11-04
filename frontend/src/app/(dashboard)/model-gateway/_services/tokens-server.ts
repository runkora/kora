import 'server-only';

import { ApiError, koraClient } from '@runkora/server/client';
import type {
  CreatePATRequest,
  CreatePATResponse,
  UpdatePATScopesRequest,
} from '@runkora/shared/api';

// ── Shared (promoted to src/server/services/) ─────────────────────────────────

export {
  listPATsFromIdentity,
  type ListPATsResult,
} from '@runkora/server/services/identity-server';

// ── Feature-local types ──────────────────────────────────────────────────────

export interface PATAttributionEntry {
  id: string;
  email: string;
  display_name: string;
}

export interface PATGrantAttribution {
  pat_name: string;
  owner?: PATAttributionEntry;
}

export interface PATAttributionResponse {
  attribution: Record<string, PATGrantAttribution>;
}

export type GetPATAttributionResult =
  | { ok: true; attribution: Record<string, PATGrantAttribution> }
  | { ok: false; status: number; message: string };

// ── Result types ──────────────────────────────────────────────────────────────

export type CreatePATResult =
  { ok: true; pat: CreatePATResponse } | { ok: false; status: number; message: string };

export type RevokePATResult = { ok: true } | { ok: false; status: number; message: string };

export type UpdatePATScopesResult = { ok: true } | { ok: false; status: number; message: string };

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

export async function createPATInIdentity(
  accessToken: string,
  req: CreatePATRequest,
): Promise<CreatePATResult> {
  try {
    const data = await koraClient.post<CreatePATResponse>('/identity/tokens', req, {
      Authorization: `Bearer ${accessToken}`,
    });
    if (!data) {
      return { ok: false, status: 502, message: 'Empty response from identity service' };
    }
    return { ok: true, pat: data };
  } catch (err) {
    return fromApiError(err);
  }
}

export async function revokePATInIdentity(
  accessToken: string,
  patId: string,
): Promise<RevokePATResult> {
  try {
    await koraClient.delete(`/identity/tokens/${patId}`, {
      Authorization: `Bearer ${accessToken}`,
    });
    return { ok: true };
  } catch (err) {
    return fromApiError(err);
  }
}

export async function updatePATScopesInIdentity(
  accessToken: string,
  patId: string,
  req: UpdatePATScopesRequest,
): Promise<UpdatePATScopesResult> {
  try {
    await koraClient.patch(`/identity/tokens/${patId}/scopes`, req, {
      Authorization: `Bearer ${accessToken}`,
    });
    return { ok: true };
  } catch (err) {
    return fromApiError(err);
  }
}

export async function fetchPATAttribution(
  accessToken: string,
  patIds: string[],
): Promise<GetPATAttributionResult> {
  if (patIds.length === 0) return { ok: true, attribution: {} };
  try {
    const ids = patIds.join(',');
    const data = await koraClient.get<PATAttributionResponse>(
      `/identity/tokens/attribution?ids=${encodeURIComponent(ids)}`,
      { Authorization: `Bearer ${accessToken}` },
    );
    return { ok: true, attribution: data?.attribution ?? {} };
  } catch (err) {
    return fromApiError(err);
  }
}

// ── Identity user profiles ────────────────────────────────────────────────────

interface IdentityUsersResponse {
  users: PATAttributionEntry[];
  total: number;
  page: number;
  limit: number;
}

export type GetUsersResult =
  { ok: true; users: PATAttributionEntry[] } | { ok: false; status: number; message: string };

export async function fetchUsersByIDs(
  accessToken: string,
  userIds: string[],
): Promise<GetUsersResult> {
  if (userIds.length === 0) return { ok: true, users: [] };
  try {
    const ids = userIds.join(',');
    const data = await koraClient.get<IdentityUsersResponse>(
      `/identity/users?ids=${encodeURIComponent(ids)}&limit=100`,
      { Authorization: `Bearer ${accessToken}` },
    );
    return { ok: true, users: data?.users ?? [] };
  } catch (err) {
    return fromApiError(err);
  }
}

export async function searchUsers(
  accessToken: string,
  query: string,
  limit = 20,
): Promise<GetUsersResult> {
  try {
    const params = new URLSearchParams({ limit: String(limit) });
    if (query.trim()) params.set('search', query.trim());
    const data = await koraClient.get<IdentityUsersResponse>(
      `/identity/users?${params.toString()}`,
      { Authorization: `Bearer ${accessToken}` },
    );
    return { ok: true, users: data?.users ?? [] };
  } catch (err) {
    return fromApiError(err);
  }
}

// Fetches all tenant users from the identity service, walking pages until every
// user has been collected. The identity API clamps `limit` to 100 server-side, so a
// single request cannot return more than one page — tenants with >100 users would
// otherwise silently lose everyone past position 100 (sorted by display_name).
// Used to seed the principals list with users who have no recent inference activity.
const TENANT_USERS_PAGE_LIMIT = 100;
const TENANT_USERS_MAX_PAGES = 100; // sane cap: 10,000 users

export async function fetchAllTenantUsers(accessToken: string): Promise<GetUsersResult> {
  const users: PATAttributionEntry[] = [];
  try {
    for (let page = 1; page <= TENANT_USERS_MAX_PAGES; page++) {
      const data = await koraClient.get<IdentityUsersResponse>(
        `/identity/users?limit=${TENANT_USERS_PAGE_LIMIT}&page=${page}`,
        { Authorization: `Bearer ${accessToken}` },
      );
      users.push(...(data?.users ?? []));
      const total = data?.total ?? users.length;
      if (users.length >= total || !data?.users?.length) break;
    }
    return { ok: true, users };
  } catch (err) {
    return fromApiError(err);
  }
}
