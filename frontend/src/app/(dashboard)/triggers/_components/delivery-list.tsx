'use client';

/**
 * @tier 3 — triggers-scoped. One trigger's delivery history as a table, newest first: quick
 * filters and search in the toolbar, each row opening in place onto its payload and outcome.
 * The rows scroll inside the table, so a long history does not lengthen the modal.
 */

import { useMemo } from 'react';
import {
  Button,
  DataTable,
  EmptyState,
  SectionLabel,
  SegmentedControl,
  Skeleton,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { deliveryDotStatus, dispatchDuration } from '../_types';
import type { DeliveryRow } from '../_types';
import type { StatsWindow, TriggerStatsRow } from '../_types/stats';
import { DeliveryDetail } from './delivery-detail';
import { NextPageError } from './next-page-error';
import type { RedeliverAction } from './delivery-detail';
import type { RoutingAgentName } from './delivery-routing';

/**
 * A quick filter: `all`, or the trigger status the API filters this trigger's deliveries on.
 * Every outcome counts under exactly one, so the counts add up to All.
 */
export type DeliveryFilter =
  | 'all'
  | 'started'
  | 'failed'
  | 'filtered'
  | 'skipped'
  | 'superseded'
  | 'duplicate'
  | 'missed'
  | 'verified';

const FILTERS: { value: DeliveryFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'started', label: 'Started' },
  { value: 'failed', label: 'Failed' },
  { value: 'filtered', label: 'Filtered' },
  { value: 'skipped', label: 'Skipped' },
  { value: 'superseded', label: 'Superseded' },
  { value: 'duplicate', label: 'Duplicate' },
  { value: 'missed', label: 'Missed' },
  { value: 'verified', label: 'Verified' },
];

/** How tall the table grows before its rows scroll inside it. */
const TABLE_MAX_HEIGHT = '26rem';

interface DeliveryListProps {
  /** Deliveries that reached this trigger and pass the filter, newest first. */
  deliveries: DeliveryRow[];
  filter: DeliveryFilter;
  onFilterChange: (filter: DeliveryFilter) => void;
  /** The trigger's stats over the window, which the filter counts show. */
  stats: TriggerStatsRow;
  /** The window the counts and the list cover, e.g. `24h`. */
  window: StatsWindow;
  /** The first page has not landed: a placeholder shows in place of the table. */
  loading?: boolean;
  /** There are older deliveries in the window; the pager fetches them. */
  hasMore?: boolean;
  /** Why the last fetch failed, if it did. */
  error?: string | null;
  onLoadOlder?: () => void;
  onRetry?: () => void;
  /** The trigger whose outcome to read out of each delivery's fan-out. */
  triggerId: string;
  /** Which delivery's payload is open, if any. */
  expandedId: string | null;
  onToggleExpanded: (id: string | null) => void;
  /** Replays a delivery. */
  onRedeliver?: RedeliverAction;
  /** False when the caller may not replay; Redeliver shows disabled with the reason. */
  redeliverAllowed?: boolean;
  /** Names the agents a routing decision mentions. */
  agents?: RoutingAgentName[];
  /** Where a run id links to. Omit when the run's agent is not known. */
  runHref?: (runId: string) => string;
  /** What the empty state says deliveries come from, e.g. `acme-github`. */
  sourceName?: string;
  /** The event the trigger listens for, for the empty state's sentence. */
  eventType?: string;
}

/** A delivery with what each cell reads, worked out once. */
interface DeliveryTableRow {
  d: DeliveryRow;
  status: string;
  tone: ReturnType<typeof deliveryDotStatus>;
  duration: string;
}

/** Compact relative time, so a column of these stays scannable. */
function shortAgo(iso: string): string {
  const ms = Date.now() - Date.parse(iso);
  if (!Number.isFinite(ms)) return '—';
  const mins = Math.floor(ms / 60000);
  if (mins < 1) return 'now';
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  return hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`;
}

/** What the empty state says deliveries come from. */
function emptyDescription(sourceName?: string, eventType?: string): string {
  return sourceName !== undefined && eventType !== undefined && eventType !== ''
    ? `Deliveries appear here when ${sourceName} sends a ${eventType} event.`
    : 'Deliveries appear here the moment this trigger fires.';
}

export function DeliveryList({
  deliveries,
  filter,
  onFilterChange,
  stats,
  window: range,
  loading = false,
  hasMore = false,
  error = null,
  onLoadOlder,
  onRetry,
  triggerId,
  expandedId,
  onToggleExpanded,
  onRedeliver,
  redeliverAllowed = true,
  agents,
  runHref,
  sourceName,
  eventType,
}: DeliveryListProps) {
  // The delivery's own status is the arrival-level verdict; this trigger's row
  // in the fan-out is the one that answers "and what happened for me". Prefer
  // the latter and fall back to the former.
  const rows = useMemo<DeliveryTableRow[]>(
    () =>
      deliveries.map((d) => {
        const outcome = d.runs.find((r) => r.triggerId === triggerId);
        const status = outcome?.status ?? d.status;
        return {
          d,
          status,
          tone: deliveryDotStatus(status),
          duration: dispatchDuration(d.receivedAt, outcome?.createdAt ?? null),
        };
      }),
    [deliveries, triggerId],
  );
  const counts = (f: DeliveryFilter) =>
    f === 'all' ? stats.deliveries : (stats.statusCounts[f] ?? 0);

  const columns = useMemo<DataTableColumn<DeliveryTableRow>[]>(
    () => [
      {
        id: 'delivery',
        header: 'Delivery',
        gridWidth: 'minmax(0, 1.4fr)',
        minWidth: '8rem',
        locked: true,
        cell: ({ d, tone }) => (
          <Stack direction="horizontal" align="center" gap="cozy" minWidth="none">
            <StatusDot status={tone} size="sm" />
            <Text mono size="caption" weight="strong" truncate>
              {d.id}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'event',
        header: 'Event',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '6rem',
        cell: ({ d }) => (
          <Text mono size="caption" color="muted" truncate>
            {d.eventType || d.kind}
          </Text>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        gridWidth: '96px',
        minWidth: '6rem',
        cell: ({ status, tone }) => (
          <Text
            mono
            size="caption"
            weight="strong"
            color={tone === 'error' ? 'status-error' : undefined}
          >
            {status}
          </Text>
        ),
      },
      {
        id: 'time',
        header: 'Time',
        gridWidth: '72px',
        minWidth: '4rem',
        align: 'right',
        cell: ({ duration }) => (
          <Text mono size="caption" weight="strong" align="end">
            {duration}
          </Text>
        ),
      },
      {
        id: 'age',
        header: 'Age',
        gridWidth: '56px',
        minWidth: '3.5rem',
        align: 'right',
        cell: ({ d }) => (
          <Text size="caption" color="muted" align="end">
            {shortAgo(d.receivedAt)}
          </Text>
        ),
      },
    ],
    [],
  );

  // A filtered list keeps its toolbar while it loads or comes back empty, so the filter can be changed back.
  if (loading && filter === 'all') {
    return (
      <Stack gap="snug">
        <SectionLabel>Deliveries</SectionLabel>
        <Stack gap="cozy" role="status" aria-label="Loading deliveries" aria-busy="true">
          <Skeleton shape="block" width="full" size="md" />
          <Skeleton shape="block" width="full" size="lg" />
          <Skeleton shape="block" width="full" size="lg" />
          <Skeleton shape="block" width="full" size="lg" />
        </Stack>
      </Stack>
    );
  }

  if (error !== null && rows.length === 0) {
    return (
      <Stack gap="snug">
        <SectionLabel>Deliveries</SectionLabel>
        <EmptyState
          icon="triangle-alert"
          title="Could not load deliveries"
          description={error}
          action={
            onRetry !== undefined ? (
              <Button size="xs" variant="ghost" onClick={onRetry}>
                Try again
              </Button>
            ) : undefined
          }
        />
      </Stack>
    );
  }

  if (rows.length === 0 && !hasMore && filter === 'all') {
    return (
      <Stack gap="snug">
        <SectionLabel>Deliveries</SectionLabel>
        <EmptyState
          icon="clock"
          title={`No deliveries in the last ${range}`}
          description={emptyDescription(sourceName, eventType)}
        />
      </Stack>
    );
  }

  return (
    <Stack gap="snug">
      <SectionLabel>Deliveries</SectionLabel>
      <DataTable
        key={filter}
        data={rows}
        columns={columns}
        getRowKey={({ d }) => d.id}
        noun="deliveries"
        columnToggle={false}
        maxHeight={TABLE_MAX_HEIGHT}
        emptyMessage={
          loading ? 'Loading deliveries…' : `No ${filter} deliveries in the last ${range}.`
        }
        searchPlaceholder="Search deliveries…"
        searchFn={({ d, status }, q) =>
          d.id.toLowerCase().includes(q) ||
          (d.eventType || d.kind).toLowerCase().includes(q) ||
          status.toLowerCase().includes(q)
        }
        rowLabel={({ d }) => `Show payload for delivery ${d.id}`}
        quickFilters={
          <Stack direction="horizontal" align="center" gap="cozy">
            <SegmentedControl
              aria-label={`Filter deliveries by outcome, with counts for the last ${range}`}
              value={filter}
              onChange={(v) => onFilterChange(v as DeliveryFilter)}
              options={FILTERS.map((f) => ({ ...f, count: counts(f.value) }))}
            />
            <Text size="caption" color="muted">
              Last {range}
            </Text>
          </Stack>
        }
        expandedKeys={new Set(expandedId === null ? [] : [expandedId])}
        onExpandedChange={(key, expanded) => onToggleExpanded(expanded ? key : null)}
        renderExpanded={({ d }) => (
          <DeliveryDetail
            delivery={d}
            triggerId={triggerId}
            onRedeliver={onRedeliver}
            redeliverAllowed={redeliverAllowed}
            agents={agents}
            runHref={runHref}
          />
        )}
        pagination={{
          pageSizeOptions: [10, 25, 50],
          defaultPageSize: 10,
          hasMore,
          onLoadMore: onLoadOlder,
        }}
      />
      <NextPageError error={error} rows={rows.length} />
    </Stack>
  );
}
