import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';

type Params = Promise<{ provider: string; modelId: string }>;

export async function PUT(
  request: NextRequest,
  { params }: { params: Params },
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }
  const { provider, modelId } = await params;

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json(
      { code: 'bad_request', message: 'Invalid JSON body' },
      { status: 400 },
    );
  }

  try {
    const upstream = await fetch(
      `${config.inferenceGatewayUrl}/catalog/provider-models/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}`,
      {
        method: 'PUT',
        headers: {
          Authorization: `Bearer ${session.accessToken}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(body),
        cache: 'no-store',
        signal: AbortSignal.timeout(15_000),
      },
    );
    if (upstream.status === 204) {
      return new NextResponse(null, { status: 204 });
    }
    const responseBody = await upstream.json();
    const status = upstream.ok ? upstream.status : upstream.status >= 500 ? 502 : upstream.status;
    return NextResponse.json(responseBody, { status });
  } catch {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to reach inference gateway' },
      { status: 502 },
    );
  }
}
