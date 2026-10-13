// RSC: fetches the agent type, its first page of runs and its default-window overview by slug.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentDetailFromBFF } from './_services/agent-detail-server';
import { loadAgentOverview } from './_services/agent-overview-server';
import { DEFAULT_PERIOD } from '../_lib/spend';
import { AgentDetailShell } from './_components/agent-detail-shell';

interface AgentDetailPageProps {
  params: Promise<{ slug: string }>;
}

export default async function AgentDetailPage({ params }: AgentDetailPageProps) {
  const { slug } = await params;
  const session = await getSession();
  const valid = isSessionValid(session);
  const [result, overview] = valid
    ? await Promise.all([
        fetchAgentDetailFromBFF(slug, session.accessToken, session.tenantId),
        loadAgentOverview(session.accessToken, slug, DEFAULT_PERIOD),
      ])
    : [null, null];

  const initialAgent = result?.ok ? result.agent : null;
  const initialRuns = result?.ok ? result.runs : [];
  const initialNextCursor = result?.ok ? result.nextCursor : null;
  const initialError = result && !result.ok ? result.message : null;
  const initialStatus =
    result === null
      ? ('idle' as const)
      : result.ok
        ? ('success' as const)
        : result.status === 404
          ? ('not_found' as const)
          : ('error' as const);

  return (
    <AgentDetailShell
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialNextCursor={initialNextCursor}
      initialError={initialError}
      initialStatus={initialStatus}
      initialOverview={overview}
    />
  );
}
