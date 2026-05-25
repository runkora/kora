// RSC — fetches agent types + recent runs in parallel server-side so the page
// renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentsFromBFF } from './_services/agents-server';
import { AgentsShell } from './_components/agents-shell';

export default async function AgentsPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchAgentsFromBFF(session.accessToken, session.tenantId)
    : null;

  const initialAgentTypes = result?.ok ? result.agentTypes : [];
  const initialRuns = result?.ok ? result.runs : [];
  const initialError = result && !result.ok ? result.message : null;

  return (
    <AgentsShell
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    />
  );
}
