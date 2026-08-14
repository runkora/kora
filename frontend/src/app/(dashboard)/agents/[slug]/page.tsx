// RSC — fetches agent type + runs by slug server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentDetailFromBFF } from './_services/agent-detail-server';
import { fetchBudgetPrincipalsFromGateway } from '../../model-gateway/_services/budgets-server';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../model-gateway/_types';
import { AgentDetailShell } from './_components/agent-detail-shell';

interface AgentDetailPageProps {
  params: Promise<{ slug: string }>;
}

export default async function AgentDetailPage({ params }: AgentDetailPageProps) {
  const { slug } = await params;
  const session = await getSession();
  const valid = isSessionValid(session);
  const [result, budgets] = valid
    ? await Promise.all([
        fetchAgentDetailFromBFF(slug, session.accessToken, session.tenantId),
        fetchBudgetPrincipalsFromGateway(session.accessToken),
      ])
    : [null, null];

  // The agent's budget usage comes from the budgets listing — its grant
  // ceilings joined with live spend. Best-effort: viewers without
  // platform:budgets:read simply see no budget card.
  let budget: BudgetPrincipalStatus | null = null;
  let tenantCap: TenantCapStatus | null = null;
  if (budgets?.ok) {
    budget =
      budgets.data.data.find(
        (p) => p.principal_type === 'AGENT' && p.principal_id.endsWith(`/agent/${slug}`),
      ) ?? null;
    tenantCap = budgets.data.tenant_cap ?? null;
  } else if (budgets && budgets.status !== 403) {
    // 403 is expected for callers without platform:budgets:read; anything else
    // is an unexpected service failure — log it so it surfaces in server logs.
    console.error(`[AgentDetailPage] budgets fetch failed (${budgets.status}): ${budgets.message}`);
  }

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
      tenantCap={tenantCap}
    />
  );
}
