/**
 * GET /api/auth/discover?email=user@example.com
 *
 * Home Realm Discovery BFF proxy (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md).
 *
 * Forwards the email query parameter to the identity service's tenant
 * discovery endpoint and returns the public OIDC config for the matching
 * tenant. No session is required — this is a pre-authentication endpoint.
 *
 * The client secret is never included in the response; the identity service
 * only surfaces: tenant_id, issuer_url, client_id, scopes.
 */
import 'server-only';

import { NextRequest, NextResponse } from 'next/server';

import { config } from '@runkora/server/config';

export async function GET(request: NextRequest): Promise<NextResponse> {
  const email = request.nextUrl.searchParams.get('email');

  if (!email) {
    return NextResponse.json(
      { error: 'invalid_field', message: 'email query parameter is required' },
      { status: 400 },
    );
  }

  const upstream = `${config.koraApiUrl}/identity/tenants/discover?email=${encodeURIComponent(email)}`;

  let response: Response;
  try {
    response = await fetch(upstream, {
      method: 'GET',
      headers: { 'Content-Type': 'application/json' },
      // This route is hit during login — disable caching so stale tenant
      // configs don't get served after a tenant is reconfigured.
      cache: 'no-store',
    });
  } catch (err) {
    console.error('[discover] upstream fetch failed', err);
    return NextResponse.json(
      { error: 'service_unavailable', message: 'tenant discovery service is unreachable' },
      { status: 503 },
    );
  }

  // Forward the upstream body and status verbatim.  The identity service
  // already returns well-formed JSON error objects on 400/404/500.
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    return NextResponse.json(
      { error: 'upstream_error', message: 'unexpected upstream response' },
      { status: 502 },
    );
  }
  return NextResponse.json(body, { status: response.status });
}
