'use client';

/** @tier 3 — skills-scoped. Card view of the skills on this page; a card opens its skill. */

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Button,
  Card,
  CardContent,
  Grid,
  Icon,
  IconTile,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import { useSkillsContext } from '../_context/skills-context';
import { skillHref, skillKey } from '../_types';
import { clampLines, CopyLinkButton, FavouriteButton, nameLine, SkillChips } from './skill-chips';

export function SkillGrid() {
  const { skills, browse, marketplaces, favourites, toggleFavourite } = useSkillsContext();
  const router = useRouter();
  const repositories = new Map(marketplaces.map((m) => [m.slug, m.repository]));

  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      {skills.map((skill) => {
        const key = skillKey(skill);
        const href = skillHref(skill, browse);
        return (
          <Card key={key} column interactive onClick={() => router.push(href)}>
            <CardContent padded compact grow>
              <Stack gap="cozy">
                <Stack direction="horizontal" align="center" gap="cozy">
                  <Stack shrink>
                    <IconTile size="lg">
                      <Icon name="file" size="sm" />
                    </IconTile>
                  </Stack>
                  <Stack grow overflow="hidden" gap="none">
                    <Text weight="strong" truncate>
                      {skill.title}
                    </Text>
                    <Text mono size="caption" weight="emphasis" color="muted" truncate>
                      {nameLine(skill)}
                    </Text>
                  </Stack>
                  <Stack shrink>
                    <FavouriteButton
                      favourite={favourites.has(key)}
                      onToggle={() => toggleFavourite(key)}
                    />
                  </Stack>
                </Stack>
                <Text
                  size="supporting"
                  color="muted"
                  style={clampLines(3)}
                  title={skill.description}
                >
                  {skill.description}
                </Text>
                <SkillChips skill={skill} />
              </Stack>
            </CardContent>
            <Separator />
            <CardContent padded compact>
              <Stack direction="horizontal" align="center" justify="between" gap="cozy">
                <Text size="caption" color="muted" mono truncate>
                  {repositories.get(skill.marketplace) ?? skill.marketplace}
                </Text>
                <Stack direction="horizontal" align="center" gap="tight" shrink>
                  <CopyLinkButton skill={skill} />
                  <Button asChild variant="ghost" size="sm">
                    <Link href={href} onClick={(event) => event.stopPropagation()}>
                      View details
                      <Icon name="chevron-right" size="2xs" />
                    </Link>
                  </Button>
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        );
      })}
    </Grid>
  );
}
