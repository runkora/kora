// RSC — fetches agent type + runs by slug server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentDetailFromBFF } from './_services/agent-detail-server';
import { AgentDetailShell } from './_components/agent-detail-shell';

interface AgentDetailPageProps {
  params: Promise<{ slug: string }>;
}

export default async function AgentDetailPage({ params }: AgentDetailPageProps) {
  const { slug } = await params;
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchAgentDetailFromBFF(slug, session.accessToken)
    : null;

  const initialAgent = result?.ok ? result.agent : null;
  const initialRuns = result?.ok ? result.runs : [];
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
      initialError={initialError}
      initialStatus={initialStatus}
    />
  );
}
