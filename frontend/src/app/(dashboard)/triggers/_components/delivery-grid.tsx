'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the grid renders delivery statuses, fan-out and
 * dispatch latency, Triggers vocabulary that promotion would spread into
 * `src/components/`.
 *
 * The delivery list: one row per arrival, and what the platform did with it.
 */

import { useMemo } from 'react';
import { Badge, DataTable, Stack, StatusDot, Text } from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import {
  deliveryDotStatus,
  deliveryDuration,
  deliverySummary,
  deliveryTriggerIdentity,
  formatTimestampUtc,
  relativeTime,
  shortId,
} from '../_types';
import type { DeliveryRow } from '../_types';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '../_types/pagination';

// ── View-model type ───────────────────────────────────────────────────────────

/**
 * DeliveryRow extended with display-specific derivations pre-computed once in
 * useMemo. Cells reference shared computed fields (dot, identity, summary,
 * arrivedVia) — the view model ensures they are computed once, not once per
 * cell.
 */
interface DeliveryDisplayRow extends DeliveryRow {
  dot: ReturnType<typeof deliveryDotStatus>;
  identity: ReturnType<typeof deliveryTriggerIdentity>;
  summary: ReturnType<typeof deliverySummary>;
  arrivedVia: string;
}

/**
 * Where this arrival came from, one line under the trigger.
 *
 * A schedule and a manual fire have no source, and naming their kind is more
 * use than an empty line holding the row open.
 */
function computeArrivedVia(d: DeliveryRow, sourceName: (id: string) => string): string {
  if (d.kind === 'scheduled') return 'schedule';
  if (d.kind === 'manual') return 'manual fire';
  return d.sourceId === '' ? 'webhook' : sourceName(d.sourceId);
}

// ── Column definitions ────────────────────────────────────────────────────────

function makeColumns(sourceName: (id: string) => string): DataTableColumn<DeliveryDisplayRow>[] {
  // sourceName is captured in the closure but unused in cells — arrivedVia is
  // pre-computed on the view model. It is referenced here to satisfy the
  // dependency on the memoised column array.
  void sourceName;

  return [
    {
      id: 'status',
      header: 'Status',
      gridWidth: '118px',
      minWidth: '6rem',
      locked: true,
      cell: (row) => (
        <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
          <StatusDot status={row.dot} size="sm" />
          <Text
            size="supporting"
            weight="emphasis"
            color={row.dot === 'error' ? 'status-error' : 'muted'}
            truncate
          >
            {row.status}
          </Text>
        </Stack>
      ),
    },
    {
      id: 'trigger',
      header: 'Trigger',
      gridWidth: 'minmax(0, 1.1fr)',
      minWidth: '9rem',
      cell: (row) => (
        <Stack gap="tight" minWidth="none">
          {row.identity.kind === 'none' ? (
            <Text size="supporting" color="subtle" truncate>
              No trigger matched
            </Text>
          ) : (
            <Text
              mono
              size="supporting"
              weight="emphasis"
              color={row.identity.kind === 'id' ? 'muted' : undefined}
              truncate
              title={row.triggerId ?? undefined}
            >
              {row.identity.label}
            </Text>
          )}
          <Text size="caption" color="subtle" truncate>
            {row.arrivedVia}
          </Text>
        </Stack>
      ),
    },
    {
      id: 'event',
      header: 'Event',
      gridWidth: '148px',
      minWidth: '8rem',
      cell: (row) => (
        <Stack direction="horizontal" minWidth="none">
          {row.eventType !== '' && (
            <Badge variant="elevated" size="sm" mono>
              {row.eventType}
            </Badge>
          )}
        </Stack>
      ),
    },
    {
      id: 'received',
      header: 'Received',
      gridWidth: '96px',
      minWidth: '5.5rem',
      cell: (row) => (
        <Text size="supporting" color="muted" title={formatTimestampUtc(row.receivedAt)}>
          {relativeTime(row.receivedAt)}
        </Text>
      ),
    },
    {
      id: 'duration',
      header: 'Duration',
      gridWidth: '76px',
      minWidth: '4.5rem',
      align: 'right',
      cell: (row) => (
        <Text mono tabular size="supporting" color="muted" align="end">
          {deliveryDuration(row)}
        </Text>
      ),
    },
    {
      id: 'detail',
      header: 'Detail',
      gridWidth: 'minmax(0, 1.3fr)',
      minWidth: '10rem',
      cell: (row) => (
        <Stack direction="horizontal" minWidth="none">
          {row.summary.kind !== 'none' && (
            <Text
              size="caption"
              color="muted"
              mono={row.summary.kind === 'runId' || row.summary.kind === 'excerpt'}
              truncate
              title={row.summary.kind === 'error' ? (row.error ?? undefined) : undefined}
            >
              {row.summary.text}
            </Text>
          )}
        </Stack>
      ),
    },
  ];
}

// ── Search ────────────────────────────────────────────────────────────────────

function deliveryMatchesSearch(row: DeliveryDisplayRow, q: string): boolean {
  return (
    row.id.toLowerCase().includes(q) ||
    row.eventType.toLowerCase().includes(q) ||
    row.status.toLowerCase().includes(q) ||
    (row.identity.kind !== 'none' && row.identity.label.toLowerCase().includes(q)) ||
    row.arrivedVia.toLowerCase().includes(q)
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

interface DeliveryGridProps {
  deliveries: DeliveryRow[];
  /** Resolves a webhook source id to its name for the row subtitle. */
  sourceName: (id: string) => string;
  onOpen: (d: DeliveryRow) => void;
  /** Shown inside the frame when there are no rows. */
  emptyMessage: string;
  /** Status filter or similar, rendered in the toolbar's right slot. */
  quickFilters?: React.ReactNode;
  /**
   * Changes whenever the filters upstream change. A new value remounts the
   * DataTable, resetting all internal state including the current page.
   */
  resetKey?: string;
}

export function DeliveryGrid({
  deliveries,
  sourceName,
  onOpen,
  emptyMessage,
  quickFilters,
  resetKey = '',
}: DeliveryGridProps) {
  // Pre-compute per-row derived values once. Multiple cells reference the same
  // computed fields — the view model ensures one computation, not one per cell.
  const rows = useMemo<DeliveryDisplayRow[]>(
    () =>
      deliveries.map((d) => ({
        ...d,
        dot: deliveryDotStatus(d.status),
        identity: deliveryTriggerIdentity(d),
        summary: deliverySummary(d),
        arrivedVia: computeArrivedVia(d, sourceName),
      })),
    [deliveries, sourceName],
  );

  const columns = useMemo(() => makeColumns(sourceName), [sourceName]);

  return (
    <DataTable
      key={resetKey}
      data={rows}
      columns={columns}
      getRowKey={(row) => row.id}
      noun="deliveries"
      emptyMessage={emptyMessage}
      onRowClick={onOpen}
      rowLabel={(row) => `View payload and outcome for delivery ${shortId(row.id)}`}
      searchFn={deliveryMatchesSearch}
      searchPlaceholder="Search deliveries…"
      quickFilters={quickFilters}
      pagination={{ pageSizeOptions: PAGE_SIZES, defaultPageSize: DEFAULT_PAGE_SIZE }}
    />
  );
}
