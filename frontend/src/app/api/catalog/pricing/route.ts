import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@runkora/server/session';
import { forwardToGateway } from '@runkora/server/catalog-gateway';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  try {
    const upstream = await forwardToGateway('/pricing', session.accessToken, { method: 'GET' });
    const body = await upstream.json();
    const status = upstream.ok ? upstream.status : upstream.status >= 500 ? 502 : upstream.status;
    return NextResponse.json(body, { status });
  } catch {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to reach inference gateway' },
      { status: 502 },
    );
  }
}
