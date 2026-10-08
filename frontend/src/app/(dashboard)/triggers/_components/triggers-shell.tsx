'use client';

/** @tier 3 — triggers-scoped. Client boundary holding the server-fetched data. */

import { TriggersPage } from './triggers-page';
import type { AgentOption, DeliveryRow, SourceRow, TriggerRow } from '../_types';

interface TriggersShellProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  initialDeliveryCursor?: string | null;
  agents: AgentOption[];
  agentsError?: string | null;
  initialError: string | null;
}

export function TriggersShell({
  sources,
  triggers,
  deliveries,
  initialDeliveryCursor = null,
  agents,
  agentsError = null,
  initialError,
}: TriggersShellProps) {
  return (
    <TriggersPage
      sources={sources}
      triggers={triggers}
      deliveries={deliveries}
      initialDeliveryCursor={initialDeliveryCursor}
      agents={agents}
      agentsError={agentsError}
      error={initialError}
    />
  );
}
