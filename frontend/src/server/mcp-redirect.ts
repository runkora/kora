import 'server-only';

import { config } from '@orbit/server/config';

/**
 * MCP OAuth redirect_uri validation, shared between the /mcp/authorize consent
 * screen (page.tsx) and the /api/mcp/authorize consent decision (route.ts).
 *
 * A redirect_uri is valid for two distinct client shapes:
 * - loopback (RFC 8252, native/DCR clients) — checked locally, no lookup needed.
 * - an exact match against a pre-registered client's redirect_uris
 *   (OAuthClient CR, hosted-app clients) — requires fetchMcpOAuthClient().
 */

/** Loopback redirect targets for public native/DCR MCP clients (RFC 8252). */
export function isLoopbackRedirect(raw: string): boolean {
  try {
    const u = new URL(raw);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
    return ['127.0.0.1', 'localhost', '::1', '[::1]'].includes(u.hostname);
  } catch {
    return false;
  }
}

/** Structural check only (parses as http/https) — not a security gate. The real
 * gate is isLoopbackRedirect() OR exact membership in a client's registered
 * redirect_uris (see fetchMcpOAuthClient). */
export function isWellFormedRedirect(raw: string): boolean {
  try {
    const u = new URL(raw);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

export type McpOAuthClientLookup =
  | { kind: 'not_found' }
  | { kind: 'ok'; clientName: string; redirectUris: string[] }
  | { kind: 'error' };

/**
 * Fetches a registered MCP OAuth client's public metadata (name + registered
 * redirect_uris) from identity. `kind: 'error'` covers both a non-404 failure
 * response and a network/parse exception — non-fatal for callers that only
 * need the name (falls back to a generic label), but treated as "not
 * registered" by any caller checking redirectUris.
 */
export async function fetchMcpOAuthClient(
  clientId: string,
  accessToken: string,
): Promise<McpOAuthClientLookup> {
  try {
    const res = await fetch(
      `${config.orbitApiUrl}/identity/oauth/clients/${encodeURIComponent(clientId)}`,
      { cache: 'no-store', headers: { Authorization: `Bearer ${accessToken}` } },
    );
    if (res.status === 404) {
      return { kind: 'not_found' };
    }
    if (res.ok) {
      const body = (await res.json()) as { client_name?: string; redirect_uris?: string[] };
      return {
        kind: 'ok',
        clientName: body.client_name ?? '',
        redirectUris: body.redirect_uris ?? [],
      };
    }
    return { kind: 'error' };
  } catch {
    return { kind: 'error' };
  }
}

/** Whether redirectUri is exactly one of clientId's pre-registered redirect_uris
 * (OAuthClient CR, hosted-app clients) — the real gate for non-loopback URIs. */
export async function isRegisteredRedirect(
  clientId: string,
  redirectUri: string,
  accessToken: string,
): Promise<boolean> {
  const lookup = await fetchMcpOAuthClient(clientId, accessToken);
  return lookup.kind === 'ok' && lookup.redirectUris.includes(redirectUri);
}
