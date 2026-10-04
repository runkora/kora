// RSC: fetches the agent type, its runs and its budget row by slug.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentDetailFromBFF } from './_services/agent-detail-server';
import { fetchGatewayInsights } from '../_services/agents-insights-server';
import { toBudgetRows } from '../_lib/insights';
import { DEFAULT_PERIOD } from '../_lib/spend';
import { AgentDetailShell } from './_components/agent-detail-shell';

interface AgentDetailPageProps {
  params: Promise<{ slug: string }>;
}

export default async function AgentDetailPage({ params }: AgentDetailPageProps) {
  const { slug } = await params;
  const session = await getSession();
  const valid = isSessionValid(session);
  const [result, insights] = valid
    ? await Promise.all([
        fetchAgentDetailFromBFF(slug, session.accessToken, session.tenantId),
        fetchGatewayInsights(session.accessToken, DEFAULT_PERIOD),
      ])
    : [null, null];

  // The budget row comes from the same read as the list page. Callers without
  // agents:usage:read get a 403, which shows no budget card.
  const row = insights?.ok ? insights.data.budgets?.find((b) => b.agent_slug === slug) : undefined;
  const budget = toBudgetRows(row ? [row] : [], [])[0] ?? null;

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
      budget={budget}
    />
  );
}
