import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchRoutes } from '../../../(dashboard)/policy/_services/policy-server';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const result = await fetchRoutes(session.accessToken);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load routes' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.data });
}
