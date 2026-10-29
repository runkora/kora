'use client';

/** @tier 3 — triggers-scoped. One trigger's delivery history as a timeline, newest first. */

import { useState } from 'react';
import {
  Button,
  Card,
  EmptyState,
  Grid,
  Icon,
  SectionLabel,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import { deliveryDotStatus, dispatchDuration } from '../_types';
import type { DeliveryRow } from '../_types';
import { DeliveryDetail } from './delivery-detail';
import type { RoutingAgentName } from './delivery-routing';

type Filter = 'all' | 'failed' | 'skipped';

const FILTERS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'failed', label: 'Failed' },
  { value: 'skipped', label: 'Skipped' },
];

interface DeliveryListProps {
  /** Deliveries that reached this trigger, newest first. */
  deliveries: DeliveryRow[];
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
  const rows = deliveries.map((d) => {
    const outcome = d.runs.find((r) => r.triggerId === triggerId);
    return { d, outcome, status: outcome?.status ?? d.status };
  });
  const visible = rows.filter(({ status }) =>
    filter === 'all'
      ? true
      : filter === 'failed'
        ? deliveryDotStatus(status) === 'error'
        : status === 'skipped',
  );

  if (rows.length === 0) {
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

      <Stack direction="horizontal" align="center" justify="between" gap="snug">
        <Stack direction="horizontal" align="center" gap="tight">
          {FILTERS.map((f) => (
            <Button
              key={f.value}
              size="xs"
              variant={filter === f.value ? 'secondary' : 'muted'}
              aria-pressed={filter === f.value}
              onClick={() => setFilter(f.value)}
            >
              {f.label}
            </Button>
          ))}
        </Stack>
        <Text size="caption" color="muted">
          {filter === 'all'
            ? `Showing the ${rows.length} most recent`
            : `${visible.length} of the ${rows.length} most recent`}
        </Text>
      </Stack>

      {visible.length === 0 ? (
        <EmptyState
          icon="clock"
          title={`No ${filter} deliveries`}
          description=""
          action={
            <Button size="xs" variant="ghost" onClick={() => setFilter('all')}>
              Show all deliveries
            </Button>
          }
        />
      ) : (
        <Card transparent border="subtle">
          {/* Four equal tracks (the last holds Time, Age and the chevron as its own
              three) so the header and every row share one set of column edges. */}
          <Stack paddingInline="comfortable" paddingBlock="cozy">
            <Grid cols={4} gap="cozy" aria-hidden="true">
              <SectionLabel>Delivery</SectionLabel>
              <SectionLabel>Event</SectionLabel>
              <SectionLabel>Status</SectionLabel>
              <Grid cols={3} gap="snug">
                <Stack direction="horizontal" justify="end">
                  <SectionLabel>Time</SectionLabel>
                </Stack>
                <Stack direction="horizontal" justify="end">
                  <SectionLabel>Age</SectionLabel>
                </Stack>
              </Grid>
            </Grid>
          </Stack>
          <Separator />
          <Stack gap="none" padding="tight">
            {visible.map(({ d, outcome, status }) => {
              const open = expandedId === d.id;
              const tone = deliveryDotStatus(status);
              const toggle = () => onToggleExpanded(open ? null : d.id);

              // No fill at rest; hover and keyboard focus tint a closed row, and an
              // open row and its foldout share one filled surface with no seam.
              return (
                <Card
                  key={d.id}
                  border="none"
                  transparent={!open}
                  subtle={open}
                  interactive={!open}
                >
                  <Stack
                    paddingInline="comfortable"
                    paddingBlock="cozy"
                    role="button"
                    tabIndex={0}
                    aria-expanded={open}
                    aria-label={`${open ? 'Hide' : 'Show'} payload for delivery ${d.id}`}
                    onClick={toggle}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        toggle();
                      }
                    }}
                  >
                    <Grid cols={4} gap="cozy">
                      <Stack direction="horizontal" align="center" gap="cozy" minWidth="none">
                        <StatusDot status={tone} size="sm" />
                        <Text mono size="caption" weight="strong" truncate>
                          {d.id}
                        </Text>
                      </Stack>
                      <Stack justify="center" minWidth="none">
                        <Text mono size="caption" color="muted" truncate>
                          {d.eventType || d.kind}
                        </Text>
                      </Stack>
                      <Stack justify="center">
                        <Text
                          mono
                          size="caption"
                          weight="strong"
                          color={tone === 'error' ? 'status-error' : undefined}
                        >
                          {status}
                        </Text>
                      </Stack>
                      <Grid cols={3} gap="snug">
                        <Stack direction="horizontal" justify="end" align="center">
                          <Text mono size="caption" weight="strong">
                            {dispatchDuration(d.receivedAt, outcome?.createdAt ?? null)}
                          </Text>
                        </Stack>
                        <Stack direction="horizontal" justify="end" align="center">
                          <Text size="caption" color="muted">
                            {shortAgo(d.receivedAt)}
                          </Text>
                        </Stack>
                        <Stack direction="horizontal" justify="end" align="center">
                          <Icon
                            name={open ? 'chevron-up' : 'chevron-down'}
                            size="2xs"
                            color="subtle"
                          />
                        </Stack>
                      </Grid>
                    </Grid>
                  </Stack>

                  {open && (
                    <Stack paddingInline="comfortable" paddingBottom="comfortable">
                      <DeliveryDetail
                        delivery={d}
                        triggerId={triggerId}
                        onRedeliver={onRedeliver}
                        redeliverAllowed={redeliverAllowed}
                        agents={agents}
                        runHref={runHref}
                      />
                    </Stack>
                  )}
                </Card>
              );
            })}
          </Stack>
        </Card>
      )}
    </Stack>
  );
}
