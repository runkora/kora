'use client';

import { useState, useMemo } from 'react';
import {
  Button,
  Icon,
  SegmentedControl,
  Stack,
  Text,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@orbit/terra';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { BadgeList } from './badge-list';
import { fmtCost } from '@orbit/utils';
import type { ModelObjectPricing, ModelRow } from '../_types';
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

// The family is already printed above the id, so the id line shows only what
// tells models in the same family apart, which truncation (from the end) would
// otherwise cut off.
function withoutFamily(row: ModelRow): string {
  const prefix = `${row.family}/`;
  return row.id.startsWith(prefix) ? row.id.slice(prefix.length) : row.id;
}

// ── Non-token dimension rows ──────────────────────────────────────────────────
// Each entry: [inputKey, outputKey, inputLabel, outputLabel]

type NonTokenDimension = {
  inputKey: keyof ModelObjectPricing;
  outputKey: keyof ModelObjectPricing;
  inputLabel: string;
  outputLabel: string;
};

const NON_TOKEN_DIMENSIONS: NonTokenDimension[] = [
  {
    inputKey: 'image_input_cost',
    outputKey: 'image_output_cost',
    inputLabel: '/image in',
    outputLabel: '/image out',
  },
  {
    inputKey: 'request_input_cost',
    outputKey: 'request_output_cost',
    inputLabel: '/req in',
    outputLabel: '/req out',
  },
  {
    inputKey: 'second_input_cost',
    outputKey: 'second_output_cost',
    inputLabel: '/sec in',
    outputLabel: '/sec out',
  },
  {
    inputKey: 'audio_input_cost_per_1m',
    outputKey: 'audio_output_cost_per_1m',
    inputLabel: '/1M audio in',
    outputLabel: '/1M audio out',
  },
];

function hasAnyNonTokenPricing(pricing: ModelObjectPricing): boolean {
  return NON_TOKEN_DIMENSIONS.some(
    (d) =>
      ((pricing[d.inputKey] as number | undefined) ?? 0) > 0 ||
      ((pricing[d.outputKey] as number | undefined) ?? 0) > 0,
  );
}

// ── Effective price cell ──────────────────────────────────────────────────────

function EffectivePriceCell({ pricing }: { pricing?: ModelObjectPricing }) {
  if (!pricing) {
    return (
      <Stack direction="horizontal" align="center" gap="tight">
        <Icon name="alert-circle" size="2xs" color="status-degraded" />
        <Text size="caption" color="muted">
          Unknown
        </Text>
      </Stack>
    );
  }

  if (pricing.non_token_only) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="muted" size="icon-sm" aria-label="Non-token pricing — see model details">
            <Icon name="info" size="2xs" color="muted" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Non-token pricing — see model details</TooltipContent>
      </Tooltip>
    );
  }

  const nonTokenPricing = hasAnyNonTokenPricing(pricing);

  return (
    <Stack gap="none">
      <Text size="supporting" mono color="foreground">
        {fmtCost(pricing.effective_input_cost_per_1m ?? 0)}/1M
      </Text>
      <Text size="caption" color="muted" mono>
        out: {fmtCost(pricing.effective_output_cost_per_1m ?? 0)}/1M
      </Text>
      {nonTokenPricing && (
        <Text size="caption" color="muted">
          + other dims
        </Text>
      )}
    </Stack>
  );
}

// ── Load error ────────────────────────────────────────────────────────────────

function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <Stack align="center" justify="center" paddingBlock="spacious">
      <Stack gap="snug" align="center">
        <Text size="supporting" color="muted">
          Couldn&apos;t load models. The inference gateway may be unavailable.
        </Text>
        <Button variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      </Stack>
    </Stack>
  );
}

// ── ModelsTab ─────────────────────────────────────────────────────────────────

export function ModelsTab() {
  const { models, status, favourites, toggleFavourite, refresh } = useModelsContext();

  const [selectedModel, setSelectedModel] = useState<ModelRow | null>(null);
  const [availabilityFilter, setAvailabilityFilter] = useState<'all' | 'available' | 'unavailable'>(
    'all',
  );

  const availabilityCounts = useMemo(
    () => ({
      all: models.length,
      available: models.filter((m) => m.status === 'available').length,
      unavailable: models.filter((m) => m.status === 'unavailable').length,
    }),
    [models],
  );

  const filteredModels = useMemo(() => {
    if (availabilityFilter === 'all') return models;
    return models.filter((m) => m.status === availabilityFilter);
  }, [models, availabilityFilter]);

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
        gridWidth: '2.5rem',
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
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '12rem',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text size="caption" color="subtle" transform="uppercase">
              {row.family}
            </Text>
            <Text size="supporting" weight="emphasis" mono>
              {withoutFamily(row)}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'displayName',
        header: 'Name',
        gridWidth: 'minmax(0, 1.2fr)',
        minWidth: '8rem',
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
        gridWidth: '8rem',
        minWidth: '6.5rem',
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
        gridWidth: 'minmax(0, 1.5fr)',
        minWidth: '8rem',
        cell: (row) => <BadgeList values={row.capabilities} maxRows={2} />,
      },
      {
        id: 'effectivePrice',
        header: 'Effective Price',
        gridWidth: '9rem',
        minWidth: '8rem',
        headerTooltip:
          'The per-1M token price for a standard text request, after any platform overrides are applied.',
        sortable: true,
        sortAccessor: (row) =>
          row.pricing
            ? (row.pricing.effective_input_cost_per_1m ?? 0) +
              (row.pricing.effective_output_cost_per_1m ?? 0)
            : -1,
        cell: (row) => <EffectivePriceCell pricing={row.pricing} />,
      },
      {
        id: 'dialects',
        header: 'Dialects',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '6rem',
        cell: (row) => <BadgeList values={row.dialects} maxRows={2} />,
      },
      {
        id: 'inputModalities',
        header: 'Input',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '6rem',
        defaultVisible: false,
        cell: (row) => <BadgeList values={row.input_modalities} maxRows={2} />,
      },
      {
        id: 'outputModalities',
        header: 'Output',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '6rem',
        defaultVisible: false,
        cell: (row) => <BadgeList values={row.output_modalities} maxRows={2} />,
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '2.5rem',
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

      <SmartTable
        tableId="gatewayModels"
        data={filteredModels}
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
        quickFilters={
          <SegmentedControl
            aria-label="Filter models by availability"
            value={availabilityFilter}
            onChange={(v) => setAvailabilityFilter(v as typeof availabilityFilter)}
            options={[
              { value: 'all', label: 'All', count: availabilityCounts.all },
              { value: 'available', label: 'Available', count: availabilityCounts.available },
              { value: 'unavailable', label: 'Unavailable', count: availabilityCounts.unavailable },
            ]}
          />
        }
      />

      <ModelDetailModal model={selectedModel} onClose={() => setSelectedModel(null)} />
    </Stack>
  );
}
