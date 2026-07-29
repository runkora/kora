/** @tier 3 — triggers-scoped. What one delivery actually did: the failure if
 * there was one, the payload that arrived, and the run it started. */

'use client';

import { useState } from 'react';
import { Button, Callout, CodeBlock, SectionLabel, Stack, Text } from '@orbit/terra';
import { failureSummary, isRedeliverable } from '../_types';
import type { DeliveryRow } from '../_types';

/**
 * Pretty-prints a payload excerpt when it is complete JSON.
 *
 * Excerpts are truncated at capture, so parsing can fail on an honest excerpt;
 * in that case the raw text is shown rather than nothing.
 */
function formatPayload(excerpt: string): string {
  try {
    return JSON.stringify(JSON.parse(excerpt), null, 2);
  } catch {
    return excerpt;
  }
}

/** Byte counts read better as the size they are than as a raw integer. */
function formatBytes(bytes: number | null): string {
  if (bytes === null) return '';
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`;
}

export interface DeliveryDetailProps {
  delivery: DeliveryRow;
  /**
   * Whose outcome to read out of the delivery's fan-out. Omit on a surface that
   * is not scoped to one trigger, where the delivery's own verdict is the
   * honest one to show.
   */
  triggerId?: string;
  /**
   * Replays the delivery, resolving with the new delivery's id or an error.
   * Absent for read-only callers, where no Redeliver is offered at all.
   */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
}

/**
 * The detail body for one delivery, shared by the trigger drawer's inline
 * expansion and the platform Deliveries panel so the two cannot drift.
 */
export function DeliveryDetail({ delivery, triggerId, onRedeliver }: DeliveryDetailProps) {
  const [redelivering, setRedelivering] = useState(false);
  const [redeliverError, setRedeliverError] = useState<string | null>(null);
  const [replayId, setReplayId] = useState<string | null>(null);

  // The delivery's own status is the arrival-level verdict; a trigger's row in
  // the fan-out is the one that answers "and what happened for me".
  const outcome =
    triggerId === undefined ? undefined : delivery.runs.find((r) => r.triggerId === triggerId);
  const error = outcome?.error ?? (triggerId === undefined ? delivery.error : null);
  const runId = outcome?.runId ?? (triggerId === undefined ? delivery.runId : null);

  // Replay is a recovery action, so it is offered only where there is something
  // to recover. A dispatched delivery already did its work; replaying it would
  // run that work a second time, and the button being there at all is enough
  // for a stray click to do it.
  const canRedeliver =
    onRedeliver !== undefined && isRedeliverable(outcome?.status ?? delivery.status);

  const replay = () => {
    if (onRedeliver === undefined) return;
    setRedelivering(true);
    setRedeliverError(null);
    void onRedeliver(delivery).then((result) => {
      setRedelivering(false);
      if (result.error !== null) {
        setRedeliverError(result.error);
        return;
      }
      setReplayId(result.deliveryId);
    });
  };

  return (
    <Stack gap="snug">
      {/* Failure first: on a failed delivery the error is what the person
          opened the row for. The summary is the actionable message; the full
          chain stays underneath for tracing. */}
      {error != null && error !== '' && (
        <Callout variant="error">
          <Stack gap="tight" minWidth="none">
            <Text size="caption" weight="emphasis" color="status-error">
              {failureSummary(error)}
            </Text>
            {failureSummary(error) !== error && (
              <Text mono size="micro" color="subtle" preserveWhitespace>
                {error}
              </Text>
            )}
          </Stack>
        </Callout>
      )}

      {/* Only when there is something to say about a payload; a header over
          "there is none" was noise on every failed manual fire. */}
      {(delivery.payloadPurged || delivery.payloadExcerpt !== null) && (
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" justify="between">
            <SectionLabel>Request payload</SectionLabel>
            <Text mono size="micro" color="subtle">
              {formatBytes(delivery.payloadBytes)}
            </Text>
          </Stack>
          {delivery.payloadPurged ? (
            <Text size="caption" color="subtle">
              Payload purged past this tenant&apos;s retention window. What happened is still
              recorded.
            </Text>
          ) : (
            <CodeBlock
              text={formatPayload(delivery.payloadExcerpt ?? '')}
              density="compact"
              language="json"
              wrap
            />
          )}
        </Stack>
      )}

      {redeliverError !== null && <Callout variant="error">{redeliverError}</Callout>}

      {replayId !== null && (
        <Callout variant="success">
          <Stack gap="tight" minWidth="none">
            <Text size="caption" weight="emphasis">
              Replayed as a new delivery.
            </Text>
            <Text mono size="micro" color="subtle">
              {replayId}
            </Text>
          </Stack>
        </Callout>
      )}

      {/* The run line, and the replay that only makes sense once something
          arrived to replay. */}
      {(runId != null || canRedeliver) && (
        <Stack direction="horizontal" align="center" justify="between" gap="snug">
          {runId != null ? (
            <Stack direction="horizontal" align="center" gap="tight">
              <Text mono size="micro" color="subtle">
                run
              </Text>
              <Text mono size="caption" color="accent">
                {runId}
              </Text>
            </Stack>
          ) : (
            <Text size="caption" color="subtle">
              No run started for this delivery.
            </Text>
          )}
          {canRedeliver && (
            <Button
              variant="ghost"
              size="xs"
              disabled={redelivering || replayId !== null}
              onClick={replay}
            >
              {redelivering ? 'Replaying…' : replayId !== null ? 'Replayed' : 'Redeliver'}
            </Button>
          )}
        </Stack>
      )}
    </Stack>
  );
}
