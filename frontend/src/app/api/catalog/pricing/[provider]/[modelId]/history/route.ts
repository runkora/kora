import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { forwardToGateway } from '@orbit/server/catalog-gateway';

type Params = Promise<{ provider: string; modelId: string }>;

export async function GET(
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
  const days = request.nextUrl.searchParams.get('days');
  const qs = days ? `?days=${encodeURIComponent(days)}` : '';

  try {
    const upstream = await forwardToGateway(
      `/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}/history${qs}`,
      session.accessToken,
      { method: 'GET' },
    );
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
