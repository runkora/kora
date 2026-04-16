import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';

async function forwardToGateway(
  path: string,
  accessToken: string,
  init?: RequestInit,
): Promise<Response> {
  return fetch(`${config.inferenceGatewayUrl}/catalog${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
      ...init?.headers,
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
}

type Params = Promise<{ id: string }>;

export async function GET(
  _req: NextRequest,
  { params }: { params: Params },
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }
  const { id } = await params;
  try {
    const upstream = await forwardToGateway(
      `/virtual-models/${encodeURIComponent(id)}`,
      session.accessToken,
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
  const { id } = await params;

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
      `/virtual-models/${encodeURIComponent(id)}`,
      session.accessToken,
      {
        method: 'PUT',
        body: JSON.stringify(body),
      },
    );
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

export async function DELETE(
  _req: NextRequest,
  { params }: { params: Params },
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }
  const { id } = await params;
  try {
    const upstream = await forwardToGateway(
      `/virtual-models/${encodeURIComponent(id)}`,
      session.accessToken,
      {
        method: 'DELETE',
      },
    );
    if (upstream.status === 204) {
      return new NextResponse(null, { status: 204 });
    }
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
