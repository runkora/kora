import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchGrants, createGrant } from '../../../(dashboard)/policy/_services/policy-server';
import type { CreateGrantRequest } from '../../../(dashboard)/policy/_types/api';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const result = await fetchGrants(session.accessToken, session.tenantId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load grants' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.data });
}

export async function POST(request: NextRequest): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  let body: CreateGrantRequest;
  try {
    body = (await request.json()) as CreateGrantRequest;
  } catch {
    return NextResponse.json(
      { code: 'bad_request', message: 'Invalid JSON body' },
      { status: 400 },
    );
  }

  const result = await createGrant(session.accessToken, {
    ...body,
    tenant_id: body.tenant_id ?? session.tenantId,
  });
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.data }, { status: 201 });
}
