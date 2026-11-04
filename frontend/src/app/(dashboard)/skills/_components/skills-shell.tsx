'use client';

/** @tier 3 — skills-scoped. Client boundary: mounts SkillsProvider and renders the overview. */

import { SkillsProvider } from '../_context/skills-context';
import type { SkillsLoadError, SkillsOverview } from '../_types';
import { SkillsPage } from './skills-page';

interface SkillsShellProps {
  initialOverview: SkillsOverview;
  initialError: SkillsLoadError | null;
}

export function SkillsShell({ initialOverview, initialError }: SkillsShellProps) {
  return (
    <SkillsProvider initialData={initialOverview} initialError={initialError}>
      <SkillsPage />
    </SkillsProvider>
  );
}
