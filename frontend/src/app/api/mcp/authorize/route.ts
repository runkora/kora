import 'server-only';
import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';

/**
 * POST /api/mcp/authorize — the consent decision endpoint for the MCP OAuth flow.
 *
 * The /mcp/authorize consent page POSTs here with the OAuth params in the query
 * string and the human's decision (approve/deny) in the form body:
 * - approve → identity mints a single-use auth code (authenticated by the
 *   session token), then 302 back to the client's loopback redirect_uri with
 *   ?code&state.
 * - deny (or lost session) → 302 back with error=access_denied.
 *
 * Security: the redirect_uri is only ever used as a redirect target if it is a
 * loopback address (RFC 8252) — otherwise 400, never an open redirect. The
 * consent page already gated this, but we re-check since this endpoint is
 * independently reachable.
 */
export async function POST(req: Request): Promise<Response> {
  const url = new URL(req.url);
  const p = url.searchParams;
  const clientId = p.get('client_id');
  const redirectUri = p.get('redirect_uri');
  const codeChallenge = p.get('code_challenge');
  const codeChallengeMethod = p.get('code_challenge_method');
  const state = p.get('state') ?? '';
  const scope = p.get('scope') ?? '';

  // Validate the redirect_uri FIRST — never redirect the browser to a
  // non-loopback URI (open-redirect / code exfiltration), not even with an error.
  if (!redirectUri || !isLoopbackRedirect(redirectUri)) {
    return badRequest('redirect_uri must be a loopback address');
  }
  if (!clientId || codeChallengeMethod !== 'S256' || !codeChallenge) {
    return redirectError(redirectUri, state, 'invalid_request');
  }

  const form = await req.formData();
  const decision = form.get('decision');

  const session = await getSession();
  if (!isSessionValid(session)) {
    // Session expired between rendering consent and submitting — deny gracefully.
    return redirectError(redirectUri, state, 'access_denied', 'session expired');
  }
  if (decision !== 'approve') {
    return redirectError(redirectUri, state, 'access_denied', 'the user denied the request');
  }

  // Approved → identity mints the code, authenticated by the session's token.
  let res: Response;
  try {
    res = await fetch(`${config.orbitApiUrl}/identity/oauth/code`, {
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
    return redirectError(redirectUri, state, 'server_error');
  }
  if (!res.ok) {
    return redirectError(
      redirectUri,
      state,
      res.status === 400 ? 'invalid_request' : 'server_error',
    );
  }
  const body = (await res.json()) as { code?: string };
  if (!body.code) {
    return redirectError(redirectUri, state, 'server_error');
  }

  const cb = new URL(redirectUri);
  cb.searchParams.set('code', body.code);
  if (state) cb.searchParams.set('state', state);
  return Response.redirect(cb.toString(), 302);
}

/** Loopback-only redirect targets for public native MCP clients (RFC 8252). */
function isLoopbackRedirect(raw: string): boolean {
  try {
    const u = new URL(raw);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
    return ['127.0.0.1', 'localhost', '::1', '[::1]'].includes(u.hostname);
  } catch {
    return false;
  }
}

function badRequest(msg: string): Response {
  return new Response(msg, { status: 400, headers: { 'Cache-Control': 'no-store' } });
}

// redirectError sends an RFC 6749 error back to the (validated loopback) client.
function redirectError(
  redirectUri: string,
  state: string,
  error: string,
  description?: string,
): Response {
  const u = new URL(redirectUri);
  u.searchParams.set('error', error);
  if (description) u.searchParams.set('error_description', description);
  if (state) u.searchParams.set('state', state);
  return Response.redirect(u.toString(), 302);
}
