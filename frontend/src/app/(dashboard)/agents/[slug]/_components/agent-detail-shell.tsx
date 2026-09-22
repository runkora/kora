'use client';

/** @tier 3 — agents/[slug]-scoped. Client boundary for the agent detail page. */

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentDetailPage } from './agent-detail-page';
import type { DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { RunStats } from '@orbit/shared/api';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';

interface AgentDetailShellProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  initialHasMoreRuns: boolean;
  initialNextCursor: string | null;
  initialRunStats: RunStats;
  initialError: string | null;
  initialStatus: DetailStatus;
  budget: BudgetPrincipalStatus | null;
  tenantCap: TenantCapStatus | null;
}

export function AgentDetailShell({
  initialAgent,
  initialRuns,
  initialHasMoreRuns,
  initialNextCursor,
  initialRunStats,
  initialError,
  initialStatus,
  budget,
  tenantCap,
}: AgentDetailShellProps) {
  return (
    <AgentDetailProvider
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialHasMoreRuns={initialHasMoreRuns}
      initialNextCursor={initialNextCursor}
      initialRunStats={initialRunStats}
      initialError={initialError}
      initialStatus={initialStatus}
    >
      <AgentDetailPage budget={budget} tenantCap={tenantCap} />
    </AgentDetailProvider>
  );
}
