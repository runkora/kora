'use client';

/** @tier 3 — marketplace-scoped. Client boundary for the skill detail page. */

import type { Skill } from '@orbit/lib/marketplace';
import { SkillDetailPage } from './skill-detail-page';

interface SkillShellProps {
  marketplace: string;
  skillName: string;
  skill: Skill | null;
  error: string | null;
}

export function SkillShell({ marketplace, skillName, skill, error }: SkillShellProps) {
  return (
    <SkillDetailPage marketplace={marketplace} skillName={skillName} skill={skill} error={error} />
  );
}
