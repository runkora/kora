'use client';

/** @tier 3 — marketplace-scoped. List view of Skill results; each row opens its detail route. */

import { useRouter } from 'next/navigation';
import { DataTable, Icon, Stack, Text, type DataTableColumn } from '@orbit/terra';
import type { PackageOverview } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';

interface SkillListProps {
  skills: PackageOverview[];
}

export function SkillList({ skills }: SkillListProps) {
  const router = useRouter();

  const columns: DataTableColumn<PackageOverview>[] = [
    {
      id: 'skill',
      header: 'Skill',
      gridWidth: 'minmax(220px, 1.5fr)',
      locked: true,
      cell: (skill) => (
        <Stack gap="none" style={{ minWidth: 0 }}>
          <Text truncate>{skill.title_of_package}</Text>
          <Text mono size="caption" color="subtle" truncate>
            {skill.slug.split('/')[2]}
          </Text>
        </Stack>
      ),
    },
    {
      id: 'description',
      header: 'Description',
      gridWidth: 'minmax(280px, 2.1fr)',
      locked: true,
      cell: (skill) => (
        <Text
          size="supporting"
          color="muted"
          style={{
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
        >
          {skill.description}
        </Text>
      ),
    },
    {
      id: 'chevron',
      header: '',
      gridWidth: '30px',
      locked: true,
      cell: () => <Icon name="chevron-right" size="2xs" color="muted" />,
    },
  ];

  return (
    <DataTable
      data={skills}
      columns={columns}
      getRowKey={(skill) => skill.slug}
      noun="skills"
      emptyMessage="No skills to show."
      onRowClick={(skill) => router.push(APP_ROUTES.marketplace.detail(skill.slug))}
      rowLabel={(skill) => `Open ${skill.title_of_package}`}
    />
  );
}
