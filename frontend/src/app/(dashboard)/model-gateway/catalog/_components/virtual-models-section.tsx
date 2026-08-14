'use client';

import { useState, useMemo } from 'react';
import { Badge, Button, Icon, SegmentedControl, Stack, StatusDot, Text } from '@orbit/terra';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { fmtCost } from '@orbit/utils';
import type { VirtualModel } from '../_types/catalog';
import { PROVIDER_LABELS } from '../_utils/catalog-constants';
import { StrategyBadge } from './strategy-badge';

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

function VmEffectivePriceCell({ model }: { model: VirtualModel }) {
  if (!model.pricing) {
    return (
      <Stack direction="horizontal" align="center" gap="tight">
        <Icon name="alert-circle" size="2xs" color="status-degraded" />
        <Text size="caption" color="muted">
          Unknown
        </Text>
      </Stack>
    );
  }
  return (
    <Stack gap="none">
      <Text size="supporting" mono color="foreground">
        {fmtCost(model.pricing.effective_input_cost_per_1m)}/1M
      </Text>
      <Text size="caption" color="muted" mono>
        out: {fmtCost(model.pricing.effective_output_cost_per_1m)}/1M
      </Text>
    </Stack>
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
  const [statusFilter, setStatusFilter] = useState<'all' | 'enabled' | 'disabled'>('all');

  const statusCounts = useMemo(
    () => ({
      all: models.length,
      enabled: models.filter((m) => m.enabled).length,
      disabled: models.filter((m) => !m.enabled).length,
    }),
    [models],
  );

  const filteredModels = useMemo(() => {
    if (statusFilter === 'all') return models;
    return models.filter((m) => (statusFilter === 'enabled') === m.enabled);
  }, [models, statusFilter]);

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
        id: 'effectivePrice',
        header: 'Effective Price',
        sortable: true,
        sortAccessor: (m) => m.pricing?.effective_input_cost_per_1m ?? -1,
        cell: (m) => <VmEffectivePriceCell model={m} />,
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
                  title={`${PROVIDER_LABELS[r.provider] ?? r.provider}: ${r.enabled ? 'active' : 'disabled'}`}
                />
              ))}
            </Stack>
          </Stack>
        ),
      },
      {
        id: 'status',
        header: 'Status',
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
        gridWidth: '2.5rem',
        cell: () => <Icon name="chevron-right" color="muted" />,
      },
    ],
    [],
  );

  return (
    <SmartTable
      tableId="catalogVirtualModels"
      data={filteredModels}
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
      quickFilters={
        <SegmentedControl
          aria-label="Filter virtual models by status"
          value={statusFilter}
          onChange={(v) => setStatusFilter(v as typeof statusFilter)}
          options={[
            { value: 'all', label: 'All', count: statusCounts.all },
            { value: 'enabled', label: 'Enabled', count: statusCounts.enabled },
            { value: 'disabled', label: 'Disabled', count: statusCounts.disabled },
          ]}
        />
      }
      toolbarActions={
        <Button size="sm" onClick={() => onSelect(NEW_VIRTUAL_MODEL)}>
          <Icon name="plus" size="2xs" />
          New Virtual Model
        </Button>
      }
    />
  );
}
