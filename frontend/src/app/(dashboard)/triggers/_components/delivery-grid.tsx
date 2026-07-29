/** @tier 3 — triggers-scoped. The delivery list: one row per arrival, and what
 * the platform did with it. */

'use client';

import { useState } from 'react';
import {
  Badge,
  DataGrid,
  DataGridEmpty,
  DataGridHeader,
  DataGridPagination,
  DataGridRow,
  DataGridToolbar,
  Icon,
  Stack,
  StatusDot,
  Text,
} from '@orbit/terra';
import {
  deliveryDotStatus,
  deliveryDuration,
  deliverySummary,
  deliveryTriggerIdentity,
  relativeTime,
  shortId,
} from '../_types';
import type { DeliveryRow } from '../_types';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES, pageWindow } from '../_types/pagination';

interface DeliveryGridProps {
  deliveries: DeliveryRow[];
  /** Resolves a webhook source id to its name for the row subtitle. */
  sourceName: (id: string) => string;
  onOpen: (d: DeliveryRow) => void;
  /** Shown inside the frame when there are no rows. */
  emptyMessage: string;
  /** Filters and search, rendered inside the grid's frame above the header. */
  toolbar?: React.ReactNode;
  /** Changes whenever the filters upstream change; returns the list to page one. */
  resetKey?: string;
}

/**
 * Column widths, fixed so every row lines up down the page. The trigger is the
 * identity a reader scans for and takes the larger share; the detail takes the
 * rest, because it is the only cell whose content has no natural length.
 */
const COLUMNS = '118px minmax(0, 1.1fr) 148px 96px 76px minmax(0, 1.3fr)';

/** The absolute time, for the row's title attribute. */
function absoluteTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

/**
 * Where this arrival came from, one line under the trigger.
 *
 * A schedule and a manual fire have no source, and naming their kind is more
 * use than an empty line holding the row open.
 */
function arrivedVia(d: DeliveryRow, sourceName: (id: string) => string): string {
  if (d.kind === 'scheduled') return 'schedule';
  if (d.kind === 'manual') return 'manual fire';
  return d.sourceId === '' ? 'webhook' : sourceName(d.sourceId);
}

export function DeliveryGrid({
  deliveries,
  sourceName,
  onOpen,
  emptyMessage,
  toolbar,
  resetKey = '',
}: DeliveryGridProps) {
  const [paging, setPaging] = useState({ key: resetKey, page: 0, size: DEFAULT_PAGE_SIZE });
  // Adjusted during render rather than in an effect, so a filter change never
  // renders one frame of the wrong page first. The reset is written back
  // instead of shadowed: leaving the old page in state means returning to a
  // previous filter resurrects the page it was left on, which reads as the
  // list having jumped on its own.
  if (paging.key !== resetKey) {
    setPaging({ key: resetKey, page: 0, size: paging.size });
  }
  const requested = paging.key === resetKey ? paging.page : 0;
  const win = pageWindow(deliveries.length, paging.size, requested);
  const visible = deliveries.slice(win.start, win.end);

  return (
    <DataGrid columns={COLUMNS} aria-label="Deliveries">
      {toolbar !== undefined && <DataGridToolbar>{toolbar}</DataGridToolbar>}
      <DataGridHeader
        cells={[
          'Status',
          'Trigger',
          'Event',
          'Received',
          { label: 'Duration', align: 'end' },
          'Detail',
        ]}
      />

      {deliveries.length === 0 ? (
        <DataGridEmpty>
          <Icon name="activity" size="sm" color="subtle" />
          <Text size="supporting" color="subtle">
            {emptyMessage}
          </Text>
        </DataGridEmpty>
      ) : (
        visible.map((d) => {
          const dot = deliveryDotStatus(d.status);
          const identity = deliveryTriggerIdentity(d);
          const summary = deliverySummary(d);

          return (
            <DataGridRow
              key={d.id}
              onClick={() => onOpen(d)}
              rowLabel={`View payload and outcome for delivery ${shortId(d.id)}`}
            >
              {/* Status: a dot and the word, so the outcome reads at a glance
                  without a filled pill competing with every other cell. */}
              <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
                <StatusDot status={dot} size="sm" />
                <Text
                  size="supporting"
                  weight="emphasis"
                  color={dot === 'error' ? 'status-error' : 'muted'}
                  truncate
                >
                  {d.status}
                </Text>
              </Stack>

              {/* Trigger: the identity a reader scans for, with where the
                  arrival came from underneath it. */}
              <Stack gap="tight" minWidth="none">
                {identity.kind === 'none' ? (
                  <Text size="supporting" color="subtle" truncate>
                    No trigger matched
                  </Text>
                ) : (
                  <Text
                    mono
                    size="supporting"
                    weight="emphasis"
                    color={identity.kind === 'id' ? 'muted' : undefined}
                    truncate
                    title={d.triggerId ?? undefined}
                  >
                    {identity.label}
                  </Text>
                )}
                <Text size="caption" color="subtle" truncate>
                  {arrivedVia(d, sourceName)}
                </Text>
              </Stack>

              {/* Event: only when the arrival named one. A placeholder in every
                  other row was the column's loudest content. */}
              <Stack direction="horizontal" minWidth="none">
                {d.eventType !== '' && (
                  <Badge variant="elevated" size="sm" mono>
                    {d.eventType}
                  </Badge>
                )}
              </Stack>

              {/* Received: relative, one line, with the exact time on hover. */}
              <Text size="supporting" color="muted" title={absoluteTime(d.receivedAt)}>
                {relativeTime(d.receivedAt)}
              </Text>

              <Stack direction="horizontal" justify="end" minWidth="none">
                <Text mono tabular size="supporting" color="muted">
                  {deliveryDuration(d)}
                </Text>
              </Stack>

              {/* Detail: one line. The full failure chain and the payload are a
                  click away in the panel, which is where they are readable. */}
              <Stack direction="horizontal" minWidth="none">
                {summary.kind !== 'none' && (
                  <Text
                    size="caption"
                    color="muted"
                    mono={summary.kind === 'runId' || summary.kind === 'excerpt'}
                    truncate
                    title={summary.kind === 'error' ? (d.error ?? undefined) : undefined}
                  >
                    {summary.text}
                  </Text>
                )}
              </Stack>
            </DataGridRow>
          );
        })
      )}

      {/* Inside the frame, like the toolbar: the controls that change what the
          grid shows belong to the grid. */}
      {deliveries.length > 0 && (
        <DataGridPagination
          from={win.from}
          to={win.to}
          total={win.total}
          page={win.page}
          pageCount={win.pageCount}
          pageSize={paging.size}
          pageSizeOptions={PAGE_SIZES}
          noun="deliveries"
          onPageChange={(page) => setPaging({ key: resetKey, page, size: paging.size })}
          // A new page size makes the old page index meaningless, so it
          // returns to the first page rather than landing mid-list.
          onPageSizeChange={(size) => setPaging({ key: resetKey, page: 0, size })}
        />
      )}
    </DataGrid>
  );
}
