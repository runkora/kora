'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the panel speaks in delivery outcomes and replay
 * semantics, Triggers vocabulary that does not belong in `src/components/`.
 *
 * One delivery's payload and outcome in a modal, shared by the platform
 * Deliveries tab and the agent page so the two surfaces cannot drift.
 */

import {
  Button,
  Modal,
  ModalBody,
  ModalHeader,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import Link from 'next/link';
import {
  DeliveryFailure,
  DeliveryPayload,
  DeliveryRoutings,
  RedeliverButton,
  RedeliverNotices,
  deliveryOutcome,
  hasPayload,
  useRedeliver,
} from './delivery-detail';
import {
  deliveryDotStatus,
  deliveryDuration,
  coalesceKeyText,
  coalesceKeysFor,
  deliveryTriggerLabel,
  shortId,
  formatTimestampUtc,
  supersededRuns,
} from '../_types';
import type { DeliveryRow, TriggerRow } from '../_types';
import { useHasScope } from '../../_components/permissions-provider';
import type { RoutingAgentName } from './delivery-routing';

/**
 * One fact about the delivery: the label on the left, its value on the right.
 * A `control` value is a Button that already carries its own inline padding, so
 * the row drops its own to keep the value aligned with the plain ones.
 */
function FactRow({
  label,
  control = false,
  children,
}: {
  label: string;
  control?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      gap="comfortable"
      paddingBlock={control ? 'none' : 'tight'}
    >
      <Stack paddingInline="cozy">
        <Text size="caption" color="muted">
          {label}
        </Text>
      </Stack>
      <Stack paddingInline={control ? 'none' : 'cozy'} minWidth="none">
        {children}
      </Stack>
    </Stack>
  );
}

/**
 * Where a run id links to for a delivery owned by `owner`.
 *
 * Only a trigger that runs one agent has a run page to name from its own slug. A
 * routed trigger picks its agent per run, so its run ids stay plain text, as in
 * the trigger modal's delivery table.
 */
export function runHrefFor(owner: TriggerRow | undefined): ((runId: string) => string) | undefined {
  if (owner === undefined || owner.routing !== null || owner.agentSlug === '') return undefined;
  const slug = owner.agentSlug;
  return (runId) => APP_ROUTES.agents.run(slug, runId);
}

export interface DeliveryDetailPanelProps {
  delivery: DeliveryRow;
  onClose: () => void;
  /** Replays the delivery. */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
  /** False when the caller lacks run authority on the trigger's agent. */
  redeliverAllowed?: boolean;
  /**
   * Jumps to the owning trigger's configuration. Offered only when the caller
   * has somewhere to jump to: the platform page opens the trigger's detail modal, the agent
   * page navigates to Triggers.
   */
  onOpenTrigger?: (triggerId: string) => void;
  /** Names the agents a routing decision mentions. */
  agents?: RoutingAgentName[];
  /** Where the run id links to. Without it the id is plain text. */
  runHref?: (runId: string) => string;
}

/**
 * The row was clicked to ask what this arrival did, not to configure anything,
 * so the panel carries no trigger settings: status, timestamp, the detail body, and at most
 * a pointer to the trigger that owns it.
 */
export function DeliveryDetailPanel({
  delivery,
  onClose,
  onRedeliver,
  redeliverAllowed = true,
  onOpenTrigger,
  agents,
  runHref,
}: DeliveryDetailPanelProps) {
  const canWrite = useHasScope('triggers:write');
  // Read once into a const so the narrowing survives into the click handler.
  const triggerId = delivery.triggerId;
  // The delivery's own coalescing facts, in the same rows as the rest. A delivery
  // that matched several coalescing triggers names each row's trigger.
  const coalesceKeys = coalesceKeysFor(delivery);
  const superseded = supersededRuns(delivery);
  const coalescingTriggers = new Set([
    ...coalesceKeys.map((c) => c.triggerId),
    ...superseded.map((r) => r.triggerId),
  ]);
  const forTrigger = (label: string, id: string) =>
    coalescingTriggers.size < 2
      ? label
      : `${label} (${delivery.runs.find((r) => r.triggerId === id)?.triggerName ?? id})`;
  // The link opens `triggerId`, so it is named from that trigger. The generic
  // label can read `first-trigger +1` for a delivery that fanned out, which would
  // name a different trigger than the one the click opens.
  const linkedName =
    triggerId === null
      ? null
      : (delivery.triggerName ??
        delivery.runs.find((r) => r.triggerId === triggerId)?.triggerName ??
        shortId(triggerId));
  // What the delivery did, read for the whole delivery since the modal is not
  // scoped to one trigger.
  const outcome = deliveryOutcome(delivery);
  const redeliver = useRedeliver(delivery, onRedeliver);
  const canRedeliver = onRedeliver !== undefined && outcome.redeliverable;
  const hasOutcome =
    outcome.routed.length > 0 ||
    (outcome.error ?? '') !== '' ||
    (outcome.note ?? '') !== '' ||
    hasPayload(delivery) ||
    canRedeliver;
  const statusLabel = delivery.status.charAt(0).toUpperCase() + delivery.status.slice(1);
  const via =
    delivery.kind === 'scheduled'
      ? 'Schedule'
      : delivery.kind === 'manual'
        ? 'Manual fire'
        : 'Webhook';
  return (
    <Modal open onClose={onClose} size="lg">
      <ModalHeader
        title={delivery.eventType || delivery.kind}
        description={`Delivery ${delivery.id}`}
      />

      {/* Status strip: between header and body, as on the model detail modal. */}
      <Stack
        direction="horizontal"
        align="center"
        gap="snug"
        paddingBlock="snug"
        paddingInline="comfortable"
      >
        <StatusDot
          status={deliveryDotStatus(delivery.status)}
          glow
          role="img"
          aria-label={statusLabel}
        />
        <Text size="supporting">{statusLabel}</Text>
      </Stack>
      <Separator />

      <ModalBody flush gapless constrained>
        <Stack padding="snug" gap="none">
          {/* The name is the way to the trigger, where the caller has somewhere to go. */}
          {triggerId !== null && onOpenTrigger !== undefined ? (
            <FactRow label="Trigger" control>
              <Button variant="ghost" size="xs" onClick={() => onOpenTrigger(triggerId)}>
                <Text as="span" size="supporting" color="accent">
                  {linkedName}
                </Text>
              </Button>
            </FactRow>
          ) : (
            <FactRow label="Trigger">
              <Text size="supporting" align="end">
                {deliveryTriggerLabel(delivery)}
              </Text>
            </FactRow>
          )}
          <FactRow label="Arrived via">
            <Text size="supporting">{via}</Text>
          </FactRow>
          <FactRow label="Received">
            <Text size="supporting">{formatTimestampUtc(delivery.receivedAt)}</Text>
          </FactRow>
          <FactRow label="Dispatch time">
            <Text mono size="supporting">
              {deliveryDuration(delivery)}
            </Text>
          </FactRow>
          {/* The run this delivery started, linked when there is a page for it. */}
          <FactRow label="Run">
            {delivery.runId === null ? (
              <Text size="supporting" color="subtle">
                No run started
              </Text>
            ) : runHref !== undefined ? (
              <Link
                href={runHref(delivery.runId)}
                className="rounded-sm font-mono text-sm text-accent hover:text-accent-hover hover:underline underline-offset-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                {delivery.runId}
              </Link>
            ) : (
              <Text mono size="supporting" color="accent">
                {delivery.runId}
              </Text>
            )}
          </FactRow>
          {coalesceKeys.map((c) => (
            <FactRow key={`key-${c.triggerId}`} label={forTrigger('Coalescing key', c.triggerId)}>
              <Text
                mono={!c.keyTooLong}
                size="supporting"
                color={c.keyTooLong ? 'status-error' : undefined}
                truncate
                title={coalesceKeyText(c)}
              >
                {coalesceKeyText(c)}
              </Text>
            </FactRow>
          ))}
          {superseded.map((r) => (
            <FactRow key={`by-${r.triggerId}`} label={forTrigger('Replaced by', r.triggerId)}>
              <Text mono size="supporting" color="accent">
                {r.supersededBy}
              </Text>
            </FactRow>
          ))}
        </Stack>

        {/* The outcome and payload, composed from the delivery detail parts. Left out
            entirely when there is nothing to say, so no empty block follows the facts. */}
        {hasOutcome && (
          <>
            <Separator />
            <Stack padding="comfortable" gap="cozy">
              <DeliveryRoutings routed={outcome.routed} agents={agents} />
              <DeliveryFailure error={outcome.error} note={outcome.note} />
              <DeliveryPayload
                delivery={delivery}
                heading={
                  <Text size="caption" color="muted">
                    Request payload
                  </Text>
                }
              />
              <RedeliverNotices redeliver={redeliver} />
              {canRedeliver && (
                <Stack direction="horizontal" justify="end">
                  <RedeliverButton redeliver={redeliver} allowed={canWrite && redeliverAllowed} />
                </Stack>
              )}
            </Stack>
          </>
        )}
      </ModalBody>
    </Modal>
  );
}
