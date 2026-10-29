'use client';

/**
 * @tier 3 — triggers-scoped. One trigger's delivery history as a table, newest first: quick
 * filters and search in the toolbar, each row opening in place onto its payload and outcome.
 * The rows scroll inside the table, so a long history does not lengthen the modal.
 */

import { useMemo, useState } from 'react';
import {
  Button,
  DataTable,
  EmptyState,
  Icon,
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
import { DeliveryDetail } from './delivery-detail';
import type { RoutingAgentName } from './delivery-routing';

type Filter = 'all' | 'dispatched' | 'failed' | 'skipped' | 'superseded' | 'duplicate';

const FILTERS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'dispatched', label: 'Dispatched' },
  { value: 'failed', label: 'Failed' },
  { value: 'skipped', label: 'Skipped' },
  { value: 'superseded', label: 'Superseded' },
  { value: 'duplicate', label: 'Duplicate' },
];

/** Whether a delivery's status (this trigger's outcome, else the delivery's own) is in a filter. */
function inFilter(filter: Filter, status: string): boolean {
  if (filter === 'all') return true;
  if (filter === 'failed') return deliveryDotStatus(status) === 'error';
  return status === filter;
}

/** How tall the table grows before its rows scroll inside it. */
const TABLE_MAX_HEIGHT = '26rem';

interface DeliveryListProps {
  /** Deliveries that reached this trigger, newest first. */
  deliveries: DeliveryRow[];
  /** The first page has not landed: a placeholder shows in place of the table. */
  loading?: boolean;
  /** An older page is being fetched. */
  loadingMore?: boolean;
  /** There may be older deliveries; "Load older" shows. */
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
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
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

export function DeliveryList({
  deliveries,
  loading = false,
  loadingMore = false,
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
  const [filter, setFilter] = useState<Filter>('all');

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
  const visible = useMemo(() => rows.filter((r) => inFilter(filter, r.status)), [rows, filter]);
  const counts = useMemo(
    () =>
      Object.fromEntries(
        FILTERS.map((f) => [f.value, rows.filter((r) => inFilter(f.value, r.status)).length]),
      ),
    [rows],
  );

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

  if (loading) {
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

  if (rows.length === 0 && !hasMore) {
    return (
      <Stack gap="snug">
        <SectionLabel>Deliveries</SectionLabel>
        <EmptyState
          icon="clock"
          title="No deliveries yet"
          description={
            sourceName !== undefined && eventType !== undefined && eventType !== ''
              ? `Deliveries appear here when ${sourceName} sends a ${eventType} event.`
              : 'Deliveries appear here the moment this trigger fires.'
          }
        />
      </Stack>
    );
  }

  return (
    <Stack gap="snug">
      <SectionLabel>Deliveries</SectionLabel>
      <DataTable
        key={filter}
        data={visible}
        columns={columns}
        getRowKey={({ d }) => d.id}
        noun="deliveries"
        columnToggle={false}
        maxHeight={TABLE_MAX_HEIGHT}
        emptyMessage={
          hasMore
            ? 'None of the deliveries loaded so far are for this trigger. Load older to keep looking.'
            : `No ${filter} deliveries.`
        }
        searchPlaceholder="Search deliveries…"
        searchFn={({ d, status }, q) =>
          d.id.toLowerCase().includes(q) ||
          (d.eventType || d.kind).toLowerCase().includes(q) ||
          status.toLowerCase().includes(q)
        }
        rowLabel={({ d }) => `Show payload for delivery ${d.id}`}
        quickFilters={
          <SegmentedControl
            aria-label="Filter deliveries by outcome"
            value={filter}
            onChange={(v) => setFilter(v as Filter)}
            options={FILTERS.map((f) => ({ ...f, count: counts[f.value] ?? 0 }))}
          />
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
        pagination={{ pageSizeOptions: [10, 25, 50], defaultPageSize: 10 }}
      />
      {(hasMore || error !== null) && (
        <Stack direction="horizontal" align="center" gap="cozy">
          {hasMore && (
            <Button size="xs" variant="secondary" disabled={loadingMore} onClick={onLoadOlder}>
              {loadingMore ? (
                <>
                  <Icon name="loader" size="2xs" />
                  Loading…
                </>
              ) : (
                'Load older'
              )}
            </Button>
          )}
          {error !== null && (
            <Text size="caption" color="status-error">
              Could not load older deliveries: {error}
            </Text>
          )}
        </Stack>
      )}
    </Stack>
  );
}
