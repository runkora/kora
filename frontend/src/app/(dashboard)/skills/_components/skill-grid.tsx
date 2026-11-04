'use client';

/** @tier 3 — skills-scoped. Card view of the skills on this page; each card links to its skill. */

import Link from 'next/link';
import { Card, CardContent, CardHeader, Grid, Stack, Text } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useSkillsContext } from '../_context/skills-context';

export function SkillGrid() {
  const { skills, browse } = useSkillsContext();

  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {skills.map((skill) => (
        <Stack asChild key={skill.path}>
          <Link href={APP_ROUTES.skills.skill(browse.marketplace, skill.path)}>
            <Card interactive column>
              <CardHeader title={skill.name} />
              <CardContent grow>
                <Text size="supporting" color="muted">
                  {skill.description || 'No description provided.'}
                </Text>
              </CardContent>
            </Card>
          </Link>
        </Stack>
      ))}
    </Grid>
  );
}
