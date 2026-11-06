'use client';

/** @tier 3 — triggers-scoped. Client boundary holding the server-fetched data. */

import { TriggersPage } from './triggers-page';
import type { AgentOption, SourceRow, TriggerRow } from '../_types';
import type { TenantStatsRow } from '../_types/stats';

interface TriggersShellProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  stats: TenantStatsRow | null;
  agents: AgentOption[];
  agentsError?: string | null;
  initialError: string | null;
}

export function TriggersShell({
  sources,
  triggers,
  stats,
  agents,
  agentsError = null,
  initialError,
}: TriggersShellProps) {
  return (
    <TriggersPage
      sources={sources}
      triggers={triggers}
      stats={stats}
      agents={agents}
      agentsError={agentsError}
      error={initialError}
    />
  );
}
