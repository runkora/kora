'use client';

/**
 * @tier 3 — triggers-scoped. The Delivery section's field layout: a bold label, a
 * line of context, the control, then its error. Hades' FormField puts the hint
 * under the control and swaps it for the error, but here the context stays above
 * the control so the error never replaces what the field is for. Also holds the
 * thin rail that groups the fields a choice reveals.
 */

import type { ReactNode } from 'react';
import { Icon, Separator, Stack, Text } from '@runkora/hades';

export function DeliveryField({
  id,
  label,
  hint,
  error,
  children,
}: {
  /** The control's id; the label points at it and the error is described by it. */
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <Text as="label" htmlFor={id} size="supporting" weight="heavy">
          {label}
        </Text>
        {hint !== undefined && (
          <Text size="caption" color="muted">
            {hint}
          </Text>
        )}
      </Stack>
      {children}
      {error !== undefined && (
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="circle-alert" size="xs" color="status-error" label="" shrink />
          <Text id={`${id}-error`} role="alert" size="caption" color="status-error">
            {error}
          </Text>
        </Stack>
      )}
    </Stack>
  );
}

/** Fields a choice reveals, set off from the rest by a thin line down their left edge. */
export function DeliveryRail({ children }: { children: ReactNode }) {
  return (
    <Stack direction="horizontal" gap="comfortable">
      <Separator orientation="vertical" tone="strong" />
      <Stack gap="comfortable" grow minWidth="none">
        {children}
      </Stack>
    </Stack>
  );
}
