import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAgentAnalyticsFromGateway } from '../../../../(dashboard)/model-gateway/_services/analytics-server';

type Params = { agentTypeId: string };

export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { agentTypeId } = await (context?.params ?? Promise.resolve({ agentTypeId: '' }));

    if (!agentTypeId) {
      return NextResponse.json(
        { code: 'bad_request', message: 'agentTypeId is required' },
        { status: 400 },
      );
    }

    const result = await fetchAgentAnalyticsFromGateway(session.accessToken, agentTypeId);

    if (!result.ok) {
      console.error(
        `[GET /api/analytics/agents/${agentTypeId}] upstream error ${result.status}: ${result.message}`,
      );
      const clientStatus = result.status >= 500 ? 502 : result.status;
      return NextResponse.json(
        { code: 'upstream_error', message: 'Failed to load agent analytics' },
        { status: clientStatus },
      );
    }

    return NextResponse.json(result.data);
  },
);
