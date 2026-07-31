'use client';

/** @tier 3 — triggers-scoped. One trigger's delivery history, newest first. */

import { Card, Icon, Separator, Stack, StatusDot, Text } from '@orbit/terra';
import { deliveryDotStatus, dispatchDuration } from '../_types';
import type { DeliveryRow } from '../_types';
import { DeliveryDetail } from './delivery-detail';

interface DeliveryListProps {
  /** Deliveries that reached this trigger, newest first. */
  deliveries: DeliveryRow[];
  /** The trigger whose outcome to read out of each delivery's fan-out. */
  triggerId: string;
  /** Which delivery's payload is open, if any. */
  expandedId: string | null;
  onToggleExpanded: (id: string | null) => void;
  /** Replays a delivery. Absent for read-only callers. */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
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
}: DeliveryListProps) {
  return (
    <Stack gap="snug">
      {deliveries.map((d) => {
        // The delivery's own status is the arrival-level verdict; this
        // trigger's row in the fan-out is the one that answers "and what
        // happened for me". Prefer the latter and fall back to the former.
        const outcome = d.runs.find((r) => r.triggerId === triggerId);
        const status = outcome?.status ?? d.status;
        const open = expandedId === d.id;

        return (
          <Card key={d.id} inset border="subtle">
            <Stack
              direction="horizontal"
              align="center"
              gap="cozy"
              paddingInline="cozy"
              paddingBlock="comfortable"
              onClick={() => onToggleExpanded(open ? null : d.id)}
              role="button"
              tabIndex={0}
              aria-expanded={open}
              aria-label={`${open ? 'Hide' : 'Show'} payload for delivery ${d.id}`}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onToggleExpanded(open ? null : d.id);
                }
              }}
            >
              <StatusDot status={deliveryDotStatus(status)} size="sm" />

              {/* The identity block takes the slack, so the outcome cluster
                  and the chevron pin to the right edge on every row. */}
              <Stack gap="tight" minWidth="none" grow>
                <Text mono size="caption" weight="emphasis" truncate>
                  {d.id}
                </Text>
                <Text mono size="micro" color="subtle" truncate>
                  {d.eventType || d.kind}
                </Text>
              </Stack>

              <Stack direction="horizontal" align="center" gap="cozy">
                <Text
                  mono
                  size="caption"
                  weight="emphasis"
                  color={deliveryDotStatus(status) === 'error' ? 'status-error' : 'muted'}
                >
                  {status}
                </Text>
                <Text mono size="micro" color="muted">
                  {dispatchDuration(d.receivedAt, outcome?.createdAt ?? null)}
                </Text>
                <Text size="micro" color="subtle">
                  {shortAgo(d.receivedAt)}
                </Text>
                <Icon name={open ? 'chevron-up' : 'chevron-down'} size="2xs" color="subtle" />
              </Stack>
            </Stack>

            {open && (
              <>
                <Separator />
                <Stack padding="cozy">
                  <DeliveryDetail delivery={d} triggerId={triggerId} onRedeliver={onRedeliver} />
                </Stack>
              </>
            )}
          </Card>
        );
      })}
    </Stack>
  );
}
