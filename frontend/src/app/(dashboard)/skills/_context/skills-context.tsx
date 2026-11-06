'use client';

import { createContext, useContext } from 'react';
import { useSkills, type SkillsStateShape } from '../_hooks/use-skills';
import type { SkillsLoadError, SkillsOverview } from '../_types';

const SkillsContext = createContext<SkillsStateShape | null>(null);

interface SkillsProviderProps {
  initialData: SkillsOverview;
  initialError: SkillsLoadError | null;
  children: React.ReactNode;
}

export function SkillsProvider({ initialData, initialError, children }: SkillsProviderProps) {
  const state = useSkills(initialData, initialError);
  return <SkillsContext.Provider value={state}>{children}</SkillsContext.Provider>;
}

export function useSkillsContext(): SkillsStateShape {
  const ctx = useContext(SkillsContext);
  if (!ctx) throw new Error('useSkillsContext must be used inside <SkillsProvider>');
  return ctx;
}
