import 'server-only';
import { NextResponse } from 'next/server';
import { config } from '@orbit/server/config';
import { getSession, isSessionValid } from '@orbit/server/session';
import type { ModelsListResponse } from '@orbit/shared/api';

const UPSTREAM_URL = `${config.inferenceGatewayUrl}/models`;

export async function GET(): Promise<NextResponse> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  let upstream: Response;
  try {
    upstream = await fetch(UPSTREAM_URL, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
    });
  } catch (err) {
    console.error('[BFF /api/models] upstream fetch failed:', err);
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to reach inference gateway' },
      { status: 502 },
    );
  }

  if (!upstream.ok) {
    const body = await upstream.text().catch(() => '');
    return NextResponse.json(
      { code: 'upstream_error', message: body || upstream.statusText },
      { status: upstream.status },
    );
  }

  const data = (await upstream.json()) as ModelsListResponse;
  return NextResponse.json(data);
}
