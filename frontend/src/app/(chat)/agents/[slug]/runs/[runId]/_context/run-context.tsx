'use client';

/**
 * @tier 3 — run-chat-scoped. Provides RunChatState + canvas mutations to the
 * entire run chat page tree. Promote only if a second feature needs it.
 */

import { createContext, useContext } from 'react';
import { useRun, type UseRunReturn } from '../_hooks/use-run';

const RunContext = createContext<UseRunReturn | null>(null);

export function RunProvider({
  slug,
  runId,
  children,
}: {
  slug: string;
  runId: string;
  children: React.ReactNode;
}) {
  const value = useRun(runId, slug);
  return <RunContext.Provider value={value}>{children}</RunContext.Provider>;
}

export function useRunContext(): UseRunReturn {
  const ctx = useContext(RunContext);
  if (!ctx) {
    throw new Error('useRunContext must be used inside <RunProvider>');
  }
  return ctx;
}
