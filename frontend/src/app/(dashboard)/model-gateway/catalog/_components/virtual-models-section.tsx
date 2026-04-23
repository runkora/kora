'use client';

import { useMemo } from 'react';
import { Badge, Button, DataTable, Icon, Stack, StatusDot, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import type { VirtualModel } from '../_types/catalog';
import { PROVIDER_LABELS } from '../_utils/catalog-constants';
import { StrategyBadge } from './strategy-badge';

const STATUS_OPTIONS = [
  { value: 'enabled', label: 'Enabled' },
  { value: 'disabled', label: 'Disabled' },
];

interface VirtualModelsSectionProps {
  models: VirtualModel[];
  onSelect: (m: VirtualModel) => void;
}

function virtualModelMatchesSearch(m: VirtualModel, q: string): boolean {
  return (
    m.family.toLowerCase().includes(q) ||
    m.name.toLowerCase().includes(q) ||
    m.displayName.toLowerCase().includes(q)
  );
}

const NEW_VIRTUAL_MODEL: VirtualModel = {
  id: '__new__',
  family: '',
  name: '',
  displayName: '',
  description: '',
  strategy: 'priority',
  routes: [],
  enabled: true,
};

export function VirtualModelsSection({ models, onSelect }: VirtualModelsSectionProps) {
  const columns = useMemo<DataTableColumn<VirtualModel>[]>(
    () => [
      {
        id: 'name',
        header: 'Virtual Model',
        locked: true,
        sortable: true,
        sortAccessor: (m) => m.name,
        cell: (m) => (
          <Stack gap="tight">
            <Text size="caption" color="subtle">
              {m.family}
            </Text>
            <Text size="supporting" weight="emphasis" mono>
              {m.name}
            </Text>
            <Text size="caption" color="muted">
              {m.displayName}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'strategy',
        header: 'Strategy',
        cell: (m) => <StrategyBadge strategy={m.strategy} />,
      },
      {
        id: 'routes',
        header: 'Routes',
        sortable: true,
        sortAccessor: (m) => m.routes.length,
        cell: (m) => (
          <Stack direction="horizontal" align="center" gap="tight">
            <Text size="supporting">{m.routes.length}</Text>
            <Stack direction="horizontal" gap="tight">
              {m.routes.map((r, ri) => (
                <StatusDot
                  key={r.id || ri}
                  status={r.enabled ? 'healthy' : 'muted'}
                  size="sm"
                  title={`${PROVIDER_LABELS[r.provider] ?? r.provider} — ${r.enabled ? 'active' : 'disabled'}`}
                />
              ))}
            </Stack>
          </Stack>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        filter: {
          mode: 'client',
          options: STATUS_OPTIONS,
          filterFn: (m, selected) => selected.has(m.enabled ? 'enabled' : 'disabled'),
        },
        cell: (m) => (
          <Badge variant={m.enabled ? 'healthy' : 'muted'} shape="pill" size="sm" dot>
            {m.enabled ? 'Enabled' : 'Disabled'}
          </Badge>
        ),
      },
      {
        id: 'chevron',
        header: '',
        locked: true,
        width: '2.5rem',
        cell: () => <Icon name="chevron-right" color="muted" />,
      },
    ],
    [],
  );

  return (
    <DataTable
      data={models}
      columns={columns}
      getRowKey={(m) => m.id}
      noun="virtual models"
      searchPlaceholder="Search virtual models…"
      searchFn={virtualModelMatchesSearch}
      defaultSortId="name"
      defaultSortDir="asc"
      emptyMessage="No virtual models configured."
      filteredEmptyMessage="No virtual models match your filters."
      onRowClick={onSelect}
      toolbarActions={
        <Button size="sm" onClick={() => onSelect(NEW_VIRTUAL_MODEL)}>
          <Icon name="plus" size="2xs" />
          New Virtual Model
        </Button>
      }
    />
  );
}
