'use client';

/**
 * @tier 3 — triggers-scoped. What one delivery actually did: the failure if
 * there was one, the payload that arrived, and the run it started.
 *
 * Exported as parts, so a surface composes the ones it shows. `DeliveryDetail`
 * is the full composition, used by the trigger modal's inline expansion; the
 * delivery modal composes its own, since it shows the run and the coalescing
 * facts among the delivery's other fields.
 */

import { useState } from 'react';
import type { ReactNode } from 'react';
import Link from 'next/link';
import { Button, Callout, CodeBlock, SectionLabel, Stack, Text } from '@runkora/hades';
import {
  coalesceKeyText,
  coalesceKeysFor,
  failureSummary,
  isRedeliverable,
  supersededRuns,
} from '../_types';
import type { DeliveryRow, DeliveryRunRow } from '../_types';
import { PermissionHint } from '@runkora/components';
import { DeliveryRouting } from './delivery-routing';
import type { RoutingAgentName } from './delivery-routing';

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

// ── Outcome ──────────────────────────────────────────────────────────────────

export interface DeliveryOutcome {
  /** The failure, if there was one. */
  error: string | null;
  /** Why an arrival ended without dispatching, shown when there is no error. */
  note: string | null;
  /** The fan-out rows that carry a routing decision. */
  routed: DeliveryRunRow[];
  /** The run this delivery started, if any. */
  runId: string | null;
  /** Whether a replay could recover anything. */
  redeliverable: boolean;
}

/**
 * Reads what a delivery did out of its row.
 *
 * `triggerId` names whose outcome to read from the fan-out. Omit it on a surface
 * that is not scoped to one trigger, where the delivery's own verdict is the
 * honest one to show.
 */
export function deliveryOutcome(delivery: DeliveryRow, triggerId?: string): DeliveryOutcome {
  // The delivery's own status is the arrival-level verdict; a trigger's row in
  // the fan-out is the one that answers "and what happened for me".
  const outcome =
    triggerId === undefined ? undefined : delivery.runs.find((r) => r.triggerId === triggerId);
  // Falls back to the delivery's own error when this trigger has no row in the
  // fan-out at all. A fire refused before it ever fanned out (invalid input, a
  // full queue) has its reason only on the delivery, and scoping past it left
  // the panel saying nothing on exactly the deliveries opened to ask why.
  // A routed trigger that started nothing under its no-run fallback did what it
  // was set to do, so its reason reads as information rather than a failure.
  const unroutedNote = outcome?.status === 'unrouted' ? (outcome.error ?? null) : null;
  const error =
    unroutedNote !== null
      ? null
      : (outcome?.error ?? (outcome === undefined ? delivery.error : null));
  return {
    error: error ?? null,
    note: delivery.reason ?? unroutedNote,
    routed:
      outcome !== undefined
        ? outcome.routing !== undefined
          ? [outcome]
          : []
        : delivery.runs.filter((r) => r.routing !== undefined),
    runId: outcome?.runId ?? (triggerId === undefined ? delivery.runId : null),
    // Replay is a recovery action, so it is offered only where there is
    // something to recover. A dispatched delivery already did its work;
    // replaying it would run that work a second time, and the button being
    // there at all is enough for a stray click to do it.
    redeliverable: isRedeliverable(outcome?.status ?? delivery.status),
  };
}

// ── Redeliver ────────────────────────────────────────────────────────────────

export type RedeliverAction = (
  delivery: DeliveryRow,
) => Promise<{ deliveryId: string | null; error: string | null }>;

export interface Redeliver {
  redelivering: boolean;
  error: string | null;
  /** The new delivery's id once the replay went through. */
  replayId: string | null;
  replay: () => void;
}

/**
 * The state of replaying one delivery, shared by the button and the notices
 * that report how it went, wherever a surface puts each.
 */
export function useRedeliver(delivery: DeliveryRow, onRedeliver?: RedeliverAction): Redeliver {
  const [redelivering, setRedelivering] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [replayId, setReplayId] = useState<string | null>(null);

  const replay = () => {
    if (onRedeliver === undefined) return;
    setRedelivering(true);
    setError(null);
    void onRedeliver(delivery).then((result) => {
      setRedelivering(false);
      if (result.error !== null) {
        setError(result.error);
        return;
      }
      setReplayId(result.deliveryId);
    });
  };

  return { redelivering, error, replayId, replay };
}

/** Says how a replay went: the failure, or the new delivery it started. */
export function RedeliverNotices({ redeliver }: { redeliver: Redeliver }) {
  return (
    <>
      {redeliver.error !== null && <Callout variant="error">{redeliver.error}</Callout>}

      {redeliver.replayId !== null && (
        <Callout variant="success">
          <Stack gap="tight" minWidth="none">
            <Text size="caption" weight="emphasis">
              Replayed as a new delivery.
            </Text>
            <Text mono size="micro" color="subtle">
              {redeliver.replayId}
            </Text>
          </Stack>
        </Callout>
      )}
    </>
  );
}

/** The replay button. Offer it only where `deliveryOutcome` says a replay recovers something. */
export function RedeliverButton({
  redeliver,
  allowed = true,
}: {
  redeliver: Redeliver;
  /** False when the caller may not replay it; the button then shows disabled with the reason. */
  allowed?: boolean;
}) {
  return (
    <PermissionHint allowed={allowed} action="redeliver to this trigger">
      <Button
        variant="ghost"
        size="xs"
        disabled={!allowed || redeliver.redelivering || redeliver.replayId !== null}
        onClick={redeliver.replay}
      >
        {redeliver.redelivering
          ? 'Replaying…'
          : redeliver.replayId !== null
            ? 'Replayed'
            : 'Redeliver'}
      </Button>
    </PermissionHint>
  );
}

// ── Parts ────────────────────────────────────────────────────────────────────

/**
 * A routed trigger's decision. It leads: it says which agent and run the
 * delivery reached before anything below describes that run.
 */
export function DeliveryRoutings({
  routed,
  agents = [],
}: {
  routed: DeliveryRunRow[];
  /** Names the agents a decision mentions. An agent missing here shows as its id. */
  agents?: RoutingAgentName[];
}) {
  return (
    <>
      {routed.map(
        (r) =>
          r.routing !== undefined && (
            <DeliveryRouting
              key={r.triggerId}
              routing={r.routing}
              runId={r.runId}
              agents={agents}
            />
          ),
      )}
    </>
  );
}

/**
 * Failure first: on a failed delivery the error is what the person opened the
 * row for. The summary is the actionable message; the full chain stays
 * underneath for tracing.
 *
 * An arrival that ended without dispatching carries its reason on the delivery
 * rather than in the fan-out: a duplicate, a fire that matched no trigger. The
 * grid's Detail column already says this, and this is where someone goes when
 * that one line was not enough, so it cannot be the one place the reason is
 * missing.
 */
export function DeliveryFailure({ error, note }: { error: string | null; note: string | null }) {
  if (error != null && error !== '') {
    return (
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
    );
  }
  if (note != null && note !== '') {
    return (
      <Callout variant="info">
        <Text size="caption">{note}</Text>
      </Callout>
    );
  }
  return null;
}

/**
 * Coalescing: the key this delivery rendered, and who replaced it. A key too
 * long to use is stated, since that fire ran on its own.
 */
export function DeliveryCoalescing({
  delivery,
  triggerId,
}: {
  delivery: DeliveryRow;
  triggerId?: string;
}) {
  const coalesceKeys = coalesceKeysFor(delivery, triggerId);
  const superseded = supersededRuns(delivery, triggerId);
  if (coalesceKeys.length === 0 && superseded.length === 0) return null;

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

  return (
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
  );
}

/** Whether a delivery has a payload, or the note that it was purged, to show. */
export function hasPayload(delivery: DeliveryRow): boolean {
  return delivery.payloadPurged || delivery.payloadExcerpt !== null;
}

/**
 * The request payload, under the caller's own heading. Shown only when there is
 * something to say about a payload; a header over "there is none" was noise on
 * every failed manual fire.
 */
export function DeliveryPayload({
  delivery,
  heading,
}: {
  delivery: DeliveryRow;
  /** The heading, in whatever style the surface uses for its section titles. */
  heading: ReactNode;
}) {
  if (!hasPayload(delivery)) return null;
  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" justify="between">
        {heading}
        <Text mono size="micro" color="subtle">
          {formatBytes(delivery.payloadBytes)}
        </Text>
      </Stack>
      {delivery.payloadPurged ? (
        <Text size="caption" color="subtle">
          Payload purged past this tenant&apos;s retention window. What happened is still recorded.
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
  );
}

/** The run a delivery started, linked where there is a page for it. */
export function RunId({
  runId,
  runHref,
}: {
  runId: string;
  /** Where the run id links to. Without it the id is plain text. */
  runHref?: (runId: string) => string;
}) {
  return (
    <Stack direction="horizontal" align="center" gap="tight">
      <Text mono size="micro" color="subtle">
        run
      </Text>
      {runHref !== undefined ? (
        <Link
          href={runHref(runId)}
          className="rounded-sm font-mono text-xs text-accent underline underline-offset-2 hover:text-accent-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {runId}
        </Link>
      ) : (
        <Text mono size="caption" color="accent">
          {runId}
        </Text>
      )}
    </Stack>
  );
}

// ── Full composition ─────────────────────────────────────────────────────────

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
   * Absent on surfaces that offer no replay.
   */
  onRedeliver?: RedeliverAction;
  /** False when the caller may not replay it; Redeliver then shows disabled with the reason. */
  redeliverAllowed?: boolean;
  /** Names the agents a routing decision mentions. An agent missing here shows as its id. */
  agents?: RoutingAgentName[];
  /** Where a run id links to. Without it the id is plain text. */
  runHref?: (runId: string) => string;
}

/**
 * Everything one delivery did, for the trigger modal's inline expansion. A
 * surface that lays the delivery out differently composes the parts above.
 */
export function DeliveryDetail({
  delivery,
  triggerId,
  onRedeliver,
  redeliverAllowed = true,
  agents = [],
  runHref,
}: DeliveryDetailProps) {
  const outcome = deliveryOutcome(delivery, triggerId);
  const redeliver = useRedeliver(delivery, onRedeliver);
  const canRedeliver = onRedeliver !== undefined && outcome.redeliverable;

  return (
    <Stack gap="comfortable">
      <DeliveryRoutings routed={outcome.routed} agents={agents} />

      <Stack gap="cozy">
        <DeliveryFailure error={outcome.error} note={outcome.note} />
        <DeliveryCoalescing delivery={delivery} triggerId={triggerId} />
        <DeliveryPayload
          delivery={delivery}
          heading={<SectionLabel>Request payload</SectionLabel>}
        />
        <RedeliverNotices redeliver={redeliver} />

        {/* The run line, and the replay that only makes sense once something
          arrived to replay. */}
        {(outcome.runId != null || canRedeliver) && (
          <Stack direction="horizontal" align="center" justify="between" gap="snug">
            {outcome.runId != null ? (
              <RunId runId={outcome.runId} runHref={runHref} />
            ) : (
              <Text size="caption" color="subtle">
                No run started for this delivery.
              </Text>
            )}
            {canRedeliver && <RedeliverButton redeliver={redeliver} allowed={redeliverAllowed} />}
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}
