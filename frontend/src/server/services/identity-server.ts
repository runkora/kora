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
