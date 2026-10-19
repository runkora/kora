'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the panel speaks in delivery outcomes and replay
 * semantics, Triggers vocabulary that does not belong in `src/components/`.
 *
 * One delivery's payload and outcome in a side panel, shared by the platform
 * Deliveries tab and the agent page so the two surfaces cannot drift.
 */

import {
  Button,
  Icon,
  SidePanel,
  SidePanelBody,
  SidePanelHeader,
  Stack,
  Text,
} from '@runkora/hades';
import { StatusBadge } from '@runkora/components';
import { DeliveryDetail } from './delivery-detail';
import { deliveryStatusTone, formatTimestampUtc } from '../_types';
import type { DeliveryRow } from '../_types';

export interface DeliveryDetailPanelProps {
  delivery: DeliveryRow;
  onClose: () => void;
  /** Replays the delivery; absent for read-only callers. */
  onRedeliver?: (
    delivery: DeliveryRow,
  ) => Promise<{ deliveryId: string | null; error: string | null }>;
  /**
   * Jumps to the owning trigger's configuration. Offered only when the caller
   * has somewhere to jump to: the platform page opens its drawer, the agent
   * page navigates to Triggers.
   */
  onOpenTrigger?: (triggerId: string) => void;
}

/**
 * The row was clicked to ask what this arrival did, not to configure anything,
 * so the panel is drawer-free: status, timestamp, the detail body, and at most
 * a pointer to the trigger that owns it.
 */
export function DeliveryDetailPanel({
  delivery,
  onClose,
  onRedeliver,
  onOpenTrigger,
}: DeliveryDetailPanelProps) {
  // Read once into a const so the narrowing survives into the click handler.
  const triggerId = delivery.triggerId;
  return (
    <SidePanel open onClose={onClose} size="md">
      <SidePanelHeader
        density="compact"
        title={delivery.eventType || delivery.kind}
        description={`Delivery ${delivery.id}`}
      />
      <SidePanelBody padding="compact">
        <Stack gap="comfortable">
          <Stack direction="horizontal" align="center" gap="cozy">
            <StatusBadge status={deliveryStatusTone(delivery.status)} label={delivery.status} />
            <Text size="caption" color="muted">
              {formatTimestampUtc(delivery.receivedAt)}
            </Text>
          </Stack>
          <DeliveryDetail delivery={delivery} onRedeliver={onRedeliver} />
          {triggerId !== null && onOpenTrigger !== undefined && (
            <Stack direction="horizontal" justify="end">
              <Button variant="ghost" size="xs" onClick={() => onOpenTrigger(triggerId)}>
                Open trigger
                <Icon name="arrow-right" size="2xs" />
              </Button>
            </Stack>
          )}
        </Stack>
      </SidePanelBody>
    </SidePanel>
  );
}
