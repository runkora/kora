import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchScopesExplain } from '../../../../(dashboard)/policy/_services/policy-server';

export async function GET(request: NextRequest): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const { searchParams } = request.nextUrl;
  const principalType = searchParams.get('principal_type') ?? undefined;
  const principalId = searchParams.get('principal_id') ?? undefined;

  const result = await fetchScopesExplain(session.accessToken, principalType, principalId);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load scope explanation' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.data });
}
