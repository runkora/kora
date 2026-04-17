import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { forwardToGateway } from '@orbit/server/catalog-gateway';

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
    const upstream = await forwardToGateway(
      `/provider-models/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}`,
      session.accessToken,
      {
        method: 'PUT',
        body: JSON.stringify(body),
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
