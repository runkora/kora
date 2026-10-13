'use client';

/**
 * @tier 3 — triggers-scoped. Coalesce settings for a webhook trigger: a key
 * template and a window. Used by the wizard's Delivery step and the drawer.
 */

import { useId, type ReactNode } from 'react';
import { FormField, Grid, Input, SectionLabel, Stack, Text } from '@orbit/hades';
import { LIMITS } from '../_types';
import { validateCoalesce } from './trigger-wizard/wizard-state';

export interface CoalesceDraft {
  /** Key template, raw while typed. Empty with an empty window means off. */
  key: string;
  /** Window in seconds, raw while typed. */
  window: string;
}

interface CoalesceEditorProps {
  value: CoalesceDraft;
  onChange: (next: CoalesceDraft) => void;
  disabled?: boolean;
  /** Skip the inline error when the caller already shows the same reason. */
  hideError?: boolean;
  /** Shown under the fields, e.g. why they are locked. */
  note?: string;
  /** Rendered under the fields, e.g. the drawer's save and discard buttons. */
  actions?: ReactNode;
}

export function CoalesceEditor({
  value,
  onChange,
  disabled,
  hideError,
  note,
  actions,
}: CoalesceEditorProps) {
  const keyId = useId();
  const windowId = useId();
  const error = validateCoalesce(value.key, value.window);
  const { min, max } = LIMITS.coalesce.window;
  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <SectionLabel>Coalesce bursts</SectionLabel>
        <Text size="caption" color="subtle">
          Fires that render the same key within the window start one run from the newest payload.
          The window starts at the first fire. Leave both empty to run every fire.
        </Text>
      </Stack>
      <Grid cols={2} gap="comfortable">
        <FormField label="Key" id={keyId} hint="A template, like the prompt. Up to 256 bytes.">
          <Input
            id={keyId}
            mono
            value={value.key}
            disabled={disabled}
            onChange={(e) => onChange({ ...value, key: e.target.value })}
            placeholder="{{ repository.full_name }}#{{ pull_request.number }}"
          />
        </FormField>
        <FormField label="Window" id={windowId} hint={`${min} to ${max} seconds.`}>
          <Input
            id={windowId}
            mono
            inputMode="numeric"
            value={value.window}
            disabled={disabled}
            onChange={(e) => onChange({ ...value, window: e.target.value })}
            endAdornment={
              <Text as="span" size="caption" color="subtle">
                seconds
              </Text>
            }
          />
        </FormField>
      </Grid>
      {error !== undefined && !hideError && (
        <Text size="caption" color="status-error">
          {error}
        </Text>
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
