'use client';

/** @tier 3 — marketplace-scoped. Card view of Skill results; each card opens its detail route. */

import Link from 'next/link';
import { Card, CardContent, CardFooter, CardHeader, Grid, Icon, Stack, Text } from '@orbit/hades';
import type { PackageOverview } from '@orbit/shared/api';
import { useMarketplaceContext } from '../_context/marketplace-context';
import { buildSkillUrl } from '../_utils/browse-params';

export function MarketplaceSkillGrid() {
  const { skills, browse } = useMarketplaceContext();

  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {skills.map((skill) => (
        <SkillCard
          key={skill.slug}
          skill={skill}
          marketplaceName={browse.marketplace}
          href={buildSkillUrl(skill.slug, browse)}
        />
      ))}
    </Grid>
  );
}

/**
 * One Skill result. The whole card is a link to the detail route. The link is
 * inside the card, so that the focus ring of the interactive card shows when
 * the link has keyboard focus.
 */
function SkillCard({
  skill,
  marketplaceName,
  href,
}: {
  skill: PackageOverview;
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
    <Card interactive column>
      <Stack asChild grow gap="none">
        <Link href={href}>
          <CardHeader
            title={skill.title_of_package}
            description={
              <Text mono size="caption" weight="emphasis" color="subtle" truncate>
                {skill.slug}
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
        </Link>
      </Stack>
    </Card>
  );
}
