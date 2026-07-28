'use client';

/** @tier 3 — triggers-scoped. Shared Delivery step: the dispatch timeout and the
 * two safety behaviours that keep a broken source from flooding the platform. */

import { FormField, Input, Stack, Text, ToggleRow } from '@orbit/terra';
import type { StepComponentProps } from './types';

export function StepDelivery({ state, patch }: StepComponentProps) {
  return (
    <Stack gap="relaxed">
      <Stack maxWidth="form-field">
        <FormField
          label="Timeout"
          id="wizard-timeout"
          hint="Bounds starting the run, not running it. Dispatch is abandoned after this window."
        >
          <Input
            id="wizard-timeout"
            mono
            inputMode="numeric"
            value={String(state.timeoutSeconds)}
            onChange={(e) => patch({ timeoutSeconds: Number(e.target.value) || 0 })}
            endAdornment={
              <Text as="span" size="caption" color="subtle">
                seconds
              </Text>
            }
          />
        </FormField>
      </Stack>

      <Stack gap="cozy">
        <ToggleRow
          label="Suppress duplicates"
          description="Ignore a replay of a delivery already processed."
          checked={state.dedupeEnabled}
          onChange={(next) => patch({ dedupeEnabled: next })}
        />
        <ToggleRow
          label="Auto-pause on repeated failure"
          description="Disable the trigger after ten consecutive failed deliveries, so a broken target stops burning runs."
          checked={state.autopauseEnabled}
          onChange={(next) => patch({ autopauseEnabled: next })}
        />
      </Stack>
    </Stack>
  );
}
