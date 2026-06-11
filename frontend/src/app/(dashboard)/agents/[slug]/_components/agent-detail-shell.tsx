'use client';

/** @tier 3 — agents/[slug]-scoped. Client boundary for the agent detail page. */

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentDetailPage } from './agent-detail-page';
import type { DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';

interface AgentDetailShellProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  initialError: string | null;
  initialStatus: DetailStatus;
  budget: BudgetPrincipalStatus | null;
  tenantCap: TenantCapStatus | null;
}

export function AgentDetailShell({
  initialAgent,
  initialRuns,
  initialError,
  initialStatus,
  budget,
  tenantCap,
}: AgentDetailShellProps) {
  return (
    <AgentDetailProvider
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialError={initialError}
      initialStatus={initialStatus}
    >
      <AgentDetailPage budget={budget} tenantCap={tenantCap} />
    </AgentDetailProvider>
  );
}
