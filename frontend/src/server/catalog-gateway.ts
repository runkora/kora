import 'server-only';
import { config } from '@orbit/server/config';

/**
 * Forwards a request to the inference gateway catalog API.
 *
 * Used by the Next.js API route layer to proxy catalog mutations from the
 * browser through the server — the server holds the session token so it
 * never reaches the client.
 */
export async function forwardToGateway(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Response> {
  return fetch(`${config.inferenceGatewayUrl}/catalog${path}`, {
    ...init,
    headers: {
      ...init?.headers,
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
}
