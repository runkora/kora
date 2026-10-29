'use client';

/**
 * @tier 3 — triggers-scoped. Coalesce settings for a webhook trigger: a toggle card,
 * then, under a rail, a key template and a window. Used by the wizard's Delivery
 * step and the modal.
 */

import { useId, type ReactNode } from 'react';
import { Input, Stack, Text } from '@runkora/hades';
import { LIMITS } from '../_types';
import { DeliveryField, DeliveryRail } from './delivery-field';
import { PlainToggleRow } from './plain-toggle-row';
import { coalesceErrors, type CoalesceDraft } from './trigger-wizard/wizard-state';

export type { CoalesceDraft };

/** What is wrong with each coalesce field, worded for the line under it. Off has no errors. */
export function coalesceFieldErrors(c: CoalesceDraft): { key?: string; window?: string } {
  const raw = coalesceErrors(c);
  const { min, max } = LIMITS.coalesce.window;
  return {
    ...(raw.key !== undefined && {
      key: c.key.trim() ? raw.key : 'Key is required.',
    }),
    ...(raw.window !== undefined && {
      window: !c.window.trim()
        ? 'Window is required.'
        : `Window must be between ${min} and ${max} seconds.`,
    }),
  };
}

interface CoalesceEditorProps {
  value: CoalesceDraft;
  onChange: (next: CoalesceDraft) => void;
  disabled?: boolean;
  /** Shown under the setting, e.g. why it is locked. */
  note?: string;
  /** Rendered under the setting, e.g. the modal's save and discard buttons. */
  actions?: ReactNode;
  /** An example key in the trigger's own fields. */
  keyPlaceholder?: string;
}

export function CoalesceEditor({
  value,
  onChange,
  disabled,
  note,
  actions,
  keyPlaceholder = '{{ repository.full_name }}#{{ pull_request.number }}',
}: CoalesceEditorProps) {
  const keyId = useId();
  const windowId = useId();
  const errors = coalesceFieldErrors(value);
  return (
    <Stack gap="comfortable">
      <PlainToggleRow
        label="Coalesce events"
        description="Events with the same key, within the window below, start a single run using the newest payload."
        checked={value.enabled}
        onChange={(enabled) => onChange({ ...value, enabled })}
        disabled={disabled}
      />
      {value.enabled && (
        <DeliveryRail>
          <DeliveryField
            id={keyId}
            label="Key"
            hint="A template in the prompt's syntax."
            error={errors.key}
          >
            <Input
              id={keyId}
              mono
              value={value.key}
              filled={errors.key ? 'danger' : undefined}
              aria-describedby={errors.key ? `${keyId}-error` : undefined}
              disabled={disabled}
              onChange={(e) => onChange({ ...value, key: e.target.value })}
              placeholder={keyPlaceholder}
            />
          </DeliveryField>
          <DeliveryField
            id={windowId}
            label="Window"
            hint="Fixed from the first event."
            error={errors.window}
          >
            <Stack maxWidth="form-field">
              <Input
                id={windowId}
                mono
                inputMode="numeric"
                value={value.window}
                filled={errors.window ? 'danger' : undefined}
                aria-describedby={errors.window ? `${windowId}-error` : undefined}
                disabled={disabled}
                onChange={(e) => onChange({ ...value, window: e.target.value })}
                placeholder="30"
                endAdornment={
                  <Text as="span" size="caption" color="subtle">
                    seconds
                  </Text>
                }
              />
            </Stack>
          </DeliveryField>
        </DeliveryRail>
      )}
      {note !== undefined && (
        <Text size="caption" color="subtle">
          {note}
        </Text>
      )}
      {actions}
    </Stack>
  );
}
