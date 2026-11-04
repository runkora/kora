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

  return (
    <AgentsShell
      initialAgentTypes={result?.agentTypes.ok ? result.agentTypes.data : []}
      initialLiveRuns={result?.runs.ok ? result.runs.data : []}
      initialRunStats={result?.runStats ?? null}
      initialError={result && !result.agentTypes.ok ? result.agentTypes.message : null}
      initialRunsError={result && !result.runs.ok ? result.runs.message : null}
      initialInsights={insights?.ok ? insights.data : null}
    />
  );
}
