import 'server-only';
import { NextResponse, type NextRequest } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { checkSimulate } from '../../../../(dashboard)/policy/_services/policy-server';
import type { CheckSimulateRequest } from '../../../../(dashboard)/policy/_types/api';

export async function POST(request: NextRequest): Promise<NextResponse> {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  let body: CheckSimulateRequest;
  try {
    body = (await request.json()) as CheckSimulateRequest;
  } catch {
    return NextResponse.json(
      { code: 'bad_request', message: 'Invalid JSON body' },
      { status: 400 },
    );
  }

  const result = await checkSimulate(session.accessToken, body);
  if (!result.ok) {
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.data });
}
