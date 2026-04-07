import 'server-only';
import { NextResponse } from 'next/server';
import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';

export async function GET(): Promise<NextResponse> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return NextResponse.json(
      { code: 'unauthorized', message: 'No valid session' },
      { status: 401 },
    );
  }

  const result = await fetchModelsFromGateway(session.accessToken);

  if (!result.ok) {
    console.error(`[GET /api/models] upstream error ${result.status}: ${result.message}`);
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load models from inference gateway' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.models });
}
