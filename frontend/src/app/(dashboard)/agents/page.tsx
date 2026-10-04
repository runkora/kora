// RSC: fetches agent types, recent runs and the default window's insights in parallel
// server-side so the page renders with real data immediately. The insights are
// optional: when they cannot be had, the page renders without them.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentsFromBFF } from './_services/agents-server';
import { loadInsights } from './_services/agents-insights-server';
import { AgentsShell } from './_components/agents-shell';
import { DEFAULT_PERIOD } from './_lib/spend';

export default async function AgentsPage() {
  const session = await getSession();
  const [result, insights] = isSessionValid(session)
    ? await Promise.all([
        fetchAgentsFromBFF(session.accessToken, session.tenantId),
        loadInsights(session.accessToken, DEFAULT_PERIOD),
      ])
    : [null, null];

  const initialAgentTypes = result?.ok ? result.agentTypes : [];
  const initialRuns = result?.ok ? result.runs : [];
  const initialError = result && !result.ok ? result.message : null;

  return (
    <AgentsShell
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
      initialInsights={insights?.ok ? insights.data : null}
    />
  );
}
