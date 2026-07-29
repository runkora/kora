/** @tier 3 — triggers-scoped. The trigger list: one row per way work starts. */

'use client';

import { useState } from 'react';
import {
  Badge,
  DataGrid,
  DataGridEmpty,
  DataGridPagination,
  DataGridHeader,
  DataGridRow,
  DataGridToolbar,
  Icon,
  IconTile,
  Sparkline,
  Stack,
  Text,
  Toggle,
} from '@orbit/terra';
import { KIND_ICON, relativeTime } from '../_types';
import type { DeliveryRow, TriggerRow } from '../_types';
import { lastFired, recentFailures, sparkline } from '../_types/fleet';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES, pageWindow } from '../_types/pagination';

interface TriggerListProps {
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  /** Resolves a webhook source id to its name for the row subtitle. */
  sourceName: (id: string) => string;
  onOpen: (t: TriggerRow) => void;
  onToggle: (t: TriggerRow) => void;
  /** Shown inside the frame when there are no rows. */
  emptyMessage: string;
  /**
   * Filters and search, rendered inside the grid's frame above the header.
   * Keeping them in the frame means a filter that matches nothing cannot take
   * its own escape hatch off screen.
   */
  toolbar?: React.ReactNode;
  /**
   * Changes whenever the filters upstream change. A new value returns the list
   * to page one, because a page index means nothing once the rows behind it
   * have been replaced.
   */
  resetKey?: string;
}

/**
 * Column widths, fixed so every row lines up down the page. Trigger takes the
 * remainder because names and sources vary most; the rest are sized to their
 * longest realistic content.
 */
const COLUMNS = '1fr 168px 148px 96px 118px 74px';

/** What this trigger fires on, in its own vocabulary. */
function firesOn(t: TriggerRow): string {
  switch (t.kind) {
    case 'scheduled':
      return t.scheduleConfig?.cronExpression ?? 'schedule';
    case 'manual':
      return 'on demand';
    default:
      return t.eventType || 'any event';
  }
}

export function TriggerList({
  triggers,
  deliveries,
  sourceName,
  onOpen,
  onToggle,
  emptyMessage,
  toolbar,
  resetKey = '',
}: TriggerListProps) {
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
  const win = pageWindow(triggers.length, paging.size, requested);
  const visible = triggers.slice(win.start, win.end);

  return (
    <DataGrid columns={COLUMNS} aria-label="Triggers">
      {toolbar !== undefined && <DataGridToolbar>{toolbar}</DataGridToolbar>}
      <DataGridHeader
        cells={[
          'Trigger',
          'Fires',
          'Target agent',
          '24h',
          'Last fired',
          { label: 'Enabled', align: 'end' },
        ]}
      />

      {triggers.length === 0 ? (
        <DataGridEmpty>
          <Icon name="webhook" size="sm" color="subtle" />
          <Text size="supporting" color="subtle">
            {emptyMessage}
          </Text>
        </DataGridEmpty>
      ) : (
        visible.map((t, i) => {
          const failures = recentFailures(t.id, deliveries);
          const buckets = sparkline(t.id, deliveries);
          const activity = buckets.map((b) => b.started + b.failed + b.other);
          const hasActivity = activity.some((v) => v > 0);

          return (
            <DataGridRow
              key={t.id}
              last={i === visible.length - 1}
              onClick={() => onOpen(t)}
              rowLabel={`Open ${t.name}`}
            >
              {/* Trigger: how it starts, what it is called, where it comes from */}
              <Stack direction="horizontal" gap="cozy" align="center" minWidth="none">
                <IconTile tone={t.autoPausedAt !== null ? 'accent' : 'muted'}>
                  <Icon name={KIND_ICON[t.kind]} size="xs" />
                </IconTile>
                <Stack gap="tight" minWidth="none">
                  <Stack direction="horizontal" gap="snug" align="center">
                    <Text mono size="supporting" weight="emphasis">
                      {t.name}
                    </Text>
                    {/* A trigger the platform stopped is not the same as one a
                        person turned off: the toggle already says the second,
                        and only the first needs looking into. */}
                    {t.autoPausedAt !== null && (
                      <Badge variant="degraded" size="sm" shape="pill">
                        Auto-paused
                      </Badge>
                    )}
                    {/* Derived from this trigger's own recent outcomes, never a
                        stored flag that could drift from the deliveries below. */}
                    {failures > 0 && (
                      <Badge variant="error" size="sm" shape="pill">
                        {failures} failed
                      </Badge>
                    )}
                  </Stack>
                  <Text size="caption" color="subtle" truncate>
                    {t.kind === 'webhook'
                      ? sourceName(t.sourceId)
                      : t.kind === 'scheduled'
                        ? (t.scheduleConfig?.timezone ?? 'schedule')
                        : `POST /api/v1/triggers/${t.id}/fire`}
                  </Text>
                </Stack>
              </Stack>

              {/* Fires: the condition, in the trigger's own vocabulary */}
              <Stack direction="horizontal" minWidth="none">
                <Badge variant="elevated" size="sm" mono>
                  {firesOn(t)}
                </Badge>
              </Stack>

              {/* Target agent: what gets started */}
              <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
                <IconTile size="sm">
                  <Text mono size="micro" weight="heavy">
                    {t.agentSlug.charAt(0).toUpperCase()}
                  </Text>
                </IconTile>
                <Text size="supporting" color="muted" truncate>
                  {t.agentSlug}
                </Text>
              </Stack>

              {/* 24h: the shape of recent activity. Muted when disabled, so the
                  row keeps its place in the column rather than going blank. */}
              {hasActivity ? (
                <Sparkline
                  values={activity}
                  tone={!t.enabled ? 'muted' : failures > 0 ? 'error' : 'accent'}
                  label={`${t.name}: activity over the last 24 hours`}
                />
              ) : (
                <Text size="caption" color="subtle">
                  —
                </Text>
              )}

              <Text size="supporting" color={failures > 0 ? 'status-error' : 'muted'}>
                {relativeTime(lastFired(t.id, deliveries))}
              </Text>

              {/* The row opens the drawer; flipping the toggle must not.
                  Stopping propagation here keeps the two gestures separate. */}
              <Stack
                direction="horizontal"
                justify="end"
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => e.stopPropagation()}
              >
                <Toggle
                  checked={t.enabled}
                  onChange={() => onToggle(t)}
                  aria-label={`${t.enabled ? 'Disable' : 'Enable'} ${t.name}`}
                />
              </Stack>
            </DataGridRow>
          );
        })
      )}

      {/* Inside the frame, like the toolbar: the controls that change what the
          grid shows belong to the grid. */}
      {triggers.length > 0 && (
        <DataGridPagination
          from={win.from}
          to={win.to}
          total={win.total}
          page={win.page}
          pageCount={win.pageCount}
          pageSize={paging.size}
          pageSizeOptions={PAGE_SIZES}
          noun="triggers"
          onPageChange={(page) => setPaging({ key: resetKey, page, size: paging.size })}
          // A new page size makes the old page index meaningless, so it
          // returns to the first page rather than landing mid-list.
          onPageSizeChange={(size) => setPaging({ key: resetKey, page: 0, size })}
        />
      )}
    </DataGrid>
  );
}
