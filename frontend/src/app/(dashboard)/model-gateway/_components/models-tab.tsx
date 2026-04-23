'use client';

import { useState, useMemo } from 'react';
import { Badge, Button, DataTable, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import type { ModelRow } from '../_types';
import { useModelsContext } from '../_context/models-context';
import { AvailabilityDot } from './availability-dot';
import { ModelDetailModal } from './model-detail-modal';
import { ModelCardGrid } from './model-card-grid';

// ── Status filter options ─────────────────────────────────────────────────────

const STATUS_OPTIONS = [
  { value: 'available', label: 'Available', dot: 'bg-status-healthy' },
  { value: 'unavailable', label: 'Unavailable', dot: 'bg-status-error' },
];

// ── Search predicate ──────────────────────────────────────────────────────────

function modelMatchesSearch(row: ModelRow, q: string): boolean {
  return (
    row.owned_by.toLowerCase().includes(q) ||
    row.family.toLowerCase().includes(q) ||
    row.name.toLowerCase().includes(q) ||
    row.id.toLowerCase().includes(q) ||
    row.capabilities.some((c) => c.toLowerCase().includes(q)) ||
    row.dialects.some((d) => d.toLowerCase().includes(q))
  );
}

// ── Chips ─────────────────────────────────────────────────────────────────────

function Chips({ values }: { values: string[] }) {
  if (values.length === 0)
    return (
      <Text size="caption" color="subtle">
        —
      </Text>
    );
  return (
    <Stack direction="horizontal" wrap gap="tight">
      {values.map((v) => (
        <Badge key={v} variant="outline" size="sm">
          {v}
        </Badge>
      ))}
    </Stack>
  );
}

// ── Load error ────────────────────────────────────────────────────────────────

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Stack align="center" justify="center">
      <figure style={{ padding: '3rem 0', textAlign: 'center', margin: 0 }}>
        <Stack gap="snug" align="center">
          <Text size="supporting" color="muted">
            Couldn&apos;t load models. The inference gateway may be unavailable.
          </Text>
          <Button variant="outline" size="sm" onClick={onRetry}>
            Try again
          </Button>
        </Stack>
      </figure>
    </Stack>
  );
}

// ── ModelsTab ─────────────────────────────────────────────────────────────────

export function ModelsTab() {
  const { models, status, favourites, toggleFavourite, refresh } = useModelsContext();

  const [selectedModel, setSelectedModel] = useState<ModelRow | null>(null);

  const favouriteRows = useMemo(
    () => models.filter((m) => favourites.has(m.id)),
    [models, favourites],
  );

  const columns = useMemo<DataTableColumn<ModelRow>[]>(
    () => [
      {
        id: 'favourite',
        header: '',
        locked: true,
        width: '2.5rem',
        cell: (row) => {
          const isFav = favourites.has(row.id);
          return (
            <Button
              variant="ghost"
              size="icon-sm"
              dim={!isFav}
              aria-label={isFav ? 'Remove from favourites' : 'Add to favourites'}
              onClick={(e) => {
                e.stopPropagation();
                toggleFavourite(row.id);
              }}
            >
              <Icon name="heart" size="2xs" color={isFav ? 'rose' : 'muted'} filled={isFav} />
            </Button>
          );
        },
      },
      {
        id: 'name',
        header: 'Model',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text
              size="caption"
              color="subtle"
              transform="uppercase"
              style={{ marginBottom: '0.125rem' }}
            >
              {row.family}
            </Text>
            <Text size="supporting" weight="emphasis" mono>
              {row.id}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'displayName',
        header: 'Name',
        defaultVisible: false,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.name}
          </Text>
        ),
      },
      {
        id: 'status',
        header: 'Availability',
        filter: {
          mode: 'client',
          options: STATUS_OPTIONS,
          filterFn: (row, selected) => selected.has(row.status),
        },
        cell: (row) => (
          <Stack direction="horizontal" align="center" gap="tight">
            <AvailabilityDot status={row.status} />
            <Text size="supporting" color="muted">
              {row.status}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'capabilities',
        header: 'Capabilities',
        cell: (row) => <Chips values={row.capabilities} />,
      },
      {
        id: 'dialects',
        header: 'Dialects',
        cell: (row) => <Chips values={row.dialects} />,
      },
      {
        id: 'inputModalities',
        header: 'Input',
        defaultVisible: false,
        cell: (row) => <Chips values={row.input_modalities} />,
      },
      {
        id: 'outputModalities',
        header: 'Output',
        defaultVisible: false,
        cell: (row) => <Chips values={row.output_modalities} />,
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '2.5rem',
        cell: (row) => (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="View model details"
            onClick={(e) => {
              e.stopPropagation();
              setSelectedModel(row);
            }}
          >
            <Icon name="more-vertical" size="2xs" />
          </Button>
        ),
      },
    ],
    [favourites, toggleFavourite],
  );

  return (
    <Stack gap="comfortable">
      <ModelCardGrid
        models={favouriteRows}
        onRemove={toggleFavourite}
        onSelect={setSelectedModel}
      />

      <DataTable
        data={models}
        columns={columns}
        getRowKey={(row) => row.id}
        noun="models"
        searchPlaceholder="Search models…"
        searchFn={modelMatchesSearch}
        defaultSortId="name"
        defaultSortDir="desc"
        error={status === 'error' ? <LoadError onRetry={() => void refresh()} /> : undefined}
        emptyMessage="No models available."
        filteredEmptyMessage="No models match your filters."
        onRowClick={setSelectedModel}
      />

      <ModelDetailModal model={selectedModel} onClose={() => setSelectedModel(null)} />
    </Stack>
  );
}
