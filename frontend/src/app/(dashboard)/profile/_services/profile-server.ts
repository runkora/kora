import 'server-only';

import { ApiError, koraClient } from '@runkora/server/client';

// ── Result type (matches established convention) ─────────────────────────────

type Result<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

function fromApiError(err: unknown): { ok: false; status: number; message: string } {
  if (err instanceof ApiError) {
    return { ok: false, status: err.status, message: err.message };
  }
  return { ok: false, status: 502, message: 'Unexpected error communicating with backend service' };
}

// ── User profile (single consumer — lives here) ─────────────────────────────

// Matches ADR-0042 GET /identity/users/{id} response shape.
// tenant_id is implicitly scoped by the caller's tenant and available via session.tenantId.
// oidc_issuer is deferred until PRD Open Question #2 is resolved and a backing column exists.
interface UserProfileResponse {
  id: string;
  email: string;
  display_name: string;
}

export async function fetchUserProfile(
  accessToken: string,
  userId: string,
): Promise<Result<UserProfileResponse>> {
  try {
    const data = await koraClient.get<UserProfileResponse>(`/identity/users/${userId}`, {
      Authorization: `Bearer ${accessToken}`,
    });
    if (!data) {
      return { ok: false, status: 502, message: 'Empty response from identity service' };
    }
    return { ok: true, data };
  } catch (err) {
    return fromApiError(err);
  }
}
