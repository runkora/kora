'use client';

/**
 * @tier 3 — triggers-scoped. The wizard's Delivery step: the shared Delivery
 * section over the wizard's draft state. The section itself is shared with a
 * stored trigger's Configuration tab; this only maps the draft in and out.
 */

import { coalesceDraftOf } from './wizard-state';
import { DeliverySection } from '../delivery-section';
import type { DeliveryValue } from '../delivery-section';
import type { StepComponentProps } from './types';

export function StepDelivery({ state, patch, definition }: StepComponentProps) {
  const value: DeliveryValue = {
    maxParallel: state.maxParallel,
    overflow: state.overflow,
    maxQueueDepth: state.maxQueueDepth,
    timeoutSeconds: state.timeoutSeconds,
    dedupeEnabled: state.dedupeEnabled,
    autopauseEnabled: state.autopauseEnabled,
    coalesce: coalesceDraftOf(state),
  };
  return (
    <DeliverySection
      value={value}
      kind={definition.apiKind}
      coalesceKeyExample={definition.coalesceKeyExample?.(state.eventType)}
      onChange={({ coalesce, ...rest }) =>
        patch({
          ...rest,
          ...(coalesce !== undefined && {
            coalesceEnabled: coalesce.enabled,
            coalesceKey: coalesce.key,
            coalesceWindow: coalesce.window,
          }),
        })
      }
    />
  );
}
