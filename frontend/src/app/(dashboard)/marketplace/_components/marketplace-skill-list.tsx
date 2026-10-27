'use client';

/** @tier 3 — marketplace-scoped. List view of Skill results; each row opens its detail route. */

import { useRouter } from 'next/navigation';
import { DataTable, Icon, Stack, Text, type DataTableColumn } from '@orbit/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { SkillSummary } from '@orbit/shared/api';
import { useMarketplaceContext } from '../_context/marketplace-context';

export function MarketplaceSkillList() {
  const { skills, browse, marketplaceName } = useMarketplaceContext();
  const router = useRouter();

  const columns: DataTableColumn<SkillSummary>[] = [
    {
      id: 'skill',
      header: 'Skill',
      gridWidth: 'minmax(220px, 1.5fr)',
      locked: true,
      cell: (skill) => (
        <Stack gap="none" style={{ minWidth: 0 }}>
          <Text truncate>{skill.name}</Text>
          <Text as="span" mono size="caption" weight="emphasis" color="subtle" truncate>
            {skill.skillSlug}
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
      // Every result comes from the selected Marketplace. The chevron sits in
      // this column, so that no separate column leaves empty space.
      id: 'marketplace',
      header: 'Marketplace',
      gridWidth: 'minmax(160px, 1fr)',
      locked: true,
      cell: () => (
        <Stack direction="horizontal" align="center" justify="between" gap="snug">
          <Text size="supporting" truncate>
            {marketplaceName}
          </Text>
          <Icon name="chevron-right" size="2xs" color="muted" />
        </Stack>
      ),
    },
  ];

  return (
    <DataTable
      data={skills}
      columns={columns}
      getRowKey={(skill) => skill.skillSlug}
      noun="skills"
      emptyMessage="No skills to show."
      onRowClick={(skill) =>
        router.push(APP_ROUTES.marketplace.detail(browse.marketplace, skill.skillName))
      }
      rowLabel={(skill) => `Open ${skill.name}`}
    />
  );
}
