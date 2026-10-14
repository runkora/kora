'use client';

/** @tier 3 — marketplace-scoped. Client boundary for the skills list page. */

import type { SkillSummary } from '@orbit/lib/marketplace';
import { SkillsPage } from './skills-page';

interface SkillsShellProps {
  marketplace: string;
  skills: SkillSummary[];
  search: string;
  error: string | null;
}

export function SkillsShell({ marketplace, skills, search, error }: SkillsShellProps) {
  return (
    <SkillsPage marketplace={marketplace} skills={skills} initialSearch={search} error={error} />
  );
}
