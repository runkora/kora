'use client';

/** @tier 3 — skills-scoped. List view of the skills on this page, as the MCP servers table; a row opens its skill. */

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Icon, IconTile, Stack, Text } from '@runkora/hades';
import { SmartTable, type DataTableColumn } from '@runkora/components';
import type { SkillSummary } from '@runkora/shared/api';
import { useSkillsContext } from '../_context/skills-context';
import { skillHref, skillKey } from '../_types';
import { FavouriteButton, nameLine, SkillChips } from './skill-chips';

/** Hades Text has no line clamp, so the description clamps with the agent card's inline style. */
const TWO_LINES = {
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical',
  overflow: 'hidden',
} as const;

/** skills-service has already searched the page, so the table keeps every row it gets. */
const keepRow = () => true;

export function SkillList({ toolbarActions }: { toolbarActions: React.ReactNode }) {
  const { skills, browse, favourites, toggleFavourite, searchText, setSearchText } =
    useSkillsContext();
  const router = useRouter();

  const columns = useMemo<DataTableColumn<SkillSummary>[]>(
    () => [
      {
        id: 'skill',
        header: 'Skill',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '12rem',
        cell: (skill) => (
          <Stack direction="horizontal" align="center" gap="snug">
            <IconTile size="md">
              <Icon name="file" size="xs" />
            </IconTile>
            <Stack gap="none" overflow="hidden">
              <Text weight="strong" size="supporting" truncate>
                {skill.title}
              </Text>
              <Text mono size="caption" color="muted" truncate>
                {nameLine(skill)}
              </Text>
            </Stack>
          </Stack>
        ),
      },
      {
        id: 'description',
        header: 'Description',
        gridWidth: 'minmax(0, 3fr)',
        minWidth: '12rem',
        wrap: true,
        cell: (skill) => (
          <Text size="supporting" color="muted" style={TWO_LINES}>
            {skill.description}
          </Text>
        ),
      },
      {
        id: 'tags',
        header: 'Tags',
        gridWidth: 'minmax(0, 1.2fr)',
        minWidth: '8rem',
        wrap: true,
        cell: (skill) => <SkillChips skill={skill} />,
      },
      {
        id: 'favourite',
        header: '',
        gridWidth: '3rem',
        locked: true,
        cell: (skill) => (
          <FavouriteButton
            favourite={favourites.has(skillKey(skill))}
            onToggle={() => toggleFavourite(skillKey(skill))}
          />
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '3rem',
        cell: (skill) => (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Open ${skill.title}`}
            onClick={(event) => {
              event.stopPropagation();
              router.push(skillHref(skill, browse));
            }}
          >
            <Icon name="chevron-right" size="2xs" />
          </Button>
        ),
      },
    ],
    [favourites, toggleFavourite, router, browse],
  );

  return (
    <SmartTable
      tableId="skills"
      data={skills}
      columns={columns}
      getRowKey={skillKey}
      noun="skills"
      searchPlaceholder="Search skills…"
      searchFn={keepRow}
      searchQuery={searchText}
      onSearchChange={setSearchText}
      onRowClick={(skill) => router.push(skillHref(skill, browse))}
      rowLabel={(skill) => `Open ${skill.title}`}
      emptyMessage="No skills on this page."
      filteredEmptyMessage="No skills match your search."
      toolbarActions={toolbarActions}
    />
  );
}
