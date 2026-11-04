import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@runkora/server/session';
import { deleteRole } from '../../../../(dashboard)/policy/_services/policy-server';

export async function DELETE(
  _request: Request,
  { params }: { params: Promise<{ role: string }> },
): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { role } = await params;
  const result = await deleteRole(session.accessToken, session.tenantId, role);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return new NextResponse(null, { status: 204 });
}
