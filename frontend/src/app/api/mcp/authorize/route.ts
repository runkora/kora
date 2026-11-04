import 'server-only';
import { getSession, isSessionValid } from '@runkora/server/session';
import { config } from '@runkora/server/config';
import { consentTokenValid } from '@runkora/server/mcp-consent';
import {
  isLoopbackRedirect,
  isRegisteredRedirect,
  isWellFormedRedirect,
} from '@runkora/server/mcp-redirect';

/**
 * POST /api/mcp/authorize — the consent decision endpoint for the MCP OAuth flow.
 *
 * The /mcp/authorize consent screen (client component) POSTs here with the OAuth
 * params in the query string and the decision + CSRF token in the form body, and
 * expects JSON back so it can render the branded result state before navigating:
 * - approve → identity mints a single-use auth code; returns { redirect } (the
 *   client's loopback redirect_uri with ?code&state) which the consent screen
 *   navigates to after showing "connected".
 * - deny / lost session → { redirect } with error=access_denied.
 * - failure → { error } with a 4xx/5xx status; the screen shows it and resets.
 *
 * Security: the redirect_uri is only ever used as a redirect target if it is a
 * loopback address (RFC 8252, native/DCR clients) or exactly one of the
 * client's pre-registered redirect_uris (OAuthClient CR, hosted-app clients) —
 * confirmed via isRegisteredRedirect() before ANY redirect is built, including
 * the deny/lost-session paths below (those never call identity's own
 * AllowsRedirect check, so this is the only gate they get). The render-bound
 * CSRF token + Sec-Fetch-Site gate a cross-site submission; nothing is minted
 * until both pass.
 */
export async function POST(req: Request): Promise<Response> {
  if (req.headers.get('sec-fetch-site') === 'cross-site') {
    return jsonError('cross-site request rejected', 403);
  }

  const url = new URL(req.url);
  const p = url.searchParams;
  const clientId = p.get('client_id');
  const redirectUri = p.get('redirect_uri');
  const codeChallenge = p.get('code_challenge');
  const codeChallengeMethod = p.get('code_challenge_method');
  const state = p.get('state') ?? '';
  const scope = p.get('scope') ?? '';

  if (!redirectUri || !isWellFormedRedirect(redirectUri)) {
    return jsonError('invalid_request', 400);
  }
  if (!clientId || codeChallengeMethod !== 'S256' || !codeChallenge) {
    return jsonError('invalid_request', 400);
  }

  const form = await req.formData();
  const decision = form.get('decision');
  const csrfToken = form.get('csrf_token');

  const session = await getSession();
  if (!isSessionValid(session)) {
    // No bearer token to verify a non-loopback client's registered redirect_uris
    // against — only take the fast error-redirect path for loopback targets;
    // a hosted-app client hitting this (rare: session expired between page
    // load and submit) gets a JSON error instead of an unverified redirect.
    if (!isLoopbackRedirect(redirectUri)) {
      return jsonError('session_expired', 401);
    }
    return json({ redirect: errorRedirect(redirectUri, state, 'access_denied') });
  }
  if (
    !isLoopbackRedirect(redirectUri) &&
    !(await isRegisteredRedirect(clientId, redirectUri, session.accessToken))
  ) {
    return jsonError('invalid_request', 400);
  }
  if (decision !== 'approve') {
    return json({ redirect: errorRedirect(redirectUri, state, 'access_denied') });
  }
  // CSRF: the token must be one this server minted when it rendered the consent
  // page for THIS user and THESE exact params. Fail closed without minting.
  if (
    !consentTokenValid(typeof csrfToken === 'string' ? csrfToken : null, {
      userKey: session.user.email,
      clientId,
      redirectUri,
      codeChallenge,
    })
  ) {
    return jsonError('invalid or missing consent token', 403);
  }

  // Approved → identity mints the code, authenticated by the session's token.
  let res: Response;
  try {
    res = await fetch(`${config.koraApiUrl}/identity/oauth/code`, {
      method: 'POST',
      cache: 'no-store',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${session.accessToken}`,
      },
      body: JSON.stringify({
        client_id: clientId,
        redirect_uri: redirectUri,
        code_challenge: codeChallenge,
        code_challenge_method: codeChallengeMethod,
        scope,
      }),
    });
  } catch {
    return jsonError('server_error', 502);
  }
  if (!res.ok) {
    return jsonError(res.status === 400 ? 'invalid_request' : 'server_error', res.status);
  }
  const body = (await res.json()) as { code?: string };
  if (!body.code) {
    return jsonError('server_error', 502);
  }

  const cb = new URL(redirectUri);
  cb.searchParams.set('code', body.code);
  if (state) cb.searchParams.set('state', state);
  return json({ redirect: cb.toString() });
}

function errorRedirect(redirectUri: string, state: string, error: string): string {
  const u = new URL(redirectUri);
  u.searchParams.set('error', error);
  if (state) u.searchParams.set('state', state);
  return u.toString();
}

function json(body: unknown): Response {
  return Response.json(body, { headers: { 'Cache-Control': 'no-store' } });
}

function jsonError(error: string, status: number): Response {
  return Response.json({ error }, { status, headers: { 'Cache-Control': 'no-store' } });
}
