'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. The skill page header and its Source card. */

import {
  Anchor,
  Button,
  Card,
  CardContent,
  CardHeader,
  Heading,
  Icon,
  Stack,
  Text,
  useCopy,
} from '@runkora/hades';
import type { Marketplace, Skill } from '@runkora/shared/api';
import { useSyncExternalStore } from 'react';
import { usePreferences } from '../../../../_preferences/preferences-context';
import { FavouriteButton, nameLine, SkillChips } from '../../../_components/skill-chips';
import { skillKey, toggleFavouriteKey } from '../../../_types';
import { relativeTime } from '@runkora/lib/relative-time';

const SHORT_COMMIT = 7;

/** The ref as the resource names it; an empty ref follows the repository's default branch. */
const refLabel = (ref: string) => ref || 'default branch';

const encode = (value: string) => value.split('/').map(encodeURIComponent).join('/');

const repoUrl = (skill: Skill) => `https://github.com/${encode(skill.repository)}`;

/** The skill folder on its host at the served commit, the same files this page shows. */
function folderUrl(skill: Skill): string {
  const at = skill.commit || skill.ref || 'HEAD';
  return `${repoUrl(skill)}/tree/${encode(at)}/${encode(skill.path)}`;
}

export function SkillHeader({ skill }: { skill: Skill }) {
  const { preferences, updatePreferences } = usePreferences();
  const key = skillKey(skill);
  const favourite = preferences.skills.favouriteSkills.includes(key);
  const toggle = () => updatePreferences((prev) => toggleFavouriteKey(prev, key));
  const separator = (
    <Text as="span" size="supporting" color="subtle">
      ·
    </Text>
  );

  return (
    <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
      <Stack direction="horizontal" align="start" gap="comfortable" grow minWidth="none">
        <Stack shrink>
          <Icon name="file" size="lg" color="accent" container containerVariant="accent" />
        </Stack>
        <Stack gap="snug" grow minWidth="none">
          <Heading variant="title">{skill.title}</Heading>
          <Stack direction="horizontal" align="center" gap="snug" wrap>
            <Text as="span" mono size="supporting" color="subtle">
              {nameLine(skill)}
            </Text>
            {separator}
            <Text as="span" mono size="supporting" color="subtle">
              {skill.repository}
            </Text>
            {separator}
            <Text as="span" mono size="supporting" color="subtle">
              {refLabel(skill.ref)} @ {skill.commit.slice(0, SHORT_COMMIT)}
            </Text>
          </Stack>
          <Stack maxWidth="document">
            <Text size="supporting" color={skill.description ? 'muted' : 'subtle'}>
              {skill.description || 'No description in SKILL.md.'}
            </Text>
          </Stack>
          <SkillChips skill={skill} />
        </Stack>
      </Stack>
      <Stack direction="horizontal" align="center" gap="snug" shrink>
        <FavouriteButton favourite={favourite} onToggle={toggle} />
        <Button asChild variant="outline" size="sm">
          <a href={folderUrl(skill)} target="_blank" rel="noopener noreferrer">
            <Icon name="external-link" size="xs" />
            View source
          </a>
        </Button>
        <CopySkillMd content={skill.content} />
      </Stack>
    </Stack>
  );
}

function CopySkillMd({ content }: { content: string }) {
  const { status, copy } = useCopy(content);
  return (
    <Button variant="outline" size="sm" onClick={copy}>
      <Icon
        name={status === 'copied' ? 'check' : 'copy'}
        size="xs"
        color={status === 'copied' ? 'status-healthy' : undefined}
      />
      {status === 'copied' ? 'Copied' : 'Copy SKILL.md'}
    </Button>
  );
}

const noSubscription = () => () => {};

export function SourceCard({ skill, marketplace }: { skill: Skill; marketplace?: Marketplace }) {
  // Synced is relative to now; it renders after hydration to match the server's HTML.
  const hydrated = useSyncExternalStore(
    noSubscription,
    () => true,
    () => false,
  );
  const link = (href: string, text: string) => (
    <Anchor href={href} target="_blank" rel="noopener noreferrer">
      {text}
    </Anchor>
  );
  const rows: { label: string; value: React.ReactNode }[] = [
    { label: 'Marketplace', value: skill.marketplace },
    {
      label: 'Repository',
      value: link(repoUrl(skill), skill.repository),
    },
    { label: 'Ref', value: refLabel(skill.ref) },
    { label: 'Commit', value: skill.commit.slice(0, SHORT_COMMIT) },
    { label: 'Folder', value: link(folderUrl(skill), skill.path) },
    ...(marketplace?.syncedAt
      ? [{ label: 'Synced', value: hydrated ? relativeTime(marketplace.syncedAt) : '' }]
      : []),
  ];

  return (
    <Card>
      <CardHeader compact title="Source" divider />
      <CardContent padded>
        <Stack gap="cozy">
          {rows.map((row) => (
            <Stack
              key={row.label}
              direction="horizontal"
              align="center"
              justify="between"
              gap="comfortable"
            >
              <Stack shrink noWrap>
                <Text as="span" size="caption" color="subtle">
                  {row.label}
                </Text>
              </Stack>
              <Stack minWidth="none">
                <Text
                  as="span"
                  mono
                  size="caption"
                  weight="emphasis"
                  color="muted"
                  truncate="start"
                  align="end"
                >
                  {row.value}
                </Text>
              </Stack>
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}
