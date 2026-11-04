'use client';

/** @tier 3 — skills-scoped. List view of the skills on this page; each row opens its skill. */

import { useRouter } from 'next/navigation';
import { DataTable, Stack, Text, type DataTableColumn } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { SkillSummary } from '@runkora/shared/api';
import { useSkillsContext } from '../_context/skills-context';
import { browseQuery } from '../_types';

const COLUMNS: DataTableColumn<SkillSummary>[] = [
  {
    id: 'skill',
    header: 'Skill',
    gridWidth: '16rem',
    minWidth: '10rem',
    locked: true,
    // The path is the skill's identity; two folders can share a name.
    cell: (skill) => (
      <Stack gap="none">
        <Text weight="strong">{skill.name}</Text>
        <Text size="caption" color="muted" mono>
          {skill.path}
        </Text>
      </Stack>
    ),
  },
  {
    id: 'description',
    header: 'Description',
    gridWidth: 'minmax(0, 1fr)',
    minWidth: '14rem',
    locked: true,
    cell: (skill) => (
      <Text size="supporting" color="muted">
        {skill.description}
      </Text>
    ),
  },
];

export function SkillList({ toolbarActions }: { toolbarActions: React.ReactNode }) {
  const { skills, browse } = useSkillsContext();
  const router = useRouter();

  return (
    <DataTable
      data={skills}
      columns={COLUMNS}
      getRowKey={(skill) => skill.path}
      noun="skills"
      toolbarActions={toolbarActions}
      emptyMessage="No skills on this page."
      onRowClick={(skill) =>
        router.push(
          `${APP_ROUTES.skills.skill(browse.marketplace, skill.path)}${browseQuery(browse)}`,
        )
      }
      rowLabel={(skill) => `Open ${skill.name}`}
    />
  );
}
