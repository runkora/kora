/** @tier 3 — triggers-scoped. The trigger list: one row per way work starts. */

'use client';

import { useMemo } from 'react';
import { DataTable, Icon, IconTile, Sparkline, Stack, Text, Toggle } from '@orbit/terra';
import type { DataTableColumn, IconName } from '@orbit/terra';
import type { DeliveryRow, TriggerKind, TriggerRow } from '../_types';
import { lastFired, recentFailures, sparkline } from '../_types/fleet';

interface TriggerListProps {
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  /** Resolves a webhook source id to its name for the row subtitle. */
  sourceName: (id: string) => string;
  onOpen: (t: TriggerRow) => void;
  onToggle: (t: TriggerRow) => void;
  /** Rendered in the table toolbar, left of the search box. */
  toolbarActions?: React.ReactNode;
}

/** Icon per kind: what wakes this trigger up. */
const KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'webhook',
  scheduled: 'clock',
  manual: 'play',
};

/** Relative time, because "2m ago" answers the question and a timestamp does not. */
function relativeTime(iso: string | null): string {
  if (iso === null) return 'Never';
  const ms = Date.now() - Date.parse(iso);
  if (!Number.isFinite(ms)) return 'Never';
  const mins = Math.floor(ms / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

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
  toolbarActions,
}: TriggerListProps) {
  const columns = useMemo<DataTableColumn<TriggerRow>[]>(
    () => [
      {
        id: 'trigger',
        header: 'Trigger',
        locked: true,
        sortable: true,
        sortAccessor: (t) => t.name,
        cell: (t) => {
          const failures = recentFailures(t.id, deliveries);
          return (
            <Stack direction="horizontal" gap="snug" align="center">
              {/* The tile carries the kind, so the list can be scanned by how
                  things start without reading a word. */}
              <IconTile tone={t.autoPausedAt !== null ? 'accent' : 'muted'}>
                <Icon name={KIND_ICON[t.kind]} size="xs" />
              </IconTile>
              <Stack gap="none">
                <Stack direction="horizontal" gap="tight" align="center">
                  <Text mono size="supporting" weight="emphasis">
                    {t.name}
                  </Text>
                  {/* Derived from this trigger's own recent outcomes, never a
                      stored flag that could drift from the deliveries below. */}
                  {failures > 0 && (
                    <Text size="micro" color="status-error" weight="heavy">
                      {failures} failed
                    </Text>
                  )}
                </Stack>
                <Text size="caption" color="muted">
                  {t.kind === 'webhook'
                    ? sourceName(t.sourceId)
                    : t.kind === 'scheduled'
                      ? (t.scheduleConfig?.timezone ?? 'schedule')
                      : `POST /api/v1/triggers/${t.id}/fire`}
                </Text>
              </Stack>
            </Stack>
          );
        },
      },
      {
        id: 'fires',
        header: 'Fires',
        width: '10.5rem',
        cell: (t) => (
          <Text mono size="micro" color="muted">
            {firesOn(t)}
          </Text>
        ),
      },
      {
        id: 'agent',
        header: 'Target agent',
        width: '9.25rem',
        cell: (t) => (
          <Stack direction="horizontal" gap="tight" align="center">
            <IconTile size="sm">
              <Text mono size="micro" weight="heavy">
                {t.agentSlug.charAt(0).toUpperCase()}
              </Text>
            </IconTile>
            <Text size="supporting" color="muted">
              {t.agentSlug}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'activity',
        header: '24h',
        width: '6rem',
        cell: (t) => {
          const buckets = sparkline(t.id, deliveries);
          const failures = recentFailures(t.id, deliveries);
          const total = buckets.reduce((n, b) => n + b.started + b.failed + b.other, 0);
          if (total === 0) {
            return (
              <Text size="caption" color="muted">
                —
              </Text>
            );
          }
          return (
            <Sparkline
              values={buckets.map((b) => b.started + b.failed + b.other)}
              // Muted when disabled so the row keeps its place in the column
              // rather than going blank and breaking the scan.
              tone={!t.enabled ? 'muted' : failures > 0 ? 'error' : 'accent'}
              label={`${t.name}: activity over the last 24 hours`}
            />
          );
        },
      },
      {
        id: 'lastFired',
        header: 'Last fired',
        width: '7.5rem',
        cell: (t) => (
          <Text
            size="supporting"
            color={recentFailures(t.id, deliveries) > 0 ? 'status-error' : 'muted'}
          >
            {relativeTime(lastFired(t.id, deliveries))}
          </Text>
        ),
      },
      {
        id: 'enabled',
        header: 'Enabled',
        locked: true,
        width: '5.5rem',
        cell: (t) => (
          <Stack direction="horizontal" justify="end">
            <Toggle
              checked={t.enabled}
              onChange={() => onToggle(t)}
              aria-label={`${t.enabled ? 'Disable' : 'Enable'} ${t.name}`}
            />
          </Stack>
        ),
      },
    ],
    [deliveries, sourceName, onToggle],
  );

  return (
    <DataTable
      data={triggers}
      columns={columns}
      getRowKey={(t) => t.id}
      noun="triggers"
      searchPlaceholder="Search triggers…"
      searchFn={(t, q) => {
        const s = q.toLowerCase();
        return (
          t.name.toLowerCase().includes(s) ||
          t.agentSlug.toLowerCase().includes(s) ||
          t.eventType.toLowerCase().includes(s)
        );
      }}
      defaultSortId="trigger"
      defaultSortDir="asc"
      onRowClick={onOpen}
      onRowClickLabel="Open trigger"
      toolbarActions={toolbarActions}
      emptyMessage="No triggers yet."
      filteredEmptyMessage="No triggers match this filter."
    />
  );
}
