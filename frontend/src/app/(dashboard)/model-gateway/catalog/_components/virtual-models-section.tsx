'use client';

import { useMemo } from 'react';
import { ChevronRight, Plus } from 'lucide-react';
import { Button, DataTable, Stack, Text } from '@orbit/terra';
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
          <Stack gap="none">
            <span className="mb-0.5 block text-[10px] uppercase tracking-wider">
              <Text size="caption" color="subtle">
                {m.family}
              </Text>
            </span>
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
            <span className="flex gap-0.5">
              {m.routes.map((r, ri) => (
                <span
                  key={r.id || ri}
                  className={`h-1.5 w-1.5 rounded-full ${r.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
                  title={`${PROVIDER_LABELS[r.provider] ?? r.provider} — ${r.enabled ? 'active' : 'disabled'}`}
                />
              ))}
            </span>
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
          <span
            className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
              m.enabled ? 'bg-status-healthy/15 text-status-healthy' : 'bg-muted/20 text-muted'
            }`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${m.enabled ? 'bg-status-healthy' : 'bg-muted/40'}`}
            />
            {m.enabled ? 'Enabled' : 'Disabled'}
          </span>
        ),
      },
      {
        id: 'chevron',
        header: '',
        locked: true,
        width: '2.5rem',
        cell: () => <ChevronRight className="size-4 text-muted" />,
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
          <Plus className="mr-1.5 size-3.5" />
          New Virtual Model
        </Button>
      }
    />
  );
}
