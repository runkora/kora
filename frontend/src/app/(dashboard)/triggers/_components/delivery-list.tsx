/** @tier 3 — triggers-scoped. One trigger's delivery history, newest first. */

'use client';

import {
  Button,
  Card,
  CodeBlock,
  Icon,
  SectionLabel,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@orbit/terra';
import { deliveryStatusTone } from '../_types';
import type { DeliveryRow } from '../_types';

/**
 * StatusDot has no "unknown" tone. An outcome the console does not recognise is
 * shown as idle rather than as healthy or failed: inventing a verdict for a
 * status we have not taught the UI about is worse than showing none.
 */
function dotStatus(status: string): 'healthy' | 'degraded' | 'error' | 'idle' {
  const tone = deliveryStatusTone(status);
  return tone === 'unknown' ? 'idle' : tone;
}

interface DeliveryListProps {
  /** Deliveries that reached this trigger, newest first. */
  deliveries: DeliveryRow[];
  /** The trigger whose outcome to read out of each delivery's fan-out. */
  triggerId: string;
  /** Which delivery's payload is open, if any. */
  expandedId: string | null;
  onToggleExpanded: (id: string | null) => void;
}

/**
 * How long the platform took to turn an arrival into a recorded outcome.
 *
 * Null when the outcome carries no timestamp — an event that was filtered or
 * denied never reached a run, so there is no duration to report and a "0ms"
 * would read as suspiciously fast rather than as not-applicable.
 */
function dispatchDuration(receivedAt: string, createdAt: string | null): string {
  if (createdAt === null) return '—';
  const ms = Date.parse(createdAt) - Date.parse(receivedAt);
  if (!Number.isFinite(ms) || ms < 0) return '—';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
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

/** Byte counts read better as the size they are than as a raw integer. */
function formatBytes(bytes: number | null): string {
  if (bytes === null) return '';
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`;
}

export function DeliveryList({
  deliveries,
  triggerId,
  expandedId,
  onToggleExpanded,
}: DeliveryListProps) {
  return (
    <Stack gap="tight">
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
                padding="snug"
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
                <StatusDot status={dotStatus(status)} size="sm" />

                <Stack gap="none" minWidth="none">
                  <Text mono size="caption" weight="emphasis" truncate>
                    {d.id}
                  </Text>
                  <Text mono size="micro" color="subtle" truncate>
                    {d.eventType || d.kind}
                  </Text>
                </Stack>

                <Text mono size="caption" weight="emphasis" color="muted">
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

              {open && (
                <>
                  <Separator />
                  <Stack gap="snug" padding="snug">
                    <Stack direction="horizontal" align="center" justify="between">
                      <SectionLabel>Request payload</SectionLabel>
                      <Text mono size="micro" color="subtle">
                        {formatBytes(d.payloadBytes)}
                      </Text>
                    </Stack>

                    {d.payloadPurged ? (
                      <Text size="caption" color="subtle">
                        Payload purged past this tenant&apos;s retention window. What happened is
                        still recorded.
                      </Text>
                    ) : d.payloadExcerpt !== null ? (
                      <CodeBlock text={d.payloadExcerpt} wrap />
                    ) : (
                      <Text size="caption" color="subtle">
                        No payload retained for this delivery.
                      </Text>
                    )}

                    {outcome?.error != null && (
                      <Text size="caption" color="status-error">
                        {outcome.error}
                      </Text>
                    )}

                    <Stack direction="horizontal" align="center" justify="between" gap="snug">
                      <Stack direction="horizontal" align="center" gap="tight">
                        <Text mono size="micro" color="subtle">
                          run
                        </Text>
                        <Text mono size="caption" color="accent">
                          {outcome?.runId ?? '—'}
                        </Text>
                      </Stack>
                      {/* Redeliver is specified but not yet built: replaying a
                          delivery has to re-run admission and dedup, which is
                          still an open decision. Shown disabled rather than
                          hidden so the affordance is discoverable. */}
                      <Button variant="ghost" size="xs" disabled>
                        Redeliver
                      </Button>
                    </Stack>
                  </Stack>
                </>
              )}
          </Card>
        );
      })}
    </Stack>
  );
}
