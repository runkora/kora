// RSC: fetches agent types, live runs, run stats and the default window's insights in parallel
// server-side so the page renders with real data immediately. The insights are
// optional: when they cannot be had, the page renders without them.

import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchAgentsFromBFF } from './_services/agents-server';
import { loadInsights } from './_services/agents-insights-server';
import { AgentsShell } from './_components/agents-shell';
import { DEFAULT_PERIOD, RUNNING_ROWS } from './_lib/spend';
import { LIVE_RUN_STATES } from './_types';

export default async function AgentsPage() {
  const session = await getSession();
  const [result, insights] = isSessionValid(session)
    ? await Promise.all([
        fetchAgentsFromBFF(
          session.accessToken,
          session.tenantId,
          new URLSearchParams({ state: LIVE_RUN_STATES.join(','), limit: String(RUNNING_ROWS) }),
        ),
        loadInsights(session.accessToken, DEFAULT_PERIOD),
      ])
    : [null, null];

  const initialAgentTypes = result?.ok ? result.agentTypes : [];
  const initialLiveRuns = result?.ok ? result.runs : [];
  const initialRunStats = result?.ok ? result.runStats : null;
  const initialError = result && !result.ok ? result.message : null;

  return (
    <AgentsShell
      initialAgentTypes={initialAgentTypes}
      initialLiveRuns={initialLiveRuns}
      initialRunStats={initialRunStats}
      initialError={initialError}
      initialInsights={insights?.ok ? insights.data : null}
    />
  );
}
