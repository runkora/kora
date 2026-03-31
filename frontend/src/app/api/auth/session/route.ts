/**
 * /api/auth/session — BFF session endpoint.
 *
 * POST — called by the OIDC callback page after `signinRedirectCallback()`.
 *        Accepts `{ id_token, tenant_id }`, forwards to the Go identity service
 *        with the `oidc_id_token` grant type, and seals the resulting Orbit
 *        tokens into an HttpOnly iron-session cookie.
 *
 *        `tenant_id` is resolved upstream via Home Realm Discovery (ADR-0031)
 *        and threaded through the PKCE state param to the callback page.
 *
 *        Request:  { id_token: string; tenant_id: string }
 *        Response: 200 {}  — session cookie set; caller handles redirect
 *                  400 { error: 'bad_request', message: string }
 *                  401 { error: 'unauthorized' }
 *                  502 { error: 'upstream_error' }
 *
 * GET  — returns the public (non-sensitive) portion of the current session for
 *        use by client components that need to know who is logged in.
 *
 *        Response: 200 { user: SessionUser, expiresAt: number } — active session
 *                  401 { error: 'unauthorized' }                — no/expired session
 *
 * The access token and refresh token are NEVER included in GET responses —
 * they live in the sealed HttpOnly cookie only.
 */
import 'server-only';

import { getIronSession } from 'iron-session';
import { cookies } from 'next/headers';

import { getSessionOptions } from '@orbit/server/auth-config';
import type {
  SessionPayload,
  SessionUser,
  BackendSessionResponse,
} from '@orbit/server/auth-config';
import { config } from '@orbit/server/config';
import { getSession, isSessionValid } from '@orbit/server/session';

// ── Internal types ────────────────────────────────────────────────────────────

/** Decoded Orbit JWT payload (unverified — trusted because it comes from our own backend). */
interface OrbitJwtPayload {
  sub: string;
  exp: number;
  'orbit.display_name'?: string;
  'orbit.claims'?: Record<string, string>;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Decodes the payload of a JWT without signature verification.
 * Safe because the token is received directly from the Go backend over a
 * trusted internal network — it is not user-supplied input.
 */
function isOrbitJwtPayload(v: unknown): v is OrbitJwtPayload {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>)['sub'] === 'string' &&
    typeof (v as Record<string, unknown>)['exp'] === 'number'
  );
}

function decodeJwtPayload(token: string): OrbitJwtPayload {
  const parts = token.split('.');
  if (parts.length !== 3) throw new Error('Invalid JWT structure');
  // Normalise base64url → base64: swap alphabet chars then restore padding.
  // JWT segments omit `=` padding; atob throws if length is not divisible by 4.
  const base64 = parts[1]
    .replace(/-/g, '+')
    .replace(/_/g, '/')
    .padEnd(Math.ceil(parts[1].length / 4) * 4, '=');
  const parsed: unknown = JSON.parse(atob(base64));
  if (!isOrbitJwtPayload(parsed)) throw new Error('JWT payload missing required claims');
  return parsed;
}

// ── POST — exchange id_token for Orbit session ────────────────────────────────

export async function POST(request: Request): Promise<Response> {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return Response.json(
      { error: 'bad_request', message: 'request body must be JSON' },
      { status: 400 },
    );
  }

  if (!body || typeof body !== 'object') {
    return Response.json(
      { error: 'bad_request', message: 'request body must be a JSON object' },
      { status: 400 },
    );
  }

  const raw = body as Record<string, unknown>;

  const idToken = raw.id_token;
  if (typeof idToken !== 'string' || !idToken) {
    return Response.json(
      { error: 'bad_request', message: 'id_token is required' },
      { status: 400 },
    );
  }

  // tenant_id is required — resolved via email-based Home Realm Discovery
  // (ADR-0031) and threaded through the PKCE state param by the callback page.
  const tenantId = raw.tenant_id;
  if (typeof tenantId !== 'string' || !tenantId) {
    return Response.json(
      { error: 'bad_request', message: 'tenant_id is required' },
      { status: 400 },
    );
  }

  // Forward to Go identity service.
  let backendRes: Response;
  try {
    backendRes = await fetch(`${config.orbitApiUrl}/identity/session`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        grant_type: 'oidc_id_token',
        id_token: idToken,
        tenant_id: tenantId,
      }),
    });
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  if (!backendRes.ok) {
    if (backendRes.status === 401) {
      return Response.json({ error: 'unauthorized' }, { status: 401 });
    }
    if (backendRes.status >= 400 && backendRes.status < 500) {
      return Response.json({ error: 'bad_request' }, { status: 400 });
    }
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  let data: BackendSessionResponse;
  try {
    data = (await backendRes.json()) as BackendSessionResponse;
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  // Decode the Orbit JWT to extract user identity claims.
  let claims: OrbitJwtPayload;
  try {
    claims = decodeJwtPayload(data.access_token);
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  const user: SessionUser = {
    sub: claims.sub,
    email: claims['orbit.claims']?.['email'] ?? '',
    name: claims['orbit.display_name'] ?? '',
  };

  const payload: SessionPayload = {
    user,
    accessToken: data.access_token,
    refreshToken: data.refresh_token,
    expiresAt: data.issued_at + data.expires_in,
    tenantId: tenantId as string,
  };

  // Create the session with a maxAge derived from the backend's refresh token
  // TTL so the cookie stays alive for exactly as long as the refresh token is
  // valid. We bypass getSession() here because that helper uses the default
  // options — iron-session bakes the maxAge in at getIronSession() time, not
  // at save() time, so we must pass the dynamic value at construction.
  const cookieStore = await cookies();
  const session = await getIronSession<SessionPayload>(
    cookieStore,
    getSessionOptions(data.refresh_token_expires_in),
  );
  Object.assign(session, payload);
  await session.save();

  return Response.json({});
}

// ── GET — read current session ────────────────────────────────────────────────

export async function GET(): Promise<Response> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return Response.json({ error: 'unauthorized' }, { status: 401 });
  }

  return Response.json({
    user: session.user,
    expiresAt: session.expiresAt,
  });
}
