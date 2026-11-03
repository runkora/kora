/**
 * /api/auth/session — BFF session endpoint.
 *
 * POST — called by the OIDC callback page after `signinRedirectCallback()`.
 *        Forwards the ID token to the Go identity service with the
 *        `oidc_id_token` grant type and seals the resulting kora tokens into
 *        an HttpOnly iron-session cookie (docs/adr/2026-11-04-tenant-switching.md).
 *
 *        `tenant_id` is resolved upstream via Home Realm Discovery and threaded
 *        through the PKCE state param. `tenant_hint` picks among several
 *        matching tenants; without one the last-used tenant cookie is the hint.
 *        `switch: true` marks a tenant switch: the exchange then names the
 *        session's tenant and replaces the session's refresh token.
 *
 *        Request:  { id_token: string; tenant_id: string; tenant_hint?: string; switch?: boolean }
 *        Response: 200 {}  — session cookie and last-used tenant cookie set
 *                  400 { error: 'bad_request', message?: string }
 *                  401 { error: 'unauthorized' }
 *                  403 { error: 'tenant_switch_denied' } — switch refused, session kept
 *                  409 { error: 'multiple_tenants_match', tenants } — sign-in needs a pick
 *                  502 { error: 'upstream_error' }
 *
 * GET  — returns the public (non-sensitive) portion of the current session for
 *        use by client components that need to know who is logged in.
 *
 *        Response: 200 { user, expiresAt, permissions, apiAuthoring, tenants } — active session
 *                  401 { error: 'unauthorized' }                — no/expired session
 *
 * The access token and refresh token are NEVER included in GET responses —
 * they live in the sealed HttpOnly cookie only.
 */
import 'server-only';

import { getIronSession, type IronSession } from 'iron-session';
import { cookies } from 'next/headers';

import {
  getSessionOptions,
  LAST_TENANT_COOKIE_NAME,
  lastTenantCookieOptions,
  parseTenants,
  tenantIdFromAccessToken,
} from '@runkora/server/auth-config';
import type {
  SessionPayload,
  SessionUser,
  BackendSessionResponse,
} from '@runkora/server/auth-config';
import { config } from '@runkora/server/config';
import { getSession, isSessionValid } from '@runkora/server/session';

// ── Internal types ────────────────────────────────────────────────────────────

/** Decoded kora JWT payload (unverified — trusted because it comes from our own backend). */
interface KoraJwtPayload {
  sub: string;
  exp: number;
  kora?: {
    display_name?: string;
    claims?: Record<string, string>;
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Decodes the payload of a JWT without signature verification.
 * Safe because the token is received directly from the Go backend over a
 * trusted internal network — it is not user-supplied input.
 */
function isKoraJwtPayload(v: unknown): v is KoraJwtPayload {
  if (
    typeof v !== 'object' ||
    v === null ||
    typeof (v as Record<string, unknown>)['sub'] !== 'string' ||
    typeof (v as Record<string, unknown>)['exp'] !== 'number'
  ) {
    return false;
  }
  const kora = (v as Record<string, unknown>)['kora'];
  if (kora !== undefined && (typeof kora !== 'object' || kora === null)) {
    return false;
  }
  return true;
}

function decodeJwtPayload(token: string): KoraJwtPayload {
  const parts = token.split('.');
  if (parts.length !== 3) throw new Error('Invalid JWT structure');
  // Normalise base64url → base64: swap alphabet chars then restore padding.
  // JWT segments omit `=` padding; atob throws if length is not divisible by 4.
  const base64 = parts[1]
    .replace(/-/g, '+')
    .replace(/_/g, '/')
    .padEnd(Math.ceil(parts[1].length / 4) * 4, '=');
  const parsed: unknown = JSON.parse(atob(base64));
  if (!isKoraJwtPayload(parsed)) throw new Error('JWT payload missing required claims');
  return parsed;
}

// ── Scopes projection ────────────────────────────────────────────────────────

/** Policy gateway response shape for GET /v1/policy/scopes. */
interface ScopesResponse {
  scopes: string[];
  tenant_id: string;
  evaluated_at: string;
}

/**
 * Fetches the principal's effective scope set from the policy gateway.
 * Fail-closed: returns an empty scope set on any error.
 */
async function fetchScopes(accessToken: string): Promise<string[]> {
  try {
    const res = await fetch(`${config.policyGatewayUrl}/policy/scopes`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${accessToken}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(5_000),
    });

    if (!res.ok) return [];

    const data = (await res.json()) as ScopesResponse;
    return data.scopes ?? [];
  } catch {
    return [];
  }
}

/** Identity's error envelope. */
interface BackendError {
  code?: string;
  details?: { tenants?: unknown };
}

/**
 * Saves the session, dropping the eligible set when it pushes the cookie past
 * the browser limit. Sign-in matters more than the switcher, which then hides.
 */
async function saveWithTenants(session: IronSession<SessionPayload>): Promise<void> {
  try {
    await session.save();
  } catch (err) {
    if (!session.tenants) throw err;
    delete session.tenants;
    await session.save();
  }
}

async function readError(res: Response): Promise<BackendError> {
  try {
    const body: unknown = await res.json();
    return body && typeof body === 'object' ? (body as BackendError) : {};
  } catch {
    return {};
  }
}

// ── POST — exchange id_token for kora session ────────────────────────────────

export async function POST(request: Request): Promise<Response> {
  // Only the callback page posts here; a cross-site post could plant a session or the last-used tenant.
  if (request.headers.get('sec-fetch-site') === 'cross-site') {
    return Response.json({ error: 'bad_request' }, { status: 403 });
  }

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
  // (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) and threaded through the PKCE state param by the callback page.
  const tenantId = raw.tenant_id;
  if (typeof tenantId !== 'string' || !tenantId) {
    return Response.json(
      { error: 'bad_request', message: 'tenant_id is required' },
      { status: 400 },
    );
  }

  const requestedHint =
    typeof raw.tenant_hint === 'string' && raw.tenant_hint ? raw.tenant_hint : undefined;
  if (raw.switch === true && !requestedHint) {
    return Response.json(
      { error: 'bad_request', message: 'tenant_hint is required for a switch' },
      { status: 400 },
    );
  }

  // A switch needs the session it replaces. Without one, the exchange runs as a
  // plain sign-in with the target as its hint.
  const current = raw.switch === true ? await getSession() : undefined;
  const replaced = current?.refreshToken && current.tenantId ? current : undefined;

  const cookieStore = await cookies();
  const tenantHint = requestedHint ?? cookieStore.get(LAST_TENANT_COOKIE_NAME)?.value;

  // Forward to Go identity service.
  let backendRes: Response;
  try {
    backendRes = await fetch(`${config.koraApiUrl}/identity/session`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        grant_type: 'oidc_id_token',
        id_token: idToken,
        tenant_id: replaced?.tenantId ?? tenantId,
        tenant_hint: tenantHint,
        replaces_refresh_token: replaced?.refreshToken,
      }),
      signal: AbortSignal.timeout(5_000),
    });
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  if (!backendRes.ok) {
    if (backendRes.status === 401) {
      return Response.json({ error: 'unauthorized' }, { status: 401 });
    }
    if (backendRes.status === 403 || backendRes.status === 409) {
      const err = await readError(backendRes);
      const tenants = parseTenants(err.details?.tenants);
      // A refused switch keeps the current session and records the fresh set.
      // Any 403 or 409 during a switch means the target is not among the matches.
      if (replaced) {
        if (tenants) {
          // Rebind with the refresh token's remaining lifetime so the kept cookie
          // does not outlive it.
          const remaining = replaced.refreshExpiresAt
            ? Math.max(1, replaced.refreshExpiresAt - Math.floor(Date.now() / 1000))
            : undefined;
          const kept = await getIronSession<SessionPayload>(
            cookieStore,
            getSessionOptions(remaining),
          );
          kept.tenants = tenants;
          await saveWithTenants(kept);
        }
        return Response.json({ error: 'tenant_switch_denied' }, { status: 403 });
      }
      if (err.code === 'multiple_tenants_match' && tenants) {
        return Response.json({ error: 'multiple_tenants_match', tenants }, { status: 409 });
      }
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

  // Decode the kora JWT to extract user identity claims.
  let claims: KoraJwtPayload;
  try {
    claims = decodeJwtPayload(data.access_token);
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  const user: SessionUser = {
    sub: claims.sub,
    email: claims.kora?.claims?.['email'] ?? '',
    name: claims.kora?.display_name ?? '',
  };

  const issuedTenant = tenantIdFromAccessToken(data.access_token) ?? tenantId;
  const payload: SessionPayload = {
    user,
    accessToken: data.access_token,
    refreshToken: data.refresh_token,
    expiresAt: data.issued_at + data.expires_in,
    refreshExpiresAt: data.refresh_token_expires_in
      ? data.issued_at + data.refresh_token_expires_in
      : undefined,
    // The token's tenant wins: a claim binding can move the person off the
    // tenant Home Realm Discovery picked, which is only a hint.
    tenantId: issuedTenant,
    apiAuthoring: data.api_authoring ?? true,
    tenants: parseTenants(data.tenants),
  };

  // Create the session with a maxAge derived from the backend's refresh token
  // TTL so the cookie stays alive for exactly as long as the refresh token is
  // valid. We bypass getSession() here because that helper uses the default
  // options — iron-session bakes the maxAge in at getIronSession() time, not
  // at save() time, so we must pass the dynamic value at construction.
  const session = await getIronSession<SessionPayload>(
    cookieStore,
    getSessionOptions(data.refresh_token_expires_in),
  );
  Object.assign(session, payload);
  await saveWithTenants(session);
  cookieStore.set(LAST_TENANT_COOKIE_NAME, issuedTenant, lastTenantCookieOptions());

  return Response.json({});
}

// ── GET — read current session ────────────────────────────────────────────────

export async function GET(): Promise<Response> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return Response.json({ error: 'unauthorized' }, { status: 401 });
  }

  const scopes = await fetchScopes(session.accessToken);

  return Response.json({
    user: session.user,
    expiresAt: session.expiresAt,
    permissions: { scopes },
    apiAuthoring: session.apiAuthoring ?? true,
    tenants: (session.tenants ?? []).map((t) => ({ ...t, active: t.id === session.tenantId })),
  });
}
