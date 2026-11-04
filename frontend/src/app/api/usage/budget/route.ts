import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchPersonalBudgetFromGateway } from '../../../(dashboard)/model-gateway/_services/budget-personal-server';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const result = await fetchPersonalBudgetFromGateway(session.accessToken);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return NextResponse.json(result.data);
}
