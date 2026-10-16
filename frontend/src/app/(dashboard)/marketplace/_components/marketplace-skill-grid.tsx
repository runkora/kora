'use client';

/** @tier 3 — marketplace-scoped. Card view of Skill results; each card links to its detail route. */

import Link from 'next/link';
import { Card, CardContent, CardFooter, CardHeader, Grid, Icon, Stack, Text } from '@orbit/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { SkillSummary } from '@orbit/shared/api';
import { useMarketplaceContext } from '../_context/marketplace-context';

export function MarketplaceSkillGrid() {
  const { skills, browse, marketplaceName } = useMarketplaceContext();

  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {skills.map((skill) => (
        <SkillCard
          key={skill.skillSlug}
          skill={skill}
          marketplaceName={marketplaceName}
          href={APP_ROUTES.marketplace.detail(browse.marketplace, skill.skillSlug)}
        />
      ))}
    </Grid>
  );
}

/**
 * One Skill result. The whole card is a link to the server-rendered detail page.
 */
function SkillCard({
  skill,
  marketplaceName,
  href,
}: {
  skill: SkillSummary;
  marketplaceName: string;
  href: string;
}) {
  let description: string;
  if (skill.description) {
    // The Skill has a description.
    description = skill.description;
  } else {
    // The description is empty. Show a placeholder.
    description = 'No description provided.';
  }

  return (
    <Link href={href} aria-label={`Open ${skill.name}`} style={{ display: 'flex' }}>
      <Card interactive column style={{ width: '100%' }}>
        <Stack grow gap="none">
          <CardHeader
            title={skill.name}
            description={
              <Text mono size="caption" weight="emphasis" color="subtle" truncate>
                {skill.skillSlug}
              </Text>
            }
          />

          {/* grow keeps the footers of the cards in one row aligned. */}
          <CardContent grow>
            <Text size="supporting" color="muted">
              {description}
            </Text>
          </CardContent>

          <CardFooter compact justify="between">
            <Text as="span" size="caption" color="subtle">
              {marketplaceName}
            </Text>
            <Icon name="chevron-right" size="2xs" color="muted" />
          </CardFooter>
        </Stack>
      </Card>
    </Link>
  );
}
