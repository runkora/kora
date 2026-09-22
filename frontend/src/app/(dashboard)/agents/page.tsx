// RSC — fetches agent types + a run summary in parallel server-side so the
// page renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentsFromBFF } from './_services/agents-server';
import { AgentsShell } from './_components/agents-shell';
import type { RunStats } from '@orbit/shared/api';

const emptyRunStats: RunStats = {
  total: 0,
  active: 0,
  completed: 0,
  failed: 0,
  avg_completed_duration_ms: null,
};

export default async function AgentsPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchAgentsFromBFF(session.accessToken, session.tenantId)
    : null;

  const initialAgentTypes = result?.ok ? result.agentTypes : [];
  const initialRunStats = result?.ok ? result.runStats : emptyRunStats;
  const initialError = result && !result.ok ? result.message : null;

  return (
    <AgentsShell
      initialAgentTypes={initialAgentTypes}
      initialRunStats={initialRunStats}
      initialError={initialError}
    />
  );
}
