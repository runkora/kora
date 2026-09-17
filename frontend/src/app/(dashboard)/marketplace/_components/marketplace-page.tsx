'use client';

/** @tier 3 — marketplace-scoped. Renders the Skill results for the overview page. */

import { Grid } from '@orbit/terra';
import type { PackageOverview } from '@orbit/shared/api';
import { SkillCard } from './marketplace-skill-card';

interface MarketplacePageProps {
  skills: PackageOverview[];
}

export function MarketplacePage({ skills }: MarketplacePageProps) {
  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {skills.map((skill) => (
        <SkillCard key={skill.slug} skill={skill} />
      ))}
    </Grid>
  );
}
