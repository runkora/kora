/**
 * Revokes kora tokens through identity's RFC 7009 endpoint, `POST /identity/revoke`. Revoking the
 * access token also revokes its session and every refresh token of that session.
 */
import 'server-only';

import { config } from '@runkora/server/config';

/** Revokes one token; throws when the endpoint answers anything but 200. */
export async function revokeToken(
  token: string,
  tokenTypeHint: 'access_token' | 'refresh_token',
): Promise<void> {
  const body = new URLSearchParams({ token, token_type_hint: tokenTypeHint });
  const res = await fetch(`${config.koraApiUrl}/identity/revoke`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
    cache: 'no-store',
    signal: AbortSignal.timeout(5_000),
  });
  if (!res.ok) {
    throw new Error(`revocation endpoint returned ${res.status}`);
  }
}
