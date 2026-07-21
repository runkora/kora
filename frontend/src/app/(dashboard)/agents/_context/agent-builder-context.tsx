'use client';

import { createContext, useContext } from 'react';
import type { AgentBuilderStateShape } from '../_hooks/use-agent-builder';

const AgentBuilderContext = createContext<AgentBuilderStateShape | null>(null);

export function AgentBuilderProvider({
  value,
  children,
}: {
  value: AgentBuilderStateShape;
  children: React.ReactNode;
}) {
  return <AgentBuilderContext.Provider value={value}>{children}</AgentBuilderContext.Provider>;
}

export function useAgentBuilderContext(): AgentBuilderStateShape {
  const ctx = useContext(AgentBuilderContext);
  if (!ctx) {
    throw new Error('useAgentBuilderContext must be used within AgentBuilderProvider');
  }
  return ctx;
}
