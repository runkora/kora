'use client';

/** @tier 3 — triggers-scoped. Client boundary holding the server-fetched data. */

import { TriggersPage } from './triggers-page';
import type { AgentOption, DeliveryRow, SourceRow, TriggerRow } from '../_types';

interface TriggersShellProps {
  sources: SourceRow[];
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  agents: AgentOption[];
  initialError: string | null;
}

export function TriggersShell({
  sources,
  triggers,
  deliveries,
  agents,
  initialError,
}: TriggersShellProps) {
  return (
    <TriggersPage
      sources={sources}
      triggers={triggers}
      deliveries={deliveries}
      agents={agents}
      error={initialError}
    />
  );
}
