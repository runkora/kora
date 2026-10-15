'use client';

/** @tier 3 — marketplace-scoped. Card view of Skill results; each card opens its Skill in place. */

import { Card, CardContent, CardFooter, CardHeader, Grid, Icon, Stack, Text } from '@orbit/hades';
import type { SkillSummary } from '@orbit/shared/api';
import { useMarketplaceContext } from '../_context/marketplace-context';

export function MarketplaceSkillGrid() {
  const { skills, browse, marketplaceName, openSkill } = useMarketplaceContext();

  return (
    <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
      {skills.map((skill) => (
        <SkillCard
          key={skill.skillSlug}
          skill={skill}
          marketplaceName={marketplaceName}
          onOpen={() => openSkill(browse.marketplace, skill.skillSlug)}
        />
      ))}
    </Grid>
  );
}

/**
 * One Skill result. The whole card is a button that opens the Skill in place,
 * so the address does not change. Enter and Space open it, as on a list row.
 */
function SkillCard({
  skill,
  marketplaceName,
  onOpen,
}: {
  skill: SkillSummary;
  marketplaceName: string;
  onOpen: () => void;
}) {
  let description: string;
  if (skill.description) {
    // The Skill has a description.
    description = skill.description;
  } else {
    // The description is empty. Show a placeholder.
    description = 'No description provided.';
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Enter' || event.key === ' ') {
      // Space would scroll the page. Open the Skill instead.
      event.preventDefault();
      onOpen();
    }
  }

  return (
    <Card
      interactive
      column
      role="button"
      tabIndex={0}
      aria-label={`Open ${skill.name}`}
      onClick={onOpen}
      onKeyDown={handleKeyDown}
      style={{ cursor: 'pointer' }}
    >
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
  );
}
