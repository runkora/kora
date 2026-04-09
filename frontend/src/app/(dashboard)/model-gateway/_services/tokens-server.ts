import 'server-only';

import { ApiError, orbitClient } from '@orbit/server/client';
import type { CreatePATRequest, CreatePATResponse, ListPATsResponse, PAT } from '@orbit/shared/api';

// ── Result types ──────────────────────────────────────────────────────────────

export type ListPATsResult =
  | { ok: true; tokens: PAT[] }
  | { ok: false; status: number; message: string };

export type CreatePATResult =
  | { ok: true; pat: CreatePATResponse }
  | { ok: false; status: number; message: string };

export type RevokePATResult = { ok: true } | { ok: false; status: number; message: string };

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

export async function createPATInIdentity(
  accessToken: string,
  req: CreatePATRequest,
): Promise<CreatePATResult> {
  try {
    const data = await orbitClient.post<CreatePATResponse>('/identity/tokens', req, {
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
    await orbitClient.delete(`/identity/tokens/${patId}`, {
      Authorization: `Bearer ${accessToken}`,
    });
    return { ok: true };
  } catch (err) {
    return fromApiError(err);
  }
}
