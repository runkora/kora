import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAllTenantUsers } from '../../../(dashboard)/model-gateway/_services/tokens-server';

export const GET = withAuth(async (_req, session) => {
  const result = await fetchAllTenantUsers(session.accessToken);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load users' },
      { status: result.status >= 500 ? 502 : result.status },
    );
  }
  return NextResponse.json({ data: result.users });
});
