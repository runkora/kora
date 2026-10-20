'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: every column renders the Triggers vocabulary
 * (kinds, fleet stats, admission state) that promotion would drag into
 * `src/components/`.
 *
 * The trigger list: one row per way work starts.
 */

import { useMemo } from 'react';
import { Badge, DataTable, Icon, IconTile, Sparkline, Stack, Text, Toggle } from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { consoleKindOf, KIND_ICON, relativeTime, runTargets } from '../_types';
import type { ConsoleKind, DeliveryRow, SourceCategory, TriggerRow } from '../_types';
import { chatEventLabel } from '../_lib/chat';
import { lastFired, recentFailures, sparkline } from '../_types/fleet';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '../_types/pagination';
import { useHasScope } from '../../_components/permissions-provider';
import { PermissionHint } from '@runkora/components';

// ── View-model type ───────────────────────────────────────────────────────────

/**
 * TriggerRow extended with display-specific derivations pre-computed once in
 * useMemo. Multiple cells reference the same values (failures, activity) — the
 * view model ensures they are computed once, not once per cell.
 */
interface TriggerDisplayRow extends TriggerRow {
  /** Recent failure count from the delivery window. */
  failures: number;
  /** Per-bucket activity volumes for the sparkline. */
  activity: number[];
  /** Whether any activity exists (sparkline vs. dash). */
  hasActivity: boolean;
  /** ISO timestamp of the last fired delivery, or null. */
  lastFiredAt: string | null;
  /** Fires-on label (schedule expression, "on demand", event type, etc). */
  firesOnLabel: string;
  /** Chat command for a webhook trigger on a chat source, else the stored kind. */
  consoleKind: ConsoleKind;
}

/** What this trigger fires on, in its own vocabulary. */
function firesOn(t: TriggerRow, kind: ConsoleKind): string {
  switch (kind) {
    case 'chat':
      return t.eventType ? chatEventLabel(t.eventType) : 'no event type (never fires)';
    case 'scheduled':
      return t.scheduleConfig?.cronExpression ?? 'schedule';
    case 'manual':
      return 'on demand';
    default:
      return t.eventType || 'no event type (never fires)';
  }
}

// ── Column definitions ────────────────────────────────────────────────────────

function makeColumns(
  sourceName: (id: string) => string,
  onToggle: (t: TriggerRow) => void,
  canWrite: boolean,
  canRunAgent: (agentTypeId: string) => boolean,
): DataTableColumn<TriggerDisplayRow>[] {
  return [
    {
      id: 'trigger',
      header: 'Trigger',
      gridWidth: 'minmax(0, 1fr)',
      minWidth: '10rem',
      locked: true,
      cell: (row) => (
        <Stack direction="horizontal" gap="cozy" align="center" minWidth="none">
          <IconTile tone={row.autoPausedAt !== null ? 'accent' : 'muted'}>
            <Icon name={KIND_ICON[row.consoleKind]} size="xs" />
          </IconTile>
          <Stack gap="tight" minWidth="none">
            <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
              <Text mono size="supporting" weight="emphasis" truncate title={row.name}>
                {row.name}
              </Text>
              {row.autoPausedAt !== null && (
                <Badge variant="degraded" size="sm" shape="pill">
                  Auto-paused
                </Badge>
              )}
              {row.failures > 0 && (
                <Badge variant="error" size="sm" shape="pill">
                  {row.failures} failed
                </Badge>
              )}
            </Stack>
            <Text size="caption" color="subtle" truncate>
              {row.kind === 'webhook'
                ? sourceName(row.sourceId)
                : row.kind === 'scheduled'
                  ? (row.scheduleConfig?.timezone ?? 'schedule')
                  : `POST /api/v1/trigger-fires/${row.id}`}
            </Text>
          </Stack>
        </Stack>
      ),
    },
    {
      id: 'fires',
      header: 'Fires',
      gridWidth: '168px',
      minWidth: '8rem',
      cell: (row) => (
        <Stack direction="horizontal" minWidth="none">
          <Badge variant="elevated" size="sm" mono>
            {row.firesOnLabel}
          </Badge>
        </Stack>
      ),
    },
    {
      id: 'agent',
      header: 'Target agent',
      gridWidth: '148px',
      minWidth: '8rem',
      cell: (row) =>
        row.routing !== null ? (
          <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
            <IconTile size="sm">
              <Icon name="route" size="2xs" label="" />
            </IconTile>
            <Text size="supporting" color="muted" truncate>
              Routed among {row.routing.candidates.length} agents
            </Text>
          </Stack>
        ) : (
          <Stack direction="horizontal" gap="snug" align="center" minWidth="none">
            <IconTile size="sm">
              <Text mono size="micro" weight="heavy">
                {row.agentSlug.charAt(0).toUpperCase()}
              </Text>
            </IconTile>
            <Text size="supporting" color="muted" truncate>
              {row.agentSlug}
            </Text>
          </Stack>
        ),
    },
    {
      id: 'activity',
      header: '24h',
      gridWidth: '96px',
      minWidth: '5rem',
      cell: (row) =>
        row.hasActivity ? (
          <Sparkline
            values={row.activity}
            tone={!row.enabled ? 'muted' : row.failures > 0 ? 'error' : 'accent'}
            label={`${row.name}: activity over the last 24 hours`}
          />
        ) : (
          <Text size="caption" color="subtle">
            —
          </Text>
        ),
    },
    {
      id: 'lastFired',
      header: 'Last fired',
      gridWidth: '118px',
      minWidth: '6rem',
      cell: (row) => (
        <Text size="supporting" color={row.failures > 0 ? 'status-error' : 'muted'}>
          {relativeTime(row.lastFiredAt)}
        </Text>
      ),
    },
    {
      id: 'enabled',
      header: 'Enabled',
      gridWidth: '74px',
      minWidth: '4.5rem',
      align: 'right',
      locked: true,
      cell: (row) => {
        // Turning a trigger on or off also needs run authority on every agent it can start.
        const runs = runTargets(row).every(canRunAgent);
        return (
          <Stack
            direction="horizontal"
            justify="end"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <PermissionHint
              allowed={canWrite && runs}
              action={canWrite ? "run this trigger's agent" : 'turn triggers on or off'}
            >
              <Toggle
                checked={row.enabled}
                onChange={() => onToggle(row)}
                disabled={!canWrite || !runs}
                aria-label={`${row.enabled ? 'Disable' : 'Enable'} ${row.name}`}
              />
            </PermissionHint>
          </Stack>
        );
      },
    },
  ];
}

// ── Search ────────────────────────────────────────────────────────────────────

function triggerMatchesSearch(row: TriggerDisplayRow, q: string): boolean {
  return (
    row.name.toLowerCase().includes(q) ||
    row.agentSlug.toLowerCase().includes(q) ||
    row.eventType.toLowerCase().includes(q) ||
    row.firesOnLabel.toLowerCase().includes(q)
  );
}

/** Used when the caller does not know source categories: every source reads as a webhook source. */
const noCategory = (): SourceCategory | undefined => undefined;

// ── Component ─────────────────────────────────────────────────────────────────

interface TriggerListProps {
  triggers: TriggerRow[];
  deliveries: DeliveryRow[];
  /** Resolves a webhook source id to its name for the row subtitle. */
  sourceName: (id: string) => string;
  /** Resolves a source id to its category, so a trigger on a chat source shows as a chat command. */
  sourceCategory?: (id: string) => SourceCategory | undefined;
  onOpen: (t: TriggerRow) => void;
  onToggle: (t: TriggerRow) => void;
  /** Whether the caller may run an agent; the on or off switch needs it. */
  canRunAgent: (agentTypeId: string) => boolean;
  /** Shown inside the frame when there are no rows. */
  emptyMessage: string;
  /** Segmented filter or similar, rendered in the toolbar's right slot. */
  quickFilters?: React.ReactNode;
  /** Action buttons (e.g. New trigger), rendered at the far right of the toolbar. */
  toolbarActions?: React.ReactNode;
  /**
   * Changes whenever the filters upstream change. A new value remounts the
   * DataTable, resetting all internal state including the current page.
   */
  resetKey?: string;
}

export function TriggerList({
  triggers,
  deliveries,
  sourceName,
  sourceCategory = noCategory,
  onOpen,
  onToggle,
  canRunAgent,
  emptyMessage,
  quickFilters,
  toolbarActions,
  resetKey = '',
}: TriggerListProps) {
  // Pre-compute per-row derived values once. Multiple cells reference the same
  // values (failures, activity) — the view model ensures one computation, not
  // one per cell per render.
  const rows = useMemo<TriggerDisplayRow[]>(
    () =>
      triggers.map((t) => {
        const buckets = sparkline(t.id, deliveries);
        const activity = buckets.map((b) => b.started + b.failed + b.other);
        const failures = recentFailures(t.id, deliveries);
        const consoleKind = consoleKindOf(t, sourceCategory);
        return {
          ...t,
          failures,
          activity,
          hasActivity: activity.some((v) => v > 0),
          lastFiredAt: lastFired(t.id, deliveries),
          firesOnLabel: firesOn(t, consoleKind),
          consoleKind,
        };
      }),
    [triggers, deliveries, sourceCategory],
  );

  const canWrite = useHasScope('triggers:write');
  const columns = useMemo(
    () => makeColumns(sourceName, onToggle, canWrite, canRunAgent),
    [sourceName, onToggle, canWrite, canRunAgent],
  );

  return (
    <DataTable
      key={resetKey}
      data={rows}
      columns={columns}
      getRowKey={(row) => row.id}
      noun="triggers"
      emptyMessage={emptyMessage}
      onRowClick={onOpen}
      rowLabel={(row) => `Open ${row.name}`}
      searchFn={triggerMatchesSearch}
      searchPlaceholder="Search triggers…"
      quickFilters={quickFilters}
      toolbarActions={toolbarActions}
      pagination={{ pageSizeOptions: PAGE_SIZES, defaultPageSize: DEFAULT_PAGE_SIZE }}
    />
  );
}
