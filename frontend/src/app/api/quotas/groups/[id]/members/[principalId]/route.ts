import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import {
  setGroupMember,
  removeGroupMember,
} from '../../../../../../(dashboard)/policy/_services/quota-server';

interface Params {
  params: Promise<{ id: string; principalId: string }>;
}

export async function PUT(
  _request: NextRequest,
  { params }: Params,
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { id, principalId } = await params;

  const result = await setGroupMember(session.accessToken, id, principalId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
}

export async function DELETE(
  _request: NextRequest,
  { params }: Params,
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { id, principalId } = await params;

  const result = await removeGroupMember(session.accessToken, id, principalId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
}
