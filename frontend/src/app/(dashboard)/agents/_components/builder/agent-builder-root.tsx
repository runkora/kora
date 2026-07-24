'use client';

/** @tier 3 — agents-scoped. The dashboard-layout mount point for the agent builder. */

// The builder opens over whatever the user was doing, addressed by search
// param: `?agent=new` (fresh) or `?agent={draftId}` (resume). Mounted in the
// dashboard layout because it must open over any dashboard route
// (docs/adr/2026-07-17-agent-builder.md §1).
//
// Reset comes from the mount boundary: the host renders only while the param
// is present, so closing unmounts all builder state and reopening starts
// fresh. The mid-session URL adoption of the draft id (new → {draftId})
// changes only a prop, so it does not remount.

import { useEffect } from 'react';
import { useSearchParams } from 'next/navigation';
import { AgentBuilderProvider } from '../../_context/agent-builder-context';
import { useAgentBuilder } from '../../_hooks/use-agent-builder';
import { adoptBuilderDraft, closeBuilder } from '../../_lib/builder-url';
import { AgentBuilderModal } from './agent-builder-modal';

export function AgentBuilderRoot() {
  const searchParams = useSearchParams();
  const agentParam = searchParams.get('agent');
  if (agentParam === null) return null;
  return <AgentBuilderHost initialDraftId={agentParam !== 'new' ? agentParam : null} />;
}

function AgentBuilderHost({ initialDraftId }: { initialDraftId: string | null }) {
  const searchParams = useSearchParams();
  const builder = useAgentBuilder({ initialDraftId });

  // Once a draft record exists, the URL adopts its id so refresh resumes.
  useEffect(() => {
    if (!builder.draftId || searchParams.get('agent') === builder.draftId) return;
    adoptBuilderDraft(builder.draftId);
  }, [builder.draftId, searchParams]);

  const close = () => {
    builder.saveNow();
    closeBuilder();
  };

  return (
    <AgentBuilderProvider value={builder}>
      <AgentBuilderModal onClose={close} />
    </AgentBuilderProvider>
  );
}
