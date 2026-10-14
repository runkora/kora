'use client';

/**
 * @tier 3 — triggers-scoped. Coalesce settings for a webhook trigger: a switch,
 * then a key template and a window. Used by the wizard's Delivery step and the drawer.
 */

import { useId, type ReactNode } from 'react';
import { Card, CardContent, FormField, Grid, Input, Stack, Text, ToggleRow } from '@orbit/hades';
import { LIMITS } from '../_types';
import { coalesceErrors, coalesceFrom, type CoalesceDraft } from './trigger-wizard/wizard-state';

export type { CoalesceDraft };

interface CoalesceEditorProps {
  value: CoalesceDraft;
  onChange: (next: CoalesceDraft) => void;
  disabled?: boolean;
  /** Shown under the setting, e.g. why it is locked. */
  note?: string;
  /** Rendered under the setting, e.g. the drawer's save and discard buttons. */
  actions?: ReactNode;
}

export function CoalesceEditor({ value, onChange, disabled, note, actions }: CoalesceEditorProps) {
  const keyId = useId();
  const windowId = useId();
  // Inline errors show only for values that are set. A missing value is
  // reported by the wizard banner or the drawer's save button.
  const errors = coalesceErrors(value);
  const valid = coalesceFrom(value);
  const { min, max } = LIMITS.coalesce.window;
  return (
    <Stack gap="cozy">
      <ToggleRow
        label="Coalesce events"
        description="Run once for events with the same key."
        checked={value.enabled}
        onChange={(enabled) => onChange({ ...value, enabled })}
        disabled={disabled}
      />
      {value.enabled && (
        <Card inset>
          <CardContent padded compact>
            <Stack gap="comfortable">
              <FormField
                label="Key"
                id={keyId}
                hint="Template, same syntax as the prompt. Up to 256 bytes."
                error={value.key.trim() ? errors.key : undefined}
              >
                <Input
                  id={keyId}
                  mono
                  value={value.key}
                  disabled={disabled}
                  onChange={(e) => onChange({ ...value, key: e.target.value })}
                  placeholder="{{ repository.full_name }}#{{ pull_request.number }}"
                />
              </FormField>
              <Grid cols={2} gap="comfortable">
                <FormField
                  label="Window"
                  id={windowId}
                  hint={`${min} to ${max} seconds from the first event.`}
                  error={value.window.trim() ? errors.window : undefined}
                >
                  <Input
                    id={windowId}
                    mono
                    inputMode="numeric"
                    value={value.window}
                    disabled={disabled}
                    onChange={(e) => onChange({ ...value, window: e.target.value })}
                    placeholder="30"
                    endAdornment={
                      <Text as="span" size="caption" color="subtle">
                        seconds
                      </Text>
                    }
                  />
                </FormField>
              </Grid>
              {valid !== undefined && (
                <Text size="caption" color="muted">
                  Events with the same key within {valid.windowSeconds} seconds run once, with the
                  newest payload.
                </Text>
              )}
            </Stack>
          </CardContent>
        </Card>
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
