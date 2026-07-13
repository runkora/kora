/**
 * GET /api/dev-environment/exit
 *
 * Clears the host-side routing cookie a dev environment's app-ingress gateway
 * sets on prefixed requests (`orbit-devenv-route`, devenvironment_app_ingress.go
 * — docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md)
 * and redirects to `/`.
 *
 * Simply linking to the unprefixed host root is not enough: the gateway
 * routes every unprefixed request by that cookie, not by URL, so as long as
 * it's set the browser lands right back in this same dev environment. This
 * route runs inside the dev environment's own frontend and clears the cookie
 * itself (no `Domain=` attribute was ever set on it, so any server answering
 * for the shared host can expire it) before redirecting to the host's real
 * Orbit install.
 */
import 'server-only';

import { cookies } from 'next/headers';
import { type NextRequest } from 'next/server';

import { APP_ROUTES } from '@orbit/lib/routing';

const DEV_ENV_ROUTE_COOKIE_NAME = 'orbit-devenv-route';

export async function GET(request: NextRequest): Promise<Response> {
  const cookieStore = await cookies();
  cookieStore.set(DEV_ENV_ROUTE_COOKIE_NAME, '', {
    path: '/',
    httpOnly: true,
    sameSite: 'lax',
    maxAge: 0,
  });

  return Response.redirect(new URL(APP_ROUTES.home, request.url));
}
