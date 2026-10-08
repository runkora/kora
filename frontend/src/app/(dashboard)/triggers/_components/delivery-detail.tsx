'use client';

/** @tier 3 — triggers-scoped. What one delivery actually did: the failure if
 * there was one, the payload that arrived, and the run it started. */

import { useState } from 'react';
import { Anchor, Button, Callout, CodeBlock, SectionLabel, Stack, Text } from '@orbit/hades';
import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import {
  coalesceKeyText,
  coalesceKeysFor,
  failureSummary,
  isRedeliverable,
  supersededRuns,
} from '../_types';
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
   * Maps a trigger to the agent whose runs it starts, so a run id can link to
   * that run's page. The delivery's fan-out names the trigger, not the agent,
   * and the run route is keyed by agent slug.
   *
   * Optional on purpose: a caller without the triggers list omits it and the
   * run renders as plain text, exactly as before. A link that cannot be built
   * is never a broken link.
   */
  resolveAgentSlug?: (triggerId: string) => string | undefined;
  /**
   * Replays the delivery, resolving with the new delivery's id or an error.
   * Absent for read-only callers, where no Redeliver is offered at all.
   */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
}

/**
 * One run id, linked to its run when the owning agent can be resolved.
 *
 * The link is composed from Anchor + next/link rather than AppLink because
 * AppLink only accepts the static routes in APP_ROUTES; this is the same
 * composition AppLink uses internally. Without a slug the id still renders, as
 * text: a link that cannot be built is better absent than broken.
 */
function RunRef({ runId, agentSlug }: { runId: string; agentSlug: string | undefined }) {
  // Empty counts as unresolved, not just undefined. agent_slug is NOT NULL but
  // not non-empty, and a blank one builds "/agents//runs/<id>", which is the
  // broken link this fallback exists to avoid rather than a missing one.
  if (agentSlug === undefined || agentSlug === '') {
    return (
      <Text mono size="caption" color="accent">
        {runId}
      </Text>
    );
  }
  return (
    <Anchor asChild size="inherit">
      <Link href={APP_ROUTES.agents.run(agentSlug, runId)}>
        <Text mono size="caption" color="accent">
          {runId}
        </Text>
      </Link>
    </Anchor>
  );
}

/**
 * The detail body for one delivery, shared by the trigger drawer's inline
 * expansion and the platform Deliveries panel so the two cannot drift.
 */
export function DeliveryDetail({
  delivery,
  triggerId,
  resolveAgentSlug,
  onRedeliver,
}: DeliveryDetailProps) {
  const [redelivering, setRedelivering] = useState(false);
  const [redeliverError, setRedeliverError] = useState<string | null>(null);
  const [replayId, setReplayId] = useState<string | null>(null);

  // The delivery's own status is the arrival-level verdict; a trigger's row in
  // the fan-out is the one that answers "and what happened for me".
  const outcome =
    triggerId === undefined ? undefined : delivery.runs.find((r) => r.triggerId === triggerId);
  // Falls back to the delivery's own error when this trigger has no row in the
  // fan-out at all. A fire refused before it ever fanned out (invalid input, a
  // full queue) has its reason only on the delivery, and scoping past it left
  // the panel saying nothing on exactly the deliveries opened to ask why.
  const error = outcome?.error ?? (outcome === undefined ? delivery.error : null);
  const runId = outcome?.runId ?? (triggerId === undefined ? delivery.runId : null);
  // The run belongs to whichever trigger started it: this trigger in a scoped
  // view, otherwise the one fan-out row that actually produced a run.
  const runTriggerId =
    outcome?.triggerId ?? delivery.runs.find((r) => r.runId === runId)?.triggerId;
  const runSlug = runTriggerId === undefined ? undefined : resolveAgentSlug?.(runTriggerId);

  // One delivery can match several triggers and start a run for each. A
  // per-trigger view answers "what happened for me" and shows only that run;
  // the unscoped panel is asked "what did this arrival do", so every run it
  // started is worth reaching.
  const fanOutRuns = triggerId === undefined ? delivery.runs.filter((r) => r.runId !== null) : [];

  // The run shown inline when there is exactly one. delivery.run_id and the
  // fan-out are written by separate statements in service_dispatch, so a read
  // between them sees a fan-out run with no delivery-level run id. Reading
  // only delivery.runId there would render the run nowhere: the list above
  // needs two to appear, and the cluster below needs runId.
  const soleRun = runId ?? (fanOutRuns.length === 1 ? fanOutRuns[0]?.runId : null) ?? null;
  const soleRunSlug = runId != null ? runSlug : resolveAgentSlug?.(fanOutRuns[0]?.triggerId ?? '');

  // Exactly one run is shown inline; several are listed above instead. "No run
  // started" has to agree with both, or the panel claims nothing ran while
  // listing what did.
  const showRunCluster = soleRun != null && fanOutRuns.length <= 1;
  const showNoRunText = soleRun == null && fanOutRuns.length === 0;

  // Replay is a recovery action, so it is offered only where there is something
  // to recover. A dispatched delivery already did its work; replaying it would
  // run that work a second time, and the button being there at all is enough
  // for a stray click to do it.
  const canRedeliver =
    onRedeliver !== undefined && isRedeliverable(outcome?.status ?? delivery.status);

  const coalesceKeys = coalesceKeysFor(delivery, triggerId);
  const superseded = supersededRuns(delivery, triggerId);
  // A delivery that matched several coalescing triggers names each row's trigger.
  const triggerIds = new Set([
    ...coalesceKeys.map((c) => c.triggerId),
    ...superseded.map((r) => r.triggerId),
  ]);
  const forTrigger = (label: string, id: string) => {
    if (triggerIds.size < 2) return label;
    const name = delivery.runs.find((r) => r.triggerId === id)?.triggerName ?? id;
    return `${label} (${name})`;
  };

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
    <Stack gap="cozy">
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

      {/* An arrival that ended without dispatching carries its reason on the
          delivery rather than in the fan-out: a duplicate, a fire that matched
          no trigger. The grid's Detail column already says this, and the panel
          is where someone goes when that one line was not enough, so it cannot
          be the one place the reason is missing. */}
      {(error == null || error === '') && delivery.reason != null && delivery.reason !== '' && (
        <Callout variant="info">
          <Text size="caption">{delivery.reason}</Text>
        </Callout>
      )}

      {/* Coalescing: the key this delivery rendered, and who replaced it. A key
          too long to use is stated, since that fire ran on its own. */}
      {(coalesceKeys.length > 0 || superseded.length > 0) && (
        <Stack gap="tight">
          <SectionLabel>Coalescing</SectionLabel>
          {coalesceKeys.map((c) => (
            <Stack key={c.triggerId} direction="horizontal" align="center" gap="cozy">
              <Text size="caption" color="muted">
                {forTrigger('Key', c.triggerId)}
              </Text>
              <Text
                mono={!c.keyTooLong}
                size="caption"
                color={c.keyTooLong ? 'status-error' : undefined}
                truncate
              >
                {coalesceKeyText(c)}
              </Text>
            </Stack>
          ))}
          {superseded.map((r) => (
            <Stack key={r.triggerId} direction="horizontal" align="center" gap="cozy">
              <Text size="caption" color="muted">
                {forTrigger('Replaced by', r.triggerId)}
              </Text>
              <Text mono size="caption" color="accent" truncate>
                {r.supersededBy}
              </Text>
            </Stack>
          ))}
        </Stack>
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
      {fanOutRuns.length > 1 && (
        <Stack gap="tight">
          <Text mono size="micro" color="subtle">
            runs
          </Text>
          {fanOutRuns.map((r) => (
            <Stack key={r.runId} direction="horizontal" align="center" gap="tight">
              <Text size="caption" color="muted" truncate>
                {r.triggerName || r.triggerId}
              </Text>
              <RunRef runId={r.runId as string} agentSlug={resolveAgentSlug?.(r.triggerId)} />
            </Stack>
          ))}
        </Stack>
      )}

      {(showRunCluster || showNoRunText || canRedeliver) && (
        // Guarded on what actually renders, not on runId: a fan-out of several
        // runs with nothing to replay has no children here at all, and guarding
        // on runId would emit an empty row that still takes a gap of space.
        // When only Redeliver remains, justify puts it on the right rather than
        // an empty element holding the left half.
        <Stack
          direction="horizontal"
          align="center"
          justify={showRunCluster || showNoRunText ? 'between' : 'end'}
          gap="snug"
        >
          {showRunCluster && (
            <Stack direction="horizontal" align="center" gap="tight">
              <Text mono size="micro" color="subtle">
                run
              </Text>
              <RunRef runId={soleRun} agentSlug={soleRunSlug} />
            </Stack>
          )}
          {showNoRunText && (
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
