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

/** Pretty-prints a payload excerpt. Excerpts are truncated at capture, so a
 * parse failure is expected and the raw text is shown. */
function formatPayload(excerpt: string): string {
  try {
    return JSON.stringify(JSON.parse(excerpt), null, 2);
  } catch {
    return excerpt;
  }
}

function formatBytes(bytes: number | null): string {
  if (bytes === null) return '';
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`;
}

export interface DeliveryDetailProps {
  delivery: DeliveryRow;
  /** Whose outcome to read out of the delivery's fan-out; omit on an unscoped surface. */
  triggerId?: string;
  /**
   * Maps a trigger to its agent slug: the fan-out names the trigger, but the run
   * route is keyed by slug. Omitted, a run renders as plain text.
   */
  resolveAgentSlug?: (triggerId: string) => string | undefined;
  /** Replays the delivery, resolving with the new delivery's id. Absent for read-only callers. */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
}

/**
 * One run id, linked when the owning agent resolves. Anchor + next/link rather
 * than AppLink, which only accepts the static routes in APP_ROUTES.
 */
function RunRef({ runId, agentSlug }: { runId: string; agentSlug: string | undefined }) {
  // Empty counts as unresolved: agent_slug is NOT NULL but not non-empty, and a
  // blank one builds "/agents//runs/<id>" — a broken link, not a missing one.
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
        {/* as="span": Text is a block <p> by default, and a <p> inside an
            <a> is invalid markup that breaks the link's inline layout. */}
        <Text as="span" mono size="caption" color="accent">
          {runId}
        </Text>
      </Link>
    </Anchor>
  );
}

/** The detail body for one delivery, shared by the trigger drawer and the Deliveries panel. */
export function DeliveryDetail({
  delivery,
  triggerId,
  resolveAgentSlug,
  onRedeliver,
}: DeliveryDetailProps) {
  const [redelivering, setRedelivering] = useState(false);
  const [redeliverError, setRedeliverError] = useState<string | null>(null);
  const [replayId, setReplayId] = useState<string | null>(null);

  // A trigger's row in the fan-out answers "and what happened for me"; the
  // delivery's own status is the arrival-level verdict.
  const outcome =
    triggerId === undefined ? undefined : delivery.runs.find((r) => r.triggerId === triggerId);
  // A fire refused before it ever fanned out (invalid input, a full queue) has its
  // reason only on the delivery, so scoping past it leaves the panel saying
  // nothing on exactly the rows opened to ask why.
  const error = outcome?.error ?? (outcome === undefined ? delivery.error : null);
  const runId = outcome?.runId ?? (triggerId === undefined ? delivery.runId : null);
  // The delivery's own trigger_id is a reachable last resort: outcomeRecorder.insert
  // logs an InsertDeliveryRun failure and carries on, so a delivery can hold a
  // run_id with no fan-out row behind it.
  const runTriggerId =
    outcome?.triggerId ??
    delivery.runs.find((r) => r.runId === runId)?.triggerId ??
    (triggerId === undefined ? (delivery.triggerId ?? undefined) : undefined);
  const runSlug = runTriggerId === undefined ? undefined : resolveAgentSlug?.(runTriggerId);

  // Every run this arrival started, on an unscoped surface only: a scoped panel
  // shows one trigger's outcome and lists no fan-out. The delivery's own run_id is folded in when
  // no fan-out row carries it: without the fold, a run whose row failed to write
  // is listed nowhere once a sibling has one, because the list renders the
  // siblings and the inline cluster below is suppressed.
  const listedRuns =
    triggerId === undefined
      ? (() => {
          const rows = delivery.runs.filter((r) => r.runId !== null);
          if (runId == null || rows.some((r) => r.runId === runId)) return rows;
          return [
            ...rows,
            {
              triggerId: delivery.triggerId ?? '',
              triggerName: delivery.triggerName ?? '',
              runId,
              status: delivery.status,
              error: null,
              createdAt: delivery.receivedAt,
            },
          ];
        })()
      : [];

  // delivery.run_id and the fan-out are written by separate statements in
  // service_dispatch, so a read between them sees a fan-out run with no
  // delivery-level run id — and reading only delivery.runId renders the run
  // nowhere: the list above needs two, the cluster below needs runId.
  const inlineRunId = runId ?? (listedRuns.length === 1 ? listedRuns[0]?.runId : null) ?? null;
  const inlineRunSlug =
    runId != null ? runSlug : resolveAgentSlug?.(listedRuns[0]?.triggerId ?? '');

  // "No run started" has to agree with both of these, or the panel claims
  // nothing ran while listing what did.
  const showRunCluster = inlineRunId != null && listedRuns.length <= 1;
  const showNoRunText = inlineRunId == null && listedRuns.length === 0;

  const canRedeliver =
    onRedeliver !== undefined && isRedeliverable(outcome?.status ?? delivery.status);

  const coalesceKeys = coalesceKeysFor(delivery, triggerId);
  const superseded = supersededRuns(delivery, triggerId);
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
      {/* Failure first: the summary is the actionable message, the full chain
          underneath is for tracing. */}
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

      {/* An arrival that ended without dispatching — a duplicate, a fire that
          matched no trigger — carries its reason on the delivery, not in the
          fan-out. */}
      {(error == null || error === '') && delivery.reason != null && delivery.reason !== '' && (
        <Callout variant="info">
          <Text size="caption">{delivery.reason}</Text>
        </Callout>
      )}

      {/* Coalescing: the key this delivery rendered, and who replaced it. */}
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

      {listedRuns.length > 1 && (
        <Stack gap="tight">
          <Text mono size="micro" color="subtle">
            runs
          </Text>
          {listedRuns.map((r) => (
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
        // runs with nothing to replay has no children here, and guarding on runId
        // would emit an empty row that still takes a gap of space.
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
              <RunRef runId={inlineRunId} agentSlug={inlineRunSlug} />
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
