import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  try {
    const upstream = await fetch(`${config.inferenceGatewayUrl}/catalog/provider-models`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(15_000),
    });
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
