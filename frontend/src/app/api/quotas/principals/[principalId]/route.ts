import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import {
  fetchPrincipalQuota,
  setPrincipalQuota,
  clearPrincipalQuota,
} from '../../../../(dashboard)/policy/_services/quota-server';

interface Params {
  params: Promise<{ principalId: string }>;
}

export async function GET(_request: NextRequest, { params }: Params): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { principalId } = await params;

  const result = await fetchPrincipalQuota(session.accessToken, principalId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return NextResponse.json(result.data);
}

export async function PUT(request: NextRequest, { params }: Params): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { principalId } = await params;

  let body: { monthly_budget_usd: number };
  try {
    body = (await request.json()) as { monthly_budget_usd: number };
  } catch {
    return NextResponse.json(
      { code: 'bad_request', message: 'Invalid JSON body' },
      { status: 400 },
    );
  }

  const result = await setPrincipalQuota(session.accessToken, principalId, body.monthly_budget_usd);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
}

export async function DELETE(_request: NextRequest, { params }: Params): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { principalId } = await params;

  const result = await clearPrincipalQuota(session.accessToken, principalId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
}
